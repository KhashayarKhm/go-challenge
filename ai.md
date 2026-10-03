# AI Usage Report

## 1. Tools & Models

List every AI tool/model you used, even if only briefly.

| Tool / Product | Model | Purpose | Frequency |
|---|---|---|---|
| NotebookLM | Gemini | Validate and get feedback and rate of my architecture | 2 prompts |
| Claude Code | Claude Opus 5.5 | The same as NotebookLM + implementation | Continuous, throughout |
| Claude (web) | Claude Opus 5.5 | Some explaination question about the implementation to don't increase the Claude code context | A few times |

---

## 2. Stages of Development

### Planning / Requirements Analysis
- Did you use AI to clarify requirements, break down the task, or design the architecture?
- What did you keep from its suggestions, and what did you discard or change?

I plan and design the architecture by myself by research on the internet. Use the NotebookLM and include DDIA, BM and FSA books which they the reference books that I think my answers are in there and let the AI to rate my architecture and decisions.

The NotebookLM rate my architecture 7.5/10 and helps me to fine tuning the ClickHouse `segment_users` table by change the `ORDER BY` clause and change the engine to `ReplacingMergeTree` which is suitable to bring the idempotency to my system. I validate the response by  [How to Implement Idempotent Data Ingestion in ClickHouse](https://oneuptime.com/blog/post/2026-03-31-clickhouse-idempotent-data-ingestion/view) and [Top 10 best practices tips for ClickHouse](https://clickhouse.com/blog/10-best-practice-tips) and some other blog posts.

NotebookLM suggest to use HyperLogLog data structure in Redis instead of ClickHouse for simplicity. I refuse the Redis approach cause it's not flexible as the ClickHouse is and we don't have the history data to make out time window bigger or take a yearly/monthly because the user segmentation is a analysis process and data history is important for this field.

I pretty confident to my architecture till do the same with Claude Code(Opus 5.5), it rates my architecture 6/10 and suggests to change some parts of my system:
  1. Use HyperLogLog Redis data structure, which I refuse it again.
  2. Use Kafka and nothing! It seems like there is a Kafka feature to connect directly to ClickHouse, but I rather to use RabbitMQ for it's simplicity and ingest service is more flexible to add logic if needed.
  3. Drop the Redis stack, cause the idempotency is handled by `ReplacingMergeTree` in ClickHouse and the batching insert can be handled by prefetch feature in RabbitMQ. The Redis approach will cause race condition at scale with many consumers.
  4. Remove length check goroutine. I suggest to have two main goroutine to drain the Redis cache for batch insert to ClickHouse. One is a ticker to drain and insert to ClickHouse periodically, another one is length limit checker which check HashSet length on each insert to Redis, if it reaches the limit drain and insert to the ClickHouse which prevents to occur Redis OOM error. We don't need the length checker after we drop the Redis stack, so we removed the race condition between goroutines on consuming Redis cache.

### Scaffolding / Boilerplate / Implementation
- Which parts of the core logic were AI-assisted vs. hand-written?
- Did you use AI for full functions, snippets, or just autocomplete?

After I finalize the architecture, switch to the plan-mode and AI did 90% of work at its first implementation prompt and my main responsibility was to check that everything is align with our plan and nothing went wrong.

### Debugging
- Did you use AI to diagnose errors or unexpected behavior?
- Describe a specific bug where AI helped (or didn't).

No, I run and test manually to find any bug like exit the consumer at the middle of the process and check that there is no problem on redelivery the messages and does not break the estimate numbers.

### Testing
- Did AI write test cases? Did you review them for correctness/coverage, or just accept them?

Yes, AI generate the tests and review/run them. there is no complex logic in this project, so no edge cases comes to my mind to write test for it.

### Documentation
- Did AI help write comments, README sections, or docstrings? Yes

### Code Review / Refactoring
- Did you ask AI to review your own code, suggest improvements, or refactor for readability/performance?

I don't use the review skill at this time.
I don't use it for readability neither cause AI can read anything and I(human) should be able to read it and I think it's ok.

---

## 3. Prompts

Log the prompts you used. For **each** prompt, include:
- **Stage:** which stage from Section 2 this belongs to
- **Prompt:** the exact text you sent
- **Result:** a short summary of what the AI produced
- **Evaluation:** how you judged whether the output was correct/good enough
- **Fixes:** what you changed, rejected, or re-prompted for, and why

### Prompt #1
- **Stage:** Planning
- **Prompt:**
  ```
  read @".tmp/Designing Data Intensive Applications.pdf" @".tmp/Building Microservices Designing Fine-Grained Systems 2nd By Sam Newman.pdf" @".tmp/Fundamentals of Software Architecture An Engineering Approach-Mark Richards, Neal Ford-O'Reilly-9781492043454-EBooksWorld.ir.pdf" books. I have a solution for @README.md challenge here is it.

  there is a challenge of building a ES service. the details are in the github README.md file. here is my architecture solution and I want to analyze and tell me what missed and what are the trade offs and rate it from 0 to 10\. I use RabbitMq to send data from USS to ES, cause it's reliable and make me sure that message are send/received correctly. adding a cache layer before ES processing with 24h TTL to idempotent the messages daily, after that push the (user\_id, segment) tuple to redis set with segment key and a HashSet of user ids. two goroutines responsible to drain these values to bulk insert:

  1. a ticker with 10s duration
  2. limit checker which check the set length on insertion to HashSet, if it is more than the threshold(like 200) we bulk insert immediately to prevent redis OOM on high load.
  I want to use ClickHouse DB cause it fits to our problem, an analytics problem. I normalize the started\_at field to date(remove the time and its schema should like YYYY-mm-dd) set the order by clause on the table to (segment, started\_at) the segment type should be LowCardinality(String). for `func estimate` function, we just need to write a query with 2 weeks window range and segment filters.
  ```
- **Result:** As mentioned in plan stage, the main improvement was to drop Redis stack and length limit check goroutine. create a trade-off table of alternative approaches.
- **Evaluation:** Searching in google and find best practices especially for storage part(ClickHouse VS Redis) with more focus on ClickHouse best practices.
- **Fixes:** It was good overall and I refuse to use Redis HyperLogLog and setting TTL for records in ClickHouse

### Prompt #2
- **Stage:** Planning
- **Prompt:**
  ```
  If you used `ORDER BY (segment, started_at, user_id)`:

  - Key on Day 1: `('sports', '2026-09-01', 'u104010')`
  - Key on Day 5: `('sports', '2026-09-05', 'u104010')`

  Because the keys are different, `ReplacingMergeTree` would not deduplicate them across days! You would end up with multiple entries for the same user.
  ```
- **Result:** Trying to explain the `PARTITION BY` clause prevent deduplication and this approach will save the segment history with trade-off table of with and without this statement. 
- **Evaluation:** Search and find [How to Implement Idempotent Data Ingestion in ClickHouse](https://oneuptime.com/blog/post/2026-03-31-clickhouse-idempotent-data-ingestion/view) blog post that use partitioning.
- **Fixes:** --

### Prompt #3
- **Stage:** Planning / Implementation
- **Prompt:**
  ```
  <switch to plan mode>
  ok, I think we done. drop the idempotency cache layer and length limit goroutine. what is our architecture? as the @README.md said, we need to add a pkg directory with interface and its adapter in it to used by USS
  ```
- **Result:** Build an implementation plan. Ask again to drop Redis stack, estimate API type
- **Evaluation:** Read the plan
- **Fixes:** Reject the plan 3 times with these feedbacks:
  ```
  1. what does the segment_daily_uniq and segment_daily_uniq_mv tables do? it seems like it over engineering
  2. remove the record ttl on table. the redis is better choise if I don't want the history
  3. the architecture.md should include the system evolution, what I suggest, what's your suggest and improvement and what I change on your suggestion
  ```

### Prompt #4
- **Stage:** Implementation
- **Prompt:**
  ```
  <interrupt when I see the agent struggling to download packages>
  use "http://localhost:10809" proxy
  ```
- **Result:** use the proxy to download packages
- **Evaluation:** --
- **Fixes:** --

### Prompt #5
- **Stage:** Reviewing
- **Prompt:**
  ```
  why don't you put the @pkg/segmentation/message.go or Membership struct from @internal/store/store.go to the domain/entity or contract directory?
  ```
- **Result:** Explain the `Message` and `Membership` purpose and said may they look the same but they are playing different role.
- **Evaluation:** Thinking about the result and I drew a conclusion that it's better to take it easy at this stage and don't try to merge them. We use interface/adapter style and this is enough.
- **Fixes:** --

### Prompt #6
- **Stage:** Reviewing / Refactoring
- **Prompt:**
  ```
  separate the grpc server and ingres service cause if we want to scale up the ingres service, we have grpc server unconditionally
  ```
- **Result:** Replaces the es command to two ingest and api commands, updates the docs and Makefile
- **Evaluation:** Review and run each commands that works correctly
- **Fixes:** --

### Prompt #7
- **Stage:** Reviewing / Refactoring
- **Prompt:**
  ```
  is it the consumer name in the "Consume" second arg at line 90 in @internal/ingest/consumer.go file? if yes, make it dynamic by env variable and if it doesn't set, add a date(YYYY-MM-dd HH:mm) postfix
  ```
- **Result:** Set dynamic tag for consumers by env variable. update docs. add unit test for tag generate utility.
- **Evaluation:** Check the tag name on RabbitMQ UI panel
- **Fixes:** --

### Prompt #8
- **Stage:** Implementation
- **Prompt:**
  ```
  add pprof http server for each ingest and api services. the pprof server should be enabled by env variable and be off by default
  ```
- **Result:** Create a pprofserver package and add it to each ingest and api command with env configurations. update docs and run tests.
- **Evaluation:** Enable the pprof server for ingest service and everything is ok.
- **Fixes:** --

### Prompt #9
- **Stage:** Reviewing / Refactoring / Testing
- **Prompt:**
  ```
  clear up the database after clickhouse integration test and change the table name on test-integration command in Makefile
  ```
- **Result:** Trying to separate tables.
- **Evaluation:** See many unrelated files are changing
- **Fixes:** interrupt the process

### Prompt #10
- **Stage:** Reviewing / Refactoring / Testing
- **Prompt:**
  ```
  no dumb a** AI. I mean database name
  ```
- **Result:** Revert the previous changes and separate databases for testing and development use. Add clean up statement after tests are finished.
- **Evaluation:** Review the ClickHouse integration test that db connecting and clearup flow I find out it create and drops the database
- **Fixes:** send the next prompt to fix that

### Prompt #11
- **Stage:** Reviewing / Refactoring / Testing
- **Prompt:**
  ```
  remove the name validation and migration part in test. the tester should prepare the env before running the tests
  ```
- **Result:** Truncate the tables instead of drop the database, remove the migration section and skip the test if the database dsn does not exits instead of setting default
- **Evaluation:** Check the test file code, run test and check the testing database
- **Fixes:** --

### Prompt #12
- **Stage:** Reviewing / Refactoring
- **Prompt:**
  ```
  use godotenv package to load .env files and put it in gitignore file and make a copy of .env with .env.example with default values to commit it
  ```
- **Result:** install godotenv package, create env files and update the document
- **Evaluation:** run tests and test manually the whole system
- **Fixes:** --

### Prompt #13
- **Stage:** Reviewing / Refactoring
- **Prompt:**
  ```
  remove the dsn env in integration test in Makefile and load .env.test.local for all kind of tests
  ```
- **Result:** Use .env.test.local file for integration tests
- **Evaluation:** run integration test
- **Fixes:** --

### Prompt #14
- **Stage:** Implementation
- **Prompt:**
  ```
  create a migrator command with golang-migrate/migrate package, implement just, up, down(last version) and version sub command
  ```
- **Result:** Create a command with desire sub commands with migrate package to migrate database manually. updates the docs too.
- **Evaluation:** run commands and check the database schema. it doesn't get the env path flag so I couldn't migrate the testing db.
- **Fixes:** send another prompt to add the flag

### Prompt #15
- **Stage:** Implementation
- **Prompt:**
  ```
  add option to set env path to load
  ```
- **Result:** add -env-file flag to all commands(migrator, ingest, api), updates docs and Makefile
- **Evaluation:** migrate the testing database with .env.test.local file and run the other apps
- **Fixes:** --

---

## 4. Token Usage Monitoring

Explain how you tracked and managed token/cost usage during this challenge.

- **Monitoring method:** e.g. built-in usage dashboard, CLI `--usage` flag, a custom script, manual tracking in a spreadsheet.
- **Tools/links:** [link to dashboard, extension, or script used]
- **Helper prompts:** if you used a prompt to summarize context, compress history, or check token counts before a big request, paste it here:
  ```
  [paste helper prompt here]
  ```
- **Management strategies:** what did you do to keep usage efficient? Examples:
  - Trimming context / clearing chat history between unrelated tasks
  - Using smaller/cheaper models for simple tasks and reserving larger models for complex ones
  - Batching related questions instead of many small round-trips
  - Summarizing long files before sending them as context
- **Rough total usage (optional but encouraged):** e.g. approximate token count, cost, or number of requests across the challenge.

---

## Notes / Reflections (optional)

Anything else worth mentioning — where AI slowed you down, where it was indispensable, judgment calls you made about when *not* to use it, etc.
