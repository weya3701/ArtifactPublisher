package npm_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	npmhandler "packagespublisher/internal/package/formats/npm"
)

type packerFake struct {
	path  string
	calls int
}

type runnerFake struct {
	versionOutput []byte
	versionErr    error
	packOutput    []byte
	packErr       error
	calls         [][]string
}

func (r *runnerFake) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if reflect.DeepEqual(args, []string{"--version"}) {
		return r.versionOutput, r.versionErr
	}
	return r.packOutput, r.packErr
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

func TestExecPackerRejectsNPMBeforeVersion11WithoutRunningPack(t *testing.T) {
	runner := &runnerFake{versionOutput: []byte("10.9.3\n")}
	_, err := (npmhandler.ExecPacker{Runner: runner}).Pack(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "npm 11 or newer") || !strings.Contains(err.Error(), "prebuilt .tgz") {
		t.Fatalf("Pack() error = %v, want actionable npm version error", err)
	}
	if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0], []string{"npm", "--version"}) {
		t.Fatalf("runner calls = %v, want version check only", runner.calls)
	}
}

func TestExecPackerUsesIgnoreScriptsWithNPM11(t *testing.T) {
	directory := t.TempDir()
	runner := &runnerFake{
		versionOutput: []byte("11.4.2\n"),
		packOutput:    []byte(`[{"filename":"demo-1.0.0.tgz"}]`),
	}
	path, err := (npmhandler.ExecPacker{Runner: runner}).Pack(context.Background(), directory)
	if err != nil {
		t.Fatalf("Pack() error = %v", err)
	}
	if want := filepath.Join(directory, "demo-1.0.0.tgz"); path != want {
		t.Fatalf("Pack() path = %q, want %q", path, want)
	}
	wantCall := []string{"npm", "pack", directory, "--json", "--ignore-scripts", "--pack-destination", directory}
	if len(runner.calls) != 2 || !reflect.DeepEqual(runner.calls[1], wantCall) {
		t.Fatalf("runner calls = %v, want second call %v", runner.calls, wantCall)
	}
}

func TestExecPackerReportsNPMVersionCommandFailure(t *testing.T) {
	runner := &runnerFake{versionOutput: []byte("npm unavailable"), versionErr: errors.New("exit status 1")}
	_, err := (npmhandler.ExecPacker{Runner: runner}).Pack(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "determine npm version") || !strings.Contains(err.Error(), "npm unavailable") {
		t.Fatalf("Pack() error = %v, want npm version command failure", err)
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
