import {
  createContext,
  createElement,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import {
  OpenRTCClient,
  type ConnectionStatus,
  type JoinOptions,
  type OpenRTCEvent,
  type OpenRTCRoomState,
  type PresencePeer,
  type PresenceState,
} from "@openrtc/client";

const OpenRTCContext = createContext<OpenRTCClient | null>(null);

export interface OpenRTCProviderProps {
  client: OpenRTCClient;
  children: ReactNode;
}

export function OpenRTCProvider(props: OpenRTCProviderProps) {
  return createElement(OpenRTCContext.Provider, { value: props.client }, props.children);
}

export function useOpenRTC(): OpenRTCClient {
  const client = useContext(OpenRTCContext);
  if (!client) {
    throw new Error("useOpenRTC must be used inside OpenRTCProvider");
  }
  return client;
}

export function useConnectionStatus(): ConnectionStatus {
  const client = useOpenRTC();
  const [status, setStatus] = useState<ConnectionStatus>(client.status);

  useEffect(() => client.on("status", setStatus), [client]);

  return status;
}

export function useRoom(room: string, options: JoinOptions = {}): OpenRTCRoomState {
  const client = useOpenRTC();
  const [state, setState] = useState<OpenRTCRoomState>(() => client.getRoomState(room));
  const limit = options.limit;
  const cursor = options.cursor;

  useEffect(() => {
    client.join(room, {
      ...(limit !== undefined ? { limit } : {}),
      ...(cursor !== undefined ? { cursor } : {}),
    });
    setState(client.getRoomState(room));

    const syncRoom = (next: OpenRTCRoomState): void => {
      if (next.room === room) {
        setState(next);
      }
    };
    const syncPresence = (event: { room: string }): void => {
      if (event.room === room) {
        setState(client.getRoomState(room));
      }
    };

    const offRoom = client.on("room", syncRoom);
    const offPresence = client.on("presence", syncPresence);
    return () => {
      offRoom();
      offPresence();
      client.leave(room);
    };
  }, [client, room, limit, cursor]);

  return state;
}

export function useOthers(room: string): PresencePeer[] {
  return useRoom(room).others;
}

export function useBroadcastEvent(room: string): (event: string, payload: unknown, traceId?: string) => void {
  const client = useOpenRTC();
  return useCallback(
    (event: string, payload: unknown, traceId?: string) => {
      client.broadcast(room, event, payload, traceId);
    },
    [client, room],
  );
}

export function useUpdateMyPresence(room: string): (state: PresenceState) => void {
  const client = useOpenRTC();
  return useCallback(
    (state: PresenceState) => {
      client.updatePresence(room, state);
    },
    [client, room],
  );
}

export function useRoomEvents(room: string): OpenRTCEvent[] {
  const client = useOpenRTC();
  const [events, setEvents] = useState<OpenRTCEvent[]>([]);

  useEffect(() => {
    return client.on("event", (event) => {
      if (event.room === room) {
        setEvents((current) => [...current, event]);
      }
    });
  }, [client, room]);

  return useMemo(() => events, [events]);
}
