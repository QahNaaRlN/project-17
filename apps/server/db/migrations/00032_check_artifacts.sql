-- +goose Up
-- STU-570…589: pipeline stages 10–11 keep screenshots and reports in object storage
-- (07 §5: projects/{projectId}/checks/{changesetId}/{checkId}/{name}); the rows record
-- what the runner uploaded under its lease.
CREATE TABLE check_artifacts (
  check_id      uuid NOT NULL REFERENCES checks(id) ON DELETE CASCADE,
  name          text NOT NULL CHECK (name ~ '^[a-z0-9._-]{1,128}$'),
  content_type  text NOT NULL,
  size          bigint NOT NULL CHECK (size >= 0),
  sha256        bytea NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (check_id, name)
);
-- STU-583: objects removed after the retention period; the rows stay with the check.
ALTER TABLE checks ADD COLUMN artifacts_expired_at timestamptz;

-- +goose Down
ALTER TABLE checks DROP COLUMN artifacts_expired_at;
DROP TABLE check_artifacts;
