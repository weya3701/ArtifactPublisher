package driver

import (
	"context"

	artifactrepository "packagespublisher/internal/artifact_repository"
	"packagespublisher/internal/model"
)

type Target struct {
	RepositoryID string
	Endpoint     string
	Credential   artifactrepository.Credential
}

type PublishDriver interface {
	Publish(context.Context, model.PackageDescriptor, Target) error
}
