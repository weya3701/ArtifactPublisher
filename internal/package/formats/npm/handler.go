package npm

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"packagespublisher/internal/model"
)

type Packer interface {
	Pack(context.Context, string) (string, error)
}

type ExecPacker struct{ Executable string }

type Handler struct{ Packer Packer }

type packageJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Private bool   `json:"private"`
}

func (Handler) Detect(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return strings.EqualFold(filepath.Ext(path), ".tgz")
	}
	matches, _ := filepath.Glob(filepath.Join(path, "*.tgz"))
	if len(matches) > 0 {
		return true
	}
	_, err = os.Stat(filepath.Join(path, "package.json"))
	return err == nil
}

func (h Handler) ParseMetadata(ctx context.Context, path string) (model.PackageDescriptor, error) {
	tarball, err := h.resolveTarball(ctx, path)
	if err != nil {
		return model.PackageDescriptor{}, err
	}
	metadata, err := readPackageJSON(tarball)
	if err != nil {
		return model.PackageDescriptor{}, err
	}
	if metadata.Private {
		return model.PackageDescriptor{}, fmt.Errorf("npm package %q is private and cannot be published", metadata.Name)
	}
	scope, name, err := splitName(metadata.Name)
	if err != nil {
		return model.PackageDescriptor{}, err
	}
	if strings.TrimSpace(metadata.Version) == "" {
		return model.PackageDescriptor{}, fmt.Errorf("npm package version is required")
	}
	checksum, err := model.FileSHA256(tarball)
	if err != nil {
		return model.PackageDescriptor{}, fmt.Errorf("hash npm tarball: %w", err)
	}
	file := model.PackageFile{Path: tarball, Name: filepath.Base(tarball), Extension: "tgz", SHA256: checksum}
	bundleChecksum, err := model.BundleSHA256([]model.PackageFile{file})
	if err != nil {
		return model.PackageDescriptor{}, err
	}
	return model.PackageDescriptor{
		Format: model.FormatNPM, Namespace: scope, Name: name, Version: metadata.Version,
		Packaging: "tgz", Files: []model.PackageFile{file}, SHA256: bundleChecksum,
	}, nil
}

func (Handler) ValidateCompleteness(descriptor model.PackageDescriptor) error {
	if descriptor.Format != model.FormatNPM || descriptor.Name == "" || descriptor.Version == "" {
		return fmt.Errorf("npm name and version are required")
	}
	if len(descriptor.Files) != 1 || descriptor.Files[0].Extension != "tgz" {
		return fmt.Errorf("npm package requires exactly one .tgz tarball")
	}
	checksum, err := model.BundleSHA256(descriptor.Files)
	if err != nil {
		return err
	}
	if checksum != descriptor.SHA256 {
		return fmt.Errorf("npm bundle checksum mismatch")
	}
	return nil
}

func (h Handler) BuildPackageDescriptor(ctx context.Context, path string) (model.PackageDescriptor, error) {
	if !h.Detect(path) {
		return model.PackageDescriptor{}, fmt.Errorf("path %q is not an npm .tgz package", path)
	}
	descriptor, err := h.ParseMetadata(ctx, path)
	if err != nil {
		return model.PackageDescriptor{}, err
	}
	if err := h.ValidateCompleteness(descriptor); err != nil {
		return model.PackageDescriptor{}, err
	}
	file := descriptor.Files[0]
	if err := os.WriteFile(file.Path+".sha256", []byte(file.SHA256+"\n"), 0o644); err != nil {
		return model.PackageDescriptor{}, fmt.Errorf("write npm SHA-256 sidecar: %w", err)
	}
	return descriptor, nil
}

func (h Handler) resolveTarball(ctx context.Context, path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("inspect npm package: %w", err)
	}
	if !info.IsDir() {
		return path, nil
	}
	matches, err := filepath.Glob(filepath.Join(path, "*.tgz"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		packer := h.Packer
		if packer == nil {
			packer = ExecPacker{}
		}
		return packer.Pack(ctx, path)
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("npm package directory %q contains multiple .tgz files", path)
	}
	return matches[0], nil
}

func (p ExecPacker) Pack(ctx context.Context, directory string) (string, error) {
	executable := p.Executable
	if executable == "" {
		executable = "npm"
	}
	output, err := exec.CommandContext(ctx, executable, "pack", directory, "--json", "--ignore-scripts", "--pack-destination", directory).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("npm pack failed: %w (output: %s)", err, string(output))
	}
	var packed []struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(output, &packed); err != nil || len(packed) != 1 || packed[0].Filename == "" {
		return "", fmt.Errorf("parse npm pack output: %s", string(output))
	}
	path := packed[0].Filename
	if !filepath.IsAbs(path) {
		path = filepath.Join(directory, path)
	}
	return path, nil
}

func readPackageJSON(tarball string) (packageJSON, error) {
	file, err := os.Open(tarball)
	if err != nil {
		return packageJSON{}, err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return packageJSON{}, fmt.Errorf("open npm gzip tarball: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return packageJSON{}, fmt.Errorf("read npm tarball: %w", err)
		}
		if strings.TrimPrefix(header.Name, "./") != "package/package.json" {
			continue
		}
		var metadata packageJSON
		if err := json.NewDecoder(io.LimitReader(tarReader, 4<<20)).Decode(&metadata); err != nil {
			return packageJSON{}, fmt.Errorf("parse package/package.json: %w", err)
		}
		return metadata, nil
	}
	return packageJSON{}, fmt.Errorf("npm tarball does not contain package/package.json")
}

func splitName(fullName string) (scope, name string, err error) {
	if strings.HasPrefix(fullName, "@") {
		parts := strings.Split(fullName, "/")
		if len(parts) != 2 || len(parts[0]) < 2 || parts[1] == "" {
			return "", "", fmt.Errorf("invalid scoped npm package name %q", fullName)
		}
		return strings.TrimPrefix(parts[0], "@"), parts[1], nil
	}
	if fullName == "" || strings.Contains(fullName, "/") {
		return "", "", fmt.Errorf("invalid npm package name %q", fullName)
	}
	return "", fullName, nil
}
