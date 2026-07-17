package pypi_test

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	pypihandler "packagespublisher/internal/package/formats/pypi"
)

func TestBuildPackageDescriptorGroupsWheelAndSourceDistribution(t *testing.T) {
	directory := t.TempDir()
	wheel := filepath.Join(directory, "Demo_Pkg-1.2.3-py3-none-any.whl")
	sdist := filepath.Join(directory, "demo-pkg-1.2.3.tar.gz")
	metadata := "Metadata-Version: 2.1\nName: Demo_Pkg\nVersion: 1.2.3\n\n"
	createWheel(t, wheel, "Demo_Pkg-1.2.3.dist-info/METADATA", metadata)
	createSourceDistribution(t, sdist, "demo-pkg-1.2.3/PKG-INFO", metadata)

	descriptor, err := (pypihandler.Handler{}).BuildPackageDescriptor(context.Background(), wheel)
	if err != nil {
		t.Fatalf("BuildPackageDescriptor() error = %v", err)
	}
	if descriptor.Name != "demo-pkg" || descriptor.Version != "1.2.3" || descriptor.Packaging != "distribution" || len(descriptor.Files) != 2 {
		t.Fatalf("descriptor = %+v", descriptor)
	}
	for _, path := range []string{wheel + ".sha256", sdist + ".sha256"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("SHA sidecar %q missing: %v", path, err)
		}
	}
}

func TestBuildPackageDescriptorRejectsArchiveWithoutCoreMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid-1.0.0.whl")
	createWheel(t, path, "invalid/data.txt", "invalid")
	if _, err := (pypihandler.Handler{}).BuildPackageDescriptor(context.Background(), path); err == nil {
		t.Fatal("expected missing core metadata error")
	}
}

func createWheel(t *testing.T, path, metadataPath, metadata string) {
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

func createSourceDistribution(t *testing.T, path, metadataPath, metadata string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	archive := tar.NewWriter(gzipWriter)
	data := []byte(metadata)
	if err := archive.WriteHeader(&tar.Header{Name: metadataPath, Mode: 0o600, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
