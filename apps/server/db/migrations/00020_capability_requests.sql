-- +goose Up
-- STU-255..274: immutable code metadata and explicit capability handoff.
ALTER TABLE objects ADD CONSTRAINT objects_capability_project_key UNIQUE(project_id,id);
ALTER TABLE agent_sessions ADD CONSTRAINT sessions_capability_project_key UNIQUE(project_id,id);
CREATE TABLE code_index_uploads (
 id uuid PRIMARY KEY,
 project_id uuid NOT NULL REFERENCES projects(id),
 manifest_hash text NOT NULL CHECK (manifest_hash ~ '^sha256:[0-9a-f]{64}$'),
 index_hash text NOT NULL CHECK (index_hash ~ '^sha256:[0-9a-f]{64}$'),
 repo_commit text NOT NULL,
 body jsonb NOT NULL,
 linked boolean NOT NULL DEFAULT false,
 created_by uuid NOT NULL REFERENCES actors(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(project_id,manifest_hash,index_hash),
 UNIQUE(project_id,id)
);
CREATE INDEX code_index_uploads_expiry ON code_index_uploads(created_at) WHERE NOT linked;

CREATE TABLE capability_requests (
 id uuid PRIMARY KEY,
 project_id uuid NOT NULL REFERENCES projects(id),
 kind text NOT NULL DEFAULT 'action' CHECK(kind='action'),
 environment_id uuid NOT NULL REFERENCES environments(id),
 environment text NOT NULL,
 source_manifest_id uuid NOT NULL,
 key text NOT NULL,
 title text NOT NULL CHECK(length(title) BETWEEN 1 AND 200),
 description text NOT NULL CHECK(length(description) BETWEEN 1 AND 4000),
 proposed_contract jsonb NOT NULL,
 acceptance jsonb NOT NULL,
 changeset_id uuid NOT NULL REFERENCES changesets(id),
 document_id uuid NOT NULL REFERENCES objects(id),
 node_id text NOT NULL,
 event text NOT NULL,
 creator_id uuid NOT NULL REFERENCES actors(id),
 owner_id uuid NOT NULL REFERENCES actors(id),
 agent_session_id uuid REFERENCES agent_sessions(id),
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','submitted','accepted','in_progress','pr_open','fulfilled','rejected','cancelled')),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 external_refs jsonb NOT NULL DEFAULT '[]',
 fulfilled_manifest_id uuid,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(project_id,id),
 FOREIGN KEY(project_id,environment_id) REFERENCES environments(project_id,id),
 FOREIGN KEY(project_id,changeset_id) REFERENCES changesets(project_id,id),
 FOREIGN KEY(project_id,document_id) REFERENCES objects(project_id,id),
 FOREIGN KEY(project_id,agent_session_id) REFERENCES agent_sessions(project_id,id),
 FOREIGN KEY(project_id,source_manifest_id) REFERENCES manifests(project_id,id),
 FOREIGN KEY(project_id,fulfilled_manifest_id) REFERENCES manifests(project_id,id)
);
CREATE UNIQUE INDEX capability_requests_open_key ON capability_requests(project_id,environment_id,kind,key)
 WHERE status NOT IN ('fulfilled','rejected','cancelled');
CREATE INDEX capability_requests_queue ON capability_requests(project_id,updated_at DESC,id DESC);
CREATE TABLE capability_context_packs (
 project_id uuid NOT NULL,
 request_id uuid NOT NULL,
 revision bigint NOT NULL,
 manifest_id uuid NOT NULL,
 index_id uuid NOT NULL,
 changeset_seq int NOT NULL,
 page_hash text NOT NULL,
 hash text NOT NULL,
 body jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(request_id,revision),
 FOREIGN KEY(project_id,request_id) REFERENCES capability_requests(project_id,id),
 FOREIGN KEY(project_id,manifest_id) REFERENCES manifests(project_id,id),
 FOREIGN KEY(project_id,index_id) REFERENCES code_index_uploads(project_id,id)
);
CREATE TABLE capability_request_audit (
 id uuid PRIMARY KEY,
 project_id uuid NOT NULL,
 request_id uuid NOT NULL,
 revision bigint NOT NULL,
 actor_id uuid NOT NULL REFERENCES actors(id),
 on_behalf_of uuid REFERENCES actors(id),
 agent_session_id uuid REFERENCES agent_sessions(id),
 event text NOT NULL,
 reason text NOT NULL,
 data jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(request_id,revision),
 FOREIGN KEY(project_id,request_id) REFERENCES capability_requests(project_id,id),
 FOREIGN KEY(project_id,agent_session_id) REFERENCES agent_sessions(project_id,id)
);

-- Humans are global principals (actors.project_id IS NULL). Check membership at
-- insertion instead of using a composite FK that would reject every human and
-- make later role revocation erase or prevent immutable history.
-- +goose StatementBegin
CREATE FUNCTION capability_actor_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE candidate uuid; candidates uuid[];
BEGIN
 IF TG_TABLE_NAME='code_index_uploads' THEN candidates:=ARRAY[NEW.created_by];
 ELSIF TG_TABLE_NAME='capability_requests' THEN candidates:=ARRAY[NEW.creator_id,NEW.owner_id];
 ELSE candidates:=ARRAY[NEW.actor_id,NEW.on_behalf_of]; END IF;
 FOREACH candidate IN ARRAY candidates LOOP
  IF candidate IS NOT NULL AND NOT EXISTS(
   SELECT 1 FROM actors a WHERE a.id=candidate AND (a.project_id=NEW.project_id OR
    (a.kind='human' AND EXISTS(SELECT 1 FROM role_bindings b WHERE b.project_id=NEW.project_id AND b.actor_id=a.id))))
  THEN RAISE EXCEPTION 'capability actor project mismatch' USING ERRCODE='23503'; END IF;
 END LOOP;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER capability_upload_actor BEFORE INSERT ON code_index_uploads FOR EACH ROW EXECUTE FUNCTION capability_actor_scope();
CREATE TRIGGER capability_request_actor BEFORE INSERT ON capability_requests FOR EACH ROW EXECUTE FUNCTION capability_actor_scope();
CREATE TRIGGER capability_audit_actor BEFORE INSERT ON capability_request_audit FOR EACH ROW EXECUTE FUNCTION capability_actor_scope();

-- +goose Down
DROP TABLE capability_request_audit;
DROP TABLE capability_context_packs;
DROP TABLE capability_requests;
DROP TABLE code_index_uploads;
DROP FUNCTION capability_actor_scope();
ALTER TABLE agent_sessions DROP CONSTRAINT sessions_capability_project_key;
ALTER TABLE objects DROP CONSTRAINT objects_capability_project_key;
