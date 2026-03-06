export type ConnectOptions = {
  endpoint: string;
  token: string;
};

export type OpenRTCClient = {
  connect(options: ConnectOptions): Promise<void>;
  disconnect(): Promise<void>;
};

export function createClient(): OpenRTCClient {
  let connected = false;

  return {
    async connect(_options: ConnectOptions): Promise<void> {
      connected = true;
    },
    async disconnect(): Promise<void> {
      connected = false;
    },
  };
}
