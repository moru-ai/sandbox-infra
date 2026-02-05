-- name: GetVolume :one
SELECT * FROM "public"."volumes"
WHERE id = @id;

-- name: GetVolumeByName :one
SELECT * FROM "public"."volumes"
WHERE team_id = @team_id AND name = @name;

-- name: ListVolumes :many
SELECT *
FROM "public"."volumes"
WHERE team_id = @team_id
  AND (@status::text[] IS NULL OR status = ANY(@status::text[]))
ORDER BY created_at DESC
LIMIT @query_limit;

-- name: GetVolumesByStatus :many
SELECT * FROM "public"."volumes"
WHERE status = @status
ORDER BY created_at ASC;

-- name: IsVolumeAttached :one
-- Returns true if the volume is currently attached to a running sandbox
SELECT EXISTS (
    SELECT 1 FROM "public"."sandbox_runs"
    WHERE volume_id = @volume_id
    AND status = 'running'
) AS is_attached;
