-- +goose Up
-- +goose StatementBegin

-- Create sandbox_runs table to track sandbox execution history
CREATE TABLE IF NOT EXISTS "public"."sandbox_runs"
(
    "id"            uuid        NOT NULL DEFAULT gen_random_uuid(),
    "sandbox_id"    text        NOT NULL,
    "team_id"       uuid        NOT NULL,
    "template_id"   text        NOT NULL,
    "build_id"      text        NULL,
    "status"        text        NOT NULL DEFAULT 'running',
    "end_reason"    text        NULL,
    "created_at"    timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at"    timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "ended_at"      timestamptz NULL,
    "timeout_at"    timestamptz NULL,
    "metadata"      jsonb       NULL,
    PRIMARY KEY ("id"),
    CONSTRAINT "sandbox_runs_team_id_fkey" FOREIGN KEY ("team_id") REFERENCES "public"."teams" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT "sandbox_runs_status_check" CHECK (status IN ('running', 'paused', 'stopped')),
    CONSTRAINT "sandbox_runs_end_reason_check" CHECK (end_reason IS NULL OR end_reason IN ('killed', 'timeout', 'error', 'shutdown'))
);

-- Create index on sandbox_id for fast lookups
CREATE UNIQUE INDEX IF NOT EXISTS "sandbox_runs_sandbox_id_idx" ON "public"."sandbox_runs" ("sandbox_id");

-- Create index on team_id for listing runs by team
CREATE INDEX IF NOT EXISTS "sandbox_runs_team_id_idx" ON "public"."sandbox_runs" ("team_id");

-- Create index on template_id for listing runs by template
CREATE INDEX IF NOT EXISTS "sandbox_runs_template_id_idx" ON "public"."sandbox_runs" ("template_id");

-- Create index on status for filtering active runs
CREATE INDEX IF NOT EXISTS "sandbox_runs_status_idx" ON "public"."sandbox_runs" ("status");

-- Create composite index for common query pattern (team + status + created_at)
CREATE INDEX IF NOT EXISTS "sandbox_runs_team_status_created_idx" ON "public"."sandbox_runs" ("team_id", "status", "created_at" DESC);

-- Enable RLS
ALTER TABLE "public"."sandbox_runs" ENABLE ROW LEVEL SECURITY;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS "public"."sandbox_runs" CASCADE;

-- +goose StatementEnd
