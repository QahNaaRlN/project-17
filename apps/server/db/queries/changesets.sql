-- name: CreateChangeset :one
INSERT INTO changesets (id, project_id, kind, title, description, owner_id, state)
VALUES ($1, $2, 'standard', $3, $4, $5, 'open')
RETURNING *;

-- name: GetChangeset :one
SELECT * FROM changesets WHERE id = $1 AND project_id = $2;

-- name: LockChangeset :one
SELECT * FROM changesets WHERE id = $1 AND project_id = $2 FOR UPDATE;

-- name: ListChangesets :many
SELECT * FROM changesets
WHERE project_id = $1 AND (sqlc.narg(state)::text IS NULL OR state = sqlc.narg(state))
ORDER BY created_at DESC, id DESC
LIMIT 100;

-- name: SetChangesetSeq :exec
UPDATE changesets SET seq = $2, updated_at = now() WHERE id = $1;

-- name: SetChangesetState :exec
UPDATE changesets SET state = $2, updated_at = now() WHERE id = $1;

-- name: CreateObject :one
INSERT INTO objects (id, project_id, kind, doc_kind) VALUES ($1, $2, 'document', $3)
RETURNING *;

-- name: GetObject :one
SELECT * FROM objects WHERE id = $1 AND project_id = $2 AND deleted_at IS NULL;

-- name: CreateWorkingVersion :one
INSERT INTO object_versions (id, project_id, object_id, state, changeset_id, parent_version_id, ir_version, path, body, body_hash, created_by)
VALUES ($1, $2, $3, 'working', $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: UpdateWorkingVersion :exec
UPDATE object_versions SET path = $2, body = $3, body_hash = $4, certified = $5 WHERE id = $1 AND state = 'working';

-- name: GetVersion :one
SELECT * FROM object_versions WHERE id = $1;

-- name: AddChangesetObject :exec
INSERT INTO changeset_objects (changeset_id, object_id, base_version_id, working_version_id)
VALUES ($1, $2, $3, $4);

-- name: GetChangesetObject :one
SELECT co.*, v.path AS working_path, v.body AS working_body, v.certified AS working_certified
FROM changeset_objects co
JOIN object_versions v ON v.id = co.working_version_id
WHERE co.changeset_id = $1 AND co.object_id = $2;

-- name: ListChangesetObjects :many
SELECT co.object_id, co.base_version_id, co.working_version_id, o.doc_kind
FROM changeset_objects co
JOIN objects o ON o.id = co.object_id
WHERE co.changeset_id = $1
ORDER BY co.object_id;

-- name: InsertOperation :one
INSERT INTO operations (id, project_id, changeset_id, seq, actor_id, source, target_object_id, type,
                        payload, before, after, inverse, reason, client_op_id, undo_of)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: ListOperations :many
SELECT * FROM operations WHERE changeset_id = $1 AND seq > $2 ORDER BY seq LIMIT 500;

-- name: LastUndoableOperation :one
-- Последняя прямая (не отменяющая) операция, ещё не отменённая.
SELECT o.* FROM operations o
WHERE o.changeset_id = $1 AND o.undo_of IS NULL AND o.status = 'applied'
  AND NOT EXISTS (SELECT 1 FROM operations u WHERE u.undo_of = o.id)
ORDER BY o.seq DESC
LIMIT 1;

-- name: GetHeadDocument :one
SELECT o.id, o.doc_kind, v.id AS version_id, v.state, v.path, v.body
FROM objects o
JOIN object_versions v ON v.id = o.head_version_id
WHERE o.id = $1 AND o.project_id = $2 AND o.kind = 'document' AND o.deleted_at IS NULL;

-- name: GetWorkingDocument :one
SELECT o.id, o.doc_kind, v.id AS version_id, v.state, v.path, v.body
FROM changeset_objects co
JOIN objects o ON o.id = co.object_id
JOIN object_versions v ON v.id = co.working_version_id
WHERE co.changeset_id = $1 AND co.object_id = $2 AND o.project_id = $3 AND o.kind = 'document';

-- name: RouteTaken :one
-- Объект, чей head уже занимает маршрут той же формы (route_shape), кроме данного.
SELECT id FROM objects
WHERE project_id = $1 AND id <> $2 AND deleted_at IS NULL AND head_path IS NOT NULL
  AND route_shape(head_path) = route_shape(sqlc.arg(path)::text)
LIMIT 1;
