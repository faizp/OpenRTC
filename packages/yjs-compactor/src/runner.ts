import type { Server } from "node:http";
import { compactRoom, type CompactRoomOutcome, type YjsCompactionStore } from "./index.ts";
import { CompactorMetrics, startMetricsServer } from "./metrics.ts";
import { OpenRTCYjsRedisStore } from "./redis-store.ts";

export interface CompactorCLIOptions {
  once: boolean;
  room?: string | undefined;
  intervalMs: number;
  minUpdates: number;
  minBytes: number;
  roomRetries: number;
  retryBackoffMs: number;
  maxConsecutiveFailures: number;
  metricsHost: string;
  metricsPort?: number | undefined;
}

export interface RunnableCompactorStore extends YjsCompactionStore {
  scanRooms(): Promise<string[]>;
  close?(): Promise<void>;
}

export interface RunCompactorLoopOptions {
  store: RunnableCompactorStore;
  options: CompactorCLIOptions;
  metrics?: CompactorMetrics | undefined;
  log?: ((line: string) => void) | undefined;
  sleep?: ((ms: number) => Promise<void>) | undefined;
  shouldStop?: (() => boolean) | undefined;
}

export interface RunCompactorLoopResult {
  metrics: CompactorMetrics;
}

export async function runCLI(args: string[], env: NodeJS.ProcessEnv = process.env): Promise<void> {
  const options = parseArgs(args, env);
  const redisUrl = env["OPENRTC_REDIS_URL"];
  if (!redisUrl) {
    throw new Error("OPENRTC_REDIS_URL is required");
  }

  const store = await OpenRTCYjsRedisStore.connect({
    redisUrl,
    onError: (error) => {
      console.error(JSON.stringify({ level: "error", msg: "redis client error", error: error.message }));
    },
  });
  const metrics = new CompactorMetrics();
  let metricsServer: Server | undefined;
  let shuttingDown = false;
  const stop = () => {
    shuttingDown = true;
  };
  process.on("SIGTERM", stop);
  process.on("SIGINT", stop);

  try {
    if (options.metricsPort !== undefined) {
      metricsServer = await startMetricsServer(metrics, options.metricsHost, options.metricsPort);
      console.log(JSON.stringify({ level: "info", msg: "metrics server started", host: options.metricsHost, port: options.metricsPort }));
    }
    await runCompactorLoop({
      store,
      options,
      metrics,
      log: (line) => console.log(line),
      shouldStop: () => shuttingDown,
    });
  } finally {
    process.off("SIGTERM", stop);
    process.off("SIGINT", stop);
    if (metricsServer !== undefined) {
      await new Promise<void>((resolve, reject) => {
        metricsServer?.close((error) => (error ? reject(error) : resolve()));
      });
    }
    await store.close();
  }
}

export function parseArgs(args: string[], env: NodeJS.ProcessEnv = process.env): CompactorCLIOptions {
  const options: CompactorCLIOptions = {
    once: false,
    intervalMs: envPositiveInt(env, "OPENRTC_YJS_COMPACTOR_INTERVAL_MS", 60_000),
    minUpdates: envPositiveInt(env, "OPENRTC_YJS_COMPACTOR_MIN_UPDATES", 500),
    minBytes: envPositiveInt(env, "OPENRTC_YJS_COMPACTOR_MIN_BYTES", 1024 * 1024),
    roomRetries: envNonNegativeInt(env, "OPENRTC_YJS_COMPACTOR_ROOM_RETRIES", 2),
    retryBackoffMs: envPositiveInt(env, "OPENRTC_YJS_COMPACTOR_RETRY_BACKOFF_MS", 1_000),
    maxConsecutiveFailures: envPositiveInt(env, "OPENRTC_YJS_COMPACTOR_MAX_CONSECUTIVE_FAILURES", 10),
    metricsHost: env["OPENRTC_YJS_COMPACTOR_METRICS_HOST"] ?? "0.0.0.0",
    metricsPort: envOptionalPort(env, "OPENRTC_YJS_COMPACTOR_METRICS_PORT"),
  };

  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index];
    switch (arg) {
      case "--once":
        options.once = true;
        break;
      case "--room":
        options.room = requireValue(args, ++index, arg);
        break;
      case "--interval-ms":
        options.intervalMs = parsePositiveInt(requireValue(args, ++index, arg), arg);
        break;
      case "--min-updates":
        options.minUpdates = parsePositiveInt(requireValue(args, ++index, arg), arg);
        break;
      case "--min-bytes":
        options.minBytes = parsePositiveInt(requireValue(args, ++index, arg), arg);
        break;
      case "--room-retries":
        options.roomRetries = parseNonNegativeInt(requireValue(args, ++index, arg), arg);
        break;
      case "--retry-backoff-ms":
        options.retryBackoffMs = parsePositiveInt(requireValue(args, ++index, arg), arg);
        break;
      case "--max-consecutive-failures":
        options.maxConsecutiveFailures = parsePositiveInt(requireValue(args, ++index, arg), arg);
        break;
      case "--metrics-host":
        options.metricsHost = requireValue(args, ++index, arg);
        break;
      case "--metrics-port":
        options.metricsPort = parsePort(requireValue(args, ++index, arg), arg);
        break;
      default:
        throw new Error(`unsupported argument: ${arg}`);
    }
  }

  return options;
}

