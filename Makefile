.PHONY: proto test test-integration up down run-ingest run-api sim

# Regenerate gRPC code (requires protoc, protoc-gen-go, protoc-gen-go-grpc).
proto:
	protoc -I api/proto \
		--go_out=api/gen --go_opt=paths=source_relative \
		--go-grpc_out=api/gen --go-grpc_opt=paths=source_relative \
		estimation/v1/estimation.proto

test:
	go test -race ./...

# Needs `make up` and a prepared es_test database (see README). Tests empty its tables afterwards.
test-integration:
	CLICKHOUSE_DSN=clickhouse://default:clickhouse@localhost:9000/es_test go test -race -count=1 ./...

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
