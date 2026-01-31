-- +goose Up
-- +goose StatementBegin

-- Create volumes table for persistent storage
CREATE TABLE IF NOT EXISTS "public"."volumes" (
    "id"                        TEXT        NOT NULL,
    "team_id"                   UUID        NOT NULL,
    "name"                      TEXT        NOT NULL,
    "status"                    TEXT        NOT NULL DEFAULT 'available',
    "redis_db"                  INT         NOT NULL,
    "redis_password_encrypted"  BYTEA       NOT NULL,
    "total_size_bytes"          BIGINT      DEFAULT 0,
    "total_file_count"          BIGINT      DEFAULT 0,
    "created_at"                TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at"                TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY ("id"),
    CONSTRAINT "volumes_team_id_fkey" FOREIGN KEY ("team_id") REFERENCES "public"."teams" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT "volumes_status_check" CHECK (status IN ('creating', 'available', 'deleting')),
    UNIQUE ("team_id", "name"),
    UNIQUE ("redis_db")
);

-- Create indexes
CREATE INDEX IF NOT EXISTS "volumes_team_id_idx" ON "public"."volumes" ("team_id");
CREATE INDEX IF NOT EXISTS "volumes_status_idx" ON "public"."volumes" ("status");
CREATE INDEX IF NOT EXISTS "volumes_team_name_idx" ON "public"."volumes" ("team_id", "name");

-- Add volume columns to sandbox_runs table
ALTER TABLE "public"."sandbox_runs"
ADD COLUMN IF NOT EXISTS "volume_id" TEXT REFERENCES "public"."volumes"("id") ON DELETE SET NULL,
ADD COLUMN IF NOT EXISTS "volume_mount_path" TEXT;

-- Create index for sandbox_runs with volumes
CREATE INDEX IF NOT EXISTS "sandbox_runs_volume_id_idx" ON "public"."sandbox_runs" ("volume_id") WHERE volume_id IS NOT NULL;

-- Redis DB allocation sequence (starts at 1, not 0)
CREATE SEQUENCE IF NOT EXISTS "volumes_redis_db_seq" START 1;

-- Enable RLS
ALTER TABLE "public"."volumes" ENABLE ROW LEVEL SECURITY;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE "public"."sandbox_runs" DROP COLUMN IF EXISTS "volume_mount_path";
ALTER TABLE "public"."sandbox_runs" DROP COLUMN IF EXISTS "volume_id";
DROP SEQUENCE IF EXISTS "volumes_redis_db_seq";
DROP TABLE IF EXISTS "public"."volumes" CASCADE;

-- +goose StatementEnd
