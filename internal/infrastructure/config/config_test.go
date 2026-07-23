package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"packagespublisher/internal/infrastructure/config"
	"packagespublisher/internal/model"
)

func TestLoadPublisherYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "publisher.yaml")
	data := []byte(`
package:
  path: ./artifacts
  format: maven
  publish_driver: maven_cli
repository_profile: internal-maven
repositories:
  internal-maven:
    provider: ado
    organization: company
    project: platform
    feed: approved
    credential_ref: ADO_PAT
options:
  existing_package_policy: SKIP_IDENTICAL
  timeout: 2m
  retry_count: 3
  dry_run: true
metadata:
  correlation_id: correlation-1
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	options, err := loaded.PublishOptions()
	if err != nil {
		t.Fatal(err)
	}
	if options.Timeout != 2*time.Minute || options.RetryCount != 3 || !options.DryRun || options.ExistingPackagePolicy != model.PolicySkipIdentical {
		t.Fatalf("unexpected options: %+v", options)
	}
	if loaded.Metadata.CorrelationID != "correlation-1" {
		t.Fatalf("metadata not parsed: %+v", loaded.Metadata)
	}
}

func TestLoadRejectsLiteralCredentialInsteadOfReference(t *testing.T) {
	configValue := config.Config{
		Package:           config.PackageConfig{Path: ".", Format: "maven", PublishDriver: "maven_cli"},
		RepositoryProfile: "feed",
		Repositories: map[string]config.RepositoryConfig{
			"feed": {Provider: "ado", Organization: "org", Feed: "feed"},
		},
	}
	if err := configValue.Validate(); err == nil {
		t.Fatal("expected missing credential_ref error")
	}
}

func TestValidateRequiresCompleteMavenFallbackCoordinate(t *testing.T) {
	configValue := config.Config{
		Package: config.PackageConfig{
			Path: ".", Format: "maven", PublishDriver: "maven_cli",
			Maven: config.MavenPackageConfig{GroupID: "com.example"},
		},
		RepositoryProfile: "feed",
		Repositories: map[string]config.RepositoryConfig{
			"feed": {Provider: "ado", Organization: "org", Feed: "feed", CredentialRef: "ADO_PAT"},
		},
	}
	if err := configValue.Validate(); err == nil {
		t.Fatal("expected incomplete Maven fallback GAV error")
	}
}

func TestValidateAcceptsNPMCLI(t *testing.T) {
	configValue := config.Config{
		Package:           config.PackageConfig{Path: "packages", Format: "npm", PublishDriver: "npm_cli", Recursive: true},
		RepositoryProfile: "feed",
		Repositories: map[string]config.RepositoryConfig{
			"feed": {Provider: "ado", Organization: "org", Feed: "feed", CredentialRef: "ADO_PAT"},
		},
	}
	if err := configValue.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateAcceptsPyPITwine(t *testing.T) {
	configValue := config.Config{
		Package:           config.PackageConfig{Path: "packages", Format: "pypi", PublishDriver: "twine", Recursive: true},
		RepositoryProfile: "feed",
		Repositories: map[string]config.RepositoryConfig{
			"feed": {Provider: "ado", Organization: "org", Feed: "feed", CredentialRef: "ADO_PAT"},
		},
	}
	if err := configValue.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateAcceptsNexus(t *testing.T) {
	configValue := config.Config{
		Package:           config.PackageConfig{Path: "packages", Format: "npm", PublishDriver: "npm_cli"},
		RepositoryProfile: "nexus-hosted",
		Repositories: map[string]config.RepositoryConfig{
			"nexus-hosted": {
				Provider: "nexus", BaseURL: "https://nexus.example.com", Repository: "npm-hosted",
				Username: "publisher", CredentialRef: "NEXUS_PASSWORD",
			},
		},
	}
	if err := configValue.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsIncompleteNexus(t *testing.T) {
	configValue := config.Config{
		Package:           config.PackageConfig{Path: "packages", Format: "npm", PublishDriver: "npm_cli"},
		RepositoryProfile: "nexus-hosted",
		Repositories: map[string]config.RepositoryConfig{
			"nexus-hosted": {Provider: "nexus", BaseURL: "https://nexus.example.com"},
		},
	}
	err := configValue.Validate()
	if err == nil || !strings.Contains(err.Error(), "base_url, repository, username and credential_ref") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateReportsAvailableRepositoryProfiles(t *testing.T) {
	configValue := config.Config{
		Package:           config.PackageConfig{Path: "packages", Format: "npm", PublishDriver: "npm_cli"},
		RepositoryProfile: "missing",
		Repositories: map[string]config.RepositoryConfig{
			"z-feed": {Provider: "ado"},
			"a-feed": {Provider: "ado"},
		},
	}
	err := configValue.Validate()
	if err == nil || !strings.Contains(err.Error(), `repository_profile "missing"`) || !strings.Contains(err.Error(), "available profiles: a-feed, z-feed") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateAcceptsTestRepositoryProfileWithoutRepository(t *testing.T) {
	configValue := config.Config{
		Package:           config.PackageConfig{Path: "packages", Format: "npm", PublishDriver: "npm_cli"},
		RepositoryProfile: config.TestRepositoryProfile,
	}
	if err := configValue.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
