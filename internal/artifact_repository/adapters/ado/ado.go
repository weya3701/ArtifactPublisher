package ado

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	artifactrepository "packagespublisher/internal/artifact_repository"
	"packagespublisher/internal/model"
)

type Config struct {
	Organization string
	Project      string
	Feed         string
	BaseURL      string
	Credential   artifactrepository.Credential
	HTTPClient   *http.Client
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
	if strings.TrimSpace(r.config.Organization) == "" || strings.TrimSpace(r.config.Feed) == "" {
		return fmt.Errorf("ADO organization and feed are required")
	}
	if r.config.Credential == nil || r.config.Credential.Kind() != "pat" || r.config.Credential.Secret() == "" {
		return fmt.Errorf("ADO requires a non-empty PAT credential")
	}
	if r.config.BaseURL != "" {
		parsed, err := url.Parse(r.config.BaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("invalid ADO base URL")
		}
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
	request, err := r.request(ctx, http.MethodGet, r.feedURL())
	if err != nil {
		return err
	}
	response, err := r.client.Do(request)
	if err != nil {
		return fmt.Errorf("check ADO feed connection: %w", err)
	}
	defer response.Body.Close()
	io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusOK {
		return statusError("check ADO feed connection", response.StatusCode)
	}
	r.connectionReady = true
	return nil
}

func (r *Repository) ResolveEndpoint(format model.PackageFormat) (string, error) {
	var suffix string
	switch format {
	case model.FormatMaven:
		suffix = "/maven/v1"
	case model.FormatNPM:
		suffix = "/npm/registry/"
	case model.FormatPyPI:
		suffix = "/pypi/upload/"
	default:
		return "", fmt.Errorf("ADO does not support package format %q", format)
	}
	if r.config.BaseURL != "" {
		return strings.TrimRight(r.config.BaseURL, "/") + r.projectPath() + "/_packaging/" + url.PathEscape(r.config.Feed) + suffix, nil
	}
	return "https://pkgs.dev.azure.com/" + url.PathEscape(r.config.Organization) + r.projectPath() + "/_packaging/" + url.PathEscape(r.config.Feed) + suffix, nil
}

func (r *Repository) CheckPackageExists(ctx context.Context, descriptor model.PackageDescriptor) (bool, error) {
	request, err := r.request(ctx, http.MethodGet, r.versionURL(descriptor))
	if err != nil {
		return false, err
	}
	response, err := r.client.Do(request)
	if err != nil {
		return false, fmt.Errorf("query ADO %s package: %w", descriptor.Format, err)
	}
	defer response.Body.Close()
	io.Copy(io.Discard, response.Body)
	switch response.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, statusError("query ADO "+string(descriptor.Format)+" package", response.StatusCode)
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
		remoteFile := file
		remoteFile.Path = ""
		remoteFile.SHA256 = checksum
		remoteFiles = append(remoteFiles, remoteFile)
	}
	bundleChecksum, err := model.BundleSHA256(remoteFiles)
	if err != nil {
		return model.RemotePackage{}, err
	}
	endpoint, _ := r.ResolveEndpoint(descriptor.Format)
	remoteURL := endpoint
	if descriptor.Format == model.FormatMaven {
		groupPath := strings.ReplaceAll(descriptor.Namespace, ".", "/")
		remoteURL += "/" + groupPath + "/" + url.PathEscape(descriptor.Name) + "/" + url.PathEscape(descriptor.Version)
	} else if descriptor.Format == model.FormatNPM {
		remoteURL += npmPackagePath(descriptor) + "/-/" + url.PathEscape(descriptor.Version)
	} else {
		remoteURL = strings.TrimSuffix(endpoint, "/upload/") + "/simple/" + url.PathEscape(descriptor.Name) + "/"
	}
	return model.RemotePackage{
		Exists: true, SHA256: bundleChecksum,
		RemoteURL: remoteURL,
	}, nil
}

func (r *Repository) Context() model.RepositoryContext {
	endpoint, _ := r.ResolveEndpoint(model.FormatMaven)
	return model.RepositoryContext{
		Provider: "ado", RepositoryName: r.config.Feed, PublishEndpoint: endpoint,
		QueryEndpoint: r.apiBase(), SupportedFormats: []model.PackageFormat{model.FormatMaven, model.FormatNPM, model.FormatPyPI},
	}
}

