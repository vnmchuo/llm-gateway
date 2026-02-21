package main

import (
    "context"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/go-chi/chi/v5"
    chimiddleware "github.com/go-chi/chi/v5/middleware"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/redis/go-redis/v9"
    "go.opentelemetry.io/otel"

    "github.com/vnmchuo/llm-gateway/config"
    "github.com/vnmchuo/llm-gateway/internal/auth"
    "github.com/vnmchuo/llm-gateway/internal/billing"
    "github.com/vnmchuo/llm-gateway/internal/provider"
    "github.com/vnmchuo/llm-gateway/internal/provider/claude"
    "github.com/vnmchuo/llm-gateway/internal/provider/gemini"
    "github.com/vnmchuo/llm-gateway/internal/provider/openai"
    "github.com/vnmchuo/llm-gateway/internal/proxy"
    "github.com/vnmchuo/llm-gateway/internal/seeder"
    "github.com/vnmchuo/llm-gateway/internal/telemetry"
    "github.com/vnmchuo/llm-gateway/pkg/ratelimit"
)

func main() {
    // 1. Load config
    cfg, err := config.Load()
    if err != nil {
        log.Fatalf("failed to load config: %v", err)
    }

    // 2. Init telemetry
    shutdownTracer, err := telemetry.InitTracer("llm-gateway", cfg)
    if err != nil {
        log.Fatalf("failed to init tracer: %v", err)
    }
    defer shutdownTracer()

    // 3. Connect PostgreSQL
    ctx := context.Background()
    pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
    if err != nil {
        log.Fatalf("failed to connect postgres: %v", err)
    }
    defer pool.Close()

    if err := pool.Ping(ctx); err != nil {
        log.Fatalf("failed to ping postgres: %v", err)
    }
    log.Println("PostgreSQL connected")

    // 4. Connect Redis
    rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
    defer rdb.Close()

    if err := rdb.Ping(ctx).Err(); err != nil {
        log.Fatalf("failed to ping redis: %v", err)
    }
    log.Println("Redis connected")

    // 5. Init auth
    authStore := auth.NewPostgresStore(pool)
    authMiddleware := auth.NewMiddleware(authStore, rdb)

    // 6. Init billing
    billingStore := billing.NewPostgresStore(pool)

    // 7. Init rate limiter
    limiter := ratelimit.NewLimiter(rdb, cfg.DefaultRateLimitTPM)

    // 8. Init providers
    providers := []provider.Provider{
        gemini.New(cfg.GeminiAPIKey),
        openai.New(cfg.OpenAIAPIKey),
        claude.New(cfg.AnthropicAPIKey),
    }

    // 9. Init router
    router := proxy.NewRouter(providers)

    // 10. Init handler
    tracer := otel.GetTracerProvider().Tracer("llm-gateway")
    handler := proxy.NewHandler(router, billingStore, limiter, tracer)

    // 11. Seed test API key if RUN_SEED=true
    if os.Getenv("RUN_SEED") == "true" {
        seeder.SeedTestAPIKey(ctx, authStore)
    }

    // 12. Init Chi router
    r := chi.NewRouter()
    r.Use(chimiddleware.RequestID)
    r.Use(chimiddleware.Logger)
    r.Use(chimiddleware.Recoverer)

    // Public routes
    r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte(`{"status":"ok","service":"llm-gateway"}`))
    })

    // Protected routes
    r.Group(func(r chi.Router) {
        r.Use(authMiddleware)
        r.Post("/v1/chat/completions", handler.HandleComplete)
        r.Post("/v1/chat/completions/stream", handler.HandleCompleteStream)
        r.Get("/v1/usage", handler.HandleUsage)
    })

    // Async job routes — Phase 2 placeholder
    r.Post("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusNotImplemented)
        _, _ = w.Write([]byte(`{"error":"async jobs coming in phase 2"}`))
    })
    r.Get("/v1/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusNotImplemented)
        _, _ = w.Write([]byte(`{"error":"async jobs coming in phase 2"}`))
    })

    // 13. Graceful shutdown
    srv := &http.Server{
        Addr:         ":" + cfg.Port,
        Handler:      r,
        ReadTimeout:  30 * time.Second,
        WriteTimeout: 90 * time.Second,
        IdleTimeout:  120 * time.Second,
    }

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

    go func() {
        log.Printf("LLM Gateway starting on port %s", cfg.Port)
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatalf("server error: %v", err)
        }
    }()

    <-quit
    log.Println("Shutting down gracefully...")

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    if err := srv.Shutdown(shutdownCtx); err != nil {
        log.Fatalf("forced shutdown: %v", err)
    }
    log.Println("Server stopped")
}
