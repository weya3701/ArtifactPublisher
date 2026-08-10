package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"packagespublisher/internal/infrastructure/config"
	"packagespublisher/internal/model"
)

func TestRunTestModePublishesWithoutRepositoryConfiguration(t *testing.T) {
	root := t.TempDir()
	packagePath := filepath.Join(root, "demo-1.0.0.tgz")
	createNPMTarball(t, packagePath, `{"name":"demo","version":"1.0.0"}`)
	configPath := filepath.Join(root, "publisher.yaml")
	configData := fmt.Sprintf(`package:
  path: %q
  format: npm
  publish_driver: npm_cli
`, packagePath)
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"publish", "--config", configPath, "--mode=test"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	var result model.PublishResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Status != model.StatusSuccess || result.RepositoryProvider != "simulation" || result.RepositoryName != "test" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRunRejectsUnsupportedMode(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"publish", "--config", "publisher.yaml", "--mode=preview"}, &stdout, &stderr)
	if exitCode != 2 || !strings.Contains(stderr.String(), `unsupported --mode "preview"`) {
		t.Fatalf("Run() exit code = %d, stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
}

func TestResolveBatchModeAutomaticallyDetectsMavenRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	versionDirectory := filepath.Join(root, "com", "example", "demo", "1.0.0")
	if err := os.MkdirAll(versionDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDirectory, "demo-1.0.0.pom"), []byte("<project/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, batchMode, err := resolveBatchMode(config.Config{Package: config.PackageConfig{Path: root}})
	if err != nil {
		t.Fatalf("resolveBatchMode() error = %v", err)
	}
	if !batchMode || len(paths) != 1 || paths[0] != versionDirectory {
		t.Fatalf("paths=%#v batchMode=%v", paths, batchMode)
	}
}

func TestResolveBatchModeKeepsSingleVersionDirectoryInSingleMode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "demo-1.0.0.pom"), []byte("<project/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, batchMode, err := resolveBatchMode(config.Config{Package: config.PackageConfig{Path: root}})
	if err != nil {
		t.Fatalf("resolveBatchMode() error = %v", err)
	}
	if batchMode || len(paths) != 0 {
		t.Fatalf("paths=%#v batchMode=%v", paths, batchMode)
	}
}

func TestResolveBatchModeRoutesNPMTarballRootToBatch(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"a/a-1.0.0.tgz", "b/b-2.0.0.tgz"} {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("tgz"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, batchMode, err := resolveBatchMode(config.Config{Package: config.PackageConfig{Path: root, Format: "npm"}})
	if err != nil {
		t.Fatalf("resolveBatchMode() error = %v", err)
	}
	if !batchMode || len(paths) != 2 {
		t.Fatalf("paths=%#v batchMode=%v", paths, batchMode)
	}
}

func createNPMTarball(t *testing.T, path, packageJSON string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	data := []byte(packageJSON)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "package/package.json", Mode: 0o600, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
