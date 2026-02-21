package billing

import (
	"context"
	"time"
)

type UsageLog struct {
	ID           string
	TenantID     string
	RequestID    string
	Provider     string
	Model        string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	LatencyMs    int64
	CreatedAt    time.Time
}

type Store interface {
	LogUsage(ctx context.Context, log *UsageLog) error
	GetUsageByTenant(ctx context.Context, tenantID string, from, to time.Time) ([]*UsageLog, error)
	GetTotalCostByTenant(ctx context.Context, tenantID string, from, to time.Time) (float64, error)
}
