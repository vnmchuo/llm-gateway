package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	// Server
	Port string // default: 8080

	// Database
	PostgresDSN string

	// Cache
	RedisAddr string

	// Providers
	OpenAIAPIKey    string
	GeminiAPIKey    string
	AnthropicAPIKey string

	// Observability
	OTELExporterType     string // "stdout" or "otlp"
	OTELExporterEndpoint string // default: "localhost:4317"

	// Rate Limiting
	DefaultRateLimitTPM int64 // tokens per minute, default: 100000
}

func Load() (*Config, error) {
	// Load .env file if present (non-fatal if missing)
	_ = godotenv.Load()

	cfg := &Config{
		Port:                 getEnv("PORT", "8080"),
		PostgresDSN:          os.Getenv("POSTGRES_DSN"),
		RedisAddr:            os.Getenv("REDIS_ADDR"),
		OpenAIAPIKey:         os.Getenv("OPENAI_API_KEY"),
		GeminiAPIKey:         os.Getenv("GEMINI_API_KEY"),
		AnthropicAPIKey:      os.Getenv("ANTHROPIC_API_KEY"),
		OTELExporterType:     getEnv("OTEL_EXPORTER_TYPE", "stdout"),
		OTELExporterEndpoint: getEnv("OTEL_EXPORTER_ENDPOINT", "localhost:4317"),
	}

	// Rate Limiting Default
	tpmStr := getEnv("DEFAULT_RATE_LIMIT_TPM", "100000")
	tpm, err := strconv.ParseInt(tpmStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid DEFAULT_RATE_LIMIT_TPM: %w", err)
	}
	cfg.DefaultRateLimitTPM = tpm

	// Validation
	if cfg.PostgresDSN == "" {
		return nil, fmt.Errorf("POSTGRES_DSN is required")
	}
	if cfg.RedisAddr == "" {
		return nil, fmt.Errorf("REDIS_ADDR is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
