-- name: PendingComponentRequest :one
SELECT id, kind, key, status, proposed_contract FROM capability_requests WHERE project_id = $1 AND id = $2;
