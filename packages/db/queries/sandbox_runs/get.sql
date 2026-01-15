-- name: GetSandboxRun :one
SELECT * FROM "public"."sandbox_runs"
WHERE sandbox_id = @sandbox_id;

-- name: ListSandboxRuns :many
SELECT
    sr.sandbox_id,
    sr.template_id,
    ea.alias,
    sr.status,
    sr.end_reason,
    sr.created_at,
    sr.ended_at
FROM "public"."sandbox_runs" sr
LEFT JOIN "public"."env_aliases" ea ON sr.template_id = ea.env_id
WHERE sr.team_id = @team_id
  AND (@status::text[] IS NULL OR sr.status = ANY(@status::text[]))
  AND sr.created_at < @cursor_time
ORDER BY sr.created_at DESC
LIMIT @query_limit;
