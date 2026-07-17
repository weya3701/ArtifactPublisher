package maven

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"packagespublisher/internal/model"
)

type Coordinates struct {
	GroupID    string
	ArtifactID string
	Version    string
}

type Handler struct {
	Fallback Coordinates
}

type pomProject struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Packaging  string `xml:"packaging"`
	Parent     struct {
		GroupID string `xml:"groupId"`
		Version string `xml:"version"`
	} `xml:"parent"`
}

func (Handler) Detect(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		poms, _ := filepath.Glob(filepath.Join(path, "*.pom"))
		jars, _ := filepath.Glob(filepath.Join(path, "*.jar"))
		return len(poms) > 0 || len(jars) > 0
	}
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".pom" || ext == ".jar"
}

func (h Handler) ParseMetadata(_ context.Context, path string) (model.PackageDescriptor, error) {
	directory, pomPath, err := h.resolveInput(path)
	if err != nil {
		return model.PackageDescriptor{}, err
	}
	data, err := os.ReadFile(pomPath)
	if err != nil {
		return model.PackageDescriptor{}, fmt.Errorf("read POM: %w", err)
	}
	var pom pomProject
	if err := xml.Unmarshal(data, &pom); err != nil {
		return model.PackageDescriptor{}, fmt.Errorf("parse POM %q: %w", pomPath, err)
	}
	if pom.GroupID == "" {
		pom.GroupID = pom.Parent.GroupID
	}
	if pom.Version == "" {
		pom.Version = pom.Parent.Version
	}
	if pom.Packaging == "" {
		pom.Packaging = "jar"
	}
	if pom.GroupID == "" || pom.ArtifactID == "" || pom.Version == "" {
		return model.PackageDescriptor{}, fmt.Errorf("POM must define groupId, artifactId and version (directly or through parent)")
	}

	base := pom.ArtifactID + "-" + pom.Version
	fileNames := []string{base + ".pom"}
	mainName := base + "." + pom.Packaging
	if pom.Packaging != "pom" {
		fileNames = append(fileNames, mainName)
	}
	for _, classifier := range []string{"sources", "javadoc"} {
		name := base + "-" + classifier + ".jar"
		if _, err := os.Stat(filepath.Join(directory, name)); err == nil {
			fileNames = append(fileNames, name)
		}
	}

	files := make([]model.PackageFile, 0, len(fileNames))
	for _, name := range fileNames {
		filePath := filepath.Join(directory, name)
		checksum, err := model.FileSHA256(filePath)
		if err != nil {
			return model.PackageDescriptor{}, fmt.Errorf("required Maven artifact %q: %w", name, err)
		}
		classifier := ""
		if strings.HasSuffix(name, "-sources.jar") {
			classifier = "sources"
		} else if strings.HasSuffix(name, "-javadoc.jar") {
			classifier = "javadoc"
		}
		files = append(files, model.PackageFile{
			Path: filePath, Name: name, Classifier: classifier,
			Extension: strings.TrimPrefix(filepath.Ext(name), "."), SHA256: checksum,
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	bundleChecksum, err := model.BundleSHA256(files)
	if err != nil {
		return model.PackageDescriptor{}, err
	}
	return model.PackageDescriptor{
		Format: model.FormatMaven, Namespace: pom.GroupID, Name: pom.ArtifactID,
		Version: pom.Version, Packaging: pom.Packaging, Files: files, SHA256: bundleChecksum,
	}, nil
}

func (Handler) ValidateCompleteness(descriptor model.PackageDescriptor) error {
	if descriptor.Format != model.FormatMaven {
		return fmt.Errorf("expected Maven package, got %q", descriptor.Format)
	}
	if descriptor.Namespace == "" || descriptor.Name == "" || descriptor.Version == "" || descriptor.Packaging == "" {
		return fmt.Errorf("Maven GAV and packaging are required")
	}
	base := descriptor.Name + "-" + descriptor.Version
	required := map[string]bool{base + ".pom": false}
	if descriptor.Packaging != "pom" {
		required[base+"."+descriptor.Packaging] = false
	}
	for _, file := range descriptor.Files {
		if _, ok := required[file.Name]; ok {
			required[file.Name] = true
		}
	}
	for name, found := range required {
		if !found {
			return fmt.Errorf("required Maven artifact %q is missing", name)
		}
	}
	checksum, err := model.BundleSHA256(descriptor.Files)
	if err != nil {
		return err
	}
	if checksum != descriptor.SHA256 {
		return fmt.Errorf("package bundle checksum mismatch")
	}
	return nil
}

func (h Handler) BuildPackageDescriptor(ctx context.Context, path string) (model.PackageDescriptor, error) {
	if !h.Detect(path) {
		return model.PackageDescriptor{}, fmt.Errorf("path %q is not a Maven package", path)
	}
	descriptor, err := h.ParseMetadata(ctx, path)
	if err != nil {
		return model.PackageDescriptor{}, err
	}
	if err := h.ValidateCompleteness(descriptor); err != nil {
		return model.PackageDescriptor{}, err
	}
	if err := writeChecksumSidecars(descriptor.Files); err != nil {
		return model.PackageDescriptor{}, err
	}
	return descriptor, nil
}

func (h Handler) resolveInput(path string) (directory, pomPath string, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("inspect package path: %w", err)
	}
	directory = path
	if !info.IsDir() {
		directory = filepath.Dir(path)
	}
	poms, err := filepath.Glob(filepath.Join(directory, "*.pom"))
	if err != nil {
		return "", "", fmt.Errorf("find POM in %q: %w", directory, err)
	}
	if len(poms) > 1 {
		return "", "", fmt.Errorf("multiple POM files found in %q; specify a single-package directory", directory)
	}
	if len(poms) == 0 {
		jarPath, err := selectMainJAR(path, info, directory)
		if err != nil {
			return "", "", err
		}
		generated, err := generatePOMFromJAR(jarPath, h.Fallback)
		if err != nil {
			return "", "", err
		}
		return directory, generated, nil
	}
	return directory, poms[0], nil
}

func writeChecksumSidecars(files []model.PackageFile) error {
	for _, file := range files {
		if err := os.WriteFile(file.Path+".sha256", []byte(file.SHA256+"\n"), 0o644); err != nil {
			return fmt.Errorf("write SHA-256 sidecar for %q: %w", file.Name, err)
		}
	}
	return nil
}
