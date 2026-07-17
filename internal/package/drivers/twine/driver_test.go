package twine_test

import (
	"context"
	"strings"
	"testing"

	"packagespublisher/internal/artifact_repository/credential"
	"packagespublisher/internal/model"
	"packagespublisher/internal/package/driver"
	twine "packagespublisher/internal/package/drivers/twine"
)

type runner struct {
	name        string
	args        []string
	environment []string
}

func (r *runner) Run(_ context.Context, name string, args, environment []string) ([]byte, error) {
	r.name = name
	r.args = append([]string(nil), args...)
	r.environment = append([]string(nil), environment...)
	return nil, nil
}

func TestPublishUsesTwineEnvironmentWithoutPATArgument(t *testing.T) {
	capture := &runner{}
	const pat = "secret-pat"
	err := (twine.Driver{PythonExecutable: "python", Runner: capture}).Publish(context.Background(), model.PackageDescriptor{
		Format: model.FormatPyPI,
		Files:  []model.PackageFile{{Path: "/tmp/demo.whl"}, {Path: "/tmp/demo.tar.gz"}},
	}, driver.Target{
		Endpoint:   "https://pkgs.dev.azure.com/org/project/_packaging/feed/pypi/upload/",
		Credential: credential.PersonalAccessToken{Token: pat},
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if capture.name != "python" || !strings.Contains(strings.Join(capture.args, " "), "/tmp/demo.whl /tmp/demo.tar.gz") {
		t.Fatalf("command = %q %#v", capture.name, capture.args)
	}
	if strings.Contains(strings.Join(capture.args, " "), pat) {
		t.Fatal("PAT leaked into Twine arguments")
	}
	if !strings.Contains(strings.Join(capture.environment, "\n"), "TWINE_PASSWORD="+pat) {
		t.Fatal("PAT missing from Twine environment")
	}
}
