package model

import "time"

type PackageFormat string

const (
	FormatMaven PackageFormat = "maven"
	FormatNPM   PackageFormat = "npm"
	FormatPyPI  PackageFormat = "pypi"
)

type PackageFile struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Classifier string `json:"classifier,omitempty"`
	Extension  string `json:"extension"`
	SHA256     string `json:"sha256"`
}

type PackageDescriptor struct {
	Format    PackageFormat `json:"format"`
	Namespace string        `json:"namespace"`
	Name      string        `json:"name"`
	Version   string        `json:"version"`
	Packaging string        `json:"packaging"`
	Files     []PackageFile `json:"files"`
	SHA256    string        `json:"sha256"`
}

type ExistingPackagePolicy string

const (
	PolicySkipIdentical ExistingPackagePolicy = "SKIP_IDENTICAL"
	PolicyFailConflict  ExistingPackagePolicy = "FAIL_ON_CONFLICT"
	PolicyAlwaysFail    ExistingPackagePolicy = "ALWAYS_FAIL_IF_EXISTS"
)

type PublishOptions struct {
	ExistingPackagePolicy ExistingPackagePolicy `json:"existingPackagePolicy" yaml:"existing_package_policy"`
	Timeout               time.Duration         `json:"timeout" yaml:"-"`
	RetryCount            int                   `json:"retryCount" yaml:"retry_count"`
	DryRun                bool                  `json:"dryRun" yaml:"dry_run"`
}

type RequestMetadata struct {
	PipelineID    string `json:"pipelineId,omitempty" yaml:"pipeline_id"`
	BuildID       string `json:"buildId,omitempty" yaml:"build_id"`
	CommitSHA     string `json:"commitSha,omitempty" yaml:"commit_sha"`
	CorrelationID string `json:"correlationId,omitempty" yaml:"correlation_id"`
}

type PublishRequest struct {
	PackagePath string
	Options     PublishOptions
	Metadata    RequestMetadata
}

type RemotePackage struct {
	Exists    bool
	SHA256    string
	RemoteURL string
	Published time.Time
}

type RepositoryContext struct {
	Provider         string
	RepositoryName   string
	PublishEndpoint  string
	QueryEndpoint    string
	SupportedFormats []PackageFormat
}

type PublishStatus string

const (
	StatusSuccess PublishStatus = "SUCCESS"
	StatusSkipped PublishStatus = "SKIPPED"
	StatusFailed  PublishStatus = "FAILED"
)

type PublishResult struct {
	Status             PublishStatus     `json:"status"`
	InputPath          string            `json:"inputPath"`
	Package            PackageDescriptor `json:"package"`
	RepositoryProvider string            `json:"repositoryProvider"`
	RepositoryName     string            `json:"repositoryName"`
	RemoteURL          string            `json:"remoteUrl,omitempty"`
	StartedAt          time.Time         `json:"startedAt"`
	FinishedAt         time.Time         `json:"finishedAt"`
	ErrorType          string            `json:"errorType,omitempty"`
	ErrorMessage       string            `json:"errorMessage,omitempty"`
	Metadata           RequestMetadata   `json:"metadata"`
}

type BatchPublishReport struct {
	Status     PublishStatus   `json:"status"`
	Total      int             `json:"total"`
	Succeeded  int             `json:"succeeded"`
	Skipped    int             `json:"skipped"`
	Failed     int             `json:"failed"`
	StartedAt  time.Time       `json:"startedAt"`
	FinishedAt time.Time       `json:"finishedAt"`
	Results    []PublishResult `json:"results"`
}
