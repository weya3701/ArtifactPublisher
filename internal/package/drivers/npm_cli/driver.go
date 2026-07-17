package npmcli

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"packagespublisher/internal/model"
	"packagespublisher/internal/package/driver"
)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type Driver struct {
	Executable string
	Runner     Runner
}

func (d Driver) Publish(ctx context.Context, descriptor model.PackageDescriptor, target driver.Target) error {
	if descriptor.Format != model.FormatNPM || len(descriptor.Files) != 1 {
		return fmt.Errorf("npm CLI driver requires one npm tarball")
	}
	if target.Endpoint == "" || target.Credential == nil || target.Credential.Secret() == "" {
		return fmt.Errorf("npm publish target is incomplete")
	}
	npmrc, cleanup, err := writeNPMRC(target)
	if err != nil {
		return err
	}
	defer cleanup()
	executable := d.Executable
	if executable == "" {
		executable = "npm"
	}
	runner := d.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	args := []string{"publish", descriptor.Files[0].Path, "--registry", target.Endpoint, "--userconfig", npmrc, "--ignore-scripts"}
	output, err := runner.Run(ctx, executable, args...)
	if err != nil {
		text := strings.ReplaceAll(string(output), target.Credential.Secret(), "[REDACTED]")
		if len(text) > 2048 {
			text = text[len(text)-2048:]
		}
		return fmt.Errorf("npm publish failed: %w (output: %s)", err, text)
	}
	return nil
}

func writeNPMRC(target driver.Target) (string, func(), error) {
	parsed, err := url.Parse(target.Endpoint)
	if err != nil || parsed.Host == "" {
		return "", func() {}, fmt.Errorf("invalid npm registry endpoint")
	}
	directory, err := os.MkdirTemp("", "package-publisher-npm-")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temporary npm config: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	endpoint := strings.TrimRight(target.Endpoint, "/") + "/"
	authKey := "//" + parsed.Host + strings.TrimRight(parsed.Path, "/") + "/"
	content := fmt.Sprintf("registry=%s\nalways-auth=true\n%s:username=%s\n%s:_password=%s\n%s:email=npm@localhost\n",
		endpoint, authKey, target.Credential.Username(), authKey,
		base64.StdEncoding.EncodeToString([]byte(target.Credential.Secret())), authKey)
	path := filepath.Join(directory, ".npmrc")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("write temporary npm config: %w", err)
	}
	return path, cleanup, nil
}
