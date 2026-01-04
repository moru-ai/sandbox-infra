-- name: GetSandboxRun :one
SELECT * FROM "public"."sandbox_runs"
WHERE sandbox_id = @sandbox_id;
