-- +goose Up
ALTER TABLE capability_context_packs ADD CONSTRAINT capability_pack_scope UNIQUE(project_id,request_id,revision);

CREATE TABLE capability_delivery_targets (
 id uuid PRIMARY KEY,
 project_id uuid NOT NULL REFERENCES projects(id),
 environment_id uuid NOT NULL,
 environment text NOT NULL,
 endpoint_id text NOT NULL,
 url text NOT NULL,
 fingerprint text NOT NULL,
 enabled boolean NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 changed_by uuid NOT NULL REFERENCES actors(id),
 changed_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(project_id,environment), UNIQUE(project_id,environment_id), UNIQUE(project_id,id),
 FOREIGN KEY(project_id,environment_id) REFERENCES environments(project_id,id)
);

CREATE TABLE capability_deliveries (
 id uuid PRIMARY KEY,
 project_id uuid NOT NULL REFERENCES projects(id),
 request_id uuid NOT NULL,
 request_revision bigint NOT NULL CHECK(request_revision>0),
 pack_revision bigint NOT NULL,
 pack_hash text NOT NULL CHECK(pack_hash ~ '^sha256:[0-9a-f]{64}$'),
 target_id uuid NOT NULL,
 target_revision bigint NOT NULL CHECK(target_revision>0),
 target_fingerprint text NOT NULL,
 destination_url text NOT NULL,
 endpoint_id text NOT NULL,
 key_id text NOT NULL,
 key_fingerprint text NOT NULL,
 initiated_by uuid NOT NULL REFERENCES actors(id),
 body bytea NOT NULL CHECK(octet_length(body)<=655360),
 body_hash text NOT NULL CHECK(body_hash ~ '^sha256:[0-9a-f]{64}$'),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','sending','retry_wait','delivered','failed','cancelled')),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 cycle int NOT NULL DEFAULT 1 CHECK(cycle BETWEEN 1 AND 3),
 attempts_in_cycle int NOT NULL DEFAULT 0 CHECK(attempts_in_cycle BETWEEN 0 AND 6),
 total_attempts int NOT NULL DEFAULT 0 CHECK(total_attempts BETWEEN 0 AND 18),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_token uuid,
 lease_until timestamptz,
 cancelled_at timestamptz,
 delivered_at timestamptz,
 last_error text NOT NULL DEFAULT '',
 last_http_status int,
 retryable_exhaustion boolean NOT NULL DEFAULT false,
 UNIQUE(project_id,id),
 UNIQUE(project_id,request_id,pack_revision,target_id,target_revision),
 FOREIGN KEY(project_id,request_id) REFERENCES capability_requests(project_id,id),
 FOREIGN KEY(project_id,request_id,pack_revision) REFERENCES capability_context_packs(project_id,request_id,revision),
 FOREIGN KEY(project_id,target_id) REFERENCES capability_delivery_targets(project_id,id),
 CHECK((lease_token IS NULL)=(lease_until IS NULL))
);
CREATE INDEX capability_delivery_history ON capability_deliveries(project_id,request_id,created_at DESC,id DESC);
CREATE INDEX capability_delivery_due ON capability_deliveries(next_attempt_at) WHERE state IN ('queued','sending','retry_wait');

CREATE TABLE capability_delivery_attempts (
 id uuid PRIMARY KEY,
 project_id uuid NOT NULL,
 delivery_id uuid NOT NULL,
 ordinal int NOT NULL CHECK(ordinal BETWEEN 1 AND 18),
 cycle int NOT NULL CHECK(cycle BETWEEN 1 AND 3),
 lease_token uuid NOT NULL,
 started_at timestamptz NOT NULL DEFAULT now(),
 finished_at timestamptz,
 http_status int,
 outcome text NOT NULL DEFAULT 'sending',
 retryable boolean NOT NULL DEFAULT false,
 receipt_delivery_id uuid,
 receipt_body_hash text,
 next_attempt_at timestamptz,
 UNIQUE(delivery_id,ordinal),
 FOREIGN KEY(project_id,delivery_id) REFERENCES capability_deliveries(project_id,id)
);
CREATE TABLE capability_delivery_audit (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 project_id uuid NOT NULL,
 delivery_id uuid,
 target_id uuid NOT NULL,
 event text NOT NULL,
 source text NOT NULL CHECK(source IN ('human','service')),
 actor_id uuid REFERENCES actors(id),
 initiated_by uuid REFERENCES actors(id),
 reason text NOT NULL,
 revision bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(project_id,delivery_id) REFERENCES capability_deliveries(project_id,id),
 FOREIGN KEY(project_id,target_id) REFERENCES capability_delivery_targets(project_id,id),
 CHECK((source='human' AND actor_id IS NOT NULL) OR (source='service' AND actor_id IS NULL))
);

-- Body and destination pins are immutable even during manual retries.
-- +goose StatementBegin
CREATE FUNCTION capability_delivery_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.project_id,NEW.request_id,NEW.request_revision,NEW.pack_revision,NEW.pack_hash,NEW.target_id,NEW.target_revision,NEW.target_fingerprint,NEW.destination_url,NEW.endpoint_id,NEW.key_id,NEW.key_fingerprint,NEW.initiated_by,NEW.body,NEW.body_hash,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.request_id,OLD.request_revision,OLD.pack_revision,OLD.pack_hash,OLD.target_id,OLD.target_revision,OLD.target_fingerprint,OLD.destination_url,OLD.endpoint_id,OLD.key_id,OLD.key_fingerprint,OLD.initiated_by,OLD.body,OLD.body_hash,OLD.created_at)
 THEN RAISE EXCEPTION 'immutable capability delivery' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER capability_delivery_immutable BEFORE UPDATE ON capability_deliveries FOR EACH ROW EXECUTE FUNCTION capability_delivery_immutable();

-- Global humans use membership at insertion; service audit remains possible
-- after the initiating human has lost their role.
-- +goose StatementBegin
CREATE FUNCTION capability_delivery_actor_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE candidate uuid;
BEGIN
 IF TG_TABLE_NAME='capability_delivery_targets' THEN candidate:=NEW.changed_by;
 ELSIF TG_TABLE_NAME='capability_deliveries' THEN candidate:=NEW.initiated_by;
 ELSE candidate:=NEW.actor_id; END IF;
 IF candidate IS NOT NULL AND NOT EXISTS(SELECT 1 FROM actors a WHERE a.id=candidate AND a.kind='human' AND a.disabled_at IS NULL AND EXISTS(SELECT 1 FROM role_bindings b WHERE b.project_id=NEW.project_id AND b.actor_id=a.id))
 THEN RAISE EXCEPTION 'delivery actor project mismatch' USING ERRCODE='23503'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER capability_delivery_target_actor BEFORE INSERT OR UPDATE ON capability_delivery_targets FOR EACH ROW EXECUTE FUNCTION capability_delivery_actor_scope();
CREATE TRIGGER capability_delivery_initiator BEFORE INSERT ON capability_deliveries FOR EACH ROW EXECUTE FUNCTION capability_delivery_actor_scope();
CREATE TRIGGER capability_delivery_audit_actor BEFORE INSERT ON capability_delivery_audit FOR EACH ROW EXECUTE FUNCTION capability_delivery_actor_scope();

-- +goose Down
DROP TABLE capability_delivery_audit;
DROP TABLE capability_delivery_attempts;
DROP TABLE capability_deliveries;
DROP TABLE capability_delivery_targets;
DROP FUNCTION capability_delivery_actor_scope();
DROP FUNCTION capability_delivery_immutable();
ALTER TABLE capability_context_packs DROP CONSTRAINT capability_pack_scope;
