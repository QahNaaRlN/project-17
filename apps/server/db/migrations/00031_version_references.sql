-- +goose Up
-- Incremental publication checks (docs/incremental-validation.md, STU-560, 561, 566).
-- version_references holds every UUID-shaped string of a version body: a superset of the
-- references validation resolves (Composed refs, entityId, assetId, page, bindings), so a
-- publication finds the documents that depend on its objects without reading the whole
-- environment. Extra matches only cause extra revalidation.
CREATE TABLE version_references (
  version_id  uuid NOT NULL REFERENCES object_versions(id) ON DELETE CASCADE,
  target_id   uuid NOT NULL,
  PRIMARY KEY (version_id, target_id)
);
CREATE INDEX version_references_target ON version_references (target_id);
CREATE INDEX published_pointers_version ON published_pointers (environment_id, version_id);

-- Every quoted string of the body text that is a UUID in any form uuid.Parse accepts (plain,
-- braced, urn:uuid:, unhyphenated). One regex pass over the text: walking the JSON with
-- jsonpath made seeding the load profile four times slower.
-- +goose StatementBegin
CREATE FUNCTION version_reference_ids(body jsonb) RETURNS SETOF uuid LANGUAGE sql IMMUTABLE AS $$
  SELECT DISTINCT m[1]::uuid
  FROM regexp_matches(body::text, '"(?:urn:uuid:|\{)?([0-9a-f]{8}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{12})\}?"', 'gi') m
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION index_version_references() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'UPDATE' THEN
    DELETE FROM version_references WHERE version_id = NEW.id;
  END IF;
  INSERT INTO version_references (version_id, target_id)
  SELECT NEW.id, t FROM version_reference_ids(NEW.body) t;
  RETURN NULL;
END $$;
-- +goose StatementEnd

CREATE TRIGGER version_references AFTER INSERT OR UPDATE OF body ON object_versions
  FOR EACH ROW EXECUTE FUNCTION index_version_references();

INSERT INTO version_references (version_id, target_id)
SELECT v.id, t FROM object_versions v, version_reference_ids(v.body) t;

-- The check basis: hash of the manifest and project settings the environment last passed
-- a full check under. NULL or a different basis means the next check is full.
ALTER TABLE environments ADD COLUMN validation_basis bytea;

-- +goose Down
ALTER TABLE environments DROP COLUMN validation_basis;
DROP TRIGGER version_references ON object_versions;
DROP FUNCTION index_version_references();
DROP FUNCTION version_reference_ids(jsonb);
DROP INDEX published_pointers_version;
DROP TABLE version_references;
