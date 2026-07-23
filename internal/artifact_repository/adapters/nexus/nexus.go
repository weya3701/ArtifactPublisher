package nexus

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	artifactrepository "packagespublisher/internal/artifact_repository"
	"packagespublisher/internal/model"
)

type Config struct {
	BaseURL    string
	Repository string
	Credential artifactrepository.Credential
	HTTPClient *http.Client
}

type Repository struct {
	config          Config
	client          *http.Client
	connectionMu    sync.Mutex
	connectionReady bool
}

func New(config Config) *Repository {
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Repository{config: config, client: client}
}

func (r *Repository) ValidateConfig() error {
	parsed, err := url.Parse(r.config.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("Nexus requires a valid base URL")
	}
	if strings.TrimSpace(r.config.Repository) == "" {
		return fmt.Errorf("Nexus repository name is required")
	}
	if r.config.Credential == nil || r.config.Credential.Kind() != "basic" ||
		strings.TrimSpace(r.config.Credential.Username()) == "" || r.config.Credential.Secret() == "" {
		return fmt.Errorf("Nexus requires a non-empty basic credential")
	}
	return nil
}

func (r *Repository) Connect(context.Context) error { return r.ValidateConfig() }

func (r *Repository) CheckConnection(ctx context.Context) error {
	r.connectionMu.Lock()
	defer r.connectionMu.Unlock()
	if r.connectionReady {
		return nil
	}
	request, err := r.request(ctx, http.MethodGet, r.repositoryURL()+"/")
	if err != nil {
		return err
	}
	response, err := r.client.Do(request)
	if err != nil {
		return fmt.Errorf("check Nexus repository connection: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return statusError("check Nexus repository connection", response.StatusCode)
	}
	r.connectionReady = true
	return nil
}

func (r *Repository) ResolveEndpoint(format model.PackageFormat) (string, error) {
	switch format {
	case model.FormatMaven:
		return r.repositoryURL(), nil
	case model.FormatNPM, model.FormatPyPI:
		return r.repositoryURL() + "/", nil
	default:
		return "", fmt.Errorf("Nexus does not support package format %q", format)
	}
}

func (r *Repository) CheckPackageExists(ctx context.Context, descriptor model.PackageDescriptor) (bool, error) {
	if len(descriptor.Files) == 0 {
		return false, fmt.Errorf("package descriptor contains no files")
	}
	request, err := r.request(ctx, http.MethodGet, r.assetURL(descriptor, descriptor.Files[0]))
	if err != nil {
		return false, err
	}
	response, err := r.client.Do(request)
	if err != nil {
		return false, fmt.Errorf("query Nexus %s package: %w", descriptor.Format, err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	switch response.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, statusError("query Nexus "+string(descriptor.Format)+" package", response.StatusCode)
	}
}

func (r *Repository) GetPackageMetadata(ctx context.Context, descriptor model.PackageDescriptor) (model.RemotePackage, error) {
	exists, err := r.CheckPackageExists(ctx, descriptor)
	if err != nil || !exists {
		return model.RemotePackage{Exists: exists}, err
	}
	remoteFiles := make([]model.PackageFile, 0, len(descriptor.Files))
	for _, file := range descriptor.Files {
		checksum, err := r.downloadChecksum(ctx, descriptor, file)
		if err != nil {
			return model.RemotePackage{}, err
		}
		remote := file
		remote.Path = ""
		remote.SHA256 = checksum
		remoteFiles = append(remoteFiles, remote)
	}
	bundleChecksum, err := model.BundleSHA256(remoteFiles)
	if err != nil {
		return model.RemotePackage{}, err
	}
	return model.RemotePackage{
		Exists: true, SHA256: bundleChecksum, RemoteURL: r.packageURL(descriptor),
	}, nil
}

func (r *Repository) Context() model.RepositoryContext {
	return model.RepositoryContext{
		Provider: "nexus", RepositoryName: r.config.Repository,
		PublishEndpoint: r.repositoryURL(), QueryEndpoint: r.repositoryURL(),
		SupportedFormats: []model.PackageFormat{model.FormatMaven, model.FormatNPM, model.FormatPyPI},
	}
}

func (r *Repository) Credential() artifactrepository.Credential { return r.config.Credential }
func (r *Repository) Close() error                              { return nil }

func (r *Repository) downloadChecksum(ctx context.Context, descriptor model.PackageDescriptor, file model.PackageFile) (string, error) {
	request, err := r.request(ctx, http.MethodGet, r.assetURL(descriptor, file))
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/octet-stream")
	response, err := r.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download remote %s artifact %q: %w", descriptor.Format, file.Name, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return "", statusError("download remote "+string(descriptor.Format)+" artifact "+file.Name, response.StatusCode)
	}
	hash := newSHA256Writer()
	if _, err := io.Copy(hash, response.Body); err != nil {
		return "", fmt.Errorf("hash remote %s artifact %q: %w", descriptor.Format, file.Name, err)
	}
	return hash.Sum(), nil
}

func (r *Repository) request(ctx context.Context, method, address string) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, address, nil)
	if err != nil {
		return nil, fmt.Errorf("create Nexus request: %w", err)
	}
	request.SetBasicAuth(r.config.Credential.Username(), r.config.Credential.Secret())
	return request, nil
}

func (r *Repository) assetURL(descriptor model.PackageDescriptor, file model.PackageFile) string {
	var parts []string
	switch descriptor.Format {
	case model.FormatMaven:
		parts = append(strings.Split(descriptor.Namespace, "."), descriptor.Name, descriptor.Version, file.Name)
	case model.FormatNPM:
		if descriptor.Namespace != "" {
			parts = []string{"@" + descriptor.Namespace, descriptor.Name, "-", file.Name}
		} else {
			parts = []string{descriptor.Name, "-", file.Name}
		}
	case model.FormatPyPI:
		parts = []string{"packages", file.Name}
	}
	return joinURL(r.repositoryURL(), parts...)
}

func (r *Repository) packageURL(descriptor model.PackageDescriptor) string {
	switch descriptor.Format {
	case model.FormatMaven:
		return joinURL(r.repositoryURL(), append(strings.Split(descriptor.Namespace, "."), descriptor.Name, descriptor.Version)...)
	case model.FormatNPM:
		if descriptor.Namespace != "" {
			return joinURL(r.repositoryURL(), "@"+descriptor.Namespace, descriptor.Name)
		}
		return joinURL(r.repositoryURL(), descriptor.Name)
	case model.FormatPyPI:
		return joinURL(r.repositoryURL(), "simple", descriptor.Name) + "/"
	default:
		return r.repositoryURL()
	}
}

func (r *Repository) repositoryURL() string {
	return joinURL(strings.TrimRight(r.config.BaseURL, "/"), "repository", r.config.Repository)
}

func joinURL(base string, parts ...string) string {
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			escaped = append(escaped, url.PathEscape(part))
		}
	}
	return strings.TrimRight(base, "/") + "/" + path.Join(escaped...)
}

func statusError(operation string, status int) error {
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("%s: authentication failed", operation)
	case http.StatusForbidden:
		return fmt.Errorf("%s: permission denied", operation)
	case http.StatusNotFound:
		return fmt.Errorf("%s: not found", operation)
	default:
		return fmt.Errorf("%s: unexpected HTTP status %d", operation, status)
	}
}
