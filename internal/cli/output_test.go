package cli

import (
	"bytes"
	"encoding/csv"
	"reflect"
	"testing"

	"packagespublisher/internal/model"
)

func TestEncodeCSVListsEveryPhysicalPackageFileForAuditing(t *testing.T) {
	result := model.PublishResult{
		Status:   model.StatusSuccess,
		Metadata: model.RequestMetadata{CorrelationID: "promotion-001"},
		Package: model.PackageDescriptor{
			Format:  model.FormatPyPI,
			Name:    "demo",
			Version: "1.0.0",
			Files: []model.PackageFile{
				{
					Name: "demo-1.0.0-py3-none-any.whl", Path: "/packages/demo-1.0.0-py3-none-any.whl", SHA256: "wheel-sha256",
				},
				{Name: "demo-1.0.0.tar.gz", Path: "/packages/demo-1.0.0.tar.gz", SHA256: "sdist-sha256"},
			},
		},
	}

	var output bytes.Buffer
	if err := encodeCSV(&output, result); err != nil {
		t.Fatalf("encodeCSV() error = %v", err)
	}
	records, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatalf("decode CSV: %v", err)
	}
	want := [][]string{
		{"correlationId", "status", "format", "name", "version", "fileName", "filePath", "fileSha256"},
		{
			"promotion-001", "SUCCESS", "pypi", "demo", "1.0.0",
			"demo-1.0.0-py3-none-any.whl", "/packages/demo-1.0.0-py3-none-any.whl", "wheel-sha256",
		},
		{
			"promotion-001", "SUCCESS", "pypi", "demo", "1.0.0",
			"demo-1.0.0.tar.gz", "/packages/demo-1.0.0.tar.gz", "sdist-sha256",
		},
	}
	if !reflect.DeepEqual(records, want) {
		t.Fatalf("CSV records = %#v, want %#v", records, want)
	}
}

func TestEncodeCSVWritesOnlyHeaderWhenPackageFilesAreUnavailable(t *testing.T) {
	result := model.PublishResult{
		Status:    model.StatusFailed,
		InputPath: "/packages/broken.whl",
	}

	var output bytes.Buffer
	if err := encodeCSV(&output, result); err != nil {
		t.Fatalf("encodeCSV() error = %v", err)
	}
	records, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatalf("decode CSV: %v", err)
	}
	want := [][]string{
		{"correlationId", "status", "format", "name", "version", "fileName", "filePath", "fileSha256"},
	}
	if !reflect.DeepEqual(records, want) {
		t.Fatalf("CSV records = %#v, want %#v", records, want)
	}
}
