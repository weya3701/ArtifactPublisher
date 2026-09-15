package ado_test

import (
	"context"
	"io"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestRepositoryUsesDefaultAzureEndpoints(t *testing.T) {
	repository := ado.New(ado.Config{
		Organization: "my org", Project: "platform", Feed: "approved",
		Credential: credential.PersonalAccessToken{Token: "pat"},
	})

	if got := repository.Context().QueryEndpoint; got != "https://feeds.dev.azure.com/my%20org" {
		t.Fatalf("query endpoint = %q", got)
	}
	endpoint, err := repository.ResolveEndpoint(model.FormatNPM)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://pkgs.dev.azure.com/my%20org/platform/_packaging/approved/npm/registry/"
	if endpoint != want {
		t.Fatalf("publish endpoint = %q, want %q", endpoint, want)
	}
}

func TestRepositoryUsesConfiguredFeedAndPackageBaseURLs(t *testing.T) {
	var requested []string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requested = append(requested, request.URL.String())
		status := http.StatusNotFound
		if request.URL.Host == "feeds.example.test" {
			status = http.StatusOK
		}
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader("{}")),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}
	repository := ado.New(ado.Config{
		Organization: "org", Project: "project", Feed: "feed",
		FeedBaseURL: "https://feeds.example.test/custom-org/", PackageBaseURL: "https://packages.example.test/custom-org/",
		Credential: credential.PersonalAccessToken{Token: "pat"}, HTTPClient: client,
	})

	if err := repository.CheckConnection(context.Background()); err != nil {
		t.Fatalf("CheckConnection() error = %v", err)
	}
	exists, err := repository.CheckPackageExists(context.Background(), testDescriptor())
	if err != nil || exists {
		t.Fatalf("CheckPackageExists() = %v, %v; want false, nil", exists, err)
	}
	endpoint, err := repository.ResolveEndpoint(model.FormatMaven)
	if err != nil {
		t.Fatal(err)
	}
	wantEndpoint := "https://packages.example.test/custom-org/project/_packaging/feed/maven/v1"
	if endpoint != wantEndpoint {
		t.Fatalf("publish endpoint = %q, want %q", endpoint, wantEndpoint)
	}
	wantRequests := []string{
		"https://feeds.example.test/custom-org/project/_apis/packaging/feeds/feed?api-version=7.1",
		"https://packages.example.test/custom-org/project/_apis/packaging/feeds/feed/maven/groups/com.example/artifacts/demo/versions/1.0.0?api-version=7.1-preview.1",
	}
	if len(requested) != len(wantRequests) {
		t.Fatalf("requests = %#v, want %#v", requested, wantRequests)
	}
	for index := range requested {
		if requested[index] != wantRequests[index] {
			t.Fatalf("request[%d] = %q, want %q", index, requested[index], wantRequests[index])
		}
	}
}

func TestRepositoryRejectsInvalidConfiguredBaseURLs(t *testing.T) {
	tests := []struct {
		name   string
		config ado.Config
	}{
		{name: "feed", config: ado.Config{FeedBaseURL: "://missing-scheme"}},
		{name: "package", config: ado.Config{PackageBaseURL: "not-a-url"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.config.Organization = "org"
			test.config.Feed = "feed"
			test.config.Credential = credential.PersonalAccessToken{Token: "pat"}
			if err := ado.New(test.config).ValidateConfig(); err == nil || !strings.Contains(err.Error(), "invalid ADO") {
				t.Fatalf("ValidateConfig() error = %v", err)
			}
		})
	}
}

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
