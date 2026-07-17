package pypi

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"packagespublisher/internal/model"
)

var normalizedNameSeparators = regexp.MustCompile(`[-_.]+`)

type Identity struct {
	Name    string
	Version string
}

type Handler struct{}

func (Handler) Detect(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return IsDistribution(path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && IsDistribution(entry.Name()) {
			return true
		}
	}
	return false
}

func IsDistribution(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".whl") || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".zip")
}

func InspectIdentity(path string) (Identity, error) {
	var reader io.ReadCloser
	var err error
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".whl"):
		reader, err = metadataFromZip(path, func(name string) bool {
			return strings.HasSuffix(name, ".dist-info/METADATA")
		})
	case strings.HasSuffix(lower, ".zip"):
		reader, err = metadataFromZip(path, func(name string) bool {
			return name == "PKG-INFO" || strings.HasSuffix(name, "/PKG-INFO")
		})
	case strings.HasSuffix(lower, ".tar.gz"):
		reader, err = metadataFromTarGZ(path)
	default:
		return Identity{}, fmt.Errorf("unsupported PyPI distribution %q", path)
	}
	if err != nil {
		return Identity{}, err
	}
	defer reader.Close()
	message, err := mail.ReadMessage(io.LimitReader(reader, 4<<20))
	if err != nil {
		return Identity{}, fmt.Errorf("parse Python core metadata in %q: %w", path, err)
	}
	identity := Identity{
		Name:    NormalizeName(strings.TrimSpace(message.Header.Get("Name"))),
		Version: strings.TrimSpace(message.Header.Get("Version")),
	}
	if identity.Name == "" || identity.Version == "" {
		return Identity{}, fmt.Errorf("Python core metadata in %q requires Name and Version", path)
	}
	return identity, nil
}

func NormalizeName(name string) string {
	return strings.ToLower(normalizedNameSeparators.ReplaceAllString(strings.TrimSpace(name), "-"))
}

