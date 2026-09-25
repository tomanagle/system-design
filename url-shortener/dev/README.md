# Local stack

Run commands from the `url-shortener` directory:

```sh
make dev                    # Base + dev Compose, Air, three app replicas, detached
make dev APP_REPLICAS=5      # Choose the number of app replicas
make dev WORKER_REPLICAS=4 KAFKA_PARTITIONS=4 # Analytics consumers and topic partitions
make up APP_REPLICAS=3       # Compiled application, detached, wait for startup
make logs
make load-test              # Create URLs, then send 10,000 redirect requests
make migrate                # Run migrations locally; requires DATABASE_URL in the environment
make down                   # Stop and remove containers; preserve database/monitoring data
```

Both modes run multiple app instances behind Traefik. Traefik discovers healthy
replicas through Docker labels and events; Prometheus discovers them through
Docker DNS as their addresses/count change. Traefik also actively checks `/healthz`.
`X-Instance-ID` on `/healthz` responses identifies the replica handling a request.
Air watches the mounted source in every development replica, with separate build
directories to prevent replicas overwriting each other's binaries. Saving shared
source rebuilds all development replicas, which can briefly interrupt requests.
Stop the current mode with `make down` before switching modes.

| Service | Local address | Login |
| --- | --- | --- |
| App | http://localhost:18080/healthz | - |
| Postgres | localhost:55439 | shortener / shortener, database `shortener` |
| Kafka | localhost:19092 | topic `redirect-events-v1` |
| ClickHouse HTTP | http://localhost:18123 | analytics / analytics, database `analytics` |
| Prometheus | http://localhost:19090 | - |
| Grafana | http://localhost:13000 | admin / admin |

Grafana starts with Prometheus configured as its default data source. In Explore,
query `up{job="url-shortener"}` for one series per replica, or `go_goroutines`
and `process_resident_memory_bytes` for runtime metrics. The app exposes `/metrics`.
Prometheus also scrapes Traefik's request and routing metrics.

Five dashboards are provisioned in the **URL Shortener** Grafana folder:

