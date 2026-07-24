package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"packagespublisher/internal/model"
)

const TestRepositoryProfile = "test"

type PackageConfig struct {
	Path          string             `yaml:"path"`
	ArchivePath   string             `yaml:"archive_path"`
	Format        string             `yaml:"format"`
	PublishDriver string             `yaml:"publish_driver"`
	Recursive     bool               `yaml:"recursive"`
	Maven         MavenPackageConfig `yaml:"maven"`
}

type MavenPackageConfig struct {
	GroupID    string `yaml:"group_id"`
	ArtifactID string `yaml:"artifact_id"`
	Version    string `yaml:"version"`
}

type RepositoryConfig struct {
	Provider      string `yaml:"provider"`
	Organization  string `yaml:"organization"`
	Project       string `yaml:"project"`
	Feed          string `yaml:"feed"`
	BaseURL       string `yaml:"base_url"`
	Repository    string `yaml:"repository"`
	Username      string `yaml:"username"`
	CredentialRef string `yaml:"credential_ref"`
}

type OptionsConfig struct {
	ExistingPackagePolicy model.ExistingPackagePolicy `yaml:"existing_package_policy"`
	Timeout               string                      `yaml:"timeout"`
	RetryCount            int                         `yaml:"retry_count"`
	DryRun                bool                        `yaml:"dry_run"`
	Parallelism           int                         `yaml:"parallelism"`
	FailFast              bool                        `yaml:"fail_fast"`
}

type Config struct {
	Package           PackageConfig               `yaml:"package"`
	RepositoryProfile string                      `yaml:"repository_profile"`
	Repositories      map[string]RepositoryConfig `yaml:"repositories"`
	Options           OptionsConfig               `yaml:"options"`
	Metadata          model.RequestMetadata       `yaml:"metadata"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("parse YAML config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	if c.Package.Path == "" && c.Package.ArchivePath == "" {
		return fmt.Errorf("one of package.path or package.archive_path is required")
	}
	if c.Package.Path != "" && c.Package.ArchivePath != "" {
		return fmt.Errorf("package.path and package.archive_path cannot be configured together")
	}
	switch c.Package.Format {
	case string(model.FormatMaven):
		if c.Package.PublishDriver != "maven_cli" {
			return fmt.Errorf("Maven packages require package.publish_driver=maven_cli")
		}
	case string(model.FormatNPM):
		if c.Package.PublishDriver != "npm_cli" {
			return fmt.Errorf("npm packages require package.publish_driver=npm_cli")
		}
	case string(model.FormatPyPI):
		if c.Package.PublishDriver != "twine" {
			return fmt.Errorf("PyPI packages require package.publish_driver=twine")
		}
	default:
		return fmt.Errorf("supported package formats are maven, npm and pypi")
	}
	fallbackFields := 0
	for _, value := range []string{c.Package.Maven.GroupID, c.Package.Maven.ArtifactID, c.Package.Maven.Version} {
		if value != "" {
			fallbackFields++
		}
	}
	if c.Package.Format != string(model.FormatMaven) && fallbackFields != 0 {
		return fmt.Errorf("package.maven fallback is valid for Maven packages only")
	}
	if fallbackFields != 0 && fallbackFields != 3 {
		return fmt.Errorf("package.maven group_id, artifact_id and version must be configured together")
	}
	if c.RepositoryProfile == TestRepositoryProfile {
		return c.validateOptions()
	}
	profile, ok := c.Repositories[c.RepositoryProfile]
	if c.RepositoryProfile == "" || !ok {
		available := make([]string, 0, len(c.Repositories))
		for name := range c.Repositories {
			available = append(available, name)
		}
		sort.Strings(available)
		return fmt.Errorf("repository_profile %q does not match a configured repository; available profiles: %s", c.RepositoryProfile, strings.Join(available, ", "))
	}
	switch profile.Provider {
	case "ado":
		if profile.Organization == "" || profile.Feed == "" || profile.CredentialRef == "" {
			return fmt.Errorf("ADO repository requires organization, feed and credential_ref")
		}
	case "nexus":
		if profile.BaseURL == "" || profile.Repository == "" || profile.Username == "" || profile.CredentialRef == "" {
			return fmt.Errorf("Nexus repository requires base_url, repository, username and credential_ref")
		}
	default:
		return fmt.Errorf("unsupported repository provider %q; supported providers are ado and nexus", profile.Provider)
	}
	return c.validateOptions()
}

func (c Config) validateOptions() error {
	switch c.Options.ExistingPackagePolicy {
	case "", model.PolicySkipIdentical, model.PolicyFailConflict, model.PolicyAlwaysFail:
	default:
		return fmt.Errorf("unsupported existing package policy %q", c.Options.ExistingPackagePolicy)
	}
	if c.Options.RetryCount < 0 {
		return fmt.Errorf("options.retry_count cannot be negative")
	}
	if c.Options.Parallelism < 0 {
		return fmt.Errorf("options.parallelism cannot be negative")
	}
	if c.Options.Timeout != "" {
		if _, err := time.ParseDuration(c.Options.Timeout); err != nil {
			return fmt.Errorf("invalid options.timeout: %w", err)
		}
	}
	return nil
}

func (c Config) PublishOptions() (model.PublishOptions, error) {
	timeout := time.Duration(0)
	var err error
	if c.Options.Timeout != "" {
		timeout, err = time.ParseDuration(c.Options.Timeout)
		if err != nil {
			return model.PublishOptions{}, err
		}
	}
	return model.PublishOptions{
		ExistingPackagePolicy: c.Options.ExistingPackagePolicy,
		Timeout:               timeout, RetryCount: c.Options.RetryCount, DryRun: c.Options.DryRun,
	}, nil
}
