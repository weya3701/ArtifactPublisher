package bootstrap_test

import (
	"fmt"
	"testing"

	"packagespublisher/internal/bootstrap"
	"packagespublisher/internal/infrastructure/config"
)

type rejectingSecretResolver struct{ calls int }

func (r *rejectingSecretResolver) Resolve(string) (string, error) {
	r.calls++
	return "", fmt.Errorf("secret resolution must not be used in simulation mode")
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
