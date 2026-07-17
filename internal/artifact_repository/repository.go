package artifact_repository

import (
	"context"

	"packagespublisher/internal/model"
)

type Credential interface {
	Kind() string
	Username() string
	Secret() string
}

type ArtifactRepository interface {
	ValidateConfig() error
	Connect(context.Context) error
	CheckConnection(context.Context) error
	ResolveEndpoint(model.PackageFormat) (string, error)
	CheckPackageExists(context.Context, model.PackageDescriptor) (bool, error)
	GetPackageMetadata(context.Context, model.PackageDescriptor) (model.RemotePackage, error)
	Context() model.RepositoryContext
	Credential() Credential
	Close() error
}
