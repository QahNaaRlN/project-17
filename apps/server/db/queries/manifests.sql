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
VALUES ($1, $2, $3, $4) ON CONFLICT (manifest_id, schema_name) DO NOTHING;

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
SELECT o.id AS object_id, v.id AS version_id, v.body, v.path, v.certified, 'head'::text AS stage, NULL::uuid AS changeset_id
FROM objects o JOIN object_versions v ON v.id = o.head_version_id AND v.object_id = o.id
CROSS JOIN target_environment e
WHERE o.project_id = sqlc.arg(project_id) AND o.kind = 'document' AND o.deleted_at IS NULL
UNION ALL
SELECT o.id, v.id, v.body, v.path, v.certified, 'published'::text, NULL::uuid
FROM target_environment e JOIN published_pointers pp ON pp.environment_id = e.id
JOIN objects o ON o.id = pp.object_id
JOIN object_versions v ON v.id = pp.version_id AND v.object_id = o.id
WHERE o.project_id = sqlc.arg(project_id) AND o.kind = 'document'
UNION ALL
SELECT o.id, v.id, v.body, v.path, v.certified, 'working'::text, cs.id
FROM changesets cs JOIN changeset_objects co ON co.changeset_id = cs.id
JOIN objects o ON o.id = co.object_id
JOIN object_versions v ON v.id = co.working_version_id AND v.object_id = o.id
CROSS JOIN target_environment e
WHERE cs.project_id = sqlc.arg(project_id) AND o.project_id = sqlc.arg(project_id)
  AND o.kind = 'document' AND cs.state NOT IN ('merged', 'abandoned')
ORDER BY object_id, stage, version_id, changeset_id;

-- name: GetManifestEnvironment :one
SELECT * FROM environments WHERE project_id = $1 AND name = $2;

-- name: PendingManifestCandidate :one
SELECT cs.id FROM changesets cs JOIN changeset_manifest_candidates b ON b.changeset_id = cs.id
WHERE b.project_id = $1 AND b.environment_id = $2 AND b.manifest_id = $3
AND b.base_manifest_id IS NOT DISTINCT FROM sqlc.narg(base_manifest_id)::uuid
AND cs.state NOT IN ('merged', 'abandoned') ORDER BY cs.created_at DESC LIMIT 1;

-- name: CreateManifestCandidate :one
INSERT INTO changesets (id, project_id, kind, title, owner_id, state, targets)
VALUES ($1, $2, 'schema', $3, $4, 'open', $5) RETURNING *;

-- name: BindManifestCandidate :exec
INSERT INTO changeset_manifest_candidates (changeset_id, project_id, environment_id, manifest_id, base_manifest_id)
VALUES ($1, $2, $3, $4, $5);

-- name: ManifestChangesets :many
SELECT * FROM changesets WHERE project_id = $1 AND state NOT IN ('merged', 'abandoned')
AND (cardinality(targets) = 0 OR sqlc.arg(environment)::text = ANY(targets)) ORDER BY id FOR UPDATE;

-- name: RecordManifestDiagnostics :exec
INSERT INTO changeset_manifest_diagnostics (changeset_id, project_id, environment_id, manifest_id, checked_seq, diagnostics)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (changeset_id, environment_id) DO UPDATE SET manifest_id = EXCLUDED.manifest_id,
checked_seq = EXCLUDED.checked_seq, diagnostics = EXCLUDED.diagnostics, stale = false, checked_at = now();

-- name: ChangesetManifestDiagnostics :many
SELECT e.name AS environment, m.hash AS manifest_hash, d.diagnostics, d.checked_at,
(d.checked_seq IS NULL OR d.stale OR d.checked_seq <> cs.seq
 OR d.manifest_id IS DISTINCT FROM COALESCE(b.manifest_id,e.active_manifest_id)
 OR (b.changeset_id IS NOT NULL AND b.base_manifest_id IS DISTINCT FROM e.active_manifest_id))::boolean AS stale
FROM changesets cs JOIN environments e ON e.project_id = cs.project_id
LEFT JOIN changeset_manifest_candidates b ON b.changeset_id = cs.id AND b.environment_id = e.id
LEFT JOIN changeset_manifest_diagnostics d ON d.changeset_id = cs.id AND d.environment_id = e.id
LEFT JOIN manifests m ON m.id = d.manifest_id
WHERE cs.project_id = $1 AND cs.id = $2
AND (cardinality(cs.targets) = 0 OR e.name = ANY(cs.targets))
AND (cs.kind <> 'schema' OR b.changeset_id IS NOT NULL) ORDER BY e.name;
