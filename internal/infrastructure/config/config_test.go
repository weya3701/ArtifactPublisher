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
  format: npm
  publish_driver: npm_cli
  npm:
    tag: legacy
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
	if options.NPMTag != "legacy" {
		t.Fatalf("npm tag = %q; want legacy", options.NPMTag)
	}
	if loaded.Metadata.CorrelationID != "correlation-1" {
		t.Fatalf("metadata not parsed: %+v", loaded.Metadata)
	}
}

func TestLoadExpandsPackagePathEnvironmentVariable(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PACKAGE_ROOT", root)
	path := filepath.Join(t.TempDir(), "publisher.yaml")
	data := []byte(`
package:
  path: "${PACKAGE_ROOT}/node_modules"
  format: npm
  publish_driver: npm_cli
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadForMode(path, config.PublishModeTest)
	if err != nil {
		t.Fatalf("LoadForMode() error = %v", err)
	}
	want := filepath.Join(root, "node_modules")
	if loaded.Package.Path != want {
		t.Fatalf("package.path = %q, want %q", loaded.Package.Path, want)
	}
}

func TestLoadExpandsPackageArchivePathEnvironmentVariable(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PACKAGE_ROOT", root)
	path := filepath.Join(t.TempDir(), "publisher.yaml")
	data := []byte(`
package:
  archive_path: "${PACKAGE_ROOT}/approved-packages.zip"
  format: npm
  publish_driver: npm_cli
  recursive: true
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadForMode(path, config.PublishModeTest)
	if err != nil {
		t.Fatalf("LoadForMode() error = %v", err)
	}
	want := filepath.Join(root, "approved-packages.zip")
	if loaded.Package.ArchivePath != want {
		t.Fatalf("package.archive_path = %q, want %q", loaded.Package.ArchivePath, want)
	}
}

func TestLoadRejectsMissingPackagePathEnvironmentVariable(t *testing.T) {
	t.Setenv("PACKAGE_ROOT", "")
	path := filepath.Join(t.TempDir(), "publisher.yaml")
	data := []byte(`
package:
  path: "${PACKAGE_ROOT}/node_modules"
  format: npm
  publish_driver: npm_cli
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.LoadForMode(path, config.PublishModeTest)
	if err == nil || !strings.Contains(err.Error(), `package.path environment variable "PACKAGE_ROOT" is not set or empty`) {
		t.Fatalf("LoadForMode() error = %v", err)
	}
}

func TestLoadRejectsInvalidPackagePathEnvironmentReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "publisher.yaml")
	data := []byte(`
package:
  path: "${PACKAGE-ROOT}/node_modules"
  format: npm
  publish_driver: npm_cli
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.LoadForMode(path, config.PublishModeTest)
	if err == nil || !strings.Contains(err.Error(), "package.path contains an invalid environment variable reference") {
		t.Fatalf("LoadForMode() error = %v", err)
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

func TestValidateForTestModeAcceptsMissingRepository(t *testing.T) {
	configValue := config.Config{
		Package: config.PackageConfig{Path: "packages", Format: "npm", PublishDriver: "npm_cli"},
	}
	if err := configValue.ValidateForMode(config.PublishModeTest); err != nil {
		t.Fatalf("ValidateForMode() error = %v", err)
	}
}

func TestValidateRejectsTestAsRepositoryProfile(t *testing.T) {
	configValue := config.Config{
		Package:           config.PackageConfig{Path: "packages", Format: "npm", PublishDriver: "npm_cli"},
		RepositoryProfile: "test",
	}
	if err := configValue.Validate(); err == nil {
		t.Fatal("expected test repository profile to require a configured repository")
	}
}

func TestValidateAcceptsArchivePathInsteadOfPath(t *testing.T) {
	configValue := config.Config{
		Package: config.PackageConfig{
			ArchivePath: "approved-packages.zip", Format: "npm", PublishDriver: "npm_cli", Recursive: true,
		},
	}
	if err := configValue.ValidateForMode(config.PublishModeTest); err != nil {
		t.Fatalf("ValidateForMode() error = %v", err)
	}
}

func TestValidateRejectsPathAndArchivePathTogether(t *testing.T) {
	configValue := config.Config{
		Package: config.PackageConfig{
			Path: "packages", ArchivePath: "approved-packages.zip", Format: "npm", PublishDriver: "npm_cli",
		},
	}
	err := configValue.ValidateForMode(config.PublishModeTest)
	if err == nil || !strings.Contains(err.Error(), "cannot be configured together") {
		t.Fatalf("Validate() error = %v", err)
	}
}
