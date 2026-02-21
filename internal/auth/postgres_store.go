package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type PostgresStore struct {
	db DB
}

func NewPostgresStore(db DB) Store {
	return &PostgresStore{db: db}
}

func hashKey(key string) string {
	h := sha256.New()
	h.Write([]byte(key))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *PostgresStore) GetByKey(ctx context.Context, key string) (*APIKey, error) {
	keyHash := hashKey(key)
	query := `
		SELECT id, tenant_id, key_hash, rate_limit, active, created_at
		FROM api_keys
		WHERE key_hash = $1 AND active = true
	`

	var k APIKey
	err := s.db.QueryRow(ctx, query, keyHash).Scan(
		&k.ID, &k.TenantID, &k.KeyHash, &k.RateLimit, &k.Active, &k.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrKeyNotFound
		}
		return nil, fmt.Errorf("failed to get api key: %w", err)
	}

	return &k, nil
}

func (s *PostgresStore) Create(ctx context.Context, apiKey *APIKey) error {
	if apiKey.KeyHash == "" {
		return fmt.Errorf("key_hash is required")
	}

	query := `
		INSERT INTO api_keys (tenant_id, key_hash, rate_limit, active)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at
	`

	err := s.db.QueryRow(ctx, query,
		apiKey.TenantID, apiKey.KeyHash, apiKey.RateLimit, apiKey.Active,
	).Scan(&apiKey.ID, &apiKey.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to create api key: %w", err)
	}

	return nil
}

func (s *PostgresStore) Revoke(ctx context.Context, keyID string) error {
	query := `UPDATE api_keys SET active = false WHERE id = $1`
	tag, err := s.db.Exec(ctx, query, keyID)
	if err != nil {
		return fmt.Errorf("failed to revoke api key: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrKeyNotFound
	}

	return nil
}
