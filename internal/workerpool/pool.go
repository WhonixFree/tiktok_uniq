package workerpool

import (
	"context"
	"sync"
)

type Handler func(context.Context, Job) Job

func Run(ctx context.Context, workers int, jobs <-chan Job, results chan<- Job, handler Handler) {
	if workers < 1 {
		workers = 1
	}
	if handler == nil {
		handler = DefaultHandler
	}

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}
					job.Status = StatusProcessing
					result := handler(ctx, job)
					results <- result
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()
}

func DefaultHandler(ctx context.Context, job Job) Job {
	if err := ctx.Err(); err != nil {
		job.Status = StatusSkipped
		job.Error = err
		return job
	}
	job.Status = StatusSuccess
	return job
}
