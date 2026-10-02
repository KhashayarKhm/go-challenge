# Estimation Service: Architecture

This document records how the design evolved: the original proposal, the review it went
through, which suggestions were accepted, changed or rejected, and the resulting architecture
and its trade-offs.

References used in the review:
- **DDIA**: Kleppmann, *Designing Data-Intensive Applications*
- **BM**: Newman, *Building Microservices*, 2nd ed.
- **FSA**: Richards & Ford, *Fundamentals of Software Architecture*

---

## 1. Problem and requirements

- USS sends `(user_id, segment)` pairs. ES stores them and answers
  `estimate(segment) -> number of users`.
- A user stays in a segment for **two weeks**. After that they must not be counted.
- Scale: millions of users and hundreds of segments.

**Definition of "two weeks" used here:** a user tagged with a segment on UTC day `D` counts on
days `D … D+13` (14 calendar days). Being tagged again extends the membership, so the rule is
*last tag + 14 days*. Day granularity means up to 24h of imprecision at the boundary (see trade-offs).

---

## 2. v0: Original proposal

```
USS ──► RabbitMQ ──► ES ──► Redis dedup cache (24h TTL)
                          └► Redis SET per segment {user_ids}
                                ├─ goroutine 1: ticker (10s) → bulk insert
                                └─ goroutine 2: on insert, if SCARD > ~200 → bulk insert now
                                               (protect Redis from OOM under load)
                                     ▼
                                 ClickHouse
```

- **RabbitMQ** between USS and ES for reliable delivery.
- **Redis cache with 24h TTL** to make messages idempotent per day.
- **Redis SET per segment** of user ids, drained by two goroutines: a 10s ticker, and a
  length checker that flushes immediately above a threshold (~200).
- **ClickHouse** because this is an analytics problem.
- `started_at` normalized to a date (`YYYY-mm-dd`), `ORDER BY (segment, started_at)`,
  `segment LowCardinality(String)`.
- `estimate` = a query with a 2-week range filter on the segment.

---

## 3. Review findings and suggestions

What v0 already got right: ClickHouse fits a distinct-count-over-a-time-range query;
`ORDER BY (segment, day)` with a low-cardinality first key prunes scans and compresses well
(DDIA ch.3, *Sort Order in Column Storage*); bucketing by day collapses repeated taggings;
batching inserts is required for ClickHouse; idempotency was considered at all.

| # | Problem found | Suggestion | Source |
|---|---|---|---|
| R1 | RabbitMQ's guarantee ends at the ack. ES must ack once the pair is in Redis, but it only reaches ClickHouse up to 10s later. A Redis restart/eviction or a failed flush loses data that RabbitMQ can no longer redeliver: a **dual write** with no atomic commit. | Remove the Redis staging set. Buffer in Go memory, bulk insert, **ack only after the insert succeeds** (`multiple=true`). | DDIA ch.11 *Dual writes*, *Acknowledgments and redelivery* |
| R2 | The dedup key is set **before** the insert. If the insert fails, the redelivered message is treated as a duplicate and dropped, so it is lost. | Make the **write** idempotent instead of filtering messages. | DDIA ch.11 *Idempotence*; BM ch.12 *Idempotency* |
| R3 | A 24h TTL is a rolling window, but rows are calendar days. Tag at 23:00 day 1 then 10:00 day 2 → day 2 is deduped, so the user drops out one day early. | If kept, key by date: `seen:{date}:{segment}:{user}`. | n/a |
| R4 | The dedup cache costs ~50M keys (10M users × ~5 segments/day), several GB of Redis, a larger OOM risk than the staging set. And since `estimate` counts **distinct** users, duplicates never change the answer anyway. | Drop the cache. Use ClickHouse `ReplacingMergeTree` + distinct count. | n/a |
| R5 | A threshold of 200 rows is far too small: hundreds of segments × small inserts → ClickHouse "too many parts". | Batches of ~10k+ rows, about ≤ 1 insert/s. One batch across all segments. | ClickHouse docs |
| R6 | Two goroutines (and several ES replicas) drain the same set. `SMEMBERS` + `DEL` is not atomic, so rows added in between are lost. | A single flusher loop. | n/a |
| R7 | A set keyed by segment only loses the timestamp. Stamping at consume time puts backlog/replayed events on the wrong day. | Use **event time** carried in the message. | DDIA ch.11 *Reasoning About Time* |
| R8 | No retention: the table grows forever although only 14 days are read. | TTL `day + 15 days`, daily partitions. | n/a |
| R9 | `estimate` scans raw rows on every call. | Materialized view of daily `uniqState` (HyperLogLog-like) per segment. | DDIA ch.3 *Data Cubes and Materialized Views*; ch.11 (HyperLogLog) |
| R10 | "Reliable RabbitMQ" depends on configuration. | Quorum queues, persistent messages, publisher confirms, manual acks, dead-letter queue. Don't trust broker "exactly once": make consumers tolerate duplicates. | BM ch.5 *Message Brokers* |
| R11 | Not covered: ClickHouse replication, observability, a test strategy. | Note as future work; make the batcher testable without infrastructure. | FSA ch.4 |

