-- name: CreateDeliveryKey :one
INSERT INTO delivery_keys (id, project_id, environment_id, name, token_hash, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListDeliveryKeys :many
SELECT k.id, k.name, k.created_by, k.created_at, k.revoked_at, e.name AS environment_name
FROM delivery_keys k JOIN environments e ON e.id = k.environment_id
WHERE k.project_id = $1
ORDER BY k.created_at, k.id;

-- name: RevokeDeliveryKey :one
UPDATE delivery_keys k SET revoked_at = now()
FROM environments e
WHERE k.id = $1 AND k.project_id = $2 AND k.revoked_at IS NULL AND e.id = k.environment_id
RETURNING k.id, k.name, k.created_by, k.created_at, k.revoked_at, e.name AS environment_name;

-- name: AuthenticateDeliveryKey :one
SELECT k.project_id, k.environment_id, p.slug AS project_slug, e.name AS environment_name
FROM delivery_keys k
JOIN projects p ON p.id = k.project_id
JOIN environments e ON e.id = k.environment_id
WHERE k.token_hash = $1 AND k.revoked_at IS NULL;

-- name: GetDeliveredDocument :one
-- Опубликованная в окружении версия документа.
SELECT o.id, o.doc_kind, v.id AS version_id, v.path, v.body
FROM published_pointers pp
JOIN objects o ON o.id = pp.object_id
JOIN object_versions v ON v.id = pp.version_id
WHERE pp.environment_id = $1 AND pp.object_id = $2 AND o.kind = 'document';

-- name: GetEnvironmentPreviewKey :one
SELECT e.id, e.name, e.preview_key, p.id AS project_id, p.slug AS project_slug
FROM environments e JOIN projects p ON p.id = e.project_id
WHERE p.slug = $1 AND e.name = $2;

-- name: ListDraftRoutes :many
-- Маршруты чернового режима (API-040): объекты Change Set — из рабочих версий, остальные — из head.
WITH ws AS (
  SELECT co.object_id, v.id AS version_id, v.path
  FROM changeset_objects co JOIN object_versions v ON v.id = co.working_version_id
  WHERE co.changeset_id = sqlc.narg(changeset_id)::uuid
)
SELECT ws.path::text AS path, ws.object_id, ws.version_id FROM ws WHERE ws.path IS NOT NULL
UNION ALL
SELECT o.head_path::text, o.id, o.head_version_id::uuid
FROM objects o
WHERE o.project_id = sqlc.arg(project_id) AND o.head_path IS NOT NULL AND o.deleted_at IS NULL
  AND o.id NOT IN (SELECT object_id FROM ws)
ORDER BY 1;
