package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Extract expands a publisher bundle into a temporary directory. The caller
// must invoke the returned cleanup function when the publish run finishes.
func Extract(path string) (directory string, cleanup func(), err error) {
	return ExtractWithExclusions(path, nil)
}

// ExtractWithExclusions expands a publisher bundle while omitting configured
// directory trees. Excluded directories are relative to the archive root.
func ExtractWithExclusions(path string, excludedDirectories []string) (directory string, cleanup func(), err error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, fmt.Errorf("inspect package archive %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("package archive %q must be a regular file", path)
	}
	excluded, err := prepareExcludedDirectories(excludedDirectories)
	if err != nil {
		return "", nil, err
	}
	directory, err = os.MkdirTemp("", "package-publisher-archive-")
	if err != nil {
		return "", nil, fmt.Errorf("create archive workspace: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(directory) }
	if err := extract(path, directory, excluded); err != nil {
		cleanup()
		return "", nil, err
	}
	return directory, cleanup, nil
}

func extract(path, destination string, excluded map[string]struct{}) error {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZIP(path, destination, excluded)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open package archive: %w", err)
		}
		defer file.Close()
		reader, err := gzip.NewReader(file)
		if err != nil {
			return fmt.Errorf("read gzip package archive: %w", err)
		}
		defer reader.Close()
		return extractTAR(tar.NewReader(reader), destination, excluded)
	case strings.HasSuffix(lower, ".tar"):
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open package archive: %w", err)
		}
		defer file.Close()
		return extractTAR(tar.NewReader(file), destination, excluded)
	default:
		return fmt.Errorf("unsupported package archive %q; supported extensions are .zip, .tar, .tar.gz and .tgz", path)
	}
}

func extractZIP(path, destination string, excluded map[string]struct{}) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("read ZIP package archive: %w", err)
	}
	defer reader.Close()
	for _, entry := range reader.File {
		target, err := safeTarget(destination, entry.Name)
		if err != nil {
			return err
		}
		if isMacOSMetadata(entry.Name) || isExcludedEntry(entry.Name, excluded) {
			continue
		}
		mode := entry.Mode()
		if mode&os.ModeSymlink != 0 {
			return fmt.Errorf("package archive contains unsupported symbolic link %q", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create archive directory %q: %w", entry.Name, err)
			}
			continue
		}
		if !mode.IsRegular() {
			return fmt.Errorf("package archive contains unsupported entry %q", entry.Name)
		}
		if err := writeFile(target, mode.Perm(), func() (io.ReadCloser, error) { return entry.Open() }); err != nil {
			return fmt.Errorf("extract archive entry %q: %w", entry.Name, err)
		}
	}
	return nil
}

func extractTAR(reader *tar.Reader, destination string, excluded map[string]struct{}) error {
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read TAR package archive: %w", err)
		}
		target, err := safeTarget(destination, header.Name)
		if err != nil {
			return err
		}
		if isMacOSMetadata(header.Name) || isExcludedEntry(header.Name, excluded) {
			continue
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create archive directory %q: %w", header.Name, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			mode := os.FileMode(header.Mode).Perm()
			if err := writeFile(target, mode, func() (io.ReadCloser, error) {
				return io.NopCloser(reader), nil
			}); err != nil {
				return fmt.Errorf("extract archive entry %q: %w", header.Name, err)
			}
		default:
			return fmt.Errorf("package archive contains unsupported link or special entry %q", header.Name)
		}
	}
}

func prepareExcludedDirectories(directories []string) (map[string]struct{}, error) {
	excluded := make(map[string]struct{}, len(directories))
	for _, directory := range directories {
		if strings.TrimSpace(directory) == "" {
			return nil, fmt.Errorf("excluded archive directory cannot be empty")
		}
		clean := filepath.Clean(filepath.FromSlash(directory))
		if clean == "." {
			return nil, fmt.Errorf("excluded archive directory %q cannot be the archive root", directory)
		}
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("excluded archive directory %q must be relative to the archive root", directory)
		}
		excluded[clean] = struct{}{}
	}
	return excluded, nil
}

func isExcludedEntry(name string, excluded map[string]struct{}) bool {
	if len(excluded) == 0 {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	for directory := range excluded {
		if clean == directory || strings.HasPrefix(clean, directory+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func isMacOSMetadata(name string) bool {
	clean := filepath.Clean(filepath.FromSlash(name))
	for _, component := range strings.Split(clean, string(filepath.Separator)) {
		if component == "__MACOSX" || component == ".DS_Store" || strings.HasPrefix(component, "._") {
			return true
		}
	}
	return false
}

func safeTarget(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("package archive entry %q escapes the extraction directory", name)
	}
	target := filepath.Join(root, clean)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("package archive entry %q escapes the extraction directory", name)
	}
	return target, nil
}

func writeFile(path string, mode os.FileMode, open func() (io.ReadCloser, error)) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	source, err := open()
	if err != nil {
		return err
	}
	defer source.Close()
	if mode == 0 {
		mode = 0o644
	}
	destination, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(destination, source)
	closeErr := destination.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
