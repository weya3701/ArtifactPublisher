package bootstrap

import (
	"fmt"

	"packagespublisher/internal/artifact_repository/adapters/ado"
	"packagespublisher/internal/artifact_repository/credential"
	"packagespublisher/internal/infrastructure/config"
	"packagespublisher/internal/model"
	packagehandler "packagespublisher/internal/package"
	"packagespublisher/internal/package/driver"
	mavencli "packagespublisher/internal/package/drivers/maven_cli"
	npmcli "packagespublisher/internal/package/drivers/npm_cli"
	twine "packagespublisher/internal/package/drivers/twine"
	mavenhandler "packagespublisher/internal/package/formats/maven"
	npmhandler "packagespublisher/internal/package/formats/npm"
	pypihandler "packagespublisher/internal/package/formats/pypi"
	"packagespublisher/internal/publisher"
)

type SecretResolver interface {
	Resolve(string) (string, error)
}

type Components struct {
	Service publisher.Service
	Request model.PublishRequest
}

func Build(c config.Config, secrets SecretResolver) (Components, error) {
	profile := c.Repositories[c.RepositoryProfile]
	pat, err := secrets.Resolve(profile.CredentialRef)
	if err != nil {
		return Components{}, err
	}
	var handler packagehandler.Handler
	if c.Package.Format == string(model.FormatMaven) {
		handler = mavenhandler.Handler{Fallback: mavenhandler.Coordinates{
			GroupID: c.Package.Maven.GroupID, ArtifactID: c.Package.Maven.ArtifactID, Version: c.Package.Maven.Version,
		}}
	}
	if c.Package.Format == string(model.FormatNPM) {
		handler = npmhandler.Handler{}
	}
	if c.Package.Format == string(model.FormatPyPI) {
		handler = pypihandler.Handler{}
	}
	var publishDriver driver.PublishDriver
	if c.Package.PublishDriver == "maven_cli" {
		publishDriver = mavencli.Driver{}
	}
	if c.Package.PublishDriver == "npm_cli" {
		publishDriver = npmcli.Driver{}
	}
	if c.Package.PublishDriver == "twine" {
		publishDriver = twine.Driver{}
	}
	if handler == nil || publishDriver == nil {
		return Components{}, fmt.Errorf("unsupported package handler or publish driver")
	}
	repository := ado.New(ado.Config{
		Organization: profile.Organization,
		Project:      profile.Project,
		Feed:         profile.Feed,
		Credential:   credential.PersonalAccessToken{Token: pat},
	})
	options, err := c.PublishOptions()
	if err != nil {
		return Components{}, err
	}
	return Components{
		Service: publisher.Service{Handler: handler, Repository: repository, Driver: publishDriver},
		Request: model.PublishRequest{PackagePath: c.Package.Path, Options: options, Metadata: c.Metadata},
	}, nil
}
