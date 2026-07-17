package cli

import (
	"os"
	"path/filepath"
	"testing"

	"packagespublisher/internal/infrastructure/config"
)

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
