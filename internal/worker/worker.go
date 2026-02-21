package worker

import (
	"context"
	"time"

	"github.com/vnmchuo/llm-gateway/internal/provider"
)

type JobStatus string

const (
	JobStatusPending JobStatus = "pending"
	JobStatusRunning JobStatus = "running"
	JobStatusDone    JobStatus = "done"
	JobStatusFailed  JobStatus = "failed"
)

type AsyncJob struct {
	ID          string
	TenantID    string
	Request     *provider.Request
	CallbackURL string
	Status      JobStatus
	CreatedAt   time.Time
}

type Queue interface {
	Enqueue(ctx context.Context, job *AsyncJob) error
	Process(ctx context.Context) error // starts the worker loop
}
