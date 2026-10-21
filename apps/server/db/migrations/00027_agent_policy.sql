-- +goose Up
-- STU-420…439: immutable versions of the conditional agent policy. The MVP
-- allow/deny setting becomes version 1 (STU-432); the setting stays for Down.
CREATE TABLE agent_policy_versions (
  project_id uuid NOT NULL REFERENCES projects(id),
  version int NOT NULL CHECK (version > 0),
  body jsonb NOT NULL,
  hash text NOT NULL,
  author_id uuid REFERENCES actors(id),
  reason text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, version)
);
INSERT INTO agent_policy_versions (project_id, version, body, hash, reason)
SELECT id, 1,
  CASE WHEN settings ? 'agentPolicy' THEN jsonb_build_object('version', 1,
    'rules', jsonb_build_array(jsonb_build_object('id', 'legacy', 'subject', jsonb_build_object('agent', '*'),
      'allow', COALESCE(settings->'agentPolicy'->'allow', '[]'::jsonb), 'deny', COALESCE(settings->'agentPolicy'->'deny', '[]'::jsonb))),
    'risk', '[]'::jsonb, 'approvals', '[]'::jsonb)
  ELSE '{"version":1,"rules":[],"risk":[],"approvals":[]}'::jsonb END,
  'migrated', 'migrated MVP agent policy'
FROM projects;
-- STU-427: requireApproval rules that matched operations of the Change Set.
ALTER TABLE changesets ADD COLUMN policy_approval text[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE changesets DROP COLUMN policy_approval;
DROP TABLE agent_policy_versions;
