-- name: InsertManifest :exec
-- Повтор hash не изменяет исходные тело, автора, время и code index (MF-002).
INSERT INTO manifests (id, project_id, hash, app_version, body, code_index_key, registered_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (project_id, hash) DO NOTHING;

-- name: GetManifestByHash :one
SELECT * FROM manifests WHERE project_id = $1 AND hash = $2;

-- name: GetManifestByID :one
SELECT * FROM manifests WHERE project_id = $1 AND id = $2;

-- name: InsertPreviewSchemaSnapshot :exec
INSERT INTO preview_schema_snapshots (manifest_id, schema_name, version, body)
VALUES ($1, $2, $3, $4);

-- name: ListPreviewSchemaSnapshots :many
SELECT s.* FROM preview_schema_snapshots s JOIN manifests m ON m.id = s.manifest_id
WHERE m.project_id = $1 AND s.manifest_id = $2 ORDER BY s.schema_name;

-- name: InsertStandardSchemaVersion :exec
INSERT INTO schema_versions (project_id, schema_name, version, body, manifest_id)
VALUES ($1, $2, $3, $4, $5);

-- name: GetStandardSchemaVersion :one
SELECT * FROM schema_versions WHERE project_id = $1 AND schema_name = $2 AND version = $3;

-- name: LatestStandardSchemaVersion :one
SELECT * FROM schema_versions WHERE project_id = $1 AND schema_name = $2 ORDER BY version DESC LIMIT 1;

-- name: LockManifestEnvironment :one
SELECT * FROM environments WHERE project_id = $1 AND name = $2 FOR UPDATE;

-- name: SetActiveManifest :execrows
UPDATE environments SET active_manifest_id = $3
WHERE project_id = $1 AND id = $2;