---

## 4. Author's decisions on the suggestions

| Suggestion | Decision | Reason |
|---|---|---|
| R1, R5, R6: drop Redis staging, batch in Go, ack after insert | **Accepted** | Removes a component and the data-loss window. RabbitMQ `prefetch = batch size` bounds memory, so the **length-limit goroutine is dropped too**; the single loop flushes when full or on tick. |
| R2, R4: drop the dedup cache, idempotent writes | **Accepted** | `ReplacingMergeTree` + distinct counting give *at-least-once + idempotent = effectively-once*. |
| R3: date-keyed dedup | **Moot** | The cache is gone. |
| R7: event time in the message | **Changed** | The README says USS sends only `user_id` and `segment`, so the client API has **no `at` parameter**. Instead the `pkg/segmentation` adapter stamps the AMQP `timestamp` property at publish time. ES uses it (falling back to consume time). |
| R8: retention TTL | **Rejected** | History is kept on purpose for future analytics. If history were not wanted, Redis would be the better store, and keeping it is the reason to choose ClickHouse. Partitioning moved to **monthly** to keep the partition count low as history grows. |
| R9: materialized view | **Rejected (for now)** | Over-engineering at this stage: the `(segment, day)` sort key already limits the scan to one segment's 14 days. Add it when measured latency calls for it. |
| R10: RabbitMQ hardening | **Accepted** | Quorum queue, DLX/DLQ, persistent messages, publisher confirms, manual acks. |
| Interface for queries | **Chosen: gRPC** | Typed, versioned contract and generated clients. |
| Scope of `pkg` | **Ingest only** | USS only writes. `pkg/segmentation` exposes `Publisher` + RabbitMQ adapter + in-memory fake. |
| One ES binary (ingest + gRPC) | **Changed (author)** | The first implementation ran the consumer and the gRPC server in one process, so scaling ingestion also scaled API servers, and vice versa. ES is now **two deployables**: `cmd/ingest` (worker) and `cmd/api` (gRPC). They share only the ClickHouse table, and each scales on its own load. |

A point clarified during the review: with `ORDER BY (segment, day, user_id)`, the same user on
different days produces **different keys**, so `ReplacingMergeTree` does not merge them. That is
intended. Those rows are the history the 2-week window needs (a day-5 row keeps the user counted
after day 1 expires). They are collapsed at query time by the distinct count. `ReplacingMergeTree`
only merges identical `(segment, day, user_id)` keys within the same partition, and does so
eventually, so queries never rely on `count()`.

---

## 5. Final architecture

```
USS ──imports pkg/segmentation──► Publisher.Publish(ctx, userID, segment)
        RabbitMQ adapter: publisher confirms · persistent · AMQP timestamp = publish time
                                 │
                 exchange "segmentation" (direct, durable)
                                 │ routing key "segment.tagged"
                 queue "estimation.segments" (quorum) ──invalid──► .dlx ──► .dlq
                                 │ prefetch = BATCH_SIZE, manual ack
                 ES ingest worker: cmd/ingest, N replicas (competing consumers)
                 ingest.Batcher (single goroutine per replica)
                   decode + validate → buffer []Membership{user, segment, day(UTC)}
                   flush when len == BATCH_SIZE or every FLUSH_INTERVAL (10s)
                   INSERT batch → Ack(last, multiple) | on error Nack(requeue) + backoff
                                 │
                 ClickHouse segment_users
                   ReplacingMergeTree · PARTITION BY toYYYYMM(day) · ORDER BY (segment, day, user_id)
                                 │
                 ES API: cmd/api, M stateless replicas
                 estimate.Service: since = today(UTC) − 13 days
                 SELECT uniq(user_id) WHERE segment = ? AND day >= since
                                 │
                 gRPC EstimationService.Estimate(segment) → {segment, users}
```

### Components

