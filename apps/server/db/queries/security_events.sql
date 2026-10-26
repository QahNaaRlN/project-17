-- name: InsertSecurityEvent :exec
INSERT INTO security_events (id, project_id, actor_id, type, data) VALUES ($1, $2, $3, $4, $5);

-- name: ListSecurityEvents :many
SELECT * FROM security_events
WHERE project_id = sqlc.arg(project_id)
  AND (sqlc.narg(type)::text IS NULL OR type = sqlc.narg(type))
  AND (sqlc.narg(before_time)::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg(before_time)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit);
