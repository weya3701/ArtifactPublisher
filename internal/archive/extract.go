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
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, fmt.Errorf("inspect package archive %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("package archive %q must be a regular file", path)
	}
	directory, err = os.MkdirTemp("", "package-publisher-archive-")
	if err != nil {
		return "", nil, fmt.Errorf("create archive workspace: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(directory) }
	if err := extract(path, directory); err != nil {
		cleanup()
		return "", nil, err
	}
	return directory, cleanup, nil
}

func extract(path, destination string) error {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZIP(path, destination)
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
		return extractTAR(tar.NewReader(reader), destination)
	case strings.HasSuffix(lower, ".tar"):
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open package archive: %w", err)
		}
		defer file.Close()
		return extractTAR(tar.NewReader(file), destination)
	default:
		return fmt.Errorf("unsupported package archive %q; supported extensions are .zip, .tar, .tar.gz and .tgz", path)
	}
}

func extractZIP(path, destination string) error {
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

func extractTAR(reader *tar.Reader, destination string) error {
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
