-- name: ObjectOperations :many
-- Операции Change Set над объектом в порядке seq (кроме исключённых при rebase).
SELECT * FROM operations
WHERE changeset_id = $1 AND target_object_id = $2 AND status <> 'dropped'
ORDER BY seq;

-- name: SetOperationStatus :exec
UPDATE operations SET status = $2 WHERE id = $1;

-- name: ReplayOperation :exec
-- Операция переиграна на новой базе: «до», «после» и обратная операция пересчитаны.
UPDATE operations SET status = 'applied', before = $2, after = $3, inverse = $4 WHERE id = $1;

-- name: RebaseWorkingVersion :exec
UPDATE object_versions SET parent_version_id = $2, body = $3, body_hash = $4
WHERE id = $1 AND state = 'working';

-- name: SetChangesetObjectBase :exec
UPDATE changeset_objects SET base_version_id = $3 WHERE changeset_id = $1 AND object_id = $2;

-- name: RemoveChangesetObject :exec
DELETE FROM changeset_objects WHERE changeset_id = $1 AND object_id = $2;

-- name: DeleteWorkingVersion :exec
DELETE FROM object_versions WHERE id = $1 AND state = 'working';

-- name: SetChangesetConflicts :exec
UPDATE changesets SET has_conflicts = $2, updated_at = now() WHERE id = $1;

-- name: RebindApprovals :exec
-- CHG-040: согласования переходят на новое содержимое после rebase без конфликтов.
UPDATE approvals SET content_hash = sqlc.arg(new_hash)
WHERE changeset_id = $1 AND invalidated_at IS NULL AND content_hash = sqlc.arg(old_hash);
