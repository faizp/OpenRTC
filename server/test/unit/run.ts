import assert from 'node:assert/strict';

import { ConfigError, loadConfigFromEnv } from '../../src/config.ts';
import { getErrorDescriptor, toWsCloseReason } from '../../src/errors.ts';
import { parseClientMessage } from '../../src/protocol.ts';

const joinResult = parseClientMessage(
  JSON.stringify({
    t: 'JOIN',
    id: 'req-1',
    room: 'tenant-a:room-1',
    meta: { limit: 50 },
  }),
  { tenantPrefix: 'tenant-a:' },
);
assert.equal(joinResult.ok, true);

const badFieldResult = parseClientMessage(
  JSON.stringify({
    t: 'LEAVE',
    id: 'req-1',
    room: 'tenant-a:room-1',
    unexpected: true,
  }),
);
assert.deepEqual(badFieldResult, {
  ok: false,
  code: 'BAD_REQUEST',
  message: 'Envelope includes unsupported fields',
});

assert.deepEqual(getErrorDescriptor('BAD_REQUEST'), {
  code: 'BAD_REQUEST',
  httpStatus: 400,
  wsCloseCode: 4400,
  retryable: false,
});
assert.equal(toWsCloseReason('QUEUE_OVERFLOW'), '4410 QUEUE_OVERFLOW');

const baseEnv = {
  OPENRTC_NODE_ID: 'node-a',
  OPENRTC_AUTH_ISSUER: 'https://issuer.example.com',
  OPENRTC_AUTH_AUDIENCE: 'openrtc-clients',
  OPENRTC_AUTH_JWKS_URL: 'https://issuer.example.com/jwks.json',
};

const singleMode = loadConfigFromEnv(baseEnv);
assert.equal(singleMode.mode, 'single');
assert.equal(singleMode.redis, undefined);

assert.throws(
  () =>
    loadConfigFromEnv({
      ...baseEnv,
      OPENRTC_MODE: 'cluster',
    }),
  new ConfigError('OPENRTC_REDIS_URL is required when OPENRTC_MODE=cluster'),
);

const clusterMode = loadConfigFromEnv({
  ...baseEnv,
  OPENRTC_MODE: 'cluster',
  OPENRTC_REDIS_URL: 'redis://localhost:6379/0',
});
assert.equal(clusterMode.mode, 'cluster');
assert.equal(clusterMode.redis?.url, 'redis://localhost:6379/0');

console.log('server unit tests passed');
