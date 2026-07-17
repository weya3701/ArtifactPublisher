package mavencli

import (
	"context"
	"encoding/xml"
	"fmt"
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
	if descriptor.Format != model.FormatMaven {
		return fmt.Errorf("Maven CLI driver cannot publish format %q", descriptor.Format)
	}
	if target.Endpoint == "" || target.RepositoryID == "" || target.Credential == nil || target.Credential.Secret() == "" {
		return fmt.Errorf("Maven publish target is incomplete")
	}
	pom, main, sources, javadoc := packageFiles(descriptor)
	if pom == "" || (descriptor.Packaging != "pom" && main == "") {
		return fmt.Errorf("Maven package requires POM and main artifact")
	}

	settingsPath, cleanup, err := writeSettings(target)
	if err != nil {
		return err
	}
	defer cleanup()

	executable := d.Executable
	if executable == "" {
		executable = "mvn"
	}
	runner := d.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	args := []string{"--batch-mode", "--no-transfer-progress", "--settings", settingsPath, "deploy:deploy-file"}
	if descriptor.Packaging == "pom" {
		main = pom
	}
	args = append(args,
		"-Dfile="+main,
		"-DpomFile="+pom,
		"-DrepositoryId="+target.RepositoryID,
		"-Durl="+target.Endpoint,
		"-DgeneratePom=false",
	)
	if sources != "" {
		args = append(args, "-Dsources="+sources)
	}
	if javadoc != "" {
		args = append(args, "-Djavadoc="+javadoc)
	}
	output, err := runner.Run(ctx, executable, args...)
	if err != nil {
		return fmt.Errorf("Maven publish failed: %w (output: %s)", err, sanitizedOutput(output, target.Credential.Secret()))
	}
	return nil
}

func packageFiles(descriptor model.PackageDescriptor) (pom, main, sources, javadoc string) {
	for _, file := range descriptor.Files {
		switch file.Classifier {
		case "sources":
			sources = file.Path
		case "javadoc":
			javadoc = file.Path
		default:
			if file.Extension == "pom" {
				pom = file.Path
			} else if file.Extension == descriptor.Packaging {
				main = file.Path
			}
		}
	}
	return
}

func writeSettings(target driver.Target) (string, func(), error) {
	directory, err := os.MkdirTemp("", "package-publisher-maven-")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temporary Maven settings: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	type server struct {
		ID       string `xml:"id"`
		Username string `xml:"username"`
		Password string `xml:"password"`
	}
	settings := struct {
		XMLName xml.Name `xml:"settings"`
		Xmlns   string   `xml:"xmlns,attr"`
		Servers []server `xml:"servers>server"`
	}{
		Xmlns: "http://maven.apache.org/SETTINGS/1.0.0",
		Servers: []server{{
			ID: target.RepositoryID, Username: target.Credential.Username(), Password: target.Credential.Secret(),
		}},
	}
	data, err := xml.Marshal(settings)
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("encode temporary Maven settings: %w", err)
	}
	path := filepath.Join(directory, "settings.xml")
	if err := os.WriteFile(path, append([]byte(xml.Header), data...), 0o600); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("write temporary Maven settings: %w", err)
	}
	return path, cleanup, nil
}

func sanitizedOutput(output []byte, secret string) string {
	const limit = 2048
	if len(output) > limit {
		output = output[len(output)-limit:]
	}
	return strings.ReplaceAll(string(output), secret, "[REDACTED]")
}
