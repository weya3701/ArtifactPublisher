package maven

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
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
	Fallback     Coordinates
	AllowPOMOnly bool
}

type artifactFileSpec struct {
	Name       string
	Classifier string
	Extension  string
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
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = pomCharsetReader
	if err := decoder.Decode(&pom); err != nil {
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
	pomName := base + ".pom"
	fileSpecs := []artifactFileSpec{{Name: pomName, Extension: "pom"}}
	mainName := base + "." + pom.Packaging
	mainAvailable := pom.Packaging == "pom"
	if pom.Packaging != "pom" {
		_, statErr := os.Stat(filepath.Join(directory, mainName))
		if statErr == nil {
			mainAvailable = true
			fileSpecs = append(fileSpecs, artifactFileSpec{Name: mainName, Extension: pom.Packaging})
		} else if !os.IsNotExist(statErr) {
			return model.PackageDescriptor{}, fmt.Errorf("inspect Maven main artifact %q: %w", mainName, statErr)
		} else if !h.AllowPOMOnly {
			// Preserve the standard completeness error when POM-only fallback is disabled.
			fileSpecs = append(fileSpecs, artifactFileSpec{Name: mainName, Extension: pom.Packaging})
		}
	}
	attached, err := discoverAttachedArtifacts(directory, base, pomName, mainName)
	if err != nil {
		return model.PackageDescriptor{}, err
	}
	fileSpecs = append(fileSpecs, attached...)

	files := make([]model.PackageFile, 0, len(fileSpecs))
	for _, spec := range fileSpecs {
		filePath := filepath.Join(directory, spec.Name)
		checksum, err := model.FileSHA256(filePath)
		if err != nil {
			return model.PackageDescriptor{}, fmt.Errorf("required Maven artifact %q: %w", spec.Name, err)
		}
		files = append(files, model.PackageFile{
			Path: filePath, Name: spec.Name, Classifier: spec.Classifier,
			Extension: spec.Extension, SHA256: checksum,
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	bundleChecksum, err := model.BundleSHA256(files)
	if err != nil {
		return model.PackageDescriptor{}, err
	}
	return model.PackageDescriptor{
		Format: model.FormatMaven, Namespace: pom.GroupID, Name: pom.ArtifactID,
		Version: pom.Version, Packaging: pom.Packaging,
		POMOnly: !mainAvailable && h.AllowPOMOnly,
		Files:   files, SHA256: bundleChecksum,
	}, nil
}

func discoverAttachedArtifacts(directory, base, pomName, mainName string) ([]artifactFileSpec, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("list Maven package directory %q: %w", directory, err)
	}
	ignoredExtensions := map[string]bool{
		"asc": true, "lastupdated": true, "md5": true,
		"sha1": true, "sha256": true, "sha512": true,
	}
	var artifacts []artifactFileSpec
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == pomName || name == mainName {
			continue
		}
		extension := strings.TrimPrefix(filepath.Ext(name), ".")
		if extension == "" || ignoredExtensions[strings.ToLower(extension)] {
			continue
		}
		unclassifiedPrefix := base + "."
		if strings.HasPrefix(name, unclassifiedPrefix) && strings.TrimPrefix(name, unclassifiedPrefix) == extension {
			artifacts = append(artifacts, artifactFileSpec{Name: name, Extension: extension})
			continue
		}
		prefix := base + "-"
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		classifier := strings.TrimSuffix(strings.TrimPrefix(name, prefix), "."+extension)
		if classifier == "" {
			continue
		}
		artifacts = append(artifacts, artifactFileSpec{
			Name: name, Classifier: classifier, Extension: extension,
		})
	}
	return artifacts, nil
}

func pomCharsetReader(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "iso-8859-1", "iso8859-1", "latin-1", "latin1":
		data, err := io.ReadAll(input)
		if err != nil {
			return nil, err
		}
		decoded := make([]rune, len(data))
		for index, value := range data {
			decoded[index] = rune(value)
		}
		return strings.NewReader(string(decoded)), nil
	default:
		return nil, fmt.Errorf("unsupported POM XML encoding %q", charset)
	}
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
	if !descriptor.POMOnly && descriptor.Packaging != "pom" {
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
