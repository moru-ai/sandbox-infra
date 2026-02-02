-- name: CreateVolume :one
INSERT INTO "public"."volumes" (
    id,
    team_id,
    name,
    status
) VALUES (
    @id,
    @team_id,
    @name,
    @status
) RETURNING *;
