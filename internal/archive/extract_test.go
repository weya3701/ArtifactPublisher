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
	for _, name := range []string{
		"__MACOSX/repository/demo/._package.json",
		"repository/demo/._package.json",
		"repository/demo/.DS_Store",
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write([]byte("macOS metadata"))
	}
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
	for _, name := range []string{
		"__MACOSX",
		filepath.Join("repository", "demo", "._package.json"),
		filepath.Join("repository", "demo", ".DS_Store"),
	} {
		if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatalf("macOS metadata %q was extracted: %v", name, err)
		}
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
	metadata := []byte("macOS metadata")
	for _, name := range []string{"repo/._demo.jar", "repo/.DS_Store", "__MACOSX/._demo.jar"} {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(metadata))}); err != nil {
			t.Fatal(err)
		}
		_, _ = writer.Write(metadata)
	}
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
	for _, name := range []string{
		filepath.Join("repo", "._demo.jar"),
		filepath.Join("repo", ".DS_Store"),
		"__MACOSX",
	} {
		if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatalf("macOS metadata %q was extracted: %v", name, err)
		}
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

func TestExtractZIPSkipsExcludedDirectoryContainingSymlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "packages.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("artifactNPM/demo-1.0.0.tgz")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("package"))
	linkHeader := &zip.FileHeader{Name: "artifactNPM/.bin/nanoid"}
	linkHeader.SetMode(os.ModeSymlink | 0o777)
	link, err := writer.CreateHeader(linkHeader)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = link.Write([]byte("../nanoid/bin/nanoid.cjs"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	directory, cleanup, err := archive.ExtractWithExclusions(path, []string{"artifactNPM/.bin"})
	if err != nil {
		t.Fatalf("ExtractWithExclusions() error = %v", err)
	}
	defer cleanup()
	if _, err := os.Stat(filepath.Join(directory, "artifactNPM", "demo-1.0.0.tgz")); err != nil {
		t.Fatalf("included package was not extracted: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(directory, "artifactNPM", ".bin")); !os.IsNotExist(err) {
		t.Fatalf("excluded directory was extracted: %v", err)
	}
}

func TestExtractTarGZIPSkipsExcludedDirectoryContainingSymlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "packages.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	writer := tar.NewWriter(gzipWriter)
	content := []byte("package")
	if err := writer.WriteHeader(&tar.Header{
		Name: "artifactNPM/demo-1.0.0.tgz", Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteHeader(&tar.Header{
		Name: "artifactNPM/.bin/nanoid", Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: "../nanoid/bin/nanoid.cjs",
	}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	directory, cleanup, err := archive.ExtractWithExclusions(path, []string{"artifactNPM/.bin"})
	if err != nil {
		t.Fatalf("ExtractWithExclusions() error = %v", err)
	}
	defer cleanup()
	if _, err := os.Stat(filepath.Join(directory, "artifactNPM", "demo-1.0.0.tgz")); err != nil {
		t.Fatalf("included package was not extracted: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(directory, "artifactNPM", ".bin")); !os.IsNotExist(err) {
		t.Fatalf("excluded directory was extracted: %v", err)
	}
}

func TestExtractStillRejectsNonExcludedSymbolicLink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "packages.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	linkHeader := &zip.FileHeader{Name: "artifactNPM/.bin/nanoid"}
	linkHeader.SetMode(os.ModeSymlink | 0o777)
	link, err := writer.CreateHeader(linkHeader)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = link.Write([]byte("../nanoid/bin/nanoid.cjs"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	_, _, err = archive.Extract(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported symbolic link") {
		t.Fatalf("Extract() error = %v", err)
	}
}
