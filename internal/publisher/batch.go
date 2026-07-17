package publisher

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"packagespublisher/internal/model"
)

type Publisher interface {
	Publish(context.Context, model.PublishRequest) (model.PublishResult, error)
}

type PublisherFactory func() (Publisher, error)

type BatchService struct {
	Factory     PublisherFactory
	Parallelism int
	FailFast    bool
	Now         func() time.Time
}

type BatchError struct{ Failed int }

func (e *BatchError) Error() string { return fmt.Sprintf("%d package promotions failed", e.Failed) }

func DefaultParallelism() int {
	parallelism := runtime.GOMAXPROCS(0)
	if parallelism > 8 {
		return 8
	}
	if parallelism < 1 {
		return 1
	}
	return parallelism
}

// Publish runs a bounded worker pool. Each worker owns one Publisher instance,
// so repository lifecycle and mutable adapter state are not shared by workers.
func (s BatchService) Publish(ctx context.Context, requests []model.PublishRequest) (model.BatchPublishReport, error) {
	now := s.Now
	if now == nil {
		now = time.Now
	}
	report := model.BatchPublishReport{StartedAt: now().UTC(), Total: len(requests), Results: make([]model.PublishResult, len(requests))}
	if len(requests) == 0 {
		report.Status = model.StatusSuccess
		report.FinishedAt = now().UTC()
		return report, nil
	}
	if s.Factory == nil {
		return report, fmt.Errorf("publisher factory is required")
	}
	parallelism := s.Parallelism
	if parallelism <= 0 {
		parallelism = DefaultParallelism()
	}
	if parallelism > len(requests) {
		parallelism = len(requests)
	}
	publishers := make([]Publisher, parallelism)
	for index := range publishers {
		publisher, err := s.Factory()
		if err != nil {
			report.Status = model.StatusFailed
			report.Failed = len(requests)
			report.FinishedAt = now().UTC()
			for resultIndex := range report.Results {
				report.Results[resultIndex] = model.PublishResult{
					Status: model.StatusFailed, InputPath: requests[resultIndex].PackagePath,
					ErrorType: "WORKER_INIT", ErrorMessage: fmt.Sprintf("create publisher worker %d: %v", index, err),
				}
			}
			return report, fmt.Errorf("create publisher worker %d: %w", index, err)
		}
		publishers[index] = publisher
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int)
	var workers sync.WaitGroup
	workers.Add(parallelism)
	for workerIndex := 0; workerIndex < parallelism; workerIndex++ {
		publisher := publishers[workerIndex]
		go func() {
			defer workers.Done()
			for {
				select {
				case <-workerCtx.Done():
					return
				case index, ok := <-jobs:
					if !ok {
						return
					}
					result, err := publisher.Publish(workerCtx, requests[index])
					if result.InputPath == "" {
						result.InputPath = requests[index].PackagePath
					}
					if err != nil && result.Status == "" {
						result.Status = model.StatusFailed
						result.ErrorMessage = err.Error()
					}
					report.Results[index] = result
					if err != nil && s.FailFast {
						cancel()
					}
				}
			}
		}()
	}

sendLoop:
	for index := range requests {
		select {
		case <-workerCtx.Done():
			break sendLoop
		case jobs <- index:
		}
	}
	close(jobs)
	workers.Wait()

	for index := range report.Results {
		result := &report.Results[index]
		if result.Status == "" {
			message := "batch item was not processed"
			if workerCtx.Err() != nil {
				message = workerCtx.Err().Error()
			}
			result.Status = model.StatusFailed
			result.InputPath = requests[index].PackagePath
			result.ErrorType = "CANCELLED"
			result.ErrorMessage = message
		}
		switch result.Status {
		case model.StatusSuccess:
			report.Succeeded++
		case model.StatusSkipped:
			report.Skipped++
		default:
			report.Failed++
		}
	}
	report.FinishedAt = now().UTC()
	if report.Failed > 0 {
		report.Status = model.StatusFailed
		return report, &BatchError{Failed: report.Failed}
	}
	report.Status = model.StatusSuccess
	return report, nil
}
