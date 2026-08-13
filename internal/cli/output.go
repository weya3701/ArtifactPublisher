package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"packagespublisher/internal/model"
)

type outputFormat string

const (
	outputJSON outputFormat = "json"
	outputCSV  outputFormat = "csv"
)

type resultOutput struct {
	format   outputFormat
	filePath string
	stdout   io.Writer
}

func newResultOutput(formatValue, filePath string, stdout io.Writer) (resultOutput, error) {
	format := outputFormat(strings.ToLower(strings.TrimSpace(formatValue)))
	extension := strings.ToLower(filepath.Ext(filePath))
	if format == "" {
		if extension == ".csv" {
			format = outputCSV
		} else {
			format = outputJSON
		}
	}
	if format != outputJSON && format != outputCSV {
		return resultOutput{}, fmt.Errorf("unsupported --output %q; supported formats are json and csv", formatValue)
	}
	if filePath != "" {
		expectedExtension := "." + string(format)
		if extension != expectedExtension {
			return resultOutput{}, fmt.Errorf("--file must use the %s extension when --output=%s", expectedExtension, format)
		}
	}
	return resultOutput{format: format, filePath: filePath, stdout: stdout}, nil
}

func (o resultOutput) Write(value any) error {
	var contents bytes.Buffer
	var err error
	if o.format == outputCSV {
		err = encodeCSV(&contents, value)
	} else {
		encoder := json.NewEncoder(&contents)
		encoder.SetIndent("", "  ")
		err = encoder.Encode(value)
	}
	if err != nil {
		return fmt.Errorf("encode %s result: %w", o.format, err)
	}
	if o.filePath != "" {
		if err := os.WriteFile(o.filePath, contents.Bytes(), 0o644); err != nil {
			return fmt.Errorf("write result file %q: %w", o.filePath, err)
		}
		return nil
	}
	if _, err := o.stdout.Write(contents.Bytes()); err != nil {
		return fmt.Errorf("write result to stdout: %w", err)
	}
	return nil
}

var publishResultCSVHeader = []string{
	"status",
	"inputPath",
	"package.format",
	"package.namespace",
	"package.name",
	"package.version",
	"package.packaging",
	"package.files",
	"package.sha256",
	"repositoryProvider",
	"repositoryName",
	"remoteUrl",
	"startedAt",
	"finishedAt",
	"errorType",
	"errorMessage",
	"metadata.pipelineId",
	"metadata.buildId",
	"metadata.commitSha",
	"metadata.correlationId",
}

var batchCSVHeader = []string{
	"batch.status",
	"batch.total",
	"batch.succeeded",
	"batch.skipped",
	"batch.failed",
	"batch.startedAt",
	"batch.finishedAt",
}

func encodeCSV(output io.Writer, value any) error {
	writer := csv.NewWriter(output)
	switch report := value.(type) {
	case model.PublishResult:
		if err := writer.Write(publishResultCSVHeader); err != nil {
			return err
		}
		row, err := publishResultCSVRow(report)
		if err != nil {
			return err
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	case model.BatchPublishReport:
		header := append(append([]string{}, batchCSVHeader...), publishResultCSVHeader...)
		if err := writer.Write(header); err != nil {
			return err
		}
		summary := batchCSVSummary(report)
		if len(report.Results) == 0 {
			if err := writer.Write(append(summary, make([]string, len(publishResultCSVHeader))...)); err != nil {
				return err
			}
		}
		for _, result := range report.Results {
			row, err := publishResultCSVRow(result)
			if err != nil {
				return err
			}
			if err := writer.Write(append(append([]string{}, summary...), row...)); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported CSV result type %T", value)
	}
	writer.Flush()
	return writer.Error()
}

func publishResultCSVRow(result model.PublishResult) ([]string, error) {
	files, err := json.Marshal(result.Package.Files)
	if err != nil {
		return nil, fmt.Errorf("encode package files: %w", err)
	}
	return []string{
		string(result.Status),
		result.InputPath,
		string(result.Package.Format),
		result.Package.Namespace,
		result.Package.Name,
		result.Package.Version,
		result.Package.Packaging,
		string(files),
		result.Package.SHA256,
		result.RepositoryProvider,
		result.RepositoryName,
		result.RemoteURL,
		formatCSVTime(result.StartedAt),
		formatCSVTime(result.FinishedAt),
		result.ErrorType,
		result.ErrorMessage,
		result.Metadata.PipelineID,
		result.Metadata.BuildID,
		result.Metadata.CommitSHA,
		result.Metadata.CorrelationID,
	}, nil
}

func batchCSVSummary(report model.BatchPublishReport) []string {
	return []string{
		string(report.Status),
		strconv.Itoa(report.Total),
		strconv.Itoa(report.Succeeded),
		strconv.Itoa(report.Skipped),
		strconv.Itoa(report.Failed),
		formatCSVTime(report.StartedAt),
		formatCSVTime(report.FinishedAt),
	}
}

func formatCSVTime(value time.Time) string {
	return value.Format(time.RFC3339Nano)
}
