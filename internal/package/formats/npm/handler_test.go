package npm_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	npmhandler "packagespublisher/internal/package/formats/npm"
)

type packerFake struct {
	path  string
	calls int
}

func (p *packerFake) Pack(context.Context, string) (string, error) {
	p.calls++
	return p.path, nil
}

func TestBuildPackageDescriptorFromScopedTarball(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scope-demo-1.2.3.tgz")
	createTarball(t, path, `{"name":"@scope/demo","version":"1.2.3"}`)
	descriptor, err := (npmhandler.Handler{}).BuildPackageDescriptor(context.Background(), path)
	if err != nil {
		t.Fatalf("BuildPackageDescriptor() error = %v", err)
	}
	if descriptor.Namespace != "scope" || descriptor.Name != "demo" || descriptor.Version != "1.2.3" {
		t.Fatalf("unexpected descriptor: %+v", descriptor)
	}
	if _, err := os.Stat(path + ".sha256"); err != nil {
		t.Fatalf("SHA-256 sidecar was not generated: %v", err)
	}
}

func TestBuildPackageDescriptorRejectsPrivatePackage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-1.0.0.tgz")
	createTarball(t, path, `{"name":"private-package","version":"1.0.0","private":true}`)
	if _, err := (npmhandler.Handler{}).BuildPackageDescriptor(context.Background(), path); err == nil {
		t.Fatal("expected private package error")
	}
}

func TestBuildPackageDescriptorPacksPackageJSONDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"demo","version":"2.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tarball := filepath.Join(t.TempDir(), "demo-2.0.0.tgz")
	createTarball(t, tarball, `{"name":"demo","version":"2.0.0"}`)
	packer := &packerFake{path: tarball}
	descriptor, err := (npmhandler.Handler{Packer: packer}).BuildPackageDescriptor(context.Background(), root)
	if err != nil {
		t.Fatalf("BuildPackageDescriptor() error = %v", err)
	}
	if packer.calls != 1 || descriptor.Name != "demo" {
		t.Fatalf("packer calls=%d descriptor=%+v", packer.calls, descriptor)
	}
}

func createTarball(t *testing.T, path, packageJSON string) {
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
