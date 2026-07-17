package publisher_test

import (
	"context"
	"errors"
	"testing"

	artifactrepository "packagespublisher/internal/artifact_repository"
	"packagespublisher/internal/artifact_repository/credential"
	"packagespublisher/internal/model"
	"packagespublisher/internal/package/driver"
	"packagespublisher/internal/publisher"
)

type handlerFake struct{ descriptor model.PackageDescriptor }

func (h handlerFake) Detect(string) bool { return true }
func (h handlerFake) ParseMetadata(context.Context, string) (model.PackageDescriptor, error) {
	return h.descriptor, nil
}
func (handlerFake) ValidateCompleteness(model.PackageDescriptor) error { return nil }
func (h handlerFake) BuildPackageDescriptor(context.Context, string) (model.PackageDescriptor, error) {
	return h.descriptor, nil
}

type repositoryFake struct {
	metadata []model.RemotePackage
	calls    int
}

func (*repositoryFake) ValidateConfig() error                 { return nil }
func (*repositoryFake) Connect(context.Context) error         { return nil }
func (*repositoryFake) CheckConnection(context.Context) error { return nil }
func (*repositoryFake) ResolveEndpoint(model.PackageFormat) (string, error) {
	return "https://example.test/maven/v1", nil
}
func (r *repositoryFake) CheckPackageExists(context.Context, model.PackageDescriptor) (bool, error) {
	value, err := r.GetPackageMetadata(context.Background(), model.PackageDescriptor{})
	return value.Exists, err
}
func (r *repositoryFake) GetPackageMetadata(context.Context, model.PackageDescriptor) (model.RemotePackage, error) {
	if len(r.metadata) == 0 {
		return model.RemotePackage{}, errors.New("unexpected metadata call")
	}
	index := r.calls
	if index >= len(r.metadata) {
		index = len(r.metadata) - 1
	}
	r.calls++
	return r.metadata[index], nil
}
func (*repositoryFake) Context() model.RepositoryContext {
	return model.RepositoryContext{Provider: "ado", RepositoryName: "feed"}
}
func (*repositoryFake) Credential() artifactrepository.Credential {
	return credential.PersonalAccessToken{Token: "pat"}
}
func (*repositoryFake) Close() error { return nil }

type driverFake struct {
	calls int
	err   error
}

func (d *driverFake) Publish(context.Context, model.PackageDescriptor, driver.Target) error {
	d.calls++
	return d.err
}

func TestPublishNewPackageAndVerify(t *testing.T) {
	descriptor := descriptorFixture()
	repository := &repositoryFake{metadata: []model.RemotePackage{
		{Exists: false},
		{Exists: true, SHA256: descriptor.SHA256, RemoteURL: "https://example.test/package"},
	}}
	publishDriver := &driverFake{}
	service := publisher.Service{Handler: handlerFake{descriptor}, Repository: repository, Driver: publishDriver}
	result, err := service.Publish(context.Background(), model.PublishRequest{PackagePath: "/artifacts/demo"})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if result.Status != model.StatusSuccess || publishDriver.calls != 1 || repository.calls != 2 {
		t.Fatalf("result=%+v driver calls=%d repository calls=%d", result, publishDriver.calls, repository.calls)
	}
}

func TestPublishSkipsIdenticalPackage(t *testing.T) {
	descriptor := descriptorFixture()
	repository := &repositoryFake{metadata: []model.RemotePackage{{Exists: true, SHA256: descriptor.SHA256}}}
	publishDriver := &driverFake{}
	service := publisher.Service{Handler: handlerFake{descriptor}, Repository: repository, Driver: publishDriver}
	result, err := service.Publish(context.Background(), model.PublishRequest{PackagePath: "/artifacts/demo"})
	if err != nil || result.Status != model.StatusSkipped || publishDriver.calls != 0 {
		t.Fatalf("result=%+v error=%v driver calls=%d", result, err, publishDriver.calls)
	}
}

func TestPublishRejectsDifferentContent(t *testing.T) {
	descriptor := descriptorFixture()
	repository := &repositoryFake{metadata: []model.RemotePackage{{Exists: true, SHA256: "different"}}}
	service := publisher.Service{Handler: handlerFake{descriptor}, Repository: repository, Driver: &driverFake{}}
	result, err := service.Publish(context.Background(), model.PublishRequest{PackagePath: "/artifacts/demo"})
	var publishErr *publisher.PublishError
	if !errors.As(err, &publishErr) || publishErr.Type != publisher.ErrorConflict || result.Status != model.StatusFailed {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestDryRunHasNoPublishSideEffect(t *testing.T) {
	descriptor := descriptorFixture()
	repository := &repositoryFake{metadata: []model.RemotePackage{{Exists: false}}}
	publishDriver := &driverFake{}
	service := publisher.Service{Handler: handlerFake{descriptor}, Repository: repository, Driver: publishDriver}
	result, err := service.Publish(context.Background(), model.PublishRequest{
		PackagePath: "/artifacts/demo", Options: model.PublishOptions{DryRun: true},
	})
	if err != nil || result.Status != model.StatusSkipped || publishDriver.calls != 0 {
		t.Fatalf("result=%+v error=%v driver calls=%d", result, err, publishDriver.calls)
	}
}

func TestAmbiguousPublishFailureIsNotRetriedWhenRemoteChecksumMatches(t *testing.T) {
	descriptor := descriptorFixture()
	repository := &repositoryFake{metadata: []model.RemotePackage{
		{Exists: false},
		{Exists: true, SHA256: descriptor.SHA256},
		{Exists: true, SHA256: descriptor.SHA256},
	}}
	publishDriver := &driverFake{err: errors.New("connection reset after upload")}
	service := publisher.Service{Handler: handlerFake{descriptor}, Repository: repository, Driver: publishDriver}
	result, err := service.Publish(context.Background(), model.PublishRequest{
		PackagePath: "/artifacts/demo", Options: model.PublishOptions{RetryCount: 2},
	})
	if err != nil || result.Status != model.StatusSuccess || publishDriver.calls != 1 {
		t.Fatalf("result=%+v error=%v driver calls=%d", result, err, publishDriver.calls)
	}
}

func descriptorFixture() model.PackageDescriptor {
	return model.PackageDescriptor{
		Format: model.FormatMaven, Namespace: "com.example", Name: "demo", Version: "1.0.0",
		Packaging: "jar", SHA256: "bundle-sha",
	}
}
