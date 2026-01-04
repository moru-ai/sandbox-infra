# Sandbox Logs Improvement Plan

## Problem

Currently, sandbox logs have issues:
1. **Shows system logs** - Users see `process_start`, `process_end` events instead of just their program output
2. **Wrong message content** - Shows "Streaming process event" instead of actual output (which is in `data` field)
3. **Level filter doesn't make sense** - Sandbox logs only have stdout/stderr, not debug/info/warn/error like build logs

## Current State

**Sandbox log structure from envd:**
```json
{
  "level": "info",           // always "info" - envd doesn't know log levels
  "event_type": "stdout",    // or "stderr", "process_start", "process_end"
  "data": "Hello world\n",   // actual program output
  "message": "Streaming process event"  // generic message
}
```

**Build log structure (for comparison):**
```json
{
  "level": "info",           // actual log level: debug/info/warn/error
  "msg": "Building template..."
}
```

## Solution

### Backend Changes (infra repo)

- [x] **1. Filter Loki query to only stdout/stderr**
  - File: `packages/client-proxy/internal/edge/logger-provider/provider_loki.go`
  - Add `| json | event_type=~"stdout|stderr"` to query for user-facing logs
  - Keep `includeSystemLogs` param for admin access via Grafana

- [x] **2. Use `data` field as message for stdout/stderr**
  - File: `packages/shared/pkg/logs/logsloki/loki.go`
  - In ResponseMapper, extract `data` field as message when `event_type` is stdout/stderr

- [x] **3. Replace level filter with event_type filter**
  - File: `packages/client-proxy/internal/edge/logger-provider/provider.go`
  - Change `level *logs.LogLevel` to `eventType *string` (values: "stdout", "stderr", nil for all)
  - File: `packages/client-proxy/internal/edge/logger-provider/provider_loki.go`
  - Update query to filter by specific event_type
  - File: `packages/client-proxy/internal/edge/handlers/sandbox-logs.go`
  - Update handler to accept eventType param instead of level

- [x] **4. Update API spec**
  - File: `spec/openapi-edge.yml`
  - Added `SandboxLogEventType` enum (stdout, stderr)
  - Changed `SandboxLogEntry.level` to `SandboxLogEntry.eventType`
  - Changed sandbox logs endpoint query param from `level` to `eventType`

### Frontend Changes (dashboard repo)

- [ ] **5. Update filter UI for sandbox logs**
  - File: `src/features/dashboard/sandbox/logs/logs-filter-params.ts`
  - Change from `LogLevelFilter` to `EventTypeFilter` (stdout, stderr, all)

- [ ] **6. Update filter component**
  - Show "stdout" / "stderr" / "all" instead of "debug" / "info" / "warn" / "error"

- [ ] **7. Update tRPC calls**
  - Pass `eventType` instead of `level` to sandbox logs endpoint

### Display Changes

- [ ] **8. Update log level badge for sandbox logs**
  - Show "stdout" (blue/info) or "stderr" (red/error) badge
  - File: `src/features/dashboard/sandbox/logs/logs-cells.tsx`

## Testing

- [x] Run sandbox, echo to stdout and stderr
- [x] Verify only stdout/stderr shown (no process_start/end)
- [x] Verify actual output shown (not "Streaming process event")
- [x] Verify stdout filter works
- [x] Verify stderr filter works
- [x] Verify build logs still work with level filter
- [x] Verify system logs filtered by sandbox created_at timestamp

## Deployment

```bash
# Backend
cd ~/moru/deploy
make build-and-upload/client-proxy
make plan && make apply

# Frontend
cd ~/moru/dashboard
# Deploy via Vercel or your deployment method
```

## Notes

- Admins can still see all logs (including system logs) via Grafana/Loki directly
- Build logs are unaffected - they still use level filter (debug/info/warn/error)
- The `event_type` field is only present in envd logs, not build logs
