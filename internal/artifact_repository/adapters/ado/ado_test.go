package ado_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"packagespublisher/internal/artifact_repository/adapters/ado"
	"packagespublisher/internal/artifact_repository/credential"
	"packagespublisher/internal/model"
	mavenhandler "packagespublisher/internal/package/formats/maven"
)

func TestRepositoryReadsMavenPackageAndChecksums(t *testing.T) {
	directory := t.TempDir()
	pom := `<project><groupId>com.example</groupId><artifactId>demo</artifactId><version>1.0.0</version></project>`
	jar := "jar-content"
	mustWrite(t, filepath.Join(directory, "demo-1.0.0.pom"), pom)
	mustWrite(t, filepath.Join(directory, "demo-1.0.0.jar"), jar)
	descriptor, err := (mavenhandler.Handler{}).BuildPackageDescriptor(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}

	var feedChecks atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		user, password, ok := request.BasicAuth()
		if !ok || user != "AzureDevOps" || password != "test-pat" {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.Contains(request.URL.Path, "/content") && strings.Contains(request.URL.Path, ".pom"):
			_, _ = response.Write([]byte(pom))
		case strings.Contains(request.URL.Path, "/content") && strings.Contains(request.URL.Path, ".jar"):
			_, _ = response.Write([]byte(jar))
		case strings.Contains(request.URL.Path, "/maven/groups/"):
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"id":"version-id","name":"demo"}`))
		case strings.Contains(request.URL.Path, "/_apis/packaging/feeds/feed"):
			feedChecks.Add(1)
			_, _ = response.Write([]byte(`{"id":"feed"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	repository := ado.New(ado.Config{
		Organization: "org", Project: "project", Feed: "feed", BaseURL: server.URL,
		Credential: credential.PersonalAccessToken{Token: "test-pat"}, HTTPClient: server.Client(),
	})
	if err := repository.CheckConnection(context.Background()); err != nil {
		t.Fatalf("CheckConnection() error = %v", err)
	}
	if err := repository.CheckConnection(context.Background()); err != nil {
		t.Fatalf("second CheckConnection() error = %v", err)
	}
	if feedChecks.Load() != 1 {
		t.Fatalf("feed checks = %d, want one cached connection check", feedChecks.Load())
	}
	remote, err := repository.GetPackageMetadata(context.Background(), descriptor)
	if err != nil {
		t.Fatalf("GetPackageMetadata() error = %v", err)
	}
	if !remote.Exists || remote.SHA256 != descriptor.SHA256 {
		t.Fatalf("remote metadata = %+v, want checksum %s", remote, descriptor.SHA256)
	}
}

func TestRepositoryTreats404AsNotFound(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	repository := ado.New(ado.Config{
		Organization: "org", Feed: "feed", BaseURL: server.URL,
		Credential: credential.PersonalAccessToken{Token: "pat"}, HTTPClient: server.Client(),
	})
	exists, err := repository.CheckPackageExists(context.Background(), testDescriptor())
	if err != nil || exists {
		t.Fatalf("CheckPackageExists() = %v, %v; want false, nil", exists, err)
	}
}

func TestRepositoryReadsScopedNPMPackageChecksum(t *testing.T) {
	const tarball = "npm-tarball-content"
	path := filepath.Join(t.TempDir(), "scope-demo-1.0.0.tgz")
	mustWrite(t, path, tarball)
	fileChecksum, err := model.FileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	packageFile := model.PackageFile{Path: path, Name: filepath.Base(path), Extension: "tgz", SHA256: fileChecksum}
	bundleChecksum, err := model.BundleSHA256([]model.PackageFile{packageFile})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := model.PackageDescriptor{
		Format: model.FormatNPM, Namespace: "scope", Name: "demo", Version: "1.0.0",
		Packaging: "tgz", Files: []model.PackageFile{packageFile}, SHA256: bundleChecksum,
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "/npm/@scope/demo/versions/1.0.0"):
			_, _ = response.Write([]byte(`{"name":"@scope/demo","version":"1.0.0"}`))
		case strings.Contains(request.URL.Path, "/npm/packages/@scope/demo/versions/1.0.0/content"):
			_, _ = response.Write([]byte(tarball))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	repository := ado.New(ado.Config{
		Organization: "org", Project: "project", Feed: "feed", BaseURL: server.URL,
		Credential: credential.PersonalAccessToken{Token: "pat"}, HTTPClient: server.Client(),
	})
	remote, err := repository.GetPackageMetadata(context.Background(), descriptor)
	if err != nil {
		t.Fatalf("GetPackageMetadata() error = %v", err)
	}
	if !remote.Exists || remote.SHA256 != descriptor.SHA256 {
		t.Fatalf("remote = %+v", remote)
	}
	endpoint, err := repository.ResolveEndpoint(model.FormatNPM)
	if err != nil || !strings.HasSuffix(endpoint, "/npm/registry/") {
		t.Fatalf("npm endpoint = %q, %v", endpoint, err)
	}
}

func TestRepositoryReadsPyPIDistributionChecksum(t *testing.T) {
	const distribution = "python-wheel-content"
	path := filepath.Join(t.TempDir(), "demo_pkg-1.0.0-py3-none-any.whl")
	mustWrite(t, path, distribution)
	fileChecksum, err := model.FileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	packageFile := model.PackageFile{Path: path, Name: filepath.Base(path), Extension: "whl", SHA256: fileChecksum}
	bundleChecksum, err := model.BundleSHA256([]model.PackageFile{packageFile})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := model.PackageDescriptor{
		Format: model.FormatPyPI, Name: "demo-pkg", Version: "1.0.0", Packaging: "wheel",
		Files: []model.PackageFile{packageFile}, SHA256: bundleChecksum,
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "/pypi/packages/demo-pkg/versions/1.0.0/") && strings.HasSuffix(request.URL.Path, "/content"):
			_, _ = response.Write([]byte(distribution))
		case strings.Contains(request.URL.Path, "/pypi/packages/demo-pkg/versions/1.0.0"):
			_, _ = response.Write([]byte(`{"name":"demo-pkg","version":"1.0.0"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	repository := ado.New(ado.Config{
		Organization: "org", Project: "project", Feed: "feed", BaseURL: server.URL,
		Credential: credential.PersonalAccessToken{Token: "pat"}, HTTPClient: server.Client(),
	})
	remote, err := repository.GetPackageMetadata(context.Background(), descriptor)
	if err != nil {
		t.Fatalf("GetPackageMetadata() error = %v", err)
	}
	if !remote.Exists || remote.SHA256 != descriptor.SHA256 {
		t.Fatalf("remote = %+v", remote)
	}
	endpoint, err := repository.ResolveEndpoint(model.FormatPyPI)
	if err != nil || !strings.HasSuffix(endpoint, "/pypi/upload/") {
		t.Fatalf("PyPI endpoint = %q, %v", endpoint, err)
	}
}

func testDescriptor() model.PackageDescriptor {
	return model.PackageDescriptor{Format: model.FormatMaven, Namespace: "com.example", Name: "demo", Version: "1.0.0"}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
