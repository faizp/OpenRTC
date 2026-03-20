# OpenRTC Reference App — Realtime Chat

A minimal but complete chat application demonstrating OpenRTC's core features:
rooms, messaging, and presence.

## What it demonstrates

- JWT authentication flow (mock JWKS provider included for local dev)
- Room join/leave lifecycle
- Real-time message broadcasting
- Presence updates (typing indicators, online status)
- Admin API publish (server-to-room messaging)
- Multi-tenant room isolation

## Architecture

```
browser (index.html)
   ↕ WebSocket
openrtc-runtime ←→ Redis ←→ openrtc-runtime (node 2, optional)
   ↑
openrtc-admin (POST /v1/publish)
   ↑
server.go (mock auth + admin publish demo)
```

## Running locally

**Prerequisites:** Go 1.18+, Docker (for Redis)

```bash
# Start Redis
docker run -d --name openrtc-redis -p 6379:6379 redis:7-alpine

# Start the reference app (runs mock JWKS + runtime + admin + serves UI)
cd reference-app
go run ./cmd/server

# Open browser
open http://localhost:3000
```

The reference server starts:
- Mock JWKS provider on :3000/jwks
- OpenRTC runtime on :8080 (WebSocket at /ws)
- OpenRTC admin on :8090
- Static file server for the chat UI on :3000

## Usage

1. Open `http://localhost:3000` in two browser tabs
2. Enter different usernames in each tab
3. Join the same room (e.g., "general")
4. Send messages — both tabs see them in real time
5. Watch presence indicators update as users type
