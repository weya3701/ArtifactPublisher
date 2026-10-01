package cli

import (
	"context"
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
		fmt.Fprintln(stderr, "usage: package-publisher publish --config publisher.yaml [--mode=test] [--output=json|csv] [--file=result.json|result.csv] [--pomonly] [--verbose]")
		return 2
	}
	flags := flag.NewFlagSet("publish", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to publisher YAML configuration")
	modeValue := flags.String("mode", "", "publish mode (test for offline simulation)")
	outputValue := flags.String("output", "", "result output format (json or csv; default json)")
	filePath := flags.String("file", "", "write the result to a .json or .csv file instead of stdout")
	pomOnly := flags.Bool("pomonly", false, "publish only the POM for Maven packages")
	verbose := flags.Bool("verbose", false, "show publish progress on stderr")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	resultDestination, err := newResultOutput(*outputValue, *filePath, stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *configPath == "" {
		fmt.Fprintln(stderr, "--config is required")
		return 2
	}
	mode := config.PublishMode(*modeValue)
	if mode != config.PublishModeDefault && mode != config.PublishModeTest {
		fmt.Fprintf(stderr, "unsupported --mode %q; supported mode is test\n", *modeValue)
		return 2
	}
	progress := newProgressReporter(*verbose, stderr)
	progress.Printf("loading configuration: %s", *configPath)
	loaded, err := config.LoadForMode(*configPath, mode)
	if err != nil {
		progress.Printf("configuration failed: %v", err)
		writeFailure(resultDestination, stderr, "CONFIGURATION", err)
		return 2
	}
	if *pomOnly && loaded.Package.Format != string(model.FormatMaven) {
		err := fmt.Errorf("--pomonly is supported for Maven packages only")
		progress.Printf("configuration failed: %v", err)
		writeFailure(resultDestination, stderr, "CONFIGURATION", err)
		return 2
	}
	loaded.Package.POMOnly = *pomOnly
	progress.Printf("configuration loaded: format=%s mode=%s", loaded.Package.Format, displayMode(mode))
	cleanup := func() {}
	if loaded.Package.ArchivePath != "" {
		progress.Printf("extracting archive: %s", loaded.Package.ArchivePath)
		loaded.Package.Path, cleanup, err = extractArchive(loaded.Package.ArchivePath, loaded.Package.Exclude)
		if err != nil {
			progress.Printf("archive extraction failed: %v", err)
			writeFailure(resultDestination, stderr, "PACKAGE", err)
			return 1
		}
		defer cleanup()
		progress.Printf("archive extracted: %s", loaded.Package.Path)
	}
	progress.Printf("inspecting package input: %s", loaded.Package.Path)
	batchPaths, batchMode, err := resolveBatchMode(loaded)
	if err != nil {
		progress.Printf("package discovery failed: %v", err)
		writeFailure(resultDestination, stderr, "PACKAGE", err)
		return 1
	}
	if batchMode {
		progress.Printf("discovered %d package(s); using batch publish", len(batchPaths))
		return runBatch(ctx, loaded, batchPaths, mode, resultDestination, stderr, progress)
	}
	progress.Printf("using single-package publish")
	components, err := bootstrap.BuildForMode(loaded, secret.Environment{}, mode)
	if err != nil {
		progress.Printf("publisher initialization failed: %v", err)
		writeFailure(resultDestination, stderr, "CONFIGURATION", err)
		return 2
	}
	result, publishErr := withProgress(components.Service, progress).Publish(ctx, components.Request)
	if err := resultDestination.Write(result); err != nil {
		fmt.Fprintf(stderr, "write result: %v\n", err)
		return 1
	}
	if publishErr != nil {
		return 1
	}
	return 0
}

var extractArchive = archive.ExtractWithExclusions

func runBatch(ctx context.Context, loaded config.Config, paths []string, mode config.PublishMode, output resultOutput, stderr io.Writer, progress *progressReporter) int {
	parallelism := loaded.Options.Parallelism
	if parallelism <= 0 {
		parallelism = publisher.DefaultParallelism()
	}
	if parallelism > len(paths) {
		parallelism = len(paths)
	}
	progress.Printf("initializing batch publisher: total=%d parallelism=%d", len(paths), parallelism)
	firstComponents, err := bootstrap.BuildForMode(loaded, secret.Environment{}, mode)
	if err != nil {
		progress.Printf("batch publisher initialization failed: %v", err)
		writeFailure(output, stderr, "CONFIGURATION", err)
		return 2
	}
	options, err := loaded.PublishOptions()
	if err != nil {
		progress.Printf("publish options are invalid: %v", err)
		writeFailure(output, stderr, "CONFIGURATION", err)
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
				return withProgress(service, progress), nil
			}
			components, err := bootstrap.BuildForMode(loaded, secret.Environment{}, mode)
			if err != nil {
				return nil, err
			}
			return withProgress(components.Service, progress), nil
		},
	}
	report, publishErr := batch.Publish(ctx, requests)
	progress.Printf("batch finished: status=%s succeeded=%d skipped=%d failed=%d", report.Status, report.Succeeded, report.Skipped, report.Failed)
	if err := output.Write(report); err != nil {
		fmt.Fprintf(stderr, "write result: %v\n", err)
		return 1
	}
	if publishErr != nil {
		return 1
	}
	return 0
}

func resolveBatchMode(loaded config.Config) ([]string, bool, error) {
	if loaded.Package.Recursive {
		paths, err := discoverPackages(loaded.Package.Format, loaded.Package.Path, loaded.Package.Exclude)
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
		paths, err := discoverPackages(loaded.Package.Format, loaded.Package.Path, loaded.Package.Exclude)
		if err == nil && len(paths) > 1 {
			return paths, true, nil
		}
		return nil, false, nil
	}
	paths, err := discoverPackages(loaded.Package.Format, loaded.Package.Path, loaded.Package.Exclude)
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

func discoverPackages(format, root string, excludedDirectories []string) ([]string, error) {
	if format == string(model.FormatNPM) {
		return discovery.NPMPackages(root, excludedDirectories...)
	}
	if format == string(model.FormatPyPI) {
		return discovery.PyPIPackages(root, excludedDirectories...)
	}
	return discovery.MavenPackages(root, excludedDirectories...)
}

func writeFailure(output resultOutput, stderr io.Writer, errorType string, err error) {
	if writeErr := output.Write(model.PublishResult{
		Status: model.StatusFailed, ErrorType: errorType, ErrorMessage: err.Error(),
	}); writeErr != nil {
		fmt.Fprintf(stderr, "write result: %v\n", writeErr)
	}
}
