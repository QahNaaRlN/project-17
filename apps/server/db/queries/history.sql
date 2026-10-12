-- name: LastRedoableOperation :one
SELECT u.* FROM operations u
JOIN changesets c ON c.id = u.changeset_id
WHERE u.changeset_id = $1 AND u.undo_of IS NOT NULL AND u.status = 'applied'
  AND u.seq > c.redo_barrier_seq
  AND NOT EXISTS (SELECT 1 FROM operations r WHERE r.redo_of = u.id)
  AND NOT EXISTS (SELECT 1 FROM operations d WHERE d.changeset_id = u.changeset_id
    AND d.seq > u.seq AND d.undo_of IS NULL AND d.redo_of IS NULL AND d.status = 'applied')
ORDER BY u.seq DESC LIMIT 1;

-- name: ActionOperations :many
SELECT * FROM operations
WHERE changeset_id = $1 AND COALESCE(batch_id, id) = sqlc.arg(action_id)::uuid
ORDER BY seq DESC;

-- name: HistoryHeads :many
SELECT o.id, o.batch_id, o.seq, o.actor_id, a.display_name, o.created_at, o.reason, o.undo_of, o.redo_of
FROM operations o JOIN actors a ON a.id = o.actor_id
WHERE o.changeset_id = sqlc.arg(changeset_id)
  AND o.seq IN (SELECT max(seq) FROM operations WHERE changeset_id = sqlc.arg(changeset_id)
    GROUP BY COALESCE(batch_id, id) HAVING max(seq) < sqlc.arg(before_seq)::integer)
ORDER BY o.seq DESC LIMIT 21;

-- name: ClearRedo :exec
UPDATE changesets SET redo_barrier_seq = seq WHERE id = $1;
