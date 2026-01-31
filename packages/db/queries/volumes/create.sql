-- name: CreateVolume :one
INSERT INTO "public"."volumes" (
    id,
    team_id,
    name,
    status,
    redis_db,
    redis_password_encrypted
) VALUES (
    @id,
    @team_id,
    @name,
    @status,
    @redis_db,
    @redis_password_encrypted
) RETURNING *;

-- name: AllocateRedisDB :one
SELECT nextval('volumes_redis_db_seq')::INT AS redis_db;
