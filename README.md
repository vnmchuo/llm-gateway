# LLM Gateway

A high-performance LLM gateway with cost-based routing, circuit breaking, and unified observability.

## Project Structure

- `cmd/gateway`: Application entry point.
- `internal/auth`: API key authentication and middleware.
- `internal/proxy`: Core routing and HTTP handlers.
- `internal/provider`: LLM provider implementations (OpenAI, Gemini, Claude).
- `internal/billing`: Usage tracking and cost management.
- `internal/worker`: Async job processing for long-running requests.
- `internal/telemetry`: OpenTelemetry integration.
- `pkg/ratelimit`: Distributed rate limiting.

## Setup

1. Copy `.env.example` to `.env` and fill in your API keys.
2. Start infrastructure: `make docker-up`.
3. Run the gateway: `make run`.
