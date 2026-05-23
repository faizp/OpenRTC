import assert from "node:assert/strict";
import { parseArgs, runCompactorLoop, type RunnableCompactorStore } from "./runner.ts";
import { CompactorMetrics } from "./metrics.ts";
import type { YjsCompactionInput } from "./index.ts";

class FakeStore implements RunnableCompactorStore {
  rooms = ["room-1"];
  loads = 0;
  compacts = 0;
  failLoads = 0;

  constructor(private readonly input: YjsCompactionInput = { updates: [] }) {}

  async scanRooms(): Promise<string[]> {
    return this.rooms;
  }

  async load(): Promise<YjsCompactionInput> {
    this.loads += 1;
    if (this.failLoads > 0) {
      this.failLoads -= 1;
      throw new Error("load failed");
    }
    return this.input;
  }

  async compact(): Promise<void> {
    this.compacts += 1;
  }
}

{
  const options = parseArgs(["--once", "--room", "tenant-a:doc-1", "--room-retries", "3", "--metrics-port", "9102"], {
    OPENRTC_YJS_COMPACTOR_INTERVAL_MS: "10",
    OPENRTC_YJS_COMPACTOR_MIN_UPDATES: "2",
    OPENRTC_YJS_COMPACTOR_MIN_BYTES: "3",
    OPENRTC_YJS_COMPACTOR_METRICS_HOST: "127.0.0.1",
  });
  assert.equal(options.once, true);
  assert.equal(options.room, "tenant-a:doc-1");
  assert.equal(options.intervalMs, 10);
  assert.equal(options.minUpdates, 2);
  assert.equal(options.minBytes, 3);
  assert.equal(options.roomRetries, 3);
  assert.equal(options.metricsHost, "127.0.0.1");
  assert.equal(options.metricsPort, 9102);
}

assert.throws(() => parseArgs(["--interval-ms", "0"]), /positive integer/);
assert.throws(() => parseArgs(["--room-retries", "-1"]), /non-negative integer/);
assert.throws(() => parseArgs(["--metrics-port", "70000"]), /TCP port/);

{
  const metrics = new CompactorMetrics();
  const logs: string[] = [];
  const result = await runCompactorLoop({
    store: new FakeStore(),
    options: {
      once: true,
      intervalMs: 1,
      minUpdates: 1,
      minBytes: 1,
      roomRetries: 0,
      retryBackoffMs: 1,
      maxConsecutiveFailures: 3,
      metricsHost: "127.0.0.1",
    },
    metrics,
    log: (line) => logs.push(line),
  });
  const snapshot = result.metrics.getSnapshot();
  assert.equal(snapshot.runsTotal, 1);
  assert.equal(snapshot.roomsScannedTotal, 1);
  assert.equal(snapshot.roomsSkippedTotal["no-updates"], 1);
  assert.match(result.metrics.toPrometheus(), /openrtc_yjs_compactor_rooms_skipped_total\{reason="no-updates"\} 1/);
  assert.equal(logs.some((line) => line.includes('"skipped":true')), true);
}

{
  const store = new FakeStore();
  store.failLoads = 1;
  const sleeps: number[] = [];
  await runCompactorLoop({
    store,
    options: {
      once: true,
      intervalMs: 1,
      minUpdates: 1,
      minBytes: 1,
      roomRetries: 1,
      retryBackoffMs: 25,
      maxConsecutiveFailures: 3,
      metricsHost: "127.0.0.1",
    },
    sleep: async (ms) => {
      sleeps.push(ms);
    },
  });
  assert.equal(store.loads, 2);
  assert.deepEqual(sleeps, [25]);
}

{
  const store = new FakeStore();
  store.rooms = ["room-1", "room-2"];
  store.failLoads = 2;
  const metrics = new CompactorMetrics();
  await assert.rejects(() => runCompactorLoop({
    store,
    options: {
      once: false,
      intervalMs: 1,
      minUpdates: 1,
      minBytes: 1,
      roomRetries: 0,
      retryBackoffMs: 1,
      maxConsecutiveFailures: 2,
      metricsHost: "127.0.0.1",
    },
    metrics,
  }), /load failed/);
  const snapshot = metrics.getSnapshot();
  assert.equal(snapshot.failuresTotal, 2);
  assert.equal(snapshot.consecutiveFailures, 2);
}

{
  const store = new FakeStore();
  store.failLoads = 1;
  const metrics = new CompactorMetrics();
  await assert.rejects(() => runCompactorLoop({
    store,
    options: {
      once: true,
      intervalMs: 1,
      minUpdates: 1,
      minBytes: 1,
      roomRetries: 0,
      retryBackoffMs: 1,
      maxConsecutiveFailures: 3,
      metricsHost: "127.0.0.1",
    },
    metrics,
  }), /load failed/);
  assert.equal(metrics.getSnapshot().failuresTotal, 1);
}
