package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"packagespublisher/internal/infrastructure/config"
	"packagespublisher/internal/model"
)

func TestRunTestModePublishesWithoutRepositoryConfiguration(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PACKAGE_ROOT", root)
	packagePath := filepath.Join(root, "demo-1.0.0.tgz")
	createNPMTarball(t, packagePath, `{"name":"demo","version":"1.0.0"}`)
	configPath := filepath.Join(root, "publisher.yaml")
	configData := `package:
  path: "${PACKAGE_ROOT}/demo-1.0.0.tgz"
  format: npm
  publish_driver: npm_cli
`
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"publish", "--config", configPath, "--mode=test"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("Run() wrote progress without --verbose: %s", stderr.String())
	}
	var result model.PublishResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Status != model.StatusSuccess || result.RepositoryProvider != "simulation" || result.RepositoryName != "test" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRunVerboseWritesProgressToStderrWithoutChangingJSON(t *testing.T) {
	root := t.TempDir()
	packagePath := filepath.Join(root, "demo-1.0.0.tgz")
	createNPMTarball(t, packagePath, `{"name":"demo","version":"1.0.0"}`)
	configPath := filepath.Join(root, "publisher.yaml")
	configData := fmt.Sprintf("package:\n  path: %q\n  format: npm\n  publish_driver: npm_cli\n", packagePath)
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"publish", "--config", configPath, "--mode=test", "--verbose"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	var result model.PublishResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("verbose output contaminated JSON result: %v; stdout=%s", err, stdout.String())
	}
	for _, expected := range []string{
		"[verbose] loading configuration:",
		"[verbose] inspecting package input:",
		"[verbose] publishing package:",
		"status=SUCCESS",
	} {
		if !strings.Contains(stderr.String(), expected) {
			t.Fatalf("stderr does not contain %q: %s", expected, stderr.String())
		}
	}
}

func TestRunVerboseReportsBatchProgress(t *testing.T) {
	root := t.TempDir()
	packagesPath := filepath.Join(root, "packages")
	if err := os.Mkdir(packagesPath, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 2; index++ {
		path := filepath.Join(packagesPath, fmt.Sprintf("demo-%d.0.0.tgz", index))
		createNPMTarball(t, path, fmt.Sprintf(`{"name":"demo-%d","version":"%d.0.0"}`, index, index))
	}
	configPath := filepath.Join(root, "publisher.yaml")
	configData := fmt.Sprintf("package:\n  path: %q\n  format: npm\n  publish_driver: npm_cli\n  recursive: true\n", packagesPath)
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"publish", "--config", configPath, "--mode=test", "--verbose"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	var report model.BatchPublishReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode batch report: %v", err)
	}
	if report.Total != 2 || report.Succeeded != 2 {
		t.Fatalf("unexpected batch report: %+v", report)
	}
	for _, expected := range []string{"discovered 2 package(s)", "total=2 parallelism=2", "batch finished: status=SUCCESS"} {
		if !strings.Contains(stderr.String(), expected) {
			t.Fatalf("stderr does not contain %q: %s", expected, stderr.String())
		}
	}
}

func TestRunOutputCSVWritesSingleResultToStdout(t *testing.T) {
	root := t.TempDir()
	packagePath := filepath.Join(root, "demo-1.0.0.tgz")
	createNPMTarball(t, packagePath, `{"name":"demo","version":"1.0.0"}`)
	configPath := filepath.Join(root, "publisher.yaml")
	configData := fmt.Sprintf("package:\n  path: %q\n  format: npm\n  publish_driver: npm_cli\n", packagePath)
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"publish", "--config", configPath, "--mode=test", "--output=csv"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	records, err := csv.NewReader(&stdout).ReadAll()
	if err != nil {
		t.Fatalf("decode CSV result: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("CSV record count = %d, want 2: %#v", len(records), records)
	}
	result := csvRecord(records[0], records[1])
	if result["status"] != "SUCCESS" || result["package.name"] != "demo" || result["package.version"] != "1.0.0" {
		t.Fatalf("unexpected CSV result: %#v", result)
	}
}

func TestRunOutputJSONWritesResultFileInsteadOfStdout(t *testing.T) {
	root := t.TempDir()
	packagePath := filepath.Join(root, "demo-1.0.0.tgz")
	createNPMTarball(t, packagePath, `{"name":"demo","version":"1.0.0"}`)
	configPath := filepath.Join(root, "publisher.yaml")
	configData := fmt.Sprintf("package:\n  path: %q\n  format: npm\n  publish_driver: npm_cli\n", packagePath)
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(root, "publish-result.json")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{
		"publish", "--config", configPath, "--mode=test", "--output=json", "--file=" + resultPath,
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("Run() stdout = %q, want empty when --file is set", stdout.String())
	}
	contents, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("read result file: %v", err)
	}
	var result model.PublishResult
	if err := json.Unmarshal(contents, &result); err != nil {
		t.Fatalf("decode result file: %v", err)
	}
	if result.Status != model.StatusSuccess || result.Package.Name != "demo" {
		t.Fatalf("unexpected result file: %+v", result)
	}
}

