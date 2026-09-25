type ShortURL = { code: string; url: string };

function positiveInteger(name: string, fallback: number): number {
  const value = Number(process.env[name] ?? fallback);
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new Error(`${name} must be a positive integer`);
  }
  return value;
}

function durationMS(value: string): number {
  const match = /^(\d+(?:\.\d+)?)(ms|s|m)$/.exec(value);
  if (!match) throw new Error('MAX_DURATION must look like 500ms, 30s, or 2m');
  const scale = { ms: 1, s: 1000, m: 60000 };
  const duration = Number(match[1]) * scale[match[2] as keyof typeof scale];
  if (!Number.isSafeInteger(duration) || duration <= 0 || duration > 2147483647) {
    throw new Error('MAX_DURATION must be a positive duration below 25 days');
  }
  return duration;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

async function main() {
  const baseURL = (process.env.BASE_URL ?? 'http://localhost:18080').replace(/\/+$/, '');
  const urlCount = positiveInteger('URL_COUNT', 100);
  const requests = positiveInteger('REQUESTS', 10000);
  const concurrency = Math.min(positiveInteger('VUS', 50), requests);
  const maxDuration = durationMS(process.env.MAX_DURATION ?? '2m');
  const p95Limit = positiveInteger('REDIRECT_P95_MS', 500);
  const links: ShortURL[] = [];
  const runID = Date.now().toString();
  const setupDeadline = AbortSignal.timeout(120000);

  console.log(`Creating ${urlCount} URLs at ${baseURL}...`);
  for (let i = 0; i < urlCount; i++) {
    const url = `https://example.com/load-test/${runID}/${i}`;
    const response = await fetch(`${baseURL}/short-url`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url }),
      redirect: 'manual',
      signal: AbortSignal.any([setupDeadline, AbortSignal.timeout(5000)]),
    });
    const body = await response.text();
    if (response.status !== 201) {
      throw new Error(`Create ${i + 1}/${urlCount}: expected 201, got ${response.status}: ${body.slice(0, 200)}`);
    }
    const link: unknown = JSON.parse(body);
    if (
      !link || typeof link !== 'object' ||
      !('code' in link) || typeof link.code !== 'string' || !/^[0-9A-Za-z]+$/.test(link.code) ||
      !('url' in link) || link.url !== url
    ) {
      throw new Error(`Create ${i + 1}/${urlCount}: invalid short URL response`);
    }
    links.push({ code: link.code, url });
  }

  console.log(`Created ${links.length} URLs. Sending ${requests} redirects with ${concurrency} concurrent clients...`);
  const deadline = AbortSignal.timeout(maxDuration);
  const latencies: number[] = [];
  const errors: string[] = [];
  let next = 0;
  let failures = 0;
  const start = performance.now();
  const progress = setInterval(() => {
    console.log(`Redirects: ${latencies.length}/${requests} finished, ${failures} failed`);
  }, 5000);

  async function worker() {
    while (next < requests && !deadline.aborted) {
      // Assigned before awaiting, so workers share the exact total without overlap.
      const index = next++;
      const link = links[index % links.length]!;
      const requestStart = performance.now();
      try {
        const response = await fetch(`${baseURL}/${link.code}`, {
          redirect: 'manual',
          signal: AbortSignal.any([deadline, AbortSignal.timeout(5000)]),
        });
        // Consume the body so connections can be reused and timing includes the response.
        await response.arrayBuffer();
        if (response.status !== 302) {
          throw new Error(`Expected 302, got ${response.status}`);
        }
        if (response.headers.get('Location') !== link.url) {
          throw new Error('Redirect Location does not match the original URL');
        }
      } catch (error) {
        failures++;
        if (errors.length < 5) errors.push(errorMessage(error));
      } finally {
        latencies.push(performance.now() - requestStart);
      }
    }
  }

  try {
    await Promise.all(Array.from({ length: concurrency }, worker));
  } finally {
    clearInterval(progress);
  }
  const elapsed = (performance.now() - start) / 1000;
  latencies.sort((a, b) => a - b);
  const percentile = (p: number): number => latencies[Math.ceil(p * latencies.length) - 1] ?? 0;
  const average = latencies.length ? latencies.reduce((sum, ms) => sum + ms, 0) / latencies.length : 0;

  console.table({
    'URLs created': links.length,
    'Redirects attempted': latencies.length,
    'Redirects successful': latencies.length - failures,
    'Redirects failed': failures,
    'Redirects not started': requests - latencies.length,
    'Elapsed (s)': elapsed.toFixed(2),
    'Requests/sec': (latencies.length / elapsed).toFixed(0),
    'Average (ms)': average.toFixed(2),
    'p50 (ms)': percentile(0.5).toFixed(2),
    'p95 (ms)': percentile(0.95).toFixed(2),
    'p99 (ms)': percentile(0.99).toFixed(2),
    'Max (ms)': (latencies.at(-1) ?? 0).toFixed(2),
  });
  for (const error of errors) console.error(`Request failure: ${error}`);
  if (latencies.length !== requests || failures > 0 || percentile(0.95) >= p95Limit) {
    throw new Error(`Load test failed: require ${requests} successful redirects and p95 < ${p95Limit}ms`);
  }
  console.log('Load test passed.');
}

main().catch((error: unknown) => {
  console.error(errorMessage(error));
  process.exitCode = 1;
});
