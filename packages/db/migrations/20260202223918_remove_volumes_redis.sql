-- +goose Up
-- +goose StatementBegin

-- Remove Redis-related columns from volumes table
-- Volumes now use SQLite metadata stored in GCS instead of Redis

ALTER TABLE "public"."volumes" DROP COLUMN IF EXISTS "redis_db";
ALTER TABLE "public"."volumes" DROP COLUMN IF EXISTS "redis_password_encrypted";

-- Drop the Redis DB allocation sequence
DROP SEQUENCE IF EXISTS "volumes_redis_db_seq";

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Recreate Redis DB allocation sequence
CREATE SEQUENCE IF NOT EXISTS "volumes_redis_db_seq" START 1;

-- Add back Redis columns
ALTER TABLE "public"."volumes"
ADD COLUMN IF NOT EXISTS "redis_db" INT,
ADD COLUMN IF NOT EXISTS "redis_password_encrypted" BYTEA;

-- Note: Down migration cannot restore data - redis_db values will be NULL
-- Manual intervention required if rollback is needed

-- +goose StatementEnd
