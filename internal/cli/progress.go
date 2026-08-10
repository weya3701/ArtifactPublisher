package cli

import (
	"context"
	"fmt"
	"io"
	"sync"

	"packagespublisher/internal/infrastructure/config"
	"packagespublisher/internal/model"
	"packagespublisher/internal/publisher"
)

type progressReporter struct {
	enabled bool
	output  io.Writer
	mu      sync.Mutex
}

func newProgressReporter(enabled bool, output io.Writer) *progressReporter {
	return &progressReporter{enabled: enabled, output: output}
}

func (r *progressReporter) Printf(format string, arguments ...any) {
	if r == nil || !r.enabled {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = fmt.Fprintf(r.output, "[verbose] "+format+"\n", arguments...)
}

func displayMode(mode config.PublishMode) string {
	if mode == config.PublishModeTest {
		return string(mode)
	}
	return "publish"
}

type reportingPublisher struct {
	inner    publisher.Publisher
	progress *progressReporter
}

func withProgress(inner publisher.Publisher, progress *progressReporter) publisher.Publisher {
	if progress == nil || !progress.enabled {
		return inner
	}
	return reportingPublisher{inner: inner, progress: progress}
}

func (p reportingPublisher) Publish(ctx context.Context, request model.PublishRequest) (model.PublishResult, error) {
	p.progress.Printf("publishing package: %s", request.PackagePath)
	result, err := p.inner.Publish(ctx, request)
	if err != nil {
		p.progress.Printf("package finished: %s status=%s error_type=%s", request.PackagePath, result.Status, result.ErrorType)
		return result, err
	}
	p.progress.Printf("package finished: %s status=%s", request.PackagePath, result.Status)
	return result, nil
}
