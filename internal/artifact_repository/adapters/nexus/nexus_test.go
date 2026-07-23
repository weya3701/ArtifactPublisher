package nexus_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"packagespublisher/internal/artifact_repository/adapters/nexus"
	"packagespublisher/internal/artifact_repository/credential"
	"packagespublisher/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestRepositoryResolvesNativeEndpoints(t *testing.T) {
	repository := nexus.New(nexus.Config{
		BaseURL: "https://nexus.example.com/nexus/", Repository: "hosted",
		Credential: credential.Basic{User: "publisher", Password: "secret"},
	})
	tests := map[model.PackageFormat]string{
		model.FormatMaven: "https://nexus.example.com/nexus/repository/hosted",
		model.FormatNPM:   "https://nexus.example.com/nexus/repository/hosted/",
		model.FormatPyPI:  "https://nexus.example.com/nexus/repository/hosted/",
	}
	for format, want := range tests {
		got, err := repository.ResolveEndpoint(format)
		if err != nil || got != want {
			t.Errorf("ResolveEndpoint(%q) = %q, %v; want %q", format, got, err, want)
		}
	}
}

func TestRepositoryReadsMavenBundleWithBasicAuth(t *testing.T) {
	assets := map[string]string{
		"/repository/maven-hosted/com/example/demo/1.0/demo-1.0.jar": "jar-content",
		"/repository/maven-hosted/com/example/demo/1.0/demo-1.0.pom": "pom-content",
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		user, password, ok := request.BasicAuth()
		if !ok || user != "publisher" || password != "secret" {
			t.Errorf("unexpected basic auth: %q %q %v", user, password, ok)
		}
		body, exists := assets[request.URL.Path]
		status := http.StatusOK
		if !exists {
			status = http.StatusNotFound
		}
		return &http.Response{
			StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header),
		}, nil
	})}
	repository := nexus.New(nexus.Config{
		BaseURL: "https://nexus.example.com", Repository: "maven-hosted", HTTPClient: client,
		Credential: credential.Basic{User: "publisher", Password: "secret"},
	})
	descriptor := model.PackageDescriptor{
		Format: model.FormatMaven, Namespace: "com.example", Name: "demo", Version: "1.0",
		Files: []model.PackageFile{
			{Name: "demo-1.0.jar", Extension: "jar"},
			{Name: "demo-1.0.pom", Extension: "pom"},
		},
	}
	remote, err := repository.GetPackageMetadata(context.Background(), descriptor)
	if err != nil {
		t.Fatalf("GetPackageMetadata() error = %v", err)
	}
	if !remote.Exists || remote.SHA256 == "" || remote.RemoteURL != "https://nexus.example.com/repository/maven-hosted/com/example/demo/1.0" {
		t.Fatalf("remote package = %+v", remote)
	}
}
