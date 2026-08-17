package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

var sourceFileCSVHeader = []string{"sourceFile", "status"}

func encodeCSV(output io.Writer, value any) error {
	writer := csv.NewWriter(output)
	if err := writer.Write(sourceFileCSVHeader); err != nil {
		return err
	}
	switch report := value.(type) {
	case model.PublishResult:
		for _, row := range publishResultCSVRows(report) {
			if err := writer.Write(row); err != nil {
				return err
			}
		}
	case model.BatchPublishReport:
		for _, result := range report.Results {
			for _, row := range publishResultCSVRows(result) {
				if err := writer.Write(row); err != nil {
					return err
				}
			}
		}
	default:
		return fmt.Errorf("unsupported CSV result type %T", value)
	}
	writer.Flush()
	return writer.Error()
}

func publishResultCSVRows(result model.PublishResult) [][]string {
	status := string(result.Status)
	if len(result.Package.Files) == 0 {
		return [][]string{{result.InputPath, status}}
	}
	rows := make([][]string, 0, len(result.Package.Files))
	for _, file := range result.Package.Files {
		sourceFile := file.Path
		if sourceFile == "" {
			sourceFile = file.Name
		}
		rows = append(rows, []string{sourceFile, status})
	}
	return rows
}
