# Production Onboarding

This guide is the product setup path for a production tenant/project, not a
local demo path.

## 1. Create The Product Container

Use an admin JWT with `admin:<tenant>:*` and `rooms:<tenant>:*` scopes.

```bash
export ADMIN_URL=https://openrtc-admin.example.com
export ADMIN_JWT=<admin-jwt>

curl -X POST "$ADMIN_URL/v1/tenants" \
  -H "Authorization: Bearer $ADMIN_JWT" \
  -H "Content-Type: application/json" \
  -d '{"id":"tenant-a","name":"Tenant A","metadata":{"plan":"production"}}'

curl -X POST "$ADMIN_URL/v1/tenants/tenant-a/projects" \
  -H "Authorization: Bearer $ADMIN_JWT" \
  -H "Content-Type: application/json" \
  -d '{"id":"app","name":"Main App"}'
```

## 2. Create A Server API Key

The `secret` is returned once. Store it in your app secret manager.

```bash
curl -X POST "$ADMIN_URL/v1/api-keys" \
  -H "Authorization: Bearer $ADMIN_JWT" \
  -H "Content-Type: application/json" \
  -d '{"tenantId":"tenant-a","projectId":"app","name":"server","scopes":["admin:tenant-a:__admin","rooms:tenant-a:*","room:read:tenant-a:*","room:write:tenant-a:*","storage:read:tenant-a:*","storage:write:tenant-a:*","comments:read:tenant-a:*","comments:write:tenant-a:*","feeds:read:tenant-a:*","feeds:write:tenant-a:*","notifications:*"]}'
```

The returned API key secret can be used as a bearer token for admin REST calls
and runtime WebSocket/Yjs connections. Revoked keys are rejected immediately.

## 3. Seed Rooms, Rich Text, And Version History

```bash
curl -X POST "$ADMIN_URL/v1/rooms" \
  -H "Authorization: Bearer $ADMIN_JWT" \
  -H "OpenRTC-Project-Id: app" \
  -H "Content-Type: application/json" \
  -d '{"id":"tenant-a:canvas","metadata":{"template":"blank"},"defaultAccesses":["room:read","room:presence:write"],"groupsAccesses":{"editors":["room:write"]}}'

curl -X PUT "$ADMIN_URL/v1/rooms/tenant-a:canvas/storage" \
  -H "Authorization: Bearer $ADMIN_JWT" \
  -H "OpenRTC-Project-Id: app" \
  -H "Content-Type: application/json" \
  -d '{"title":"Welcome","blocks":[]}'

curl -X PUT "$ADMIN_URL/v1/rooms/tenant-a:canvas/rich-text/doc-1" \
  -H "Authorization: Bearer $ADMIN_JWT" \
  -H "OpenRTC-Project-Id: app" \
  -H "Content-Type: application/json" \
  -d '{"content":{"type":"doc","format":"html","html":"<p>Welcome doc</p>","text":"Welcome doc"},"metadata":{"title":"Welcome doc"}}'
```

The reference console includes a managed rich-text editor in the Production tab.
It stores sanitized HTML/text in the rich-text document API and automatically
creates a version snapshot for each managed document update.

Storage writes automatically create managed version snapshots. Manual snapshots
are available when you need a named release point:

```bash
curl -X POST "$ADMIN_URL/v1/rooms/tenant-a:canvas/versions" \
  -H "Authorization: Bearer $ADMIN_JWT" \
  -H "OpenRTC-Project-Id: app" \
  -H "Content-Type: application/json" \
  -d '{"documentId":"storage","label":"launch"}'
```

## 4. Add Resumable Session Cursors

Use this for product-managed recovery beyond the bounded room replay window.
Clients can store the session id and let the runtime update room cursors from
ACK state automatically.

```bash
curl -X POST "$ADMIN_URL/v1/resume-sessions" \
  -H "Authorization: Bearer $ADMIN_JWT" \
  -H "Content-Type: application/json" \
  -d '{"tenantId":"tenant-a","projectId":"app","subject":"user-1","rooms":["tenant-a:canvas"],"roomCursors":{"tenant-a:canvas":42},"ttlSeconds":86400}'
```

## 5. Inspect Operations

