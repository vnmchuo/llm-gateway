package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type PostgresStore struct {
	db DB
}

func NewPostgresStore(db DB) Store {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) LogUsage(ctx context.Context, log *UsageLog) error {
	query := `
		INSERT INTO usage_logs (tenant_id, request_id, provider, model, input_tokens, output_tokens, cost_usd, latency_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at
	`
	err := s.db.QueryRow(ctx, query,
		log.TenantID, log.RequestID, log.Provider, log.Model,
		log.InputTokens, log.OutputTokens, log.CostUSD, log.LatencyMs,
	).Scan(&log.ID, &log.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to log usage: %w", err)
	}

	return nil
}

func (s *PostgresStore) GetUsageByTenant(ctx context.Context, tenantID string, from, to time.Time) ([]*UsageLog, error) {
	query := `
		SELECT id, tenant_id, request_id, provider, model, input_tokens, output_tokens, cost_usd, latency_ms, created_at
		FROM usage_logs
		WHERE tenant_id = $1 AND created_at BETWEEN $2 AND $3
		ORDER BY created_at DESC
	`
	rows, err := s.db.Query(ctx, query, tenantID, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to query usage logs: %w", err)
	}
	defer rows.Close()

	var logs []*UsageLog
	for rows.Next() {
		var l UsageLog
		err := rows.Scan(
			&l.ID, &l.TenantID, &l.RequestID, &l.Provider, &l.Model,
			&l.InputTokens, &l.OutputTokens, &l.CostUSD, &l.LatencyMs, &l.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan usage log: %w", err)
		}
		logs = append(logs, &l)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating usage logs: %w", err)
	}

	return logs, nil
}

func (s *PostgresStore) GetTotalCostByTenant(ctx context.Context, tenantID string, from, to time.Time) (float64, error) {
	query := `
		SELECT COALESCE(SUM(cost_usd), 0)
		FROM usage_logs
		WHERE tenant_id = $1 AND created_at BETWEEN $2 AND $3
	`
	var total float64
	err := s.db.QueryRow(ctx, query, tenantID, from, to).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to get total cost: %w", err)
	}

	return total, nil
}
