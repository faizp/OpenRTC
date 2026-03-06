export const MAX_PAYLOAD_BYTES_DEFAULT = 16 * 1024;
export const MAX_ENVELOPE_BYTES_DEFAULT = 20 * 1024;

export type ClientMessageType = 'JOIN' | 'LEAVE' | 'EMIT' | 'PRESENCE_SET';

type MessageBase = {
  id: string;
  room: string;
};

export type JoinMessage = MessageBase & {
  t: 'JOIN';
  meta?: {
    limit?: number;
    cursor?: string;
  };
};

export type LeaveMessage = MessageBase & {
  t: 'LEAVE';
};

export type EmitMessage = MessageBase & {
  t: 'EMIT';
  event: string;
  payload: unknown;
  meta?: {
    trace_id?: string;
  };
};

export type PresenceSetMessage = MessageBase & {
  t: 'PRESENCE_SET';
  payload: Record<string, unknown>;
};

export type ClientMessage = JoinMessage | LeaveMessage | EmitMessage | PresenceSetMessage;

const COMMON_KEYS = new Set(['t', 'id', 'room', 'event', 'payload', 'meta']);
const JOIN_META_KEYS = new Set(['limit', 'cursor']);
const EMIT_META_KEYS = new Set(['trace_id']);

export type ParseErrorCode =
  | 'BAD_REQUEST'
  | 'PAYLOAD_TOO_LARGE'
  | 'ROOM_FORBIDDEN'
  | 'RATE_LIMITED'
  | 'QUEUE_OVERFLOW'
  | 'INTERNAL';

export type ParseResult =
  | ParseSuccess
  | ParseFailure;

type ParseSuccess = {
  ok: true;
  value: ClientMessage;
};

type ParseFailure = {
  ok: false;
  code: ParseErrorCode;
  message: string;
};

export type ParseOptions = {
  maxEnvelopeBytes?: number;
  maxPayloadBytes?: number;
  tenantPrefix?: string;
};

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function byteLength(value: string): number {
  return Buffer.byteLength(value, 'utf8');
}

function fail(code: ParseErrorCode, message: string): ParseFailure {
  return { ok: false, code, message };
}

function hasOnlyKeys(obj: Record<string, unknown>, allowed: Set<string>): boolean {
  return Object.keys(obj).every((key) => allowed.has(key));
}

function isRoomAllowed(room: string, tenantPrefix?: string): boolean {
  if (!tenantPrefix) {
    return true;
  }

  return room.startsWith(tenantPrefix);
}

function isString(value: unknown): value is string {
  return typeof value === 'string';
}

function isNonEmptyString(value: unknown): value is string {
  return isString(value) && value.length > 0;
}

function isIntegerInRange(value: unknown, min: number, max: number): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= min && value <= max;
}

function parseAndCheckEnvelope(
  raw: string,
  maxEnvelopeBytes: number,
): { ok: true; envelope: Record<string, unknown> } | ParseFailure {
  if (byteLength(raw) > maxEnvelopeBytes) {
    return fail('PAYLOAD_TOO_LARGE', 'Envelope exceeds max size');
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return fail('BAD_REQUEST', 'Message must be valid JSON');
  }

  if (!isObject(parsed)) {
    return fail('BAD_REQUEST', 'Message must be a JSON object');
  }

  if (!hasOnlyKeys(parsed, COMMON_KEYS)) {
    return fail('BAD_REQUEST', 'Envelope includes unsupported fields');
  }

  return { ok: true, envelope: parsed };
}

