# Load testing

Run from `url-shortener` with Bun installed. Start the app, then run:

```sh
bun run loadtests/short-url.ts
# Or:
make load-test
```

No npm packages are required. The default run creates 100 URLs with sequential
`POST /short-url` requests, then sends 10,000 redirect requests using 50 concurrent
clients. Requests go through Traefik at `http://localhost:18080` to the app replicas.
Destinations use `https://example.com/load-test/<run-id>/<number>`; the script never
follows redirects or contacts those destinations. Created rows remain in the database.

To change the workload:

```sh
URL_COUNT=1000 REQUESTS=100000 VUS=100 MAX_DURATION=5m bun run loadtests/short-url.ts
# Or:
make load-test URL_COUNT=1000 REQUESTS=100000 VUS=100 MAX_DURATION=5m
```

`BASE_URL` overrides the target, for example `BASE_URL=http://localhost:18081`.
`VUS` controls concurrent clients, not requests per second. Each client sends its
next request as soon as the previous one finishes. Redirects cycle evenly through
the seeded URLs. Writes finish before reads begin; this is not a simultaneous
read/write workload.

Each request has a five-second timeout. URL creation has a two-minute deadline.
`MAX_DURATION` limits the redirect phase (default `2m`; accepts `ms`, `s`, or `m`).
Reaching it aborts in-flight requests and stops new requests. Progress prints every
five seconds. The final summary reports successful/failed/unstarted requests,
throughput, and latency percentiles including response bodies and failed attempts.

The command exits unsuccessfully if URL creation fails, any redirect does not
return 302 with the correct Location, the requested total is not completed, or
redirect p95 reaches `REDIRECT_P95_MS` (default 500 ms):

```sh
make load-test REDIRECT_P95_MS=200
```

For performance comparisons, use the compiled app:

```sh
make down
make up APP_REPLICAS=3
make load-test
```

Apply migrations manually before testing a fresh database (see
[local stack instructions](../dev/README.md#sql-migrations)). The load test uses the
currently running stack and does not start the app or switch modes. These are local
measurements: Bun, Docker, the database, proxy, and app share your machine. Traefik
access logs and dev-mode debug logs also add work.

View the [HTTP](http://localhost:13000/d/url-shortener-http),
[database](http://localhost:13000/d/url-shortener-db), and
[runtime](http://localhost:13000/d/url-shortener-runtime) dashboards during a run.
Client measurements print in the terminal; Prometheus collects server metrics.
