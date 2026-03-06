import assert from 'node:assert/strict';

import { createClient } from '../../src/client.ts';
import type { OpenRTCClientState } from '../../src/index.ts';

const state: OpenRTCClientState = 'connected';
assert.equal(state, 'connected');

const client = createClient();
await client.connect({ endpoint: 'ws://localhost:8080/ws', token: 'token' });
await client.disconnect();

console.log('sdk-ts unit tests passed');
