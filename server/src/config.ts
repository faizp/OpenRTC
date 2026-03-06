export type RuntimeMode = 'single' | 'cluster';

export type LimitsConfig = {
  payloadMaxBytes: number;
  envelopeMaxBytes: number;
  roomsPerConnection: number;
  emitsPerSecond: number;
  outboundQueueDepth: number;
};

export type RuntimeConfig = {
  mode: RuntimeMode;
  nodeId: string;
  server: {
    host: string;
    port: number;
    wsPath: string;
  };
  redis?: {
    url: string;
    channelPrefix: string;
  };
  auth: {
    issuer: string;
    audience: string;
    jwksUrl: string;
  };
  adminAuth?: {
    issuer: string;
    audience: string;
  };
  tenant: {
    enforcePrefix: boolean;
    separator: string;
  };
  limits: LimitsConfig;
};

const DEFAULT_LIMITS: LimitsConfig = {
  payloadMaxBytes: 16 * 1024,
  envelopeMaxBytes: 20 * 1024,
  roomsPerConnection: 50,
  emitsPerSecond: 100,
  outboundQueueDepth: 256,
};

const DEFAULT_SERVER = {
  host: '0.0.0.0',
  port: 8080,
  wsPath: '/ws',
};

export class ConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ConfigError';
  }
}

function readString(env: NodeJS.ProcessEnv, key: string): string | undefined {
  const value = env[key]?.trim();
  return value ? value : undefined;
}

function readInt(env: NodeJS.ProcessEnv, key: string, defaultValue: number): number {
  const value = readString(env, key);
  if (!value) {
    return defaultValue;
  }

  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    throw new ConfigError(`${key} must be a positive integer`);
  }

  return parsed;
}

function readBool(env: NodeJS.ProcessEnv, key: string, defaultValue: boolean): boolean {
  const value = readString(env, key);
  if (!value) {
    return defaultValue;
  }

  if (value.toLowerCase() === 'true') {
    return true;
  }

  if (value.toLowerCase() === 'false') {
    return false;
  }

  throw new ConfigError(`${key} must be true or false`);
}

function requireString(env: NodeJS.ProcessEnv, key: string): string {
  const value = readString(env, key);
  if (!value) {
    throw new ConfigError(`${key} is required`);
  }

  return value;
}

export function loadConfigFromEnv(env: NodeJS.ProcessEnv): RuntimeConfig {
  const modeRaw = readString(env, 'OPENRTC_MODE') ?? 'single';
  if (modeRaw !== 'single' && modeRaw !== 'cluster') {
    throw new ConfigError('OPENRTC_MODE must be either single or cluster');
  }

  const nodeId = requireString(env, 'OPENRTC_NODE_ID');

  const auth = {
    issuer: requireString(env, 'OPENRTC_AUTH_ISSUER'),
    audience: requireString(env, 'OPENRTC_AUTH_AUDIENCE'),
    jwksUrl: requireString(env, 'OPENRTC_AUTH_JWKS_URL'),
  };

  const separator = readString(env, 'OPENRTC_TENANT_SEPARATOR') ?? ':';
  if (separator.length !== 1) {
    throw new ConfigError('OPENRTC_TENANT_SEPARATOR must be a single character');
  }

  const config: RuntimeConfig = {
    mode: modeRaw,
    nodeId,
    server: {
      host: readString(env, 'OPENRTC_SERVER_HOST') ?? DEFAULT_SERVER.host,
      port: readInt(env, 'OPENRTC_SERVER_PORT', DEFAULT_SERVER.port),
      wsPath: readString(env, 'OPENRTC_WS_PATH') ?? DEFAULT_SERVER.wsPath,
    },
    auth,
    tenant: {
      enforcePrefix: readBool(env, 'OPENRTC_TENANT_ENFORCE_PREFIX', true),
      separator,
    },
    limits: {
      payloadMaxBytes: readInt(env, 'OPENRTC_LIMIT_PAYLOAD_MAX_BYTES', DEFAULT_LIMITS.payloadMaxBytes),
      envelopeMaxBytes: readInt(
        env,
        'OPENRTC_LIMIT_ENVELOPE_MAX_BYTES',
        DEFAULT_LIMITS.envelopeMaxBytes,
      ),
      roomsPerConnection: readInt(
        env,
        'OPENRTC_LIMIT_ROOMS_PER_CONNECTION',
        DEFAULT_LIMITS.roomsPerConnection,
      ),
      emitsPerSecond: readInt(env, 'OPENRTC_LIMIT_EMITS_PER_SECOND', DEFAULT_LIMITS.emitsPerSecond),
      outboundQueueDepth: readInt(
        env,
        'OPENRTC_LIMIT_OUTBOUND_QUEUE_DEPTH',
        DEFAULT_LIMITS.outboundQueueDepth,
      ),
    },
  };

  const adminIssuer = readString(env, 'OPENRTC_ADMIN_AUTH_ISSUER');
  const adminAudience = readString(env, 'OPENRTC_ADMIN_AUTH_AUDIENCE');
  if (adminIssuer || adminAudience) {
    if (!adminIssuer || !adminAudience) {
      throw new ConfigError(
        'OPENRTC_ADMIN_AUTH_ISSUER and OPENRTC_ADMIN_AUTH_AUDIENCE must both be set',
      );
    }

    config.adminAuth = {
      issuer: adminIssuer,
      audience: adminAudience,
    };
  }

  if (config.mode === 'cluster') {
    const redisUrl = readString(env, 'OPENRTC_REDIS_URL');
    if (!redisUrl) {
      throw new ConfigError('OPENRTC_REDIS_URL is required when OPENRTC_MODE=cluster');
    }

    config.redis = {
      url: redisUrl,
      channelPrefix: readString(env, 'OPENRTC_REDIS_CHANNEL_PREFIX') ?? 'room:',
    };
  }

  if (config.limits.envelopeMaxBytes < config.limits.payloadMaxBytes) {
    throw new ConfigError('OPENRTC_LIMIT_ENVELOPE_MAX_BYTES must be >= OPENRTC_LIMIT_PAYLOAD_MAX_BYTES');
  }

  return config;
}
