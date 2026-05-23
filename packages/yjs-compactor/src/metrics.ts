import { createServer, type Server } from "node:http";

export interface CompactorMetricsSnapshot {
  runsTotal: number;
  failuresTotal: number;
  roomsScannedTotal: number;
  roomsCompactedTotal: number;
  roomsSkippedTotal: Record<string, number>;
  updatesCompactedTotal: number;
  beforeBytesTotal: number;
  afterBytesTotal: number;
  lastRunTimestampSeconds: number;
  lastSuccessTimestampSeconds: number;
  consecutiveFailures: number;
}

export class CompactorMetrics {
  private readonly snapshot: CompactorMetricsSnapshot = {
    runsTotal: 0,
    failuresTotal: 0,
    roomsScannedTotal: 0,
    roomsCompactedTotal: 0,
    roomsSkippedTotal: {},
    updatesCompactedTotal: 0,
    beforeBytesTotal: 0,
    afterBytesTotal: 0,
    lastRunTimestampSeconds: 0,
    lastSuccessTimestampSeconds: 0,
    consecutiveFailures: 0,
  };

  markRun(now = new Date()): void {
    this.snapshot.runsTotal += 1;
    this.snapshot.lastRunTimestampSeconds = unixSeconds(now);
  }

  recordRoomsScanned(count: number): void {
    this.snapshot.roomsScannedTotal += count;
  }

  recordCompacted(compactedUpdates: number, beforeBytes: number, afterBytes: number, now = new Date()): void {
    this.snapshot.roomsCompactedTotal += 1;
    this.snapshot.updatesCompactedTotal += compactedUpdates;
    this.snapshot.beforeBytesTotal += beforeBytes;
    this.snapshot.afterBytesTotal += afterBytes;
    this.recordSuccess(now);
  }

  recordSkipped(reason: string, now = new Date()): void {
    this.snapshot.roomsSkippedTotal[reason] = (this.snapshot.roomsSkippedTotal[reason] ?? 0) + 1;
    this.recordSuccess(now);
  }

  recordFailure(consecutiveFailures: number, now = new Date()): void {
    this.snapshot.failuresTotal += 1;
    this.snapshot.consecutiveFailures = consecutiveFailures;
    this.snapshot.lastRunTimestampSeconds = Math.max(this.snapshot.lastRunTimestampSeconds, unixSeconds(now));
  }

  recordSuccess(now = new Date()): void {
    this.snapshot.consecutiveFailures = 0;
    this.snapshot.lastSuccessTimestampSeconds = unixSeconds(now);
  }

  getSnapshot(): CompactorMetricsSnapshot {
    return {
      ...this.snapshot,
      roomsSkippedTotal: { ...this.snapshot.roomsSkippedTotal },
    };
  }

  toPrometheus(): string {
    const snapshot = this.getSnapshot();
    const lines = [
      "# HELP openrtc_yjs_compactor_runs_total Total compactor scan loops started.",
      "# TYPE openrtc_yjs_compactor_runs_total counter",
      metric("openrtc_yjs_compactor_runs_total", snapshot.runsTotal),
      "# HELP openrtc_yjs_compactor_failures_total Total compactor scan or room failures.",
      "# TYPE openrtc_yjs_compactor_failures_total counter",
      metric("openrtc_yjs_compactor_failures_total", snapshot.failuresTotal),
      "# HELP openrtc_yjs_compactor_rooms_scanned_total Total rooms selected for compaction evaluation.",
      "# TYPE openrtc_yjs_compactor_rooms_scanned_total counter",
      metric("openrtc_yjs_compactor_rooms_scanned_total", snapshot.roomsScannedTotal),
      "# HELP openrtc_yjs_compactor_rooms_compacted_total Total rooms successfully compacted.",
      "# TYPE openrtc_yjs_compactor_rooms_compacted_total counter",
      metric("openrtc_yjs_compactor_rooms_compacted_total", snapshot.roomsCompactedTotal),
      "# HELP openrtc_yjs_compactor_updates_compacted_total Total Yjs update records compacted.",
      "# TYPE openrtc_yjs_compactor_updates_compacted_total counter",
      metric("openrtc_yjs_compactor_updates_compacted_total", snapshot.updatesCompactedTotal),
      "# HELP openrtc_yjs_compactor_bytes_before_total Total Yjs bytes before compaction.",
      "# TYPE openrtc_yjs_compactor_bytes_before_total counter",
      metric("openrtc_yjs_compactor_bytes_before_total", snapshot.beforeBytesTotal),
      "# HELP openrtc_yjs_compactor_bytes_after_total Total Yjs bytes after compaction.",
      "# TYPE openrtc_yjs_compactor_bytes_after_total counter",
      metric("openrtc_yjs_compactor_bytes_after_total", snapshot.afterBytesTotal),
      "# HELP openrtc_yjs_compactor_last_run_timestamp_seconds Unix timestamp for the last compactor loop start.",
      "# TYPE openrtc_yjs_compactor_last_run_timestamp_seconds gauge",
      metric("openrtc_yjs_compactor_last_run_timestamp_seconds", snapshot.lastRunTimestampSeconds),
      "# HELP openrtc_yjs_compactor_last_success_timestamp_seconds Unix timestamp for the last room success or skip.",
      "# TYPE openrtc_yjs_compactor_last_success_timestamp_seconds gauge",
      metric("openrtc_yjs_compactor_last_success_timestamp_seconds", snapshot.lastSuccessTimestampSeconds),
      "# HELP openrtc_yjs_compactor_consecutive_failures Current consecutive scan or room failures.",
      "# TYPE openrtc_yjs_compactor_consecutive_failures gauge",
      metric("openrtc_yjs_compactor_consecutive_failures", snapshot.consecutiveFailures),
      "# HELP openrtc_yjs_compactor_rooms_skipped_total Total rooms skipped by reason.",
      "# TYPE openrtc_yjs_compactor_rooms_skipped_total counter",
    ];
    for (const reason of Object.keys(snapshot.roomsSkippedTotal).sort()) {
      lines.push(metric("openrtc_yjs_compactor_rooms_skipped_total", snapshot.roomsSkippedTotal[reason] ?? 0, { reason }));
    }
    return `${lines.join("\n")}\n`;
  }
}

export async function startMetricsServer(metrics: CompactorMetrics, host: string, port: number): Promise<Server> {
  const server = createServer((request, response) => {
    if (request.url?.split("?")[0] !== "/metrics") {
      response.writeHead(404, { "Content-Type": "text/plain; charset=utf-8" });
      response.end("not found\n");
      return;
    }
    response.writeHead(200, { "Content-Type": "text/plain; version=0.0.4; charset=utf-8" });
    response.end(metrics.toPrometheus());
  });

  await new Promise<void>((resolve, reject) => {
    const onError = (error: Error) => {
      server.off("listening", onListening);
      reject(error);
    };
    const onListening = () => {
      server.off("error", onError);
      resolve();
    };
    server.once("error", onError);
    server.once("listening", onListening);
    server.listen(port, host);
  });
  return server;
}

function metric(name: string, value: number, labels?: Record<string, string>): string {
  const labelText = labels
    ? `{${Object.entries(labels)
        .map(([key, labelValue]) => `${key}="${escapeLabel(labelValue)}"`)
        .join(",")}}`
    : "";
  return `${name}${labelText} ${Number.isFinite(value) ? value : 0}`;
}

function escapeLabel(value: string): string {
  return value.replaceAll("\\", "\\\\").replaceAll("\n", "\\n").replaceAll('"', '\\"');
}

function unixSeconds(date: Date): number {
  return Math.floor(date.getTime() / 1000);
}
