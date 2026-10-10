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
