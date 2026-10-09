-- name: GetEnvironmentByName :one
SELECT * FROM environments WHERE project_id = $1 AND name = $2;

-- name: LockObject :one
SELECT * FROM objects WHERE id = $1 FOR UPDATE;

-- name: NextVersionNumber :one
SELECT COALESCE(max(number), 0) + 1 FROM object_versions WHERE object_id = $1;

-- name: CommitVersion :exec
UPDATE object_versions SET state = 'committed', number = $2, committed_at = now()
WHERE id = $1 AND state = 'working';

-- name: SetHead :exec
UPDATE objects SET head_version_id = $2 WHERE id = $1;

-- name: GetPublishedPointer :one
SELECT * FROM published_pointers WHERE environment_id = $1 AND object_id = $2;

-- name: UpsertPublishedPointer :exec
INSERT INTO published_pointers (environment_id, object_id, version_id, publication_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (environment_id, object_id) DO UPDATE
SET version_id = EXCLUDED.version_id, publication_id = EXCLUDED.publication_id;

-- name: DeletePublishedPointer :exec
DELETE FROM published_pointers WHERE environment_id = $1 AND object_id = $2;

-- name: CreatePublication :one
INSERT INTO publications (id, project_id, environment_id, changeset_id, kind, source_publication_id, actor_id, reason)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: AddPublicationItem :exec
INSERT INTO publication_items (publication_id, object_id, previous_version_id, current_version_id)
VALUES ($1, $2, $3, $4);

-- name: GetPublication :one
SELECT p.*, e.name AS environment_name
FROM publications p JOIN environments e ON e.id = p.environment_id
WHERE p.id = $1 AND p.project_id = $2;

-- name: ListPublicationItems :many
SELECT * FROM publication_items WHERE publication_id = $1 ORDER BY object_id;

-- name: ListPublications :many
SELECT p.*, e.name AS environment_name
FROM publications p JOIN environments e ON e.id = p.environment_id
WHERE p.project_id = $1 AND (sqlc.narg(environment)::text IS NULL OR e.name = sqlc.narg(environment))
ORDER BY p.created_at DESC, p.id DESC
LIMIT 100;

-- name: GetPublishedDocument :one
SELECT o.id, o.doc_kind, v.id AS version_id, v.state, v.body
FROM published_pointers pp
JOIN objects o ON o.id = pp.object_id
JOIN object_versions v ON v.id = pp.version_id
JOIN environments e ON e.id = pp.environment_id
WHERE pp.object_id = $1 AND o.project_id = $2 AND e.name = $3 AND o.kind = 'document';
