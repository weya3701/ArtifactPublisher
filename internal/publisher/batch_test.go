package publisher_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"packagespublisher/internal/model"
	"packagespublisher/internal/publisher"
)

type concurrencyTracker struct {
	active int32
	max    int32
}

type batchPublisher struct{ tracker *concurrencyTracker }

func (p batchPublisher) Publish(_ context.Context, request model.PublishRequest) (model.PublishResult, error) {
	active := atomic.AddInt32(&p.tracker.active, 1)
	for {
		maximum := atomic.LoadInt32(&p.tracker.max)
		if active <= maximum || atomic.CompareAndSwapInt32(&p.tracker.max, maximum, active) {
			break
		}
	}
	defer atomic.AddInt32(&p.tracker.active, -1)
	time.Sleep(10 * time.Millisecond)
	result := model.PublishResult{InputPath: request.PackagePath, Status: model.StatusSuccess}
	if request.PackagePath == "package-5" {
		result.Status = model.StatusFailed
		result.ErrorType = "PUBLISH"
		result.ErrorMessage = "simulated failure"
		return result, errors.New("simulated failure")
	}
	return result, nil
}

func TestBatchPublishUsesBoundedParallelismAndPreservesOrder(t *testing.T) {
	tracker := &concurrencyTracker{}
	requests := make([]model.PublishRequest, 12)
	for index := range requests {
		requests[index].PackagePath = fmt.Sprintf("package-%d", index)
	}
	service := publisher.BatchService{
		Parallelism: 3,
		Factory: func() (publisher.Publisher, error) {
			return batchPublisher{tracker: tracker}, nil
		},
	}
	report, err := service.Publish(context.Background(), requests)
	var batchErr *publisher.BatchError
	if !errors.As(err, &batchErr) || batchErr.Failed != 1 {
		t.Fatalf("Publish() error = %v, want one-item BatchError", err)
	}
	if maximum := atomic.LoadInt32(&tracker.max); maximum < 2 || maximum > 3 {
		t.Fatalf("max concurrency = %d, want 2..3", maximum)
	}
	if report.Succeeded != 11 || report.Failed != 1 || len(report.Results) != len(requests) {
		t.Fatalf("unexpected report: %+v", report)
	}
	for index, result := range report.Results {
		if result.InputPath != requests[index].PackagePath {
			t.Fatalf("result %d path = %q, want %q", index, result.InputPath, requests[index].PackagePath)
		}
	}
}
