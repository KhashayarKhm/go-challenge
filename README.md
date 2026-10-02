# How many users? (Go Challenge)

Suppose there is a "User Segmentation Service" (USS) that segments users based on their activities.
For example, if a user visits sports news, USS classifies and tags "sport" to him.
So we have a pair: (the user_id, and the segment) for example, (u104010, "sports").

We want to develop an Estimation Service (ES) that interacts with USS directly.
ES receives the pair from USS as input and stores it.
The responsibility of ES is to answer a simple query: "How many users exist on a specific segment?".
For example, "how many users are in the sports segment?".

![](https://raw.githubusercontent.com/ArmanCreativeSolutions/go-challenge/main/Untitled%20Diagram.drawio.png?raw=true)

The query is simple, but two assumptions may make it a little challenging:
- A specific user remains just two weeks on a segment. After that,
we should not count "u104010" on the sports segment.
- There are millions of users and hundreds of segments. So your solution(s) must be scalable


## Requirements

- Implement a (REST API, RESTful API, soap, Graphql, RPC, gRPC, or whatever protocol you prefer)
interface to receive data (user_id, segment pair) from USS. 
- Implement a method to estimate the number of users in a specific segment. ( `func estimate(segment) -> number of users`)

## Implementation details

Try to write your code as reusable and readable as possible.
Also, don't forget to document your code and clear the reasons for all your decisions in the code.

If your solution is not simple enough for implementing fast, you can just describe it in your documents.

Use any tools that you prefer just explain the reason of choices in your documents.
For example explain why you choose REST API for receiving data.

It is more valuable to us that the project comes with unit tests.

Please fork this repository and add your code to that.
Don't forget that your commits are so important.
So be sure that you're committing your code often with a proper commit message.


## We all use AI. Don't be ashamed

Everyone uses AI for everything. And we expect you to do the same. AI is a big part of our development process, and we care a lot about how you use AI tools during this task.

To earn our immunity after the AI takeover of Earth, we want to make sure you're using it properly.

Add a file named "ai.md" to your project. This file should contain the following information (the more explicit, the better):

1. What AI tools and models did you use?
2. Explain the different stages of software development where you used AI help. Describe how you used your tools at each stage.
3. We want to know how you prompt. So add your prompts to this file too. For each prompt, explain which stage you used it in, how you evaluated the result, and what you did to fix any misbehavior.
4 Explain how you monitor your token usage and what you do to manage it. Link your tools or add your helper prompts.

*There's a sample.ai.md file in the project representing the expected template. Don't forget: "ai.md" is the only file we expect you to write entirely by yourself.*


## Solution

Design decisions, the review the design went through, and trade-offs: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

```
USS ─► pkg/segmentation ─► RabbitMQ ─► cmd/ingest ─► ClickHouse ◄─ cmd/api ◄─ gRPC Estimate(segment)
```

- **USS integration:** import `github.com/KhashayarKhm/go-challenge/pkg/segmentation` and depend on the
  `Publisher` interface (`Publish(ctx, userID, segment)`); use `NewRabbitMQPublisher` in production and
  `InMemoryPublisher` in tests.
- **Estimate:** `estimation.v1.EstimationService/Estimate` (see `api/proto`).
- **Two processes:** `cmd/ingest` (RabbitMQ → ClickHouse) and `cmd/api` (gRPC) are deployed and
  scaled independently.

### Run locally

```sh
make up                 # RabbitMQ + ClickHouse (schema applied automatically)
make run-ingest         # ingestion worker: RabbitMQ → ClickHouse (start before the simulator)
make run-api            # gRPC API on :9090 (another terminal)
make sim                # USS simulator: publishes random pairs, prints expected counts
grpcurl -plaintext -d '{"segment":"sports"}' localhost:9090 estimation.v1.EstimationService/Estimate
```

Configuration is via environment variables, optionally loaded from a `.env` file in the working
directory (`cp .env.example .env`; real environment variables take precedence). Defaults match
`docker-compose.yml`.
`cmd/ingest` reads `RABBITMQ_URL`, `RABBITMQ_QUEUE`, `RABBITMQ_CONSUMER_TAG`, `CLICKHOUSE_DSN`,
`BATCH_SIZE` and `FLUSH_INTERVAL`.
`cmd/api` reads `CLICKHOUSE_DSN` and `GRPC_ADDR`.

### Profiling

Both processes can serve Go runtime profiles (`net/http/pprof`). It is off by default; enable it with
`PPROF_ENABLED=true`. The address comes from `PPROF_ADDR` (defaults: `localhost:6060` for ingest,
`localhost:6061` for api).

```sh
PPROF_ENABLED=true make run-ingest
go tool pprof http://localhost:6060/debug/pprof/heap
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30   # CPU
``` With the default
`FLUSH_INTERVAL=10s`, counts appear up to 10 seconds after publishing.

### Tests

```sh
cp .env.test.example .env.test.local   # test configuration (gitignored)
make test               # unit tests, no infrastructure needed
make test-integration   # unit + ClickHouse integration tests (build tag "integration")
```

Both targets load `.env.test.local` (if present) with the godotenv CLI, so every test package sees its
variables; real environment variables take precedence.

`make test-integration` expects a prepared environment: `make up`, then the database named in
`CLICKHOUSE_DSN` (default `es_test`) with the migrations applied. The tests empty `segment_users` in
it when they finish.

```sh
docker compose exec clickhouse clickhouse-client --password clickhouse -q "CREATE DATABASE IF NOT EXISTS es_test"
docker compose exec -T clickhouse clickhouse-client --password clickhouse -d es_test --multiquery < migrations/001_init.sql
```
