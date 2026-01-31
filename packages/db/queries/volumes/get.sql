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
