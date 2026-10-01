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

func TestPublishPOMOnlyUsesPOMAsDeployFile(t *testing.T) {
	runner := &captureRunner{}
	driverImpl := mavencli.Driver{Runner: runner}
	descriptor := model.PackageDescriptor{
		Format: model.FormatMaven, Packaging: "jar", POMOnly: true,
		Files: []model.PackageFile{{Path: "/tmp/demo.pom", Extension: "pom"}},
	}
	err := driverImpl.Publish(context.Background(), descriptor, driver.Target{
		RepositoryID: "feed", Endpoint: "https://example.test/maven/v1",
		Credential: credential.PersonalAccessToken{Token: "pat"},
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	arguments := strings.Join(runner.args, " ")
	if !strings.Contains(arguments, "-Dfile=/tmp/demo.pom") ||
		!strings.Contains(arguments, "-DpomFile=/tmp/demo.pom") ||
		!strings.Contains(arguments, "-Dpackaging=pom") {
		t.Fatalf("POM-only Maven arguments = %q", arguments)
	}
}

func TestPublishPOMOnlyDoesNotSkipAvailableArtifacts(t *testing.T) {
	runner := &captureRunner{}
	driverImpl := mavencli.Driver{Runner: runner}
	descriptor := model.PackageDescriptor{
		Format: model.FormatMaven, Packaging: "jar", POMOnly: true,
		Files: []model.PackageFile{
			{Path: "/tmp/demo.pom", Extension: "pom"},
			{Path: "/tmp/demo.jar", Extension: "jar"},
			{Path: "/tmp/demo-sources.jar", Extension: "jar", Classifier: "sources"},
			{Path: "/tmp/demo.module", Extension: "module"},
			{Path: "/tmp/demo-tests.jar", Extension: "jar", Classifier: "tests"},
		},
	}
	err := driverImpl.Publish(context.Background(), descriptor, driver.Target{
		RepositoryID: "feed", Endpoint: "https://example.test/maven/v1",
		Credential: credential.PersonalAccessToken{Token: "pat"},
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	arguments := strings.Join(runner.args, " ")
	for _, expected := range []string{
		"-Dfile=/tmp/demo.jar",
		"-Dsources=/tmp/demo-sources.jar",
		"-Dfiles=/tmp/demo.module,/tmp/demo-tests.jar",
		"-Dtypes=module,jar",
		"-Dclassifiers=,tests",
	} {
		if !strings.Contains(arguments, expected) {
			t.Fatalf("Maven arguments %q do not contain %q", arguments, expected)
		}
	}
	if strings.Contains(arguments, "-Dpackaging=pom") {
		t.Fatalf("available main artifact was replaced by POM: %q", arguments)
	}
}
