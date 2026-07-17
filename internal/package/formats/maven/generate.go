package maven

import (
	"archive/zip"
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func selectMainJAR(input string, info os.FileInfo, directory string) (string, error) {
	if !info.IsDir() && strings.EqualFold(filepath.Ext(input), ".jar") {
		return input, nil
	}
	jars, err := filepath.Glob(filepath.Join(directory, "*.jar"))
	if err != nil {
		return "", fmt.Errorf("find JAR in %q: %w", directory, err)
	}
	mainJars := make([]string, 0, len(jars))
	for _, jar := range jars {
		name := strings.ToLower(filepath.Base(jar))
		if strings.HasSuffix(name, "-sources.jar") || strings.HasSuffix(name, "-javadoc.jar") || strings.HasSuffix(name, "-tests.jar") {
			continue
		}
		mainJars = append(mainJars, jar)
	}
	if len(mainJars) != 1 {
		return "", fmt.Errorf("JAR-only Maven directory %q must contain exactly one main JAR; found %d", directory, len(mainJars))
	}
	return mainJars[0], nil
}

func generatePOMFromJAR(jarPath string, fallback Coordinates) (string, error) {
	coordinates, err := embeddedCoordinates(jarPath)
	if err != nil {
		return "", err
	}
	if coordinates.GroupID == "" {
		coordinates.GroupID = fallback.GroupID
	}
	if coordinates.ArtifactID == "" {
		coordinates.ArtifactID = fallback.ArtifactID
	}
	if coordinates.Version == "" {
		coordinates.Version = fallback.Version
	}
	if coordinates.GroupID == "" || coordinates.ArtifactID == "" || coordinates.Version == "" {
		return "", fmt.Errorf("JAR %q has no complete META-INF/maven pom.properties; configure package.maven group_id, artifact_id and version", jarPath)
	}
	expectedName := coordinates.ArtifactID + "-" + coordinates.Version + ".jar"
	if filepath.Base(jarPath) != expectedName {
		return "", fmt.Errorf("JAR filename %q does not match embedded/configured Maven coordinate; expected %q", filepath.Base(jarPath), expectedName)
	}

	type generatedProject struct {
		XMLName      xml.Name `xml:"project"`
		ModelVersion string   `xml:"modelVersion"`
		GroupID      string   `xml:"groupId"`
		ArtifactID   string   `xml:"artifactId"`
		Version      string   `xml:"version"`
		Packaging    string   `xml:"packaging"`
	}
	data, err := xml.MarshalIndent(generatedProject{
		ModelVersion: "4.0.0", GroupID: coordinates.GroupID, ArtifactID: coordinates.ArtifactID,
		Version: coordinates.Version, Packaging: "jar",
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("generate POM XML: %w", err)
	}
	pomPath := filepath.Join(filepath.Dir(jarPath), coordinates.ArtifactID+"-"+coordinates.Version+".pom")
	file, err := os.OpenFile(pomPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("create generated POM %q: %w", pomPath, err)
	}
	content := append([]byte(xml.Header), data...)
	content = append(content, '\n')
	if _, err := file.Write(content); err != nil {
		file.Close()
		_ = os.Remove(pomPath)
		return "", fmt.Errorf("write generated POM %q: %w", pomPath, err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close generated POM %q: %w", pomPath, err)
	}
	return pomPath, nil
}

func embeddedCoordinates(jarPath string) (Coordinates, error) {
	reader, err := zip.OpenReader(jarPath)
	if err != nil {
		return Coordinates{}, fmt.Errorf("open JAR %q: %w", jarPath, err)
	}
	defer reader.Close()
	var candidates []Coordinates
	for _, file := range reader.File {
		if !strings.HasPrefix(file.Name, "META-INF/maven/") || !strings.HasSuffix(file.Name, "/pom.properties") {
			continue
		}
		coordinates, err := readProperties(file)
		if err != nil {
			return Coordinates{}, err
		}
		candidates = append(candidates, coordinates)
	}
	if len(candidates) == 0 {
		return Coordinates{}, nil
	}
	unique := make(map[Coordinates]struct{}, len(candidates))
	matching := make(map[Coordinates]struct{})
	jarName := filepath.Base(jarPath)
	for _, candidate := range candidates {
		unique[candidate] = struct{}{}
		if candidate.ArtifactID != "" && candidate.Version != "" && candidate.ArtifactID+"-"+candidate.Version+".jar" == jarName {
			matching[candidate] = struct{}{}
		}
	}
	if len(matching) == 1 {
		for candidate := range matching {
			return candidate, nil
		}
	}
	if len(unique) == 1 {
		for candidate := range unique {
			return candidate, nil
		}
	}
	return Coordinates{}, fmt.Errorf("JAR %q contains multiple Maven coordinates and none can be selected unambiguously", jarPath)
}

func readProperties(file *zip.File) (Coordinates, error) {
	reader, err := file.Open()
	if err != nil {
		return Coordinates{}, fmt.Errorf("read %q: %w", file.Name, err)
	}
	defer reader.Close()
	values := make(map[string]string)
	scanner := bufio.NewScanner(io.LimitReader(reader, 1<<20))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			key, value, found = strings.Cut(line, ":")
		}
		if found {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return Coordinates{}, fmt.Errorf("parse %q: %w", file.Name, err)
	}
	return Coordinates{GroupID: values["groupId"], ArtifactID: values["artifactId"], Version: values["version"]}, nil
}
