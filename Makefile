.PHONY: run build test migrate up down lint check-container-tool

# Variabel untuk memudahkan penggantian runtime di masa depan
CONTAINER_TOOL ?= podman

# Deteksi otomatis jika podman tidak ada, fallback ke docker
ifeq ($(shell which podman 2>/dev/null),)
    CONTAINER_TOOL = docker
endif

# Cek apakah podman-compose atau docker compose tersedia
ifeq ($(CONTAINER_TOOL), podman)
    COMPOSE_CMD = $(shell which podman-compose 2>/dev/null || echo "podman compose")
else
    COMPOSE_CMD = docker compose
endif

cct: check-container-tool
	@echo "Using container tool: $(CONTAINER_TOOL)"
	@echo "Using compose command: $(COMPOSE_CMD)"
	@which $(CONTAINER_TOOL) > /dev/null 2>&1 || (echo "ERROR: $(CONTAINER_TOOL) not found!" && exit 1)

run:
	go run cmd/gateway/main.go

build:
	go build -o bin/gateway cmd/gateway/main.go

test:
	go test ./...

tidy:
	go mod tidy

migrate: check-container-tool
	@echo "Running migrations..."
	@for file in migrations/*.sql; do \
		echo "Applying $$file..."; \
		$(CONTAINER_TOOL) exec -i postgres-gateway psql -U postgres -d llm_gateway < $$file; \
	done

up: check-container-tool
	@if [ "$(CONTAINER_TOOL)" = "podman" ]; then \
		$(COMPOSE_CMD) up -d || (echo "Falling back to manual podman run..." && \
		podman run -d --name redis-gateway -p 6379:6379 redis:alpine && \
		podman run -d --name postgres-gateway -p 5432:5432 -e POSTGRES_PASSWORD=postgres -e POSTGRES_USER=postgres -e POSTGRES_DB=llm_gateway postgres:latest); \
	else \
		$(COMPOSE_CMD) up -d; \
	fi

down: check-container-tool
	$(COMPOSE_CMD) down

down-volumes: check-container-tool
	@echo "WARNING: This will delete all volumes including database data!"
	$(COMPOSE_CMD) down -v

logs: check-container-tool
	$(COMPOSE_CMD) logs -f

ps: check-container-tool
	$(COMPOSE_CMD) ps

lint:
	golangci-lint run
