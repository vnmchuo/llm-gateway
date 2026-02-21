package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var ErrKeyNotFound = errors.New("api key not found")

type APIKey struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	KeyHash   string    `json:"key_hash"`
	RateLimit int64     `json:"rate_limit"` // max tokens per minute
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// MarshalBinary implements encoding.BinaryMarshaler for Redis
func (a *APIKey) MarshalBinary() ([]byte, error) {
	return json.Marshal(a)
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler for Redis
func (a *APIKey) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, a)
}

type Store interface {
	GetByKey(ctx context.Context, key string) (*APIKey, error)
	Create(ctx context.Context, apiKey *APIKey) error
	Revoke(ctx context.Context, keyID string) error
}

type Middleware func(next http.Handler) http.Handler

type contextKey string

const (
	tenantIDKey  contextKey = "tenant_id"
	apiKeyIDKey  contextKey = "api_key_id"
	requestIDKey contextKey = "request_id"
)

func NewMiddleware(store Store, cache *redis.Client) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Generate RequestID
			requestID := uuid.New().String()
			ctx = context.WithValue(ctx, requestIDKey, requestID)
			w.Header().Set("X-Request-ID", requestID)

			// Extract Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, "Unauthorized: missing or invalid Authorization header", http.StatusUnauthorized)
				return
			}
			key := strings.TrimPrefix(authHeader, "Bearer ")

			// Hash key for Redis lookup
			h := sha256.New()
			h.Write([]byte(key))
			keyHash := hex.EncodeToString(h.Sum(nil))
			redisKey := fmt.Sprintf("auth:%s", keyHash)

			var apiKey APIKey
			err := cache.Get(ctx, redisKey).Scan(&apiKey)
			if err == nil {
				// Cache hit
				ctx = context.WithValue(ctx, tenantIDKey, apiKey.TenantID)
				ctx = context.WithValue(ctx, apiKeyIDKey, apiKey.ID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			} else if err != redis.Nil {
				log.Printf("auth: redis error: %v", err)
			}

			// Cache miss or error: lookup in store
			apiK, err := store.GetByKey(ctx, key)
			if err != nil {
				if errors.Is(err, ErrKeyNotFound) {
					http.Error(w, "Unauthorized: invalid API key", http.StatusUnauthorized)
					return
				}
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			// Cache the result for 5 minutes
			_ = cache.Set(ctx, redisKey, apiK, 5*time.Minute).Err()

			ctx = context.WithValue(ctx, tenantIDKey, apiK.TenantID)
			ctx = context.WithValue(ctx, apiKeyIDKey, apiK.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Helpers to extract from context
func GetTenantID(ctx context.Context) string {
	if id, ok := ctx.Value(tenantIDKey).(string); ok {
		return id
	}
	return ""
}

func GetAPIKeyID(ctx context.Context) string {
	if id, ok := ctx.Value(apiKeyIDKey).(string); ok {
		return id
	}
	return ""
}

func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// Helpers for testing
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantIDKey, tenantID)
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

func WithAPIKeyID(ctx context.Context, apiKeyID string) context.Context {
	return context.WithValue(ctx, apiKeyIDKey, apiKeyID)
}
