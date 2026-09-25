# Local stack

Run commands from the `url-shortener` directory:

```sh
make dev                    # Base + dev Compose, Air, three app replicas, detached
make dev APP_REPLICAS=5      # Choose the number of app replicas
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
| Prometheus | http://localhost:19090 | - |
| Grafana | http://localhost:13000 | admin / admin |

Grafana starts with Prometheus configured as its default data source. In Explore,
query `up{job="url-shortener"}` for one series per replica, or `go_goroutines`
and `process_resident_memory_bytes` for runtime metrics. The app exposes `/metrics`.
Prometheus also scrapes Traefik's request and routing metrics.

Four dashboards are provisioned in the **URL Shortener** Grafana folder:

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

The default Prometheus registry already registers the Go and process collectors;
`promhttp.Handler()` exposes their `go_*` and `process_*` metrics alongside our
histograms. Prometheus captures these from every app replica. Runtime charts
show each replica separately; CPU is measured in cores (1 = one busy core).

Dashboard JSON lives in `dev/grafana/dashboards/`. Run `make dev` or `make up` to
apply Grafana provisioning/mount changes; subsequent JSON edits are picked up
automatically. Allow a few scrapes after generating traffic for rates to appear.

`/healthz` checks HTTP availability only. Startup verifies the database connection
with a 10-second timeout; the pool closes after HTTP shutdown.
The app receives `DATABASE_URL` pointing at `postgres:5432` inside the network.
`config.MustLoad()` requires `DATABASE_URL` and `LOG_LEVEL` before startup.
The log level is parsed using `slog` (`debug`, `info`, `warn`, or `error`);
missing values or an invalid level panic. Compose supplies `info` in normal mode
and `debug` in dev mode, enabling the store's duration/success debug logs.
Each app replica allows up to 10 open database connections (5 idle). Account for
the total across replicas when adjusting Postgres connection limits.

To run the app directly against the Compose database:

```sh
LOG_LEVEL=debug DATABASE_URL='postgres://shortener:shortener@localhost:55439/shortener?sslmode=disable' go run ./cmd
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
and Grafana each have one instance; it does not provide database failover or
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
