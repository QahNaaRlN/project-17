-- +goose Up
-- STU-504: events without a project (a login for an unknown project) or without an
-- actor (an anonymous refusal); the journal is read per project, newest first.
ALTER TABLE security_events ALTER COLUMN project_id DROP NOT NULL;
ALTER TABLE security_events ALTER COLUMN actor_id DROP NOT NULL;
CREATE INDEX security_events_by_project ON security_events (project_id, created_at DESC, id DESC);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM security_events WHERE project_id IS NULL OR actor_id IS NULL) THEN
  RAISE EXCEPTION 'project-less or anonymous security events prevent downgrade';
 END IF;
END $$;
-- +goose StatementEnd
DROP INDEX security_events_by_project;
ALTER TABLE security_events ALTER COLUMN actor_id SET NOT NULL;
ALTER TABLE security_events ALTER COLUMN project_id SET NOT NULL;
