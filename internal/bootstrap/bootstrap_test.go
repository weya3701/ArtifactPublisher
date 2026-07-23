package bootstrap_test

import (
	"fmt"
	"testing"

	"packagespublisher/internal/bootstrap"
	"packagespublisher/internal/infrastructure/config"
)

type staticSecretResolver struct{ secret string }

func (r staticSecretResolver) Resolve(string) (string, error) { return r.secret, nil }

type rejectingSecretResolver struct{ calls int }

func (r *rejectingSecretResolver) Resolve(string) (string, error) {
	r.calls++
	return "", fmt.Errorf("secret resolution must not be used in simulation mode")
}

func TestBuildSelectsNexusFromRepositoryProfile(t *testing.T) {
	components, err := bootstrap.Build(config.Config{
		Package:           config.PackageConfig{Path: "package.tgz", Format: "npm", PublishDriver: "npm_cli"},
		RepositoryProfile: "nexus-npm",
		Repositories: map[string]config.RepositoryConfig{
			"nexus-npm": {
				Provider: "nexus", BaseURL: "https://nexus.example.com", Repository: "npm-hosted",
				Username: "publisher", CredentialRef: "NEXUS_PASSWORD",
			},
		},
	}, staticSecretResolver{secret: "password"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	context := components.Service.Repository.Context()
	if context.Provider != "nexus" || context.RepositoryName != "npm-hosted" {
		t.Fatalf("repository context = %+v", context)
	}
	endpoint, err := components.Service.Repository.ResolveEndpoint("npm")
	if err != nil || endpoint != "https://nexus.example.com/repository/npm-hosted/" {
		t.Fatalf("endpoint = %q, error = %v", endpoint, err)
	}
}

func TestBuildTestProfileDoesNotResolveSecrets(t *testing.T) {
	resolver := &rejectingSecretResolver{}
	components, err := bootstrap.Build(config.Config{
		Package:           config.PackageConfig{Path: "package.tgz", Format: "npm", PublishDriver: "npm_cli"},
		RepositoryProfile: config.TestRepositoryProfile,
	}, resolver)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if resolver.calls != 0 || !components.Service.Simulation {
		t.Fatalf("secret calls=%d simulation=%v", resolver.calls, components.Service.Simulation)
	}
}
