-- +goose Up
-- +goose StatementBegin

-- Add RLS policy for volumes table to enforce team isolation
-- The app.team_id setting must be set by the application before queries
CREATE POLICY "volumes_team_isolation" ON volumes
  FOR ALL USING (team_id = current_setting('app.team_id')::uuid);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP POLICY IF EXISTS "volumes_team_isolation" ON volumes;

-- +goose StatementEnd
