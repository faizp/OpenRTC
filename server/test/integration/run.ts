import assert from 'node:assert/strict';

import { SERVER_NAME } from '../../src/index.ts';

assert.equal(SERVER_NAME, 'openrtc-server');

console.log('server integration smoke passed');
