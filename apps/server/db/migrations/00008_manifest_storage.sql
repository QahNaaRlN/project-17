-- +goose Up
-- MF-002, MF-003, CNT-002, MF-033: неизменяемые manifest и изоляция схем preview.
CREATE TABLE manifests (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL REFERENCES projects(id),
  hash text NOT NULL CHECK (hash ~ '^sha256:[0-9a-f]{64}$'),
  app_version text NOT NULL,
  body jsonb NOT NULL,
  code_index_key text,
  registered_by uuid NOT NULL REFERENCES actors(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, hash),
  UNIQUE (project_id, id)
);
ALTER TABLE environments ADD CONSTRAINT environments_manifest_project_fk
  FOREIGN KEY (project_id, active_manifest_id) REFERENCES manifests(project_id, id);

-- Общие версии standard. Кандидаты и preview не резервируют номер до согласованного применения.
CREATE TABLE schema_versions (
  project_id uuid NOT NULL REFERENCES projects(id),
  schema_name text NOT NULL,
  version int NOT NULL CHECK (version > 0),
  body jsonb NOT NULL,
  manifest_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, schema_name, version),
  FOREIGN KEY (project_id, manifest_id) REFERENCES manifests(project_id, id)
);
CREATE TABLE preview_schema_snapshots (
  manifest_id uuid NOT NULL REFERENCES manifests(id),
  schema_name text NOT NULL,
  version int NOT NULL CHECK (version > 0),
  body jsonb NOT NULL,
  PRIMARY KEY (manifest_id, schema_name)
);

-- +goose StatementBegin
CREATE FUNCTION reject_manifest_storage_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'manifest storage is immutable' USING ERRCODE = '23514';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER immutable_manifest BEFORE UPDATE OR DELETE ON manifests
  FOR EACH ROW EXECUTE FUNCTION reject_manifest_storage_mutation();
CREATE TRIGGER immutable_schema_version BEFORE UPDATE OR DELETE ON schema_versions
  FOR EACH ROW EXECUTE FUNCTION reject_manifest_storage_mutation();
CREATE TRIGGER immutable_preview_schema BEFORE UPDATE OR DELETE ON preview_schema_snapshots
  FOR EACH ROW EXECUTE FUNCTION reject_manifest_storage_mutation();

-- CNT-002: сериализация разных standard-окружений на проекте защищает монотонность.
-- +goose StatementBegin
CREATE FUNCTION enforce_standard_schema_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM 1 FROM projects WHERE id = NEW.project_id FOR UPDATE;
  IF EXISTS (SELECT 1 FROM schema_versions WHERE project_id = NEW.project_id
             AND schema_name = NEW.schema_name AND version >= NEW.version) THEN
    RAISE EXCEPTION 'standard schema version must increase' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER increasing_standard_schema_version BEFORE INSERT ON schema_versions
  FOR EACH ROW EXECUTE FUNCTION enforce_standard_schema_version();

-- +goose Down
ALTER TABLE environments DROP CONSTRAINT environments_manifest_project_fk;
DROP TABLE preview_schema_snapshots;
DROP TABLE schema_versions;
DROP TABLE manifests;
DROP FUNCTION reject_manifest_storage_mutation();
DROP FUNCTION enforce_standard_schema_version();
