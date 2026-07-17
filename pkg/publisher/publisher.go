// Package publisher exposes the reusable Publisher Service API.
// Concrete MVP adapters are assembled by constructors in this package;
// callers may also provide implementations of the public interfaces below.
package publisher

import (
	"context"
	"fmt"
	"net/http"

	artifactrepository "packagespublisher/internal/artifact_repository"
	"packagespublisher/internal/artifact_repository/adapters/ado"
	"packagespublisher/internal/artifact_repository/credential"
	"packagespublisher/internal/model"
	packagehandler "packagespublisher/internal/package"
	"packagespublisher/internal/package/driver"
	mavencli "packagespublisher/internal/package/drivers/maven_cli"
	npmcli "packagespublisher/internal/package/drivers/npm_cli"
	twine "packagespublisher/internal/package/drivers/twine"
	mavenhandler "packagespublisher/internal/package/formats/maven"
	npmhandler "packagespublisher/internal/package/formats/npm"
	pypihandler "packagespublisher/internal/package/formats/pypi"
	internalpublisher "packagespublisher/internal/publisher"
)

type PackageDescriptor = model.PackageDescriptor
type PackageFile = model.PackageFile
type PublishRequest = model.PublishRequest
type PublishResult = model.PublishResult
type PublishOptions = model.PublishOptions
type RequestMetadata = model.RequestMetadata
type PublishStatus = model.PublishStatus
type ExistingPackagePolicy = model.ExistingPackagePolicy
type PackageFormat = model.PackageFormat
type RemotePackage = model.RemotePackage
type RepositoryContext = model.RepositoryContext
type Credential = artifactrepository.Credential
type PublishTarget = driver.Target
type BatchPublishReport = model.BatchPublishReport
type BatchError = internalpublisher.BatchError

const (
	StatusSuccess = model.StatusSuccess
	StatusSkipped = model.StatusSkipped
	StatusFailed  = model.StatusFailed

	PolicySkipIdentical = model.PolicySkipIdentical
	PolicyFailConflict  = model.PolicyFailConflict
	PolicyAlwaysFail    = model.PolicyAlwaysFail
	FormatMaven         = model.FormatMaven
	FormatNPM           = model.FormatNPM
	FormatPyPI          = model.FormatPyPI
)

type PackageHandler = packagehandler.Handler
type ArtifactRepository = artifactrepository.ArtifactRepository
type PublishDriver = driver.PublishDriver

type Service struct{ inner internalpublisher.Service }

type Publisher interface {
	Publish(context.Context, PublishRequest) (PublishResult, error)
}

func NewNPMADOService(config NPMADOConfig) (Service, error) {
	if config.PAT == "" {
		return Service{}, fmt.Errorf("ADO PAT is required")
	}
	repository := ado.New(ado.Config{
		Organization: config.Organization, Project: config.Project, Feed: config.Feed,
		Credential: credential.PersonalAccessToken{Token: config.PAT}, HTTPClient: config.HTTPClient,
	})
	if err := repository.ValidateConfig(); err != nil {
		return Service{}, err
	}
	return NewService(npmhandler.Handler{}, repository, npmcli.Driver{Executable: config.NPMExecutable}), nil
}

type PublisherFactory func() (Publisher, error)

type BatchService struct {
	Factory     PublisherFactory
	Parallelism int
	FailFast    bool
}

type MavenADOConfig struct {
	Organization    string
	Project         string
	Feed            string
	PAT             string
	MavenExecutable string
	HTTPClient      *http.Client
	GroupID         string
	ArtifactID      string
	Version         string
}

type NPMADOConfig struct {
	Organization  string
	Project       string
	Feed          string
	PAT           string
	NPMExecutable string
	HTTPClient    *http.Client
}

type PyPIADOConfig struct {
	Organization     string
	Project          string
	Feed             string
	PAT              string
	PythonExecutable string
	HTTPClient       *http.Client
}

func NewPyPIADOService(config PyPIADOConfig) (Service, error) {
	if config.PAT == "" {
		return Service{}, fmt.Errorf("ADO PAT is required")
	}
	repository := ado.New(ado.Config{
		Organization: config.Organization, Project: config.Project, Feed: config.Feed,
		Credential: credential.PersonalAccessToken{Token: config.PAT}, HTTPClient: config.HTTPClient,
	})
	if err := repository.ValidateConfig(); err != nil {
		return Service{}, err
	}
	return NewService(pypihandler.Handler{}, repository, twine.Driver{PythonExecutable: config.PythonExecutable}), nil
}

func NewService(handler PackageHandler, repository ArtifactRepository, publishDriver PublishDriver) Service {
	return Service{inner: internalpublisher.Service{
		Handler: handler, Repository: repository, Driver: publishDriver,
	}}
}

// NewMavenADOService assembles the built-in Maven and Azure DevOps adapters.
// Callers remain responsible for obtaining the PAT from a secret provider.
func NewMavenADOService(config MavenADOConfig) (Service, error) {
	if config.PAT == "" {
		return Service{}, fmt.Errorf("ADO PAT is required")
	}
	repository := ado.New(ado.Config{
		Organization: config.Organization,
		Project:      config.Project,
		Feed:         config.Feed,
		Credential:   credential.PersonalAccessToken{Token: config.PAT},
		HTTPClient:   config.HTTPClient,
	})
	if err := repository.ValidateConfig(); err != nil {
		return Service{}, err
	}
	return NewService(
		mavenhandler.Handler{Fallback: mavenhandler.Coordinates{
			GroupID: config.GroupID, ArtifactID: config.ArtifactID, Version: config.Version,
		}},
		repository,
		mavencli.Driver{Executable: config.MavenExecutable},
	), nil
}

func (s Service) Publish(ctx context.Context, request PublishRequest) (PublishResult, error) {
	return s.inner.Publish(ctx, request)
}

func (s BatchService) Publish(ctx context.Context, requests []PublishRequest) (BatchPublishReport, error) {
	var factory internalpublisher.PublisherFactory
	if s.Factory != nil {
		factory = func() (internalpublisher.Publisher, error) {
			return s.Factory()
		}
	}
	inner := internalpublisher.BatchService{
		Parallelism: s.Parallelism,
		FailFast:    s.FailFast,
		Factory:     factory,
	}
	return inner.Publish(ctx, requests)
}
