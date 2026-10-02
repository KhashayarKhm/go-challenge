.PHONY: proto test test-integration up down run sim

# Regenerate gRPC code (requires protoc, protoc-gen-go, protoc-gen-go-grpc).
proto:
	protoc -I api/proto \
		--go_out=api/gen --go_opt=paths=source_relative \
		--go-grpc_out=api/gen --go-grpc_opt=paths=source_relative \
		estimation/v1/estimation.proto

test:
	go test -race ./...

# Needs `make up` first.
test-integration:
	CLICKHOUSE_DSN=clickhouse://default:clickhouse@localhost:9000/default go test -race -count=1 ./...

up:
	docker compose up -d --wait

down:
	docker compose down -v

run:
	go run ./cmd/es

sim:
	go run ./cmd/uss-sim
