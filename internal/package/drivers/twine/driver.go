package twine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"packagespublisher/internal/model"
	"packagespublisher/internal/package/driver"
)

type Runner interface {
	Run(context.Context, string, []string, []string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args, environment []string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = append(os.Environ(), environment...)
	return command.CombinedOutput()
}

type Driver struct {
	PythonExecutable string
	Runner           Runner
}

func (d Driver) Publish(ctx context.Context, descriptor model.PackageDescriptor, target driver.Target) error {
	if descriptor.Format != model.FormatPyPI || len(descriptor.Files) == 0 {
		return fmt.Errorf("Twine driver requires at least one PyPI distribution")
	}
	if target.Endpoint == "" || target.Credential == nil || target.Credential.Secret() == "" {
		return fmt.Errorf("Twine publish target is incomplete")
	}
	python := d.PythonExecutable
	if python == "" {
		python = "python3"
	}
	args := []string{"-m", "twine", "upload", "--non-interactive", "--repository-url", target.Endpoint}
	for _, file := range descriptor.Files {
		args = append(args, file.Path)
	}
	runner := d.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	environment := []string{
		"TWINE_USERNAME=" + target.Credential.Username(),
		"TWINE_PASSWORD=" + target.Credential.Secret(),
	}
	output, err := runner.Run(ctx, python, args, environment)
	if err != nil {
		text := strings.ReplaceAll(string(output), target.Credential.Secret(), "[REDACTED]")
		if len(text) > 2048 {
			text = text[len(text)-2048:]
		}
		return fmt.Errorf("Twine upload failed: %w (output: %s)", err, text)
	}
	return nil
}