func (r *Repository) Credential() artifactrepository.Credential { return r.config.Credential }
func (r *Repository) Close() error                              { return nil }

func (r *Repository) downloadChecksum(ctx context.Context, descriptor model.PackageDescriptor, file model.PackageFile) (string, error) {
	request, err := r.request(ctx, http.MethodGet, r.downloadURL(descriptor, file.Name))
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
		io.Copy(io.Discard, response.Body)
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
		return nil, fmt.Errorf("create ADO request: %w", err)
	}
	request.SetBasicAuth(r.config.Credential.Username(), r.config.Credential.Secret())
	request.Header.Set("Accept", "application/json")
	return request, nil
}

func (r *Repository) feedURL() string {
	return r.apiBase() + r.projectPath() + "/_apis/packaging/feeds/" + url.PathEscape(r.config.Feed) + "?api-version=7.1"
}

func (r *Repository) versionURL(descriptor model.PackageDescriptor) string {
	base := r.packageBase() + r.projectPath() + "/_apis/packaging/feeds/" + url.PathEscape(r.config.Feed)
	if descriptor.Format == model.FormatNPM {
		return base + "/npm/" + npmPackagePath(descriptor) + "/versions/" + url.PathEscape(descriptor.Version) + "?api-version=7.1"
	}
	if descriptor.Format == model.FormatPyPI {
		return base + "/pypi/packages/" + url.PathEscape(descriptor.Name) + "/versions/" + url.PathEscape(descriptor.Version) + "?api-version=7.1-preview.1"
	}
	return base + "/maven/groups/" + url.PathEscape(descriptor.Namespace) + "/artifacts/" + url.PathEscape(descriptor.Name) +
		"/versions/" + url.PathEscape(descriptor.Version) + "?api-version=7.1-preview.1"
}

func (r *Repository) downloadURL(descriptor model.PackageDescriptor, fileName string) string {
	base := r.packageBase() + r.projectPath() + "/_apis/packaging/feeds/" + url.PathEscape(r.config.Feed)
	if descriptor.Format == model.FormatNPM {
		apiVersion := "7.1-preview.1"
		if descriptor.Namespace != "" {
			apiVersion = "7.1"
		}
		return base + "/npm/packages/" + npmPackagePath(descriptor) + "/versions/" + url.PathEscape(descriptor.Version) + "/content?api-version=" + apiVersion
	}
	if descriptor.Format == model.FormatPyPI {
		return base + "/pypi/packages/" + url.PathEscape(descriptor.Name) + "/versions/" +
			url.PathEscape(descriptor.Version) + "/" + url.PathEscape(fileName) + "/content?api-version=7.1-preview.1"
	}
	return base +
		"/maven/" + url.PathEscape(descriptor.Namespace) + "/" + url.PathEscape(descriptor.Name) + "/" +
		url.PathEscape(descriptor.Version) + "/" + url.PathEscape(fileName) + "/content?api-version=7.1-preview.1"
}

func npmPackagePath(descriptor model.PackageDescriptor) string {
	if descriptor.Namespace != "" {
		return "@" + url.PathEscape(descriptor.Namespace) + "/" + url.PathEscape(descriptor.Name)
	}
	return url.PathEscape(descriptor.Name)
}

func (r *Repository) apiBase() string {
	if r.config.BaseURL != "" {
		return strings.TrimRight(r.config.BaseURL, "/")
	}
	return "https://feeds.dev.azure.com/" + url.PathEscape(r.config.Organization)
}

func (r *Repository) packageBase() string {
	if r.config.BaseURL != "" {
		return strings.TrimRight(r.config.BaseURL, "/")
	}
	return "https://pkgs.dev.azure.com/" + url.PathEscape(r.config.Organization)
}

func (r *Repository) projectPath() string {
	if r.config.Project == "" {
		return ""
	}
	return "/" + url.PathEscape(r.config.Project)
}

func statusError(operation string, status int) error {
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("%s: authentication failed", operation)
	case http.StatusForbidden:
		return fmt.Errorf("%s: permission denied", operation)
	default:
		return fmt.Errorf("%s: unexpected HTTP status %d", operation, status)
	}
}
