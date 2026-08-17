package cli

import (
	"bytes"
	"encoding/csv"
	"reflect"
	"testing"

	"packagespublisher/internal/model"
)

func TestEncodeCSVListsEachSourceFileAndItsStatus(t *testing.T) {
	result := model.PublishResult{
		Status: model.StatusSuccess,
		Package: model.PackageDescriptor{Files: []model.PackageFile{
			{Path: "/packages/demo-1.0.0-py3-none-any.whl"},
			{Path: "/packages/demo-1.0.0.tar.gz"},
		}},
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
		{"sourceFile", "status"},
		{"/packages/demo-1.0.0-py3-none-any.whl", "SUCCESS"},
		{"/packages/demo-1.0.0.tar.gz", "SUCCESS"},
	}
	if !reflect.DeepEqual(records, want) {
		t.Fatalf("CSV records = %#v, want %#v", records, want)
	}
}

func TestEncodeCSVUsesInputPathWhenPackageFilesAreUnavailable(t *testing.T) {
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
		{"sourceFile", "status"},
		{"/packages/broken.whl", "FAILED"},
	}
	if !reflect.DeepEqual(records, want) {
		t.Fatalf("CSV records = %#v, want %#v", records, want)
	}
}