- [HTTP Requests](http://localhost:13000/d/url-shortener-http): request rate,
  p50/p95/p99 latency, HTTP statuses, and per-route/per-replica breakdowns.
- [Database Queries](http://localhost:13000/d/url-shortener-db): query rate,
  p50/p95/p99 latency, outcomes, and per-operation/per-replica breakdowns.
- [Go Runtime & Process](http://localhost:13000/d/url-shortener-runtime): scrape
  health, uptime, CPU usage, resident/heap memory, allocation rate, goroutines,
  threads, GC pauses/cycles, and file descriptors for each replica.
- [Product Metrics](http://localhost:13000/d/url-shortener-product): successful
  short URL creations and redirects since app startup, estimated counts in the
  selected time range, and rates. Uses `short_urls_created_total` and
  `short_url_redirects_total`; failed inserts, collisions, and failed lookups are
  excluded. Counters reset on app restart. Creation counts are not database row
  counts; redirects count issued 302 responses, not destination visits.

- [Event Pipeline](http://localhost:13000/d/url-shortener-pipeline): consumer lag per
  partition, group members, producer queue depth/capacity, throughput, drops,
  publish latency/errors, and event age when stored in ClickHouse. Kafka panels
  have topic/group filters; app and worker panels cover all replicas.

All default to all replicas and refresh every five seconds. Use the instance,
route, and operation dropdowns to narrow the view. Latency percentiles aggregate
histogram buckets across the selected replicas. Request totals use `increase`,
so they are estimates from scrapes rather than an exact audit count.

`http_request_duration_seconds{route,method,code}` records completed HTTP requests,
including `/healthz`, `/metrics`, and unmatched routes. Route labels use the mux
pattern (`GET /` for redirects), never individual short codes or query strings.
Select `GET /` or `POST /short-url` to exclude monitoring traffic from charts.

`db_request_duration_seconds{operation,outcome}` measures `create_short_url` and
`get_short_url` store methods using deferred observations, including code generation,
connection pool wait, and result scanning. Every insert retry is a separate observation. Outcomes are `success`,
`no_rows` (lookup miss or code collision), and `error`. Startup pings and migration
queries are excluded. Both histograms expose `_bucket`, `_sum`, and `_count`.
They use Prometheus's default histogram buckets.

Each entrypoint creates its own Prometheus registry and registers the Go and
process collectors explicitly. `promhttp.HandlerFor` exposes their `go_*` and
`process_*` metrics alongside our histograms. Collectors are injected into the
store, producer, consumer, and redirect handler; tests use separate registries. Prometheus captures these from every app replica. Runtime charts
show each replica separately; CPU is measured in cores (1 = one busy core).

Dashboard JSON lives in `dev/grafana/dashboards/`. Run `make dev` or `make up` to
apply Grafana provisioning/mount changes; subsequent JSON edits are picked up
automatically. Allow a few scrapes after generating traffic for rates to appear.

`/healthz` checks HTTP availability only. Startup verifies the database connection
with a 10-second timeout; the pool closes after HTTP shutdown.
The app receives `DATABASE_URL` pointing at `postgres:5432` inside the network.
`config.MustLoad()` loads database, logging, Kafka, and ClickHouse settings in one
place. Compose supplies the environment shared by the app and worker.
The log level is parsed using `slog` (`debug`, `info`, `warn`, or `error`);
missing values or an invalid level panic. Compose supplies `info` in normal mode
and `debug` in dev mode, enabling the store's duration/success debug logs.
Each app replica allows up to 10 open database connections (5 idle). Account for
the total across replicas when adjusting Postgres connection limits.

To run the app directly against the Compose database:

```sh
export LOG_LEVEL=debug
export DATABASE_URL='postgres://shortener:shortener@localhost:55439/shortener?sslmode=disable'
export KAFKA_BROKERS=localhost:19092 KAFKA_TOPIC=redirect-events-v1 KAFKA_PARTITIONS=4
export KAFKA_CONSUMER_GROUP=redirect-analytics-v1 ANALYTICS_QUEUE_SIZE=10000
export ANALYTICS_HASH_KEY='local-development-key-change-before-deploying'
export CLICKHOUSE_URL=http://localhost:18123 CLICKHOUSE_DB=analytics
export CLICKHOUSE_USER=analytics CLICKHOUSE_PASSWORD=analytics
go run ./cmd
# In another terminal with the same environment: go run ./cmd/worker
```

Override `APP_REPLICAS`, `APP_PORT`, `POSTGRES_PORT`, `PROMETHEUS_PORT`,
`GRAFANA_PORT`, or `GRAFANA_ADMIN_PASSWORD` through the environment or Make:

```sh
make dev APP_REPLICAS=4 POSTGRES_PORT=55440
```

Direct Compose equivalents:

```sh
APP_REPLICAS=3 docker compose -f dev/docker-compose.yml up --build -d --wait
APP_REPLICAS=3 docker compose -f dev/docker-compose.yml -f dev/docker-compose.dev.yml up --build
```

This provides app replication on one Docker host. Postgres, Traefik, Prometheus,
Grafana, Kafka, and ClickHouse each have one instance; it does not provide database failover or
survive loss of the host. The default credentials and loopback-only published
ports are intended for local development.

See [load test instructions](../loadtests/README.md) for workload settings,
latency thresholds, and testing the compiled app.

## SQL migrations

Write SQL in `internal/db/migrations/`, using increasing zero-padded filenames:
`0001_create_short_url.sql`, `0002_add_created_at.sql`, etc. Files are embedded
in the migration binary. The first migration creates `short_url` with a unique
`code` and destination `url`.

Migrations are manual. App replicas wait for Postgres to be healthy, then start
without applying migrations. For a fresh database, start Postgres and migrate
before starting the app:

```sh
docker compose -f dev/docker-compose.yml up -d --wait postgres
DATABASE_URL='postgres://shortener:shortener@localhost:55439/shortener?sslmode=disable' make migrate
make dev
```

`make migrate` runs `go run ./cmd/migrate` locally, so it requires Go and a
reachable `DATABASE_URL`. Use your configured host port if it differs from 55439.
Run it again after adding a migration; restarting Compose or Air does not apply
schema changes. There is no migration container.

`schema_migrations` records filenames, SHA-256 checksums, and application times.
Repeat runs skip applied files and reject changed, removed, or reordered history.
All pending files run in one transaction under a Postgres advisory lock; concurrent
runners wait their turn. A failure rolls back the entire pending batch, including
its history records, so it can be retried after correction. The command times out
after two minutes, including lock waits.

Add new files to change an existing schema. There are no automatic down migrations.
Do not put `BEGIN`, `COMMIT`, or `ROLLBACK` in migration files, or commands that
cannot run in a transaction (such as `CREATE INDEX CONCURRENTLY`).

Run migration integration tests against local Postgres (each test creates and
removes its own isolated schema):

```sh
TEST_DATABASE_URL='postgres://shortener:shortener@localhost:55439/shortener?sslmode=disable' go test ./internal/db -v
```

## Analytics

`make dev` starts Kafka, ClickHouse, and four Air worker replicas alongside the
app. `make up` uses compiled app and worker binaries. No migration container is
added. ClickHouse initializes `analytics.redirect_events` from
`dev/clickhouse/init/001_redirect_events.sql`; workers create or validate the Kafka
topic at startup. The app does not wait for Kafka or ClickHouse to serve redirects.

The redirect handler creates and JSON-encodes events using an event factory wired
in `main`. The producer accepts only message keys and payload bytes; it copies
them into its bounded queue and handles publishing, retries, and shutdown.
Encoding failures are logged by the handler without changing the redirect.

Use `WORKER_REPLICAS` to change consumer concurrency. `KAFKA_PARTITIONS` controls a
new topic's partition count (default 4). For an existing topic, a mismatched count
stops worker startup. Changing counts safely requires draining/migrating the
stream; do not resize it while depending on per-code ordering. `KAFKA_TOPIC`,
`KAFKA_PORT`, `CLICKHOUSE_PORT`, `CLICKHOUSE_PASSWORD`, `ANALYTICS_QUEUE_SIZE`, and
`ANALYTICS_HASH_KEY` are configurable in Compose. Keep the hash key identical on
all app replicas; rotating it changes the browser IDs.

Query stored events (deduplicated), using a real short code:

```sh
curl -u analytics:analytics http://localhost:18123/ --data-binary \
  "SELECT short_code, count() AS redirects, uniqExact(user_id) AS browser_signatures
   FROM analytics.redirect_events FINAL GROUP BY short_code"
```

To inspect per-code order, query `event_id, occurred_at, kafka_partition,
kafka_offset` with `WHERE short_code = 'yourcode' ORDER BY kafka_partition,
kafka_offset`. Kafka order need not match timestamps from different app replicas.

Inspect consumer assignments and lag:

```sh
docker compose -f dev/docker-compose.yml exec kafka \
  /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server kafka:9092 \
  --describe --group redirect-analytics-v1
```

Prometheus discovers workers under `job="url-shortener-worker"`. Producers expose
`stream_producer_enqueued_messages_total`, `stream_producer_published_messages_total`,
`stream_producer_dropped_messages_total{reason}`, and `stream_producer_publish_errors_total`.
Workers expose `analytics_events_stored_total`, `analytics_store_errors_total`,
`analytics_commit_errors_total`, and `analytics_invalid_events_total`. These are
operational counts; replayed events may count more than once. Use ClickHouse for
deduplicated analytics.

`kafka-exporter` exposes broker and consumer group metrics internally on port 9308;
Prometheus scrapes it under `job="kafka-exporter"`. Consumer lag compares Kafka end
offsets with group commits, which happen after ClickHouse writes. Uncommitted
partitions are counted separately, and missing lag is not treated as zero.

`stream_producer_queue_depth` and `stream_producer_queue_capacity` describe
each app's bounded queue, excluding the batch being published.
`stream_producer_publish_duration_seconds{outcome}` times batch write attempts, including
internal writer retries but excluding queue wait and pauses between app retries.
`analytics_event_age_seconds` measures creation-to-storage time after a successful
insert, including retries and replays. It relies on synchronized clocks and does
not update for events still waiting to be stored. Both histograms use default
Prometheus buckets; percentiles above 10 seconds are capped at 10 seconds. The
dashboard also shows uncapped mean age and the fraction exceeding 10 seconds.

See [Decisions and tradeoffs](../README.md#redirect-analytics) for the outbox
alternative, event-loss window, browser hash limitations, and delivery semantics.
