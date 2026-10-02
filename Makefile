.PHONY: proto test test-integration up down migrate-up migrate-down migrate-version run-ingest run-api sim

# Regenerate gRPC code (requires protoc, protoc-gen-go, protoc-gen-go-grpc).
proto:
	protoc -I api/proto \
		--go_out=api/gen --go_opt=paths=source_relative \
		--go-grpc_out=api/gen --go-grpc_opt=paths=source_relative \
		estimation/v1/estimation.proto

# Tests load .env.test.local (if present) through the godotenv CLI, so every test package sees it.
# Real environment variables take precedence over the file.
TEST_ENV_FILE := .env.test.local
WITH_TEST_ENV = $(if $(wildcard $(TEST_ENV_FILE)),go run github.com/joho/godotenv/cmd/godotenv -f $(TEST_ENV_FILE),)

test:
	$(WITH_TEST_ENV) go test -race ./...

# Unit + integration tests (build tag "integration"). Needs `make up` and a prepared database
# whose DSN is in .env.test.local (see README). Tests empty its tables afterwards.
test-integration:
	$(WITH_TEST_ENV) go test -race -count=1 -tags integration ./...

up:
	docker compose up -d --wait

down:
	docker compose down -v

# Ingestion worker and gRPC API are separate processes; run each in its own terminal.
# Commands load .env by default; pass ENV_FILE=path to load another file, e.g. make run-api ENV_FILE=.env.staging
ENV_FLAG = $(if $(ENV_FILE),-env-file $(ENV_FILE),)

# ClickHouse schema (cmd/migrate, golang-migrate). Uses CLICKHOUSE_DSN from the environment or the env file.
migrate-up:
	go run ./cmd/migrate $(ENV_FLAG) up

# Rolls back only the last applied migration.
migrate-down:
	go run ./cmd/migrate $(ENV_FLAG) down

migrate-version:
	go run ./cmd/migrate $(ENV_FLAG) version

run-ingest:
	go run ./cmd/ingest $(ENV_FLAG)

run-api:
	go run ./cmd/api $(ENV_FLAG)

sim:
	go run ./cmd/uss-sim
