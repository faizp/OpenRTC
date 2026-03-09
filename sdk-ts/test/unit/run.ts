import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

import { OpenRTCAdminError, createAdminClient } from '../../src/admin.ts';
import { createClient } from '../../src/client.ts';
import type { OpenRTCClientState, WebSocketLike } from '../../src/index.ts';

const state: OpenRTCClientState = 'connected';
assert.equal(state, 'connected');

class FakeSocket implements WebSocketLike {
  sent: string[] = [];
  listeners: Record<string, Array<(event: unknown) => void>> = {};

  send(data: string): void {
    this.sent.push(data);
  }

  close(): void {
    this.emit('close', {});
  }

  addEventListener(type: 'open' | 'message' | 'error' | 'close', listener: (event: unknown) => void): void {
    this.listeners[type] ??= [];
    this.listeners[type].push(listener);
  }

  emit(type: string, event: unknown): void {
    for (const listener of this.listeners[type] ?? []) {
      listener(event);
    }
  }
}

const socket = new FakeSocket();
const client = createClient({
  socketFactory: () => socket,
});

const events: string[] = [];
client.onEvent((event) => events.push(String(event.event)));

const connectPromise = client.connect({ endpoint: 'ws://localhost:8080/ws', token: 'token' });
socket.emit('open', {});
await connectPromise;
await client.join('tenant-a:room-1', { limit: 25 });
await client.emit('tenant-a:room-1', 'chat.message', { text: 'hello' }, { traceId: 'trace-1' });
await client.presence.set('tenant-a:room-1', { status: 'online' });
await client.leave('tenant-a:room-1');

socket.emit('message', {
  data: JSON.stringify({
    t: 'EVENT',
    room: 'tenant-a:room-1',
    event: 'chat.message',
    payload: { text: 'hello' },
  }),
});

assert.equal(events[0], 'chat.message');
assert.equal(socket.sent.length, 4);

const requestFixture = JSON.parse(
  await readFile(new URL('../../../testdata/contracts/publish-request.json', import.meta.url), 'utf8'),
) as { room: string; event: string; payload: unknown };
const errorFixture = JSON.parse(
  await readFile(new URL('../../../testdata/contracts/error-response.json', import.meta.url), 'utf8'),
) as { code: string; message: string; request_id: string };

const adminClient = createAdminClient({
  baseUrl: 'https://openrtc.example.com',
  token: 'token',
  fetchImpl: async (_input, init) => {
    assert.equal(init?.method, 'POST');
    const body = JSON.parse(init?.body ?? '{}') as { room: string };
    assert.equal(body.room, requestFixture.room);
    return {
      ok: false,
      status: 403,
      async json() {
        return errorFixture;
      },
      async text() {
        return JSON.stringify(errorFixture);
      },
    };
  },
});

await assert.rejects(
  adminClient.publish({
    room: requestFixture.room,
    event: requestFixture.event,
    payload: requestFixture.payload,
  }),
  (error: unknown) =>
    error instanceof OpenRTCAdminError &&
    error.code === 'ROOM_FORBIDDEN' &&
    error.requestId === 'req-123',
);

await client.disconnect();

console.log('sdk-ts unit tests passed');