export async function runCompactorLoop({
  store,
  options,
  metrics = new CompactorMetrics(),
  log = () => undefined,
  sleep = defaultSleep,
  shouldStop = () => false,
}: RunCompactorLoopOptions): Promise<RunCompactorLoopResult> {
  let consecutiveFailures = 0;
  do {
    metrics.markRun();
    let rooms: string[];
    try {
      rooms = options.room ? [options.room] : await store.scanRooms();
      metrics.recordRoomsScanned(rooms.length);
    } catch (error) {
      consecutiveFailures += 1;
      metrics.recordFailure(consecutiveFailures);
      log(JSON.stringify({ level: "error", msg: "compactor scan failed", error: errorMessage(error) }));
      if (options.once || consecutiveFailures >= options.maxConsecutiveFailures) {
        throw error;
      }
      if (!shouldStop()) {
        await sleep(options.intervalMs);
      }
      continue;
    }

    for (const room of rooms) {
      try {
        const outcome = await compactRoomWithRetry(store, room, options, sleep, log);
        consecutiveFailures = 0;
        recordOutcome(metrics, outcome);
        log(JSON.stringify(outcome));
      } catch (error) {
        consecutiveFailures += 1;
        metrics.recordFailure(consecutiveFailures);
        log(JSON.stringify({ level: "error", msg: "room compaction failed", room, error: errorMessage(error) }));
        if (options.once || consecutiveFailures >= options.maxConsecutiveFailures) {
          throw error;
        }
      }
    }
    if (options.once) {
      break;
    }
    if (!shouldStop()) {
      await sleep(options.intervalMs);
    }
  } while (!shouldStop());

  return { metrics };
}

async function compactRoomWithRetry(
  store: YjsCompactionStore,
  room: string,
  options: CompactorCLIOptions,
  sleep: (ms: number) => Promise<void>,
  log: (line: string) => void,
): Promise<CompactRoomOutcome> {
  let attempt = 0;
  for (;;) {
    try {
      return await compactRoom(store, room, {
        minUpdates: options.minUpdates,
        minBytes: options.minBytes,
      });
    } catch (error) {
      if (attempt >= options.roomRetries) {
        throw error;
      }
      attempt += 1;
      log(JSON.stringify({ level: "warn", msg: "retrying room compaction", room, attempt, error: errorMessage(error) }));
      await sleep(options.retryBackoffMs);
    }
  }
}

function recordOutcome(metrics: CompactorMetrics, outcome: CompactRoomOutcome): void {
  if (outcome.skipped === true) {
    metrics.recordSkipped(outcome.reason);
    return;
  }
  metrics.recordCompacted(outcome.compactedUpdates, outcome.beforeBytes, outcome.afterBytes);
}

function requireValue(args: string[], index: number, flag: string): string {
  const value = args[index];
  if (!value) {
    throw new Error(`${flag} requires a value`);
  }
  return value;
}

function envPositiveInt(env: NodeJS.ProcessEnv, name: string, fallback: number): number {
  const raw = env[name];
  if (raw === undefined || raw === "") {
    return fallback;
  }
  return parsePositiveInt(raw, name);
}

function envNonNegativeInt(env: NodeJS.ProcessEnv, name: string, fallback: number): number {
  const raw = env[name];
  if (raw === undefined || raw === "") {
    return fallback;
  }
  return parseNonNegativeInt(raw, name);
}

function envOptionalPort(env: NodeJS.ProcessEnv, name: string): number | undefined {
  const raw = env[name];
  if (raw === undefined || raw === "") {
    return undefined;
  }
  return parsePort(raw, name);
}

function parsePositiveInt(value: string, flag: string): number {
  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    throw new Error(`${flag} must be a positive integer`);
  }
  return parsed;
}

function parseNonNegativeInt(value: string, flag: string): number {
  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed) || parsed < 0) {
    throw new Error(`${flag} must be a non-negative integer`);
  }
  return parsed;
}

function parsePort(value: string, flag: string): number {
  const parsed = parsePositiveInt(value, flag);
  if (parsed > 65_535) {
    throw new Error(`${flag} must be a TCP port`);
  }
  return parsed;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

async function defaultSleep(ms: number): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, ms));
}
