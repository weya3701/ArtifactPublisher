package npmcli_test

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"packagespublisher/internal/artifact_repository/credential"
	"packagespublisher/internal/model"
	"packagespublisher/internal/package/driver"
	npmcli "packagespublisher/internal/package/drivers/npm_cli"
)

type runner struct {
	args  []string
	npmrc string
}

func (r *runner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.args = append([]string(nil), args...)
	for index, arg := range args {
		if arg == "--userconfig" {
			data, err := os.ReadFile(args[index+1])
			if err != nil {
				return nil, err
			}
			r.npmrc = string(data)
		}
	}
	return nil, nil
}

func TestPublishUsesTemporaryNPMRCWithoutPATArgument(t *testing.T) {
	capture := &runner{}
	const pat = "secret-pat"
	err := (npmcli.Driver{Runner: capture}).Publish(context.Background(), model.PackageDescriptor{
		Format: model.FormatNPM, Files: []model.PackageFile{{Path: "/tmp/demo.tgz"}},
	}, driver.Target{
		Endpoint:   "https://pkgs.dev.azure.com/org/project/_packaging/feed/npm/registry/",
		Credential: credential.PersonalAccessToken{Token: pat},
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if strings.Contains(strings.Join(capture.args, " "), pat) {
		t.Fatal("PAT leaked into npm arguments")
	}
	if !strings.Contains(strings.Join(capture.args, " "), "--provenance=false") {
		t.Fatalf("npm arguments = %q; want provenance disabled", capture.args)
	}
	if !strings.Contains(capture.npmrc, base64.StdEncoding.EncodeToString([]byte(pat))) {
		t.Fatal("encoded PAT missing from temporary npmrc")
	}
}

func TestPublishPassesConfiguredDistTag(t *testing.T) {
	capture := &runner{}
	err := (npmcli.Driver{Runner: capture}).Publish(context.Background(), model.PackageDescriptor{
		Format: model.FormatNPM, Files: []model.PackageFile{{Path: "/tmp/demo.tgz"}},
	}, driver.Target{
		Endpoint:   "https://pkgs.dev.azure.com/org/project/_packaging/feed/npm/registry/",
		Credential: credential.PersonalAccessToken{Token: "secret-pat"},
		NPMTag:     "legacy",
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	arguments := strings.Join(capture.args, " ")
	if !strings.Contains(arguments, "--tag legacy") {
		t.Fatalf("npm arguments = %q; want --tag legacy", arguments)
	}
	if strings.Contains(capture.npmrc, "always-auth") {
		t.Fatalf("deprecated always-auth found in temporary npmrc: %q", capture.npmrc)
	}
}
