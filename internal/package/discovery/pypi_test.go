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