```bash
curl "$ADMIN_URL/v1/audit-logs?tenantId=tenant-a&projectId=app" \
  -H "Authorization: Bearer $ADMIN_JWT"

curl "$ADMIN_URL/v1/usage?tenantId=tenant-a&projectId=app&roomId=tenant-a:canvas" \
  -H "Authorization: Bearer $ADMIN_JWT"

curl "$ADMIN_URL/v1/webhook-deliveries?status=failed" \
  -H "Authorization: Bearer $ADMIN_JWT"
```

The customer console can aggregate the same data into product dashboard,
customer status, and support debug bundle views:

```bash
curl "$ADMIN_URL/v1/dashboard?tenantId=tenant-a&projectId=app&roomId=tenant-a:canvas" \
  -H "Authorization: Bearer $ADMIN_JWT"

curl "$ADMIN_URL/v1/status?tenantId=tenant-a&projectId=app&roomId=tenant-a:canvas" \
  -H "Authorization: Bearer $ADMIN_JWT"

curl "$ADMIN_URL/v1/support/debug-bundle?tenantId=tenant-a&projectId=app&roomId=tenant-a:canvas" \
  -H "Authorization: Bearer $ADMIN_JWT"
```

`/v1/dashboard` includes rooms, active users, recent room events, current
storage, API keys, usage, audit logs, webhook deliveries, resume sessions,
rich-text documents, version snapshots, and customer-visible error summaries.
`/v1/status` returns status checks plus public-page copy. The support debug
bundle includes dashboard/status data and safe runtime configuration only; it
does not include API key hashes, secrets, JWKS private material, or bearer
tokens.

Failed webhook deliveries are retried by the admin background worker when
`nextAttemptAt` is due. You can also retry or dead-letter a failed webhook
delivery manually:

```bash
curl -X POST "$ADMIN_URL/v1/webhook-deliveries/wd_123/retry" \
  -H "Authorization: Bearer $ADMIN_JWT"

curl -X POST "$ADMIN_URL/v1/webhook-deliveries/wd_123/dead-letter" \
  -H "Authorization: Bearer $ADMIN_JWT"
```

## TypeScript Admin Client

```ts
import { OpenRTCAdminClient } from "@openrtc/client";

const admin = new OpenRTCAdminClient({
  url: process.env.OPENRTC_ADMIN_URL!,
  token: () => process.env.OPENRTC_ADMIN_JWT!,
});

await admin.createTenant({ id: "tenant-a", name: "Tenant A" });
await admin.createProject("tenant-a", { id: "app", name: "Main App" });
const key = await admin.createAPIKey({
  tenantId: "tenant-a",
  projectId: "app",
  name: "server",
  scopes: [
    "admin:tenant-a:__admin",
    "rooms:tenant-a:*",
    "room:read:tenant-a:*",
    "room:write:tenant-a:*",
    "storage:read:tenant-a:*",
    "storage:write:tenant-a:*",
    "comments:read:tenant-a:*",
    "comments:write:tenant-a:*",
    "notifications:*",
  ],
});

await admin.setRichTextDocument("tenant-a:canvas", "doc-1", {
  content: { type: "doc", format: "html", html: "<p>Welcome doc</p>", text: "Welcome doc" },
});

await admin.upsertResumeSession({
  tenantId: "tenant-a",
  projectId: "app",
  subject: "user-1",
  rooms: ["tenant-a:canvas"],
  roomCursors: { "tenant-a:canvas": 42 },
});

const dashboard = await admin.dashboard({
  tenantId: "tenant-a",
  projectId: "app",
  roomId: "tenant-a:canvas",
});

const status = await admin.status({
  tenantId: "tenant-a",
  projectId: "app",
  roomId: "tenant-a:canvas",
});

const supportBundle = await admin.supportDebugBundle({
  tenantId: "tenant-a",
  projectId: "app",
  roomId: "tenant-a:canvas",
});
```

## TypeScript Runtime Client

```ts
import { OpenRTCClient } from "@openrtc/client";

const client = new OpenRTCClient({
  url: process.env.OPENRTC_WS_URL!,
  token: () => process.env.OPENRTC_USER_OR_API_TOKEN!,
  projectId: "app",
  resumeSession: { id: "rs_user_1", ttlSeconds: 86400 },
});
```
