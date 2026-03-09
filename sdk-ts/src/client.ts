export type OpenRTCClientState = 'disconnected' | 'connecting' | 'connected';

export type ConnectOptions = {
  endpoint: string;
  token: string;
};

export type JoinOptions = {
  limit?: number;
  cursor?: string;
};

export type EmitOptions = {
  traceId?: string;
};

export type WebSocketMessageEvent = {
  data: string;
};

export type WebSocketLike = {
  send(data: string): void;
  close(code?: number, reason?: string): void;
  addEventListener(
    type: 'open' | 'message' | 'error' | 'close',
    listener: (event: unknown) => void,
  ): void;
};

export type WebSocketFactory = (url: string) => WebSocketLike;

export type OpenRTCEvent = {
  room: string;
  event: string;
  payload: unknown;
  traceId?: string;
};

export type PresenceEvent = {
  room: string;
  connId: string;
  state: unknown;
};

export type OpenRTCClient = {
  connect(options: ConnectOptions): Promise<void>;
  disconnect(code?: number, reason?: string): Promise<void>;
  join(room: string, options?: JoinOptions): Promise<void>;
  leave(room: string): Promise<void>;
  emit(room: string, event: string, payload: unknown, options?: EmitOptions): Promise<void>;
  presence: {
    set(room: string, state: Record<string, unknown>): Promise<void>;
  };
  onLifecycle(handler: (state: OpenRTCClientState) => void): void;
  onEvent(handler: (event: OpenRTCEvent) => void): void;
  onPresence(handler: (event: PresenceEvent) => void): void;
  onError(handler: (payload: unknown) => void): void;
  getState(): OpenRTCClientState;
};

type ClientOptions = {
  socketFactory?: WebSocketFactory;
};

type ServerEnvelope = {
  t: string;
  room?: string;
  event?: string;
  payload?: unknown;
  meta?: {
    trace_id?: string;
  };
};

export function createClient(options: ClientOptions = {}): OpenRTCClient {
  const lifecycleHandlers: Array<(state: OpenRTCClientState) => void> = [];
  const eventHandlers: Array<(event: OpenRTCEvent) => void> = [];
  const presenceHandlers: Array<(event: PresenceEvent) => void> = [];
  const errorHandlers: Array<(payload: unknown) => void> = [];
  const socketFactory = options.socketFactory ?? defaultSocketFactory;

  let socket: WebSocketLike | undefined;
  let state: OpenRTCClientState = 'disconnected';

  function setState(next: OpenRTCClientState): void {
    state = next;
    for (const handler of lifecycleHandlers) {
      handler(next);
    }
  }

  function requireSocket(): WebSocketLike {
    if (!socket || state !== 'connected') {
      throw new Error('client is not connected');
    }
    return socket;
  }

  function send(payload: Record<string, unknown>): void {
    requireSocket().send(JSON.stringify(payload));
  }

  return {
    async connect(connectOptions: ConnectOptions): Promise<void> {
      if (state !== 'disconnected') {
        return;
      }

      const url = new URL(connectOptions.endpoint);
      url.searchParams.set('token', connectOptions.token);
      socket = socketFactory(url.toString());
      setState('connecting');

      await new Promise<void>((resolve, reject) => {
        let resolved = false;

        socket?.addEventListener('open', () => {
          if (!resolved) {
            resolved = true;
            setState('connected');
            resolve();
          }
        });

        socket?.addEventListener('close', () => {
          setState('disconnected');
          if (!resolved) {
            reject(new Error('socket closed before open'));
          }
        });

        socket?.addEventListener('error', (event) => {
          if (!resolved) {
            reject(event instanceof Error ? event : new Error('socket error'));
            return;
          }
          for (const handler of errorHandlers) {
            handler(event);
          }
        });

        socket?.addEventListener('message', (event) => {
          const message = event as WebSocketMessageEvent;
          const payload = JSON.parse(message.data) as ServerEnvelope;
          switch (payload.t) {
            case 'EVENT':
              for (const handler of eventHandlers) {
                const eventPayload: OpenRTCEvent = {
                  room: payload.room ?? '',
                  event: payload.event ?? '',
                  payload: payload.payload,
                };
                if (payload.meta?.trace_id !== undefined) {
                  eventPayload.traceId = payload.meta.trace_id;
                }
                handler(eventPayload);
              }
              break;
            case 'PRESENCE': {
              const presencePayload = payload.payload as { conn_id?: string; state?: unknown } | undefined;
              for (const handler of presenceHandlers) {
                handler({
                  room: payload.room ?? '',
                  connId: presencePayload?.conn_id ?? '',
                  state: presencePayload?.state,
                });
              }
              break;
            }
            case 'ERROR':
              for (const handler of errorHandlers) {
                handler(payload.payload);
              }
              break;
            default:
              break;
          }
        });
      });
    },
    async disconnect(code?: number, reason?: string): Promise<void> {
      if (!socket) {
        setState('disconnected');
        return;
      }
      socket.close(code, reason);
      socket = undefined;
      setState('disconnected');
    },
    async join(room: string, joinOptions?: JoinOptions): Promise<void> {
      send({
        t: 'JOIN',
        id: `join:${room}`,
        room,
        meta: {
          limit: joinOptions?.limit,
          cursor: joinOptions?.cursor,
        },
      });
    },
    async leave(room: string): Promise<void> {
      send({
        t: 'LEAVE',
        id: `leave:${room}`,
        room,
      });
    },
    async emit(room: string, event: string, payload: unknown, emitOptions?: EmitOptions): Promise<void> {
      send({
        t: 'EMIT',
        id: `emit:${room}:${event}`,
        room,
        event,
        payload,
        meta: {
          trace_id: emitOptions?.traceId,
        },
      });
    },
    presence: {
      async set(room: string, statePayload: Record<string, unknown>): Promise<void> {
        send({
          t: 'PRESENCE_SET',
          id: `presence:${room}`,
          room,
          payload: statePayload,
        });
      },
    },
    onLifecycle(handler: (state: OpenRTCClientState) => void): void {
      lifecycleHandlers.push(handler);
    },
    onEvent(handler: (event: OpenRTCEvent) => void): void {
      eventHandlers.push(handler);
    },
    onPresence(handler: (event: PresenceEvent) => void): void {
      presenceHandlers.push(handler);
    },
    onError(handler: (payload: unknown) => void): void {
      errorHandlers.push(handler);
    },
    getState(): OpenRTCClientState {
      return state;
    },
  };
}

function defaultSocketFactory(url: string): WebSocketLike {
  const ctor = (globalThis as typeof globalThis & {
    WebSocket?: new (input: string) => WebSocketLike;
  }).WebSocket;
  if (!ctor) {
    throw new Error('WebSocket is not available; pass socketFactory explicitly');
  }
  return new ctor(url);
}
