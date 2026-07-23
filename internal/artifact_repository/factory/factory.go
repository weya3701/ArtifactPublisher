package factory

import (
	"fmt"

	artifactrepository "packagespublisher/internal/artifact_repository"
	"packagespublisher/internal/artifact_repository/adapters/ado"
	"packagespublisher/internal/artifact_repository/adapters/nexus"
	"packagespublisher/internal/artifact_repository/credential"
	"packagespublisher/internal/infrastructure/config"
)

type SecretResolver interface {
	Resolve(string) (string, error)
}

// New is the single composition point for repository providers. Bootstrap only
// selects a profile; provider-specific construction stays in this package.
func New(profile config.RepositoryConfig, secrets SecretResolver) (artifactrepository.ArtifactRepository, error) {
	secret, err := secrets.Resolve(profile.CredentialRef)
	if err != nil {
		return nil, fmt.Errorf("resolve %s credential: %w", profile.Provider, err)
	}

	var repository artifactrepository.ArtifactRepository
	switch profile.Provider {
	case "ado":
		repository = ado.New(ado.Config{
			Organization: profile.Organization,
			Project:      profile.Project,
			Feed:         profile.Feed,
			BaseURL:      profile.BaseURL,
			Credential:   credential.PersonalAccessToken{Token: secret},
		})
	case "nexus":
		repository = nexus.New(nexus.Config{
			BaseURL:    profile.BaseURL,
			Repository: profile.Repository,
			Credential: credential.Basic{User: profile.Username, Password: secret},
		})
	default:
		return nil, fmt.Errorf("unsupported repository provider %q", profile.Provider)
	}
	if err := repository.ValidateConfig(); err != nil {
		return nil, err
	}
	return repository, nil
}
