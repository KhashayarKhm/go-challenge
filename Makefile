.PHONY: proto test test-integration up down run-ingest run-api sim

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
run-ingest:
	go run ./cmd/ingest

run-api:
	go run ./cmd/api

sim:
	go run ./cmd/uss-sim
