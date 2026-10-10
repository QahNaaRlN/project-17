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

-- name: ManifestImpactVersions :many
-- MF-020, MF-023: стадии сохраняются отдельно даже для одного version_id.
-- Опубликованные версии выбираются только для указанного окружения; head/рабочие — по проекту.
WITH target_environment AS (
 SELECT env.id FROM environments env WHERE env.project_id = sqlc.arg(project_id)::uuid AND env.name = sqlc.arg(environment)::text
)
SELECT o.id AS object_id, v.id AS version_id, v.body, v.path, 'head'::text AS stage, NULL::uuid AS changeset_id
FROM objects o JOIN object_versions v ON v.id = o.head_version_id AND v.object_id = o.id
CROSS JOIN target_environment e
WHERE o.project_id = sqlc.arg(project_id) AND o.kind = 'document' AND o.deleted_at IS NULL
UNION ALL
SELECT o.id, v.id, v.body, v.path, 'published'::text, NULL::uuid
FROM target_environment e JOIN published_pointers pp ON pp.environment_id = e.id
JOIN objects o ON o.id = pp.object_id
JOIN object_versions v ON v.id = pp.version_id AND v.object_id = o.id
WHERE o.project_id = sqlc.arg(project_id) AND o.kind = 'document'
UNION ALL
SELECT o.id, v.id, v.body, v.path, 'working'::text, cs.id
FROM changesets cs JOIN changeset_objects co ON co.changeset_id = cs.id
JOIN objects o ON o.id = co.object_id
JOIN object_versions v ON v.id = co.working_version_id AND v.object_id = o.id
CROSS JOIN target_environment e
WHERE cs.project_id = sqlc.arg(project_id) AND o.project_id = sqlc.arg(project_id)
  AND o.kind = 'document' AND cs.state NOT IN ('merged', 'abandoned')
ORDER BY object_id, stage, version_id, changeset_id;
