-- name: SubmitChangeset :exec
UPDATE changesets SET state = $2, risk = $3, content_hash = $4, targets = $5,
  submitted_at = now(), updated_at = now()
WHERE id = $1;

-- name: MergeChangeset :exec
UPDATE changesets SET state = 'merged', merged_at = now(), updated_at = now() WHERE id = $1;

-- name: ChangesetBaseMismatches :many
-- Объекты Change Set, head которых изменился после начала работы (нужен rebase).
SELECT co.object_id
FROM changeset_objects co
JOIN objects o ON o.id = co.object_id
WHERE co.changeset_id = $1
  AND o.head_version_id IS DISTINCT FROM co.base_version_id
ORDER BY co.object_id;

-- name: ChangesetWorkingVersions :many
SELECT co.object_id, co.base_version_id, v.id AS version_id, v.path, v.body, v.body_hash, v.certified
FROM changeset_objects co
JOIN object_versions v ON v.id = co.working_version_id
WHERE co.changeset_id = $1
ORDER BY co.object_id;

-- name: ChangesetOperationActors :many
SELECT DISTINCT actor_id, type FROM operations WHERE changeset_id = $1 AND status = 'applied';

-- name: ChangesetAuthors :many
-- UNION убирает повторы; «DISTINCT on_behalf_of» парсер принял бы за DISTINCT ON.
SELECT a.actor_id AS author_id FROM operations a WHERE a.changeset_id = sqlc.arg(changeset_id)::uuid
UNION
SELECT b.on_behalf_of FROM operations b WHERE b.changeset_id = sqlc.arg(changeset_id)::uuid AND b.on_behalf_of IS NOT NULL;

-- name: InsertCheck :exec
INSERT INTO checks (id, changeset_id, content_hash, stage, status, blocking, details, started_at, finished_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now());

-- name: ListChecks :many
SELECT * FROM checks WHERE changeset_id = $1 AND content_hash = $2 ORDER BY stage;

-- name: InsertApproval :exec
INSERT INTO approvals (id, changeset_id, approver_id, decision, content_hash, comment)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: CountValidApprovals :one
SELECT count(DISTINCT approver_id) FROM approvals
WHERE changeset_id = $1 AND content_hash = $2 AND decision = 'approve' AND invalidated_at IS NULL;

-- name: InvalidateApprovals :exec
UPDATE approvals SET invalidated_at = now() WHERE changeset_id = $1 AND invalidated_at IS NULL;

-- name: ListApprovals :many
SELECT * FROM approvals WHERE changeset_id = $1 ORDER BY created_at;

-- name: GetProjectSettings :one
SELECT settings FROM projects WHERE id = $1;

-- name: SetProjectSettings :exec
UPDATE projects SET settings = $2 WHERE id = $1;