func (Handler) ParseMetadata(_ context.Context, path string) (model.PackageDescriptor, error) {
	inputInfo, err := os.Stat(path)
	if err != nil {
		return model.PackageDescriptor{}, fmt.Errorf("inspect PyPI package: %w", err)
	}
	distributions, err := distributionsFor(path)
	if err != nil {
		return model.PackageDescriptor{}, err
	}
	primary, err := InspectIdentity(distributions[0])
	if err != nil {
		return model.PackageDescriptor{}, err
	}
	files := make([]model.PackageFile, 0, len(distributions))
	for _, distribution := range distributions {
		if !inputInfo.IsDir() && distribution != path && !filenameMayMatch(distribution, primary) {
			continue
		}
		identity, err := InspectIdentity(distribution)
		if err != nil {
			return model.PackageDescriptor{}, err
		}
		if identity != primary {
			if inputInfo.IsDir() {
				return model.PackageDescriptor{}, fmt.Errorf("PyPI package directory %q contains multiple package coordinates", path)
			}
			continue
		}
		checksum, err := model.FileSHA256(distribution)
		if err != nil {
			return model.PackageDescriptor{}, fmt.Errorf("hash PyPI distribution %q: %w", distribution, err)
		}
		files = append(files, model.PackageFile{
			Path: distribution, Name: filepath.Base(distribution), Extension: distributionExtension(distribution), SHA256: checksum,
		})
	}
	if len(files) == 0 {
		return model.PackageDescriptor{}, fmt.Errorf("no distributions match PyPI package %s %s", primary.Name, primary.Version)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	bundleChecksum, err := model.BundleSHA256(files)
	if err != nil {
		return model.PackageDescriptor{}, err
	}
	packaging := "distribution"
	if len(files) == 1 {
		if files[0].Extension == "whl" {
			packaging = "wheel"
		} else {
			packaging = "sdist"
		}
	}
	return model.PackageDescriptor{
		Format: model.FormatPyPI, Name: primary.Name, Version: primary.Version,
		Packaging: packaging, Files: files, SHA256: bundleChecksum,
	}, nil
}

func (Handler) ValidateCompleteness(descriptor model.PackageDescriptor) error {
	if descriptor.Format != model.FormatPyPI || descriptor.Name == "" || descriptor.Version == "" {
		return fmt.Errorf("PyPI package name and version are required")
	}
	if len(descriptor.Files) == 0 {
		return fmt.Errorf("PyPI package requires at least one wheel or source distribution")
	}
	for _, file := range descriptor.Files {
		if !IsDistribution(file.Name) {
			return fmt.Errorf("unsupported PyPI distribution %q", file.Name)
		}
	}
	checksum, err := model.BundleSHA256(descriptor.Files)
	if err != nil {
		return err
	}
	if checksum != descriptor.SHA256 {
		return fmt.Errorf("PyPI bundle checksum mismatch")
	}
	return nil
}

func (h Handler) BuildPackageDescriptor(ctx context.Context, path string) (model.PackageDescriptor, error) {
	if !h.Detect(path) {
		return model.PackageDescriptor{}, fmt.Errorf("path %q is not a supported PyPI distribution", path)
	}
	descriptor, err := h.ParseMetadata(ctx, path)
	if err != nil {
		return model.PackageDescriptor{}, err
	}
	if err := h.ValidateCompleteness(descriptor); err != nil {
		return model.PackageDescriptor{}, err
	}
	for _, file := range descriptor.Files {
		if err := os.WriteFile(file.Path+".sha256", []byte(file.SHA256+"\n"), 0o644); err != nil {
			return model.PackageDescriptor{}, fmt.Errorf("write PyPI SHA-256 sidecar: %w", err)
		}
	}
	return descriptor, nil
}

func distributionsFor(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect PyPI package: %w", err)
	}
	directory := path
	if !info.IsDir() {
		directory = filepath.Dir(path)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read PyPI package directory: %w", err)
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() && IsDistribution(entry.Name()) {
			paths = append(paths, filepath.Join(directory, entry.Name()))
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no supported PyPI distributions found in %q", directory)
	}
	sort.Strings(paths)
	if !info.IsDir() {
		for index, candidate := range paths {
			if candidate == path {
				paths[0], paths[index] = paths[index], paths[0]
				break
			}
		}
	}
	return paths, nil
}

func metadataFromZip(path string, match func(string) bool) (io.ReadCloser, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open PyPI zip distribution %q: %w", path, err)
	}
	for _, file := range archive.File {
		if match(strings.TrimPrefix(file.Name, "./")) {
			reader, err := file.Open()
			if err != nil {
				archive.Close()
				return nil, err
			}
			return &joinedReadCloser{Reader: reader, closers: []io.Closer{reader, archive}}, nil
		}
	}
	archive.Close()
	return nil, fmt.Errorf("PyPI distribution %q does not contain core metadata", path)
}

func metadataFromTarGZ(path string) (io.ReadCloser, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("open PyPI source distribution %q: %w", path, err)
	}
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			gzipReader.Close()
			file.Close()
			return nil, fmt.Errorf("read PyPI source distribution %q: %w", path, err)
		}
		name := strings.TrimPrefix(header.Name, "./")
		if name == "PKG-INFO" || strings.HasSuffix(name, "/PKG-INFO") {
			return &joinedReadCloser{Reader: tarReader, closers: []io.Closer{gzipReader, file}}, nil
		}
	}
	gzipReader.Close()
	file.Close()
	return nil, fmt.Errorf("PyPI source distribution %q does not contain PKG-INFO", path)
}

type joinedReadCloser struct {
	io.Reader
	closers []io.Closer
}

func (r *joinedReadCloser) Close() error {
	for _, closer := range r.closers {
		_ = closer.Close()
	}
	return nil
}

func distributionExtension(path string) string {
	if strings.HasSuffix(strings.ToLower(path), ".tar.gz") {
		return "tar.gz"
	}
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
}

func filenameMayMatch(path string, identity Identity) bool {
	name := filepath.Base(path)
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".whl") {
		parts := strings.Split(strings.TrimSuffix(name, filepath.Ext(name)), "-")
		return len(parts) >= 5 && NormalizeName(parts[0]) == identity.Name && parts[1] == identity.Version
	}
	for _, suffix := range []string{".tar.gz", ".zip"} {
		if strings.HasSuffix(lower, suffix) {
			stem := name[:len(name)-len(suffix)]
			versionSuffix := "-" + identity.Version
			return strings.HasSuffix(stem, versionSuffix) && NormalizeName(strings.TrimSuffix(stem, versionSuffix)) == identity.Name
		}
	}
	return false
}
