-- name: UpdateSandboxRunStatus :exec
UPDATE "public"."sandbox_runs"
SET
    status = @status,
    updated_at = NOW()
WHERE sandbox_id = @sandbox_id;

-- name: UpdateSandboxRunTimeout :exec
UPDATE "public"."sandbox_runs"
SET
    timeout_at = @timeout_at,
    updated_at = NOW()
WHERE sandbox_id = @sandbox_id;

-- name: EndSandboxRun :exec
UPDATE "public"."sandbox_runs"
SET
    status = 'stopped',
    end_reason = @end_reason,
    ended_at = NOW(),
    updated_at = NOW()
WHERE sandbox_id = @sandbox_id;
