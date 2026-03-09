export type FetchResponseLike = {
  ok: boolean;
  status: number;
  json(): Promise<unknown>;
  text(): Promise<string>;
};

export type FetchLike = (
  input: string,
  init?: {
    method?: string;
    headers?: Record<string, string>;
    body?: string;
  },
) => Promise<FetchResponseLike>;

export type PublishRequest = {
  room: string;
  event: string;
  payload: unknown;
  excludeSenderConnId?: string;
  traceId?: string;
};

export type StatsResponse = {
  active_connections: number;
  active_rooms: number;
  joins_total: number;
  leaves_total: number;
  events_total: number;
  presence_updates_total: number;
  queue_overflows_total: number;
  admin_publishes_total: number;
};

type AdminClientOptions = {
  baseUrl: string;
  token: string;
  fetchImpl?: FetchLike;
};

type APIErrorPayload = {
  code?: string;
  message?: string;
  request_id?: string;
};

export class OpenRTCAdminError extends Error {
  readonly code: string;
  readonly status: number;
  readonly requestId: string | undefined;

  constructor(
    code: string,
    message: string,
    status: number,
    requestId?: string,
  ) {
    super(`${code}: ${message}`);
    this.name = 'OpenRTCAdminError';
    this.code = code;
    this.status = status;
    this.requestId = requestId;
  }
}

export type OpenRTCAdminClient = {
  publish(request: PublishRequest): Promise<void>;
  stats(): Promise<StatsResponse>;
};

export function createAdminClient(options: AdminClientOptions): OpenRTCAdminClient {
  const fetchImpl =
    options.fetchImpl ??
    ((globalThis as typeof globalThis & { fetch?: FetchLike }).fetch
      ? ((globalThis as typeof globalThis & { fetch: FetchLike }).fetch)
      : undefined);

  if (!fetchImpl) {
    throw new Error('fetch is not available; pass fetchImpl explicitly');
  }
  const resolvedFetch = fetchImpl;

  const baseUrl = options.baseUrl.replace(/\/+$/, '');

  async function requestJSON<T>(path: string, init?: Parameters<FetchLike>[1]): Promise<T> {
    const response = await resolvedFetch(`${baseUrl}${path}`, {
      ...init,
      headers: {
        Authorization: `Bearer ${options.token}`,
        'Content-Type': 'application/json',
        ...init?.headers,
      },
    });

    if (!response.ok) {
      const payload = (await response.json()) as APIErrorPayload;
      throw new OpenRTCAdminError(
        payload.code ?? 'INTERNAL',
        payload.message ?? 'request failed',
        response.status,
        payload.request_id,
      );
    }

    if (response.status === 202) {
      return undefined as T;
    }
    return (await response.json()) as T;
  }

  return {
    async publish(request: PublishRequest): Promise<void> {
      await requestJSON<void>('/v1/publish', {
        method: 'POST',
        body: JSON.stringify({
          room: request.room,
          event: request.event,
          payload: request.payload,
          exclude_sender_conn_id: request.excludeSenderConnId,
          trace_id: request.traceId,
        }),
      });
    },
    async stats(): Promise<StatsResponse> {
      return requestJSON<StatsResponse>('/v1/stats', { method: 'GET' });
    },
  };
}
