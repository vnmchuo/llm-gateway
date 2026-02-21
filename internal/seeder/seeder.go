package seeder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"

	"github.com/vnmchuo/llm-gateway/internal/auth"
)

const (
	TestAPIKey   = "test-api-key-12345"
	TestTenantID = "00000000-0000-0000-0000-000000000001"
)

func SeedTestAPIKey(ctx context.Context, store auth.Store) {
	h := sha256.New()
	h.Write([]byte(TestAPIKey))
	keyHash := hex.EncodeToString(h.Sum(nil))

	apiKey := &auth.APIKey{
		TenantID:  TestTenantID,
		KeyHash:   keyHash,
		RateLimit: 1000000,
		Active:    true,
	}

	err := store.Create(ctx, apiKey)
	if err != nil {
		log.Printf("[Seeder] API key may already exist, skipping: %v", err)
		return
	}
	log.Printf("[Seeder] Test API key created successfully")
	log.Printf("[Seeder] Key: %s", TestAPIKey)
	log.Printf("[Seeder] TenantID: %s", TestTenantID)
}
