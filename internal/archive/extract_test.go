package archive_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"packagespublisher/internal/archive"
)

func TestExtractZIP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "packages.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("repository/demo/package.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte(`{"name":"demo"}`))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	directory, cleanup, err := archive.Extract(path)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	extracted := filepath.Join(directory, "repository", "demo", "package.json")
	if content, err := os.ReadFile(extracted); err != nil || string(content) != `{"name":"demo"}` {
		t.Fatalf("content=%q error=%v", content, err)
	}
	cleanup()
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("temporary directory still exists: %v", err)
	}
}

func TestExtractTarGZIP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "packages.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	writer := tar.NewWriter(gzipWriter)
	content := []byte("artifact")
	if err := writer.WriteHeader(&tar.Header{Name: "repo/demo.jar", Mode: 0o644, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	_, _ = writer.Write(content)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	directory, cleanup, err := archive.Extract(path)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	defer cleanup()
	if got, err := os.ReadFile(filepath.Join(directory, "repo", "demo.jar")); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("content=%q error=%v", got, err)
	}
}

func TestExtractRejectsPathTraversal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "malicious.zip")
	file, _ := os.Create(path)
	writer := zip.NewWriter(file)
	entry, _ := writer.Create("../escaped")
	_, _ = entry.Write([]byte("bad"))
	_ = writer.Close()
	_ = file.Close()

	_, _, err := archive.Extract(path)
	if err == nil || !strings.Contains(err.Error(), "escapes the extraction directory") {
		t.Fatalf("Extract() error = %v", err)
	}
}
