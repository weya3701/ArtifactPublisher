package discovery_test

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"packagespublisher/internal/package/discovery"
)

func TestPyPIPackagesDeduplicatesDistributionsByNameAndVersion(t *testing.T) {
	root := t.TempDir()
	wheel := filepath.Join(root, "demo_pkg-1.0.0-py3-none-any.whl")
	sdist := filepath.Join(root, "demo-pkg-1.0.0.zip")
	other := filepath.Join(root, "other-2.0.0-py3-none-any.whl")
	createMetadataZip(t, wheel, "demo_pkg-1.0.0.dist-info/METADATA", "Name: Demo_Pkg\nVersion: 1.0.0\n\n")
	createMetadataZip(t, sdist, "demo-pkg-1.0.0/PKG-INFO", "Name: demo-pkg\nVersion: 1.0.0\n\n")
	createMetadataZip(t, other, "other-2.0.0.dist-info/METADATA", "Name: other\nVersion: 2.0.0\n\n")

	paths, err := discovery.PyPIPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != sdist || paths[1] != other {
		t.Fatalf("paths = %#v, want [%q, %q]", paths, sdist, other)
	}
}

func TestPyPIPackagesIgnoresHiddenFilesAndDirectories(t *testing.T) {
	root := t.TempDir()
	wheel := filepath.Join(root, "demo-1.0.0-py3-none-any.whl")
	createMetadataZip(t, wheel, "demo-1.0.0.dist-info/METADATA", "Name: demo\nVersion: 1.0.0\n\n")
	if err := os.WriteFile(filepath.Join(root, "._PyYAML-6.0.1.tar.gz"), []byte("AppleDouble metadata"), 0o600); err != nil {
		t.Fatal(err)
	}
	hiddenDirectory := filepath.Join(root, ".cache")
	if err := os.Mkdir(hiddenDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	createMetadataZip(t, filepath.Join(hiddenDirectory, "hidden-2.0.0.whl"), "hidden-2.0.0.dist-info/METADATA", "Name: hidden\nVersion: 2.0.0\n\n")

	paths, err := discovery.PyPIPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != wheel {
		t.Fatalf("paths = %#v, want [%q]", paths, wheel)
	}
}

func TestPyPIPackagesExcludesConfiguredDirectoryTree(t *testing.T) {
	root := t.TempDir()
	included := filepath.Join(root, "approved", "included-1.0.0-py3-none-any.whl")
	excluded := filepath.Join(root, "quarantine", "nested", "excluded-2.0.0-py3-none-any.whl")
	if err := os.MkdirAll(filepath.Dir(included), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(excluded), 0o700); err != nil {
		t.Fatal(err)
	}
	createMetadataZip(t, included, "included-1.0.0.dist-info/METADATA", "Name: included\nVersion: 1.0.0\n\n")
	createMetadataZip(t, excluded, "excluded-2.0.0.dist-info/METADATA", "Name: excluded\nVersion: 2.0.0\n\n")

	paths, err := discovery.PyPIPackages(root, "quarantine")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != included {
		t.Fatalf("paths = %#v, want [%q]", paths, included)
	}
}

func createMetadataZip(t *testing.T, path, metadataPath, metadata string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entry, err := archive.Create(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(metadata)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
