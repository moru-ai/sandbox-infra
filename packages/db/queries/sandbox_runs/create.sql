-- name: CreateSandboxRun :one
INSERT INTO "public"."sandbox_runs" (
    sandbox_id,
    team_id,
    template_id,
    build_id,
    status,
    timeout_at,
    metadata,
    volume_id
) VALUES (
    @sandbox_id,
    @team_id,
    @template_id,
    @build_id,
    'running',
    @timeout_at,
    @metadata,
    @volume_id
) RETURNING *;
