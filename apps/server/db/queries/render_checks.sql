-- name: InsertRenderCheck :exec
INSERT INTO checks (id, changeset_id, content_hash, environment, stage, status, blocking, details, started_at, finished_at)
VALUES (sqlc.arg(id), sqlc.arg(changeset_id), sqlc.arg(content_hash), sqlc.arg(environment), sqlc.arg(stage),
  sqlc.arg(status)::text, sqlc.arg(blocking), sqlc.arg(details),
  CASE WHEN sqlc.arg(status)::text = 'queued' THEN NULL ELSE now() END,
  CASE WHEN sqlc.arg(status)::text = 'queued' THEN NULL ELSE now() END);

-- name: ClaimRenderCheck :one
UPDATE checks SET status = 'running', lease_id = sqlc.arg(lease_id)::uuid, lease_expires_at = now() + interval '5 minutes',
  started_at = COALESCE(started_at, now()), attempts = attempts + 1
WHERE checks.id = (
  SELECT c.id FROM checks c JOIN changesets cs ON cs.id = c.changeset_id
  WHERE cs.project_id = sqlc.arg(project_id) AND cs.state = 'checking' AND c.content_hash = cs.content_hash
    AND c.status = 'queued' AND c.stage IN ('preview', 'axe')
  ORDER BY c.created_at, c.id LIMIT 1 FOR UPDATE OF c SKIP LOCKED)
RETURNING *;

-- name: LockRenderCheck :one
SELECT c.* FROM checks c JOIN changesets cs ON cs.id = c.changeset_id
WHERE c.id = $1 AND cs.project_id = $2 AND c.stage IN ('preview', 'axe') FOR UPDATE OF c;

-- name: FinishRenderCheck :exec
UPDATE checks SET status = $2, details = $3, finished_at = now(), lease_id = NULL, lease_expires_at = NULL WHERE id = $1;

-- name: RequeueRenderCheck :exec
UPDATE checks SET status = 'queued', lease_id = NULL, lease_expires_at = NULL WHERE id = $1;

-- name: ExpiredRenderChecks :many
SELECT c.id, cs.id AS changeset_id, cs.project_id FROM checks c JOIN changesets cs ON cs.id = c.changeset_id
WHERE c.status = 'running' AND c.stage IN ('preview', 'axe') AND c.lease_expires_at < now()
ORDER BY c.lease_expires_at, c.id LIMIT 100;

-- name: SkipOpenRenderChecks :exec
UPDATE checks SET status = 'skipped', details = details || jsonb_build_object('reason', sqlc.arg(reason)::text),
  finished_at = now(), lease_id = NULL, lease_expires_at = NULL
WHERE changeset_id = sqlc.arg(changeset_id) AND status IN ('queued', 'running');

-- name: SetEnvironmentAppURL :one
UPDATE environments SET app_url = $3 WHERE project_id = $1 AND name = $2 AND kind = 'standard' RETURNING *;

-- name: RenderCheckChangeset :one
SELECT c.changeset_id FROM checks c JOIN changesets cs ON cs.id = c.changeset_id
WHERE c.id = $1 AND cs.project_id = $2 AND c.stage IN ('preview', 'axe');

-- name: ProjectRenderChecks :one
SELECT COALESCE((SELECT enabled FROM project_render_checks WHERE project_id = $1), true)::boolean;
