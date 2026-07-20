package publisher

import (
	"context"
	"errors"
	"fmt"
	"time"

	artifactrepository "packagespublisher/internal/artifact_repository"
	"packagespublisher/internal/model"
	packagehandler "packagespublisher/internal/package"
	"packagespublisher/internal/package/driver"
)

type ErrorType string

const (
	ErrorConfiguration ErrorType = "CONFIGURATION"
	ErrorPackage       ErrorType = "PACKAGE"
	ErrorConnection    ErrorType = "CONNECTION"
	ErrorConflict      ErrorType = "CONFLICT"
	ErrorPublish       ErrorType = "PUBLISH"
	ErrorVerification  ErrorType = "VERIFICATION"
)

type PublishError struct {
	Type ErrorType
	Err  error
}

func (e *PublishError) Error() string { return fmt.Sprintf("%s: %v", e.Type, e.Err) }
func (e *PublishError) Unwrap() error { return e.Err }

type Service struct {
	Handler    packagehandler.Handler
	Repository artifactrepository.ArtifactRepository
	Driver     driver.PublishDriver
	Simulation bool
	Now        func() time.Time
}

func (s Service) Publish(ctx context.Context, request model.PublishRequest) (model.PublishResult, error) {
	now := s.Now
	if now == nil {
		now = time.Now
	}
	result := model.PublishResult{Status: model.StatusFailed, InputPath: request.PackagePath, StartedAt: now().UTC(), Metadata: request.Metadata}
	finish := func(errorType ErrorType, err error) (model.PublishResult, error) {
		result.FinishedAt = now().UTC()
		if err != nil {
			result.ErrorType = string(errorType)
			result.ErrorMessage = err.Error()
			return result, &PublishError{Type: errorType, Err: err}
		}
		return result, nil
	}
	if s.Handler == nil || (!s.Simulation && (s.Repository == nil || s.Driver == nil)) {
		return finish(ErrorConfiguration, fmt.Errorf("publisher service dependencies are required"))
	}
	if request.PackagePath == "" {
		return finish(ErrorConfiguration, fmt.Errorf("package path is required"))
	}
	if request.Options.ExistingPackagePolicy == "" {
		request.Options.ExistingPackagePolicy = model.PolicySkipIdentical
	}

	descriptor, err := s.Handler.BuildPackageDescriptor(ctx, request.PackagePath)
	if err != nil {
		return finish(ErrorPackage, err)
	}
	result.Package = descriptor
	if s.Simulation {
		result.RepositoryProvider = "simulation"
		result.RepositoryName = "test"
		result.Status = model.StatusSuccess
		return finish("", nil)
	}
	if err := s.Repository.ValidateConfig(); err != nil {
		return finish(ErrorConfiguration, err)
	}
	if err := s.Repository.Connect(ctx); err != nil {
		return finish(ErrorConnection, err)
	}
	defer s.Repository.Close()
	if err := s.Repository.CheckConnection(ctx); err != nil {
		return finish(ErrorConnection, err)
	}
	repositoryContext := s.Repository.Context()
	result.RepositoryProvider = repositoryContext.Provider
	result.RepositoryName = repositoryContext.RepositoryName
	endpoint, err := s.Repository.ResolveEndpoint(descriptor.Format)
	if err != nil {
		return finish(ErrorConfiguration, err)
	}

	remote, err := s.Repository.GetPackageMetadata(ctx, descriptor)
	if err != nil {
		return finish(ErrorConnection, err)
	}
	if remote.Exists {
		if request.Options.ExistingPackagePolicy == model.PolicyAlwaysFail {
			return finish(ErrorConflict, fmt.Errorf("package version already exists"))
		}
		if remote.SHA256 == descriptor.SHA256 {
			result.Status = model.StatusSkipped
			result.RemoteURL = remote.RemoteURL
			return finish("", nil)
		}
		return finish(ErrorConflict, fmt.Errorf("package version exists with different content"))
	}
	if request.Options.DryRun {
		result.Status = model.StatusSkipped
		return finish("", nil)
	}

	publishCtx := ctx
	cancel := func() {}
	if request.Options.Timeout > 0 {
		publishCtx, cancel = context.WithTimeout(ctx, request.Options.Timeout)
	}
	defer cancel()
	target := driver.Target{RepositoryID: repositoryContext.RepositoryName, Endpoint: endpoint, Credential: s.Repository.Credential()}
	if err := s.publishWithRetry(publishCtx, descriptor, target, request.Options.RetryCount); err != nil {
		return finish(ErrorPublish, err)
	}

	var verified model.RemotePackage
	if err := retry(request.Options.RetryCount, func() error {
		var queryErr error
		verified, queryErr = s.Repository.GetPackageMetadata(publishCtx, descriptor)
		if queryErr != nil {
			return queryErr
		}
		if !verified.Exists {
			return errors.New("published package was not found")
		}
		if verified.SHA256 != descriptor.SHA256 {
			return errors.New("published package checksum differs from local bundle")
		}
		return nil
	}); err != nil {
		return finish(ErrorVerification, err)
	}
	result.Status = model.StatusSuccess
	result.RemoteURL = verified.RemoteURL
	return finish("", nil)
}

// publishWithRetry makes an ambiguous failed upload idempotent by checking the
// remote checksum before sending the artifact again.
func (s Service) publishWithRetry(ctx context.Context, descriptor model.PackageDescriptor, target driver.Target, retryCount int) error {
	var lastErr error
	for attempt := 0; attempt <= retryCount; attempt++ {
		if lastErr = s.Driver.Publish(ctx, descriptor, target); lastErr == nil {
			return nil
		}
		remote, lookupErr := s.Repository.GetPackageMetadata(ctx, descriptor)
		if lookupErr == nil && remote.Exists {
			if remote.SHA256 == descriptor.SHA256 {
				return nil
			}
			return fmt.Errorf("publish failed and remote version has different content: %w", lastErr)
		}
	}
	return lastErr
}

func retry(retryCount int, operation func() error) error {
	var err error
	for attempt := 0; attempt <= retryCount; attempt++ {
		if err = operation(); err == nil {
			return nil
		}
	}
	return err
}
