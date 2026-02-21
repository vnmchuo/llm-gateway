package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	extratelimit "github.com/vnmchuo/ratelimiter"
)

// Limiter is a thin wrapper around github.com/vnmchuo/ratelimiter
type Limiter struct {
	store extratelimit.Limiter
}

func NewLimiter(rdb *redis.Client, defaultTPM int64) *Limiter {
	store := extratelimit.NewRedisStore(rdb,
		extratelimit.WithLimit(int(defaultTPM)),
		extratelimit.WithWindow(time.Minute),
	)
	return &Limiter{store: store}
}

func NewTestLimiter(store extratelimit.Limiter) *Limiter {
	return &Limiter{store: store}
}

func (l *Limiter) Allow(ctx context.Context, tenantID string, tokens int) (bool, error) {
	key := fmt.Sprintf("ratelimit:tenant:%s", tenantID)
	res, err := l.store.AllowN(ctx, key, tokens)
	if err != nil {
		return false, err
	}
	return res.Allowed, nil
}

func (l *Limiter) Status(ctx context.Context, tenantID string) (*extratelimit.Result, error) {
	key := fmt.Sprintf("ratelimit:tenant:%s", tenantID)
	return l.store.Status(ctx, key)
}
