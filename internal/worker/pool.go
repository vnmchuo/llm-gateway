package worker

import (
	"context"
)

type WorkerPool struct {
	// TODO: add internal queue and worker configuration
}

func NewWorkerPool() Queue {
	return &WorkerPool{}
}

func (p *WorkerPool) Enqueue(ctx context.Context, job *AsyncJob) error {
	// TODO: implement
	return nil
}

func (p *WorkerPool) Process(ctx context.Context) error {
	// TODO: implement worker loop
	return nil
}