func TestRunOutputCSVWritesBatchResultFile(t *testing.T) {
	root := t.TempDir()
	packagesPath := filepath.Join(root, "packages")
	if err := os.Mkdir(packagesPath, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 2; index++ {
		path := filepath.Join(packagesPath, fmt.Sprintf("demo-%d.0.0.tgz", index))
		createNPMTarball(t, path, fmt.Sprintf(`{"name":"demo-%d","version":"%d.0.0"}`, index, index))
	}
	configPath := filepath.Join(root, "publisher.yaml")
	configData := fmt.Sprintf("package:\n  path: %q\n  format: npm\n  publish_driver: npm_cli\n  recursive: true\n", packagesPath)
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(root, "publish-result.csv")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{
		"publish", "--config", configPath, "--mode=test", "--output=csv", "--file=" + resultPath,
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("Run() stdout = %q, want empty when --file is set", stdout.String())
	}
	file, err := os.Open(resultPath)
	if err != nil {
		t.Fatalf("open result file: %v", err)
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("decode CSV result file: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("CSV record count = %d, want 3: %#v", len(records), records)
	}
	for _, record := range records[1:] {
		result := csvRecord(records[0], record)
		if result["batch.status"] != "SUCCESS" || result["batch.total"] != "2" || result["status"] != "SUCCESS" {
			t.Fatalf("unexpected batch CSV result: %#v", result)
		}
	}
}

func TestRunProductionModeSupportsJSONAndCSVOutput(t *testing.T) {
	var feedChecks atomic.Int32
	var packageChecks atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		username, password, ok := request.BasicAuth()
		if !ok || username != "AzureDevOps" || password != "production-pat" {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.Contains(request.URL.Path, "/npm/demo/versions/1.0.0"):
			packageChecks.Add(1)
			http.NotFound(response, request)
		case strings.Contains(request.URL.Path, "/_apis/packaging/feeds/feed"):
			feedChecks.Add(1)
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"id":"feed"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	packagePath := filepath.Join(root, "demo-1.0.0.tgz")
	createNPMTarball(t, packagePath, `{"name":"demo","version":"1.0.0"}`)
	configPath := filepath.Join(root, "publisher.yaml")
	configData := fmt.Sprintf(`package:
  path: %q
  format: npm
  publish_driver: npm_cli
repository_profile: production
repositories:
  production:
    provider: ado
    organization: org
    feed: feed
    base_url: %q
    credential_ref: PRODUCTION_PAT
options:
  dry_run: true
`, packagePath, server.URL)
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PRODUCTION_PAT", "production-pat")

	t.Run("JSON stdout", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := Run(context.Background(), []string{
			"publish", "--config", configPath, "--output=json",
		}, &stdout, &stderr)
		if exitCode != 0 {
			t.Fatalf("Run() exit code = %d, stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
		}
		var result model.PublishResult
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("decode production JSON result: %v", err)
		}
		if result.Status != model.StatusSkipped || result.RepositoryProvider != "ado" || result.RepositoryName != "feed" {
			t.Fatalf("unexpected production JSON result: %+v", result)
		}
	})

	t.Run("CSV file", func(t *testing.T) {
		resultPath := filepath.Join(root, "production-result.csv")
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := Run(context.Background(), []string{
			"publish", "--config", configPath, "--output=csv", "--file=" + resultPath,
		}, &stdout, &stderr)
		if exitCode != 0 {
			t.Fatalf("Run() exit code = %d, stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("Run() stdout = %q, want empty when --file is set", stdout.String())
		}
		file, err := os.Open(resultPath)
		if err != nil {
			t.Fatalf("open production CSV result: %v", err)
		}
		defer file.Close()
		records, err := csv.NewReader(file).ReadAll()
		if err != nil {
			t.Fatalf("decode production CSV result: %v", err)
		}
		if len(records) != 2 {
			t.Fatalf("CSV record count = %d, want 2: %#v", len(records), records)
		}
		result := csvRecord(records[0], records[1])
		if result["status"] != "SKIPPED" || result["repositoryProvider"] != "ado" || result["repositoryName"] != "feed" {
			t.Fatalf("unexpected production CSV result: %#v", result)
		}
	})

	if feedChecks.Load() != 2 || packageChecks.Load() != 2 {
		t.Fatalf("formal publish repository calls: feed=%d package=%d, want 2 each", feedChecks.Load(), packageChecks.Load())
	}
}

func TestRunRejectsOutputFileExtensionMismatch(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{
		"publish", "--config", "publisher.yaml", "--output=csv", "--file=result.json",
	}, &stdout, &stderr)
	if exitCode != 2 || !strings.Contains(stderr.String(), "--file must use the .csv extension") {
		t.Fatalf("Run() exit code = %d, stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
}

func TestRunRejectsUnsupportedOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"publish", "--config", "publisher.yaml", "--output=xml"}, &stdout, &stderr)
	if exitCode != 2 || !strings.Contains(stderr.String(), `unsupported --output "xml"`) {
		t.Fatalf("Run() exit code = %d, stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
}

func TestNewResultOutputInfersFormatFromFileExtension(t *testing.T) {
	for _, testCase := range []struct {
		filePath string
		want     outputFormat
	}{
		{filePath: "publish-result.json", want: outputJSON},
		{filePath: "publish-result.csv", want: outputCSV},
	} {
		t.Run(testCase.filePath, func(t *testing.T) {
			output, err := newResultOutput("", testCase.filePath, io.Discard)
			if err != nil {
				t.Fatalf("newResultOutput() error = %v", err)
			}
			if output.format != testCase.want {
				t.Fatalf("newResultOutput() format = %q, want %q", output.format, testCase.want)
			}
		})
	}
}

func TestRunRejectsUnsupportedMode(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"publish", "--config", "publisher.yaml", "--mode=preview"}, &stdout, &stderr)
	if exitCode != 2 || !strings.Contains(stderr.String(), `unsupported --mode "preview"`) {
		t.Fatalf("Run() exit code = %d, stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
}

func TestResolveBatchModeAutomaticallyDetectsMavenRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	versionDirectory := filepath.Join(root, "com", "example", "demo", "1.0.0")
	if err := os.MkdirAll(versionDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDirectory, "demo-1.0.0.pom"), []byte("<project/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, batchMode, err := resolveBatchMode(config.Config{Package: config.PackageConfig{Path: root}})
	if err != nil {
		t.Fatalf("resolveBatchMode() error = %v", err)
	}
	if !batchMode || len(paths) != 1 || paths[0] != versionDirectory {
		t.Fatalf("paths=%#v batchMode=%v", paths, batchMode)
	}
}

func TestResolveBatchModeKeepsSingleVersionDirectoryInSingleMode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "demo-1.0.0.pom"), []byte("<project/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, batchMode, err := resolveBatchMode(config.Config{Package: config.PackageConfig{Path: root}})
	if err != nil {
		t.Fatalf("resolveBatchMode() error = %v", err)
	}
	if batchMode || len(paths) != 0 {
		t.Fatalf("paths=%#v batchMode=%v", paths, batchMode)
	}
}

func TestResolveBatchModeRoutesNPMTarballRootToBatch(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"a/a-1.0.0.tgz", "b/b-2.0.0.tgz"} {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("tgz"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, batchMode, err := resolveBatchMode(config.Config{Package: config.PackageConfig{Path: root, Format: "npm"}})
	if err != nil {
		t.Fatalf("resolveBatchMode() error = %v", err)
	}
	if !batchMode || len(paths) != 2 {
		t.Fatalf("paths=%#v batchMode=%v", paths, batchMode)
	}
}

func createNPMTarball(t *testing.T, path, packageJSON string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	data := []byte(packageJSON)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "package/package.json", Mode: 0o600, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func csvRecord(header, record []string) map[string]string {
	fields := make(map[string]string, len(header))
	for index, name := range header {
		fields[name] = record[index]
	}
	return fields
}
