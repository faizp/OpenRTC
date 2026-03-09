import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

import { createAdminClient } from '../../src/admin.ts';

const statsFixture = JSON.parse(
  await readFile(new URL('../../../testdata/contracts/publish-request.json', import.meta.url), 'utf8'),
) as { room: string; event: string; payload: unknown };

const client = createAdminClient({
  baseUrl: 'https://openrtc.example.com',
  token: 'token',
  fetchImpl: async (_input, init) => {
    assert.equal(init?.method, 'GET');
    return {
      ok: true,
      status: 200,
      async json() {
        return {
          active_connections: 1,
          active_rooms: 1,
          joins_total: 1,
          leaves_total: 0,
          events_total: 1,
          presence_updates_total: 0,
          queue_overflows_total: 0,
          admin_publishes_total: 1,
          room: statsFixture.room,
        };
      },
      async text() {
        return '';
      },
    };
  },
});

const stats = await client.stats();
assert.equal(stats.active_connections, 1);
assert.equal(stats.admin_publishes_total, 1);

console.log('sdk-ts integration smoke passed');