function validateMessageShape(
  envelope: Record<string, unknown>,
  maxPayloadBytes: number,
  tenantPrefix?: string,
): ParseResult {
  if (!isNonEmptyString(envelope.t)) {
    return fail('BAD_REQUEST', 'Message type `t` is required');
  }

  if (!isNonEmptyString(envelope.id)) {
    return fail('BAD_REQUEST', 'Message id `id` is required');
  }

  const messageType = envelope.t;
  if (messageType !== 'JOIN' && messageType !== 'LEAVE' && messageType !== 'EMIT' && messageType !== 'PRESENCE_SET') {
    return fail('BAD_REQUEST', `Unsupported message type: ${messageType}`);
  }

  if (!isNonEmptyString(envelope.room)) {
    return fail('BAD_REQUEST', 'Room is required for this message type');
  }

  if (!isRoomAllowed(envelope.room, tenantPrefix)) {
    return fail('ROOM_FORBIDDEN', 'Room is outside the allowed tenant prefix');
  }

  if (envelope.payload !== undefined) {
    const payloadBytes = byteLength(JSON.stringify(envelope.payload));
    if (payloadBytes > maxPayloadBytes) {
      return fail('PAYLOAD_TOO_LARGE', 'Payload exceeds max size');
    }
  }

  if (envelope.meta !== undefined && !isObject(envelope.meta)) {
    return fail('BAD_REQUEST', 'Meta must be an object when present');
  }

  switch (messageType) {
    case 'JOIN': {
      if (envelope.meta !== undefined && !hasOnlyKeys(envelope.meta, JOIN_META_KEYS)) {
        return fail('BAD_REQUEST', 'JOIN meta includes unsupported fields');
      }

      const limit = envelope.meta?.limit;
      const cursor = envelope.meta?.cursor;

      if (limit !== undefined && !isIntegerInRange(limit, 1, 200)) {
        return fail('BAD_REQUEST', 'JOIN meta.limit must be an integer between 1 and 200');
      }

      if (cursor !== undefined && !isNonEmptyString(cursor)) {
        return fail('BAD_REQUEST', 'JOIN meta.cursor must be a non-empty string');
      }

      const value: JoinMessage = {
        t: 'JOIN',
        id: envelope.id,
        room: envelope.room,
      };

      if (envelope.meta !== undefined) {
        const meta: JoinMessage['meta'] = {};
        if (limit !== undefined) {
          meta.limit = limit;
        }
        if (cursor !== undefined) {
          meta.cursor = cursor;
        }
        value.meta = meta;
      }

      return { ok: true, value };
    }
    case 'LEAVE':
      return {
        ok: true,
        value: {
          t: 'LEAVE',
          id: envelope.id,
          room: envelope.room,
        },
      };
    case 'EMIT': {
      if (!isNonEmptyString(envelope.event)) {
        return fail('BAD_REQUEST', 'EMIT requires `event`');
      }

      if (envelope.payload === undefined) {
        return fail('BAD_REQUEST', 'EMIT requires `payload`');
      }

      if (envelope.meta !== undefined && !hasOnlyKeys(envelope.meta, EMIT_META_KEYS)) {
        return fail('BAD_REQUEST', 'EMIT meta includes unsupported fields');
      }

      const traceId = envelope.meta?.trace_id;
      if (traceId !== undefined && !isNonEmptyString(traceId)) {
        return fail('BAD_REQUEST', 'EMIT meta.trace_id must be a non-empty string');
      }

      const value: EmitMessage = {
        t: 'EMIT',
        id: envelope.id,
        room: envelope.room,
        event: envelope.event,
        payload: envelope.payload,
      };

      if (traceId !== undefined) {
        value.meta = { trace_id: traceId };
      }

      return { ok: true, value };
    }
    case 'PRESENCE_SET': {
      if (!isObject(envelope.payload)) {
        return fail('BAD_REQUEST', 'PRESENCE_SET requires object payload');
      }

      return {
        ok: true,
        value: {
          t: 'PRESENCE_SET',
          id: envelope.id,
          room: envelope.room,
          payload: envelope.payload,
        },
      };
    }
  }
}

export function parseClientMessage(raw: string, options: ParseOptions = {}): ParseResult {
  const maxEnvelopeBytes = options.maxEnvelopeBytes ?? MAX_ENVELOPE_BYTES_DEFAULT;
  const maxPayloadBytes = options.maxPayloadBytes ?? MAX_PAYLOAD_BYTES_DEFAULT;

  const parsedEnvelope = parseAndCheckEnvelope(raw, maxEnvelopeBytes);
  if (!parsedEnvelope.ok) {
    return parsedEnvelope;
  }

  return validateMessageShape(parsedEnvelope.envelope, maxPayloadBytes, options.tenantPrefix);
}