| Path | Responsibility |
|---|---|
| `pkg/segmentation` | Public client for USS: `Publisher` interface, `RabbitMQPublisher`, `InMemoryPublisher` (test fake), and the wire contract `Message` shared with ES. |
| `internal/ingest` | `Consumer` declares the RabbitMQ topology and consumes. `Batcher` batches, writes and acks. |
| `internal/store` | Ports (`Writer`, `Reader`), the `Membership` model, and `DayOf` (the one definition of "a day"). |
| `internal/store/clickhouse` | ClickHouse adapter: batch insert and distinct count. |
| `internal/estimate` | The business rule: 14-day window computed in UTC. |
| `internal/pprofserver` | Optional pprof HTTP server for both processes. Off by default (`PPROF_ENABLED`), binds to localhost. |
| `internal/transport/grpc` | gRPC adapter that maps domain errors to status codes. |
| `api/proto` / `api/gen` | gRPC contract and generated code. |
| `cmd/ingest` | Ingestion worker: RabbitMQ → ClickHouse. Flushes and acks its buffer on shutdown. |
| `cmd/api` | gRPC API: ClickHouse → `Estimate`. Stateless, graceful stop. |
| `cmd/uss-sim` | Publishes random pairs through `pkg/segmentation` for end-to-end checks. |
| `migrations` | ClickHouse schema (auto-applied by docker compose). |

### Delivery semantics

- **At-least-once:** a message is acked only after its batch is in ClickHouse. A crash, a failed
  insert or a lost connection leads to redelivery, never to loss.
- **Idempotent writes:** a redelivered pair produces the same `(segment, day, user_id)` row,
  merged by `ReplacingMergeTree` and ignored by `uniq` anyway.
- Together these give **effectively-once** results (DDIA ch.11).
- Invalid payloads are rejected without requeue → dead-letter queue, so they never block the queue.

### Deployment and scaling

The ingestion worker and the API are separate processes that share only the ClickHouse table:

| Process | Scales with | How to scale |
|---|---|---|
| `cmd/ingest` | Write rate from USS (queue depth) | Add replicas. RabbitMQ spreads messages across them (competing consumers); each acks only its own batches. |
| `cmd/api` | Estimate query rate | Add replicas behind a gRPC load balancer. They hold no state. |

Running one kind of process never forces running the other. Deploying or crashing one doesn't
affect the other either: a failing API doesn't stop ingestion, and a backlog in ingestion doesn't
slow queries.

### Ordering of startup

`cmd/ingest` declares the queue, so it must start (once) before USS publishes. Messages published to an
exchange with no bound queue are confirmed and dropped by RabbitMQ.

---

## 6. Trade-offs

> "There are no right or wrong answers in architecture—only trade-offs." (FSA ch.2)

| Decision | Gain | Cost |
|---|---|---|
| RabbitMQ (vs Kafka) | Simple operations, per-message acks, DLQ built in | No replay log: ClickHouse cannot be rebuilt from the broker. Kafka + the ClickHouse Kafka engine would need almost no ingestion code. |
| Batch in memory, ack after insert | No extra component, no loss window | New taggings become visible up to `FLUSH_INTERVAL` later. A crash causes redelivery (harmless). |
| Day granularity | One row per user/segment/day, simple partitions | Up to 24h imprecision at the 2-week edge |
| `uniq` (vs `uniqExact`) | Fixed memory per query, fast for multi-million-user segments | ~1–2% error on large segments (it is an "estimate"). Exact for small sets. |
| ClickHouse (vs Redis-only, e.g. a set per segment per day + `SUNIONSTORE`/`SCARD`) | Keeps history cheaply on disk, scales to billions of rows, analytics-ready | More moving parts than Redis-only at small scale |
| Keep history (no TTL) | Future analytics, auditability | Storage grows without bound. A TTL/archival policy can be added later without code changes. |
| No pre-aggregation | Simpler schema, one table | Each estimate scans up to 14 days of one segment's rows |
| Separate ingest and API processes (vs one binary) | Independent scaling, deploys and failure isolation | Two deployables to build, configure and monitor |
| gRPC (vs REST) | Typed contract, generated clients, HTTP/2 | Not curl-friendly (reflection enabled for `grpcurl`) |
| Publish timestamp as event time | Correct day despite backlog/replay, no `at` in API | Relies on the USS host clock. A missing timestamp falls back to consume time. |

---

## 7. Future improvements

- **Materialized view** of daily `uniqState(user_id)` per segment (R9) if estimate latency matters.
- **ClickHouse replication** (`ReplicatedReplacingMergeTree` + Keeper): the single node is a SPOF.
- **Retention / tiered storage** if history growth becomes a cost problem.
- **Kafka + ClickHouse Kafka table engine** if replay or much higher throughput is needed.
- **Observability:** queue depth/lag, batch size, flush latency, insert errors, DLQ depth
  (Prometheus metrics), and tracing across USS → ES.
- **Reconnect loop** in the consumer and publisher instead of restart-on-failure.
