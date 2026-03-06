export type ErrorCode =
  | 'AUTH_INVALID'
  | 'AUTH_EXPIRED'
  | 'ROOM_FORBIDDEN'
  | 'RATE_LIMITED'
  | 'PAYLOAD_TOO_LARGE'
  | 'QUEUE_OVERFLOW'
  | 'BAD_REQUEST'
  | 'INTERNAL';

export type ErrorDescriptor = {
  code: ErrorCode;
  httpStatus: number;
  wsCloseCode: number;
  retryable: boolean;
};

const CATALOG: Record<ErrorCode, ErrorDescriptor> = {
  AUTH_INVALID: { code: 'AUTH_INVALID', httpStatus: 401, wsCloseCode: 4001, retryable: false },
  AUTH_EXPIRED: { code: 'AUTH_EXPIRED', httpStatus: 401, wsCloseCode: 4002, retryable: true },
  ROOM_FORBIDDEN: { code: 'ROOM_FORBIDDEN', httpStatus: 403, wsCloseCode: 4403, retryable: false },
  RATE_LIMITED: { code: 'RATE_LIMITED', httpStatus: 429, wsCloseCode: 4408, retryable: true },
  PAYLOAD_TOO_LARGE: {
    code: 'PAYLOAD_TOO_LARGE',
    httpStatus: 413,
    wsCloseCode: 4409,
    retryable: false,
  },
  QUEUE_OVERFLOW: { code: 'QUEUE_OVERFLOW', httpStatus: 503, wsCloseCode: 4410, retryable: true },
  BAD_REQUEST: { code: 'BAD_REQUEST', httpStatus: 400, wsCloseCode: 4400, retryable: false },
  INTERNAL: { code: 'INTERNAL', httpStatus: 500, wsCloseCode: 4500, retryable: true },
};

export function getErrorDescriptor(code: ErrorCode): ErrorDescriptor {
  return CATALOG[code];
}

export function toWsCloseReason(code: ErrorCode): string {
  return `${CATALOG[code].wsCloseCode} ${code}`;
}
