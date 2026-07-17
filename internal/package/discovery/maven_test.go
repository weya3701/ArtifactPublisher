package discovery_test

import (
	"os"
	"path/filepath"
	"testing"

	"packagespublisher/internal/package/discovery"
)

func TestMavenPackagesDiscoversVersionDirectoriesInStableOrder(t *testing.T) {
	root := t.TempDir()
	second := filepath.Join(root, "org", "example", "b", "2.0.0")
	first := filepath.Join(root, "com", "example", "a", "1.0.0")
	for _, fixture := range []struct{ directory, pom string }{{second, "b-2.0.0.pom"}, {first, "a-1.0.0.pom"}} {
		if err := os.MkdirAll(fixture.directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fixture.directory, fixture.pom), []byte("<project/>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := discovery.MavenPackages(root)
	if err != nil {
		t.Fatalf("MavenPackages() error = %v", err)
	}
	if len(paths) != 2 || paths[0] != first || paths[1] != second {
		t.Fatalf("paths = %#v, want [%q, %q]", paths, first, second)
	}
}

func TestMavenPackagesRejectsAmbiguousDirectory(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.pom", "b.pom"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("<project/>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := discovery.MavenPackages(root); err == nil {
		t.Fatal("expected multiple POM error")
	}
}

func TestMavenPackagesDiscoversJAROnlyDirectory(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "com", "example", "demo", "1.0.0")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "demo-1.0.0.jar"), []byte("jar"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := discovery.MavenPackages(root)
	if err != nil {
		t.Fatalf("MavenPackages() error = %v", err)
	}
	if len(paths) != 1 || paths[0] != directory {
		t.Fatalf("paths = %#v, want %q", paths, directory)
	}
}

func TestNPMPackagesDiscoversTarballsInStableOrder(t *testing.T) {
	root := t.TempDir()
	second := filepath.Join(root, "z", "z-2.0.0.tgz")
	first := filepath.Join(root, "a", "a-1.0.0.tgz")
	for _, path := range []string{second, first} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("tgz"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := discovery.NPMPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != first || paths[1] != second {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestNPMPackagesDiscoversAndDeduplicatesInstalledDependencies(t *testing.T) {
	root := t.TempDir()
	fixtures := []struct {
		directory string
		manifest  string
	}{
		{root, `{"name":"download-project","version":"1.0.0"}`},
		{filepath.Join(root, "node_modules", "plain"), `{"name":"plain","version":"2.0.0"}`},
		{filepath.Join(root, "node_modules", "@scope", "scoped"), `{"name":"@scope/scoped","version":"3.0.0"}`},
		{filepath.Join(root, "node_modules", "parent", "node_modules", "plain"), `{"name":"plain","version":"2.0.0"}`},
		{filepath.Join(root, "node_modules", "plain", "test", "fixture"), `{"name":"not-a-package-root","version":"1.0.0"}`},
	}
	for _, fixture := range fixtures {
		if err := os.MkdirAll(fixture.directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fixture.directory, "package.json"), []byte(fixture.manifest), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	paths, err := discovery.NPMPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, "node_modules", "@scope", "scoped"),
		filepath.Join(root, "node_modules", "plain"),
	}
	if len(paths) != len(want) || paths[0] != want[0] || paths[1] != want[1] {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}
