package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"packagespublisher/internal/archive"
	"packagespublisher/internal/bootstrap"
	"packagespublisher/internal/infrastructure/config"
	"packagespublisher/internal/infrastructure/secret"
	"packagespublisher/internal/model"
	"packagespublisher/internal/package/discovery"
	mavenhandler "packagespublisher/internal/package/formats/maven"
	npmhandler "packagespublisher/internal/package/formats/npm"
	pypihandler "packagespublisher/internal/package/formats/pypi"
	"packagespublisher/internal/publisher"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "publish" {
		fmt.Fprintln(stderr, "usage: package-publisher publish --config publisher.yaml")
		return 2
	}
	flags := flag.NewFlagSet("publish", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to publisher YAML configuration")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *configPath == "" {
		fmt.Fprintln(stderr, "--config is required")
		return 2
	}
	loaded, err := config.Load(*configPath)
	if err != nil {
		writeFailure(stdout, "CONFIGURATION", err)
		return 2
	}
	cleanup := func() {}
	if loaded.Package.ArchivePath != "" {
		loaded.Package.Path, cleanup, err = extractArchive(loaded.Package.ArchivePath)
		if err != nil {
			writeFailure(stdout, "PACKAGE", err)
			return 1
		}
		defer cleanup()
	}
	batchPaths, batchMode, err := resolveBatchMode(loaded)
	if err != nil {
		writeFailure(stdout, "PACKAGE", err)
		return 1
	}
	if batchMode {
		return runBatch(ctx, loaded, batchPaths, stdout)
	}
	components, err := bootstrap.Build(loaded, secret.Environment{})
	if err != nil {
		writeFailure(stdout, "CONFIGURATION", err)
		return 2
	}
	result, publishErr := components.Service.Publish(ctx, components.Request)
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(stderr, "encode result: %v\n", err)
		return 1
	}
	if publishErr != nil {
		return 1
	}
	return 0
}

var extractArchive = archive.Extract

func runBatch(ctx context.Context, loaded config.Config, paths []string, stdout io.Writer) int {
	firstComponents, err := bootstrap.Build(loaded, secret.Environment{})
	if err != nil {
		writeFailure(stdout, "CONFIGURATION", err)
		return 2
	}
	options, err := loaded.PublishOptions()
	if err != nil {
		writeFailure(stdout, "CONFIGURATION", err)
		return 2
	}
	requests := make([]model.PublishRequest, len(paths))
	for index, path := range paths {
		requests[index] = model.PublishRequest{PackagePath: path, Options: options, Metadata: loaded.Metadata}
	}
	firstAvailable := true
	batch := publisher.BatchService{
		Parallelism: loaded.Options.Parallelism,
		FailFast:    loaded.Options.FailFast,
		Factory: func() (publisher.Publisher, error) {
			if firstAvailable {
				firstAvailable = false
				service := firstComponents.Service
				return service, nil
			}
			components, err := bootstrap.Build(loaded, secret.Environment{})
			if err != nil {
				return nil, err
			}
			return components.Service, nil
		},
	}
	report, publishErr := batch.Publish(ctx, requests)
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return 1
	}
	if publishErr != nil {
		return 1
	}
	return 0
}

func resolveBatchMode(loaded config.Config) ([]string, bool, error) {
	if loaded.Package.Recursive {
		paths, err := discoverPackages(loaded.Package.Format, loaded.Package.Path)
		return paths, true, err
	}
	info, err := os.Stat(loaded.Package.Path)
	if err != nil || !info.IsDir() {
		return nil, false, nil
	}
	directPackage := false
	if loaded.Package.Format == string(model.FormatNPM) {
		directPackage = (npmhandler.Handler{}).Detect(loaded.Package.Path)
	} else if loaded.Package.Format == string(model.FormatPyPI) {
		directPackage = (pypihandler.Handler{}).Detect(loaded.Package.Path)
	} else {
		directPackage = (mavenhandler.Handler{}).Detect(loaded.Package.Path)
	}
	if directPackage {
		if loaded.Package.Format == string(model.FormatMaven) {
			return nil, false, nil
		}
		paths, err := discoverPackages(loaded.Package.Format, loaded.Package.Path)
		if err == nil && len(paths) > 1 {
			return paths, true, nil
		}
		return nil, false, nil
	}
	paths, err := discoverPackages(loaded.Package.Format, loaded.Package.Path)
	if err != nil {
		return nil, false, err
	}
	root, _ := filepath.Abs(loaded.Package.Path)
	if len(paths) == 1 {
		discovered, _ := filepath.Abs(paths[0])
		if discovered == root {
			return nil, false, nil
		}
	}
	return paths, true, nil
}

func discoverPackages(format, root string) ([]string, error) {
	if format == string(model.FormatNPM) {
		return discovery.NPMPackages(root)
	}
	if format == string(model.FormatPyPI) {
		return discovery.PyPIPackages(root)
	}
	return discovery.MavenPackages(root)
}

func writeFailure(output io.Writer, errorType string, err error) {
	_ = json.NewEncoder(output).Encode(model.PublishResult{
		Status: model.StatusFailed, ErrorType: errorType, ErrorMessage: err.Error(),
	})
}
