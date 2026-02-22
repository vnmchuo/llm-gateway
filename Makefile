POSTGRES_CONTAINER ?= llm-gateway-postgres-1
CONTAINER_TOOL ?= podman

ifeq ($(shell which podman 2>/dev/null),)
    CONTAINER_TOOL = docker
endif

ifeq ($(CONTAINER_TOOL), podman)
    COMPOSE_CMD = podman compose
else
    COMPOSE_CMD = docker compose
endif

run:
	go run cmd/gateway/main.go

build:
	go build -o bin/gateway cmd/gateway/main.go

test:
	go test ./...

tidy:
	go mod tidy

migrate:
	@echo "Running migrations..."
	@for file in migrations/*.sql; do \
		echo "Applying $$file..."; \
		$(CONTAINER_TOOL) exec -i $(POSTGRES_CONTAINER) psql -U postgres -d llm_gateway < $$file; \
	done

up:
	$(COMPOSE_CMD) up -d

down:
	$(COMPOSE_CMD) down

down-volumes:
	@echo "WARNING: This will delete all volumes including database data!"
	$(COMPOSE_CMD) down -v

logs:
	$(COMPOSE_CMD) logs -f

ps:
	$(COMPOSE_CMD) ps

seed:
	RUN_SEED=true go run cmd/gateway/main.go

lint:
	golangci-lint run
