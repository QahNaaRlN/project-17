-- +goose Up
ALTER TABLE actors ADD COLUMN oidc_issuer text;
ALTER TABLE actors DROP CONSTRAINT actors_oidc_subject_key;
ALTER TABLE actors ADD CONSTRAINT actors_oidc_identity_key UNIQUE (oidc_issuer, oidc_subject);
CREATE TABLE human_sessions (
  token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash)=32),
  actor_id uuid NOT NULL REFERENCES actors(id),
  csrf_token text NOT NULL,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX human_sessions_expiry ON human_sessions(expires_at);
CREATE TABLE oidc_logins (
  state_hash bytea PRIMARY KEY CHECK (octet_length(state_hash)=32),
  browser_hash bytea NOT NULL CHECK (octet_length(browser_hash)=32),
  verifier text NOT NULL,
  nonce text NOT NULL,
  project_slug text NOT NULL REFERENCES projects(slug),
  expires_at timestamptz NOT NULL
);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM actors WHERE oidc_issuer IS NOT NULL) OR EXISTS (SELECT 1 FROM human_sessions) OR EXISTS (SELECT 1 FROM oidc_logins) THEN
    RAISE EXCEPTION 'human identity/session data prevents downgrade';
  END IF;
END $$;
-- +goose StatementEnd
DROP TABLE oidc_logins;
DROP TABLE human_sessions;
ALTER TABLE actors DROP CONSTRAINT actors_oidc_identity_key;
ALTER TABLE actors DROP COLUMN oidc_issuer;
ALTER TABLE actors ADD CONSTRAINT actors_oidc_subject_key UNIQUE (oidc_subject);
