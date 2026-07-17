package mavencli_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"packagespublisher/internal/artifact_repository/credential"
	"packagespublisher/internal/model"
	"packagespublisher/internal/package/driver"
	mavencli "packagespublisher/internal/package/drivers/maven_cli"
)

type captureRunner struct {
	args            []string
	settingsContent string
}

func (r *captureRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.args = append([]string(nil), args...)
	for index, arg := range args {
		if arg == "--settings" && index+1 < len(args) {
			data, err := os.ReadFile(args[index+1])
			if err != nil {
				return nil, err
			}
			r.settingsContent = string(data)
		}
	}
	return []byte("success"), nil
}

func TestPublishUsesTemporarySettingsAndNoSecretArgument(t *testing.T) {
	runner := &captureRunner{}
	driverImpl := mavencli.Driver{Runner: runner}
	descriptor := model.PackageDescriptor{
		Format: model.FormatMaven, Packaging: "jar",
		Files: []model.PackageFile{
			{Path: "/tmp/demo.pom", Extension: "pom"},
			{Path: "/tmp/demo.jar", Extension: "jar"},
		},
	}
	const secret = "never-log-this-pat"
	err := driverImpl.Publish(context.Background(), descriptor, driver.Target{
		RepositoryID: "feed", Endpoint: "https://example.test/maven/v1",
		Credential: credential.PersonalAccessToken{Token: secret},
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if !strings.Contains(runner.settingsContent, secret) {
		t.Fatal("temporary Maven settings did not contain credential")
	}
	if strings.Contains(strings.Join(runner.args, " "), secret) {
		t.Fatal("PAT leaked into process arguments")
	}
}
