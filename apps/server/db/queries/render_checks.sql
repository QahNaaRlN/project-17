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
  WHERE cs.project_id = sqlc.arg(project_id) AND c.content_hash = cs.content_hash
    AND c.status = 'queued' AND c.stage IN ('preview', 'axe', 'visual', 'lighthouse')
    -- STU-571: non-blocking checks also run after the Change Set left checking.
    AND (cs.state = 'checking' OR (NOT c.blocking AND cs.state IN ('in_review', 'approved', 'changes_requested')))
  ORDER BY c.blocking DESC, c.created_at, c.id LIMIT 1 FOR UPDATE OF c SKIP LOCKED)
RETURNING *;

-- name: LockRenderCheck :one
SELECT c.* FROM checks c JOIN changesets cs ON cs.id = c.changeset_id
WHERE c.id = $1 AND cs.project_id = $2 AND c.stage IN ('preview', 'axe', 'visual', 'lighthouse') FOR UPDATE OF c;

-- name: FinishRenderCheck :exec
UPDATE checks SET status = $2, details = $3, finished_at = now(), lease_id = NULL, lease_expires_at = NULL WHERE id = $1;

-- name: RequeueRenderCheck :exec
UPDATE checks SET status = 'queued', lease_id = NULL, lease_expires_at = NULL WHERE id = $1;

-- name: ExpiredRenderChecks :many
SELECT c.id, cs.id AS changeset_id, cs.project_id FROM checks c JOIN changesets cs ON cs.id = c.changeset_id
WHERE c.status = 'running' AND c.stage IN ('preview', 'axe', 'visual', 'lighthouse') AND c.lease_expires_at < now()
ORDER BY c.lease_expires_at, c.id LIMIT 100;

-- name: SkipOpenRenderChecks :exec
UPDATE checks SET status = 'skipped', details = details || jsonb_build_object('reason', sqlc.arg(reason)::text),
  finished_at = now(), lease_id = NULL, lease_expires_at = NULL
WHERE changeset_id = sqlc.arg(changeset_id) AND status IN ('queued', 'running');

-- name: SetEnvironmentAppURL :one
UPDATE environments SET app_url = $3 WHERE project_id = $1 AND name = $2 AND kind = 'standard' RETURNING *;

-- name: RenderCheckChangeset :one
SELECT c.changeset_id FROM checks c JOIN changesets cs ON cs.id = c.changeset_id
WHERE c.id = $1 AND cs.project_id = $2 AND c.stage IN ('preview', 'axe', 'visual', 'lighthouse');

-- name: ProjectRenderChecks :one
SELECT COALESCE((SELECT enabled FROM project_render_checks WHERE project_id = $1), true)::boolean;

-- name: ExtendRenderLease :one
-- STU-585: the runner extends its lease by 5 minutes, never past the stage limit.
UPDATE checks SET lease_expires_at = LEAST(now() + interval '5 minutes', started_at + sqlc.arg(limit_seconds)::int * interval '1 second')
WHERE id = sqlc.arg(id) AND status = 'running' AND lease_id = sqlc.arg(lease_id) AND lease_expires_at > now()
RETURNING lease_expires_at;

-- name: UpsertCheckArtifact :exec
INSERT INTO check_artifacts (check_id, name, content_type, size, sha256)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (check_id, name) DO UPDATE SET content_type = EXCLUDED.content_type, size = EXCLUDED.size,
  sha256 = EXCLUDED.sha256, created_at = now();

-- name: CheckArtifactTotals :one
SELECT count(*)::int AS count, COALESCE(sum(size), 0)::bigint AS size FROM check_artifacts
WHERE check_id = sqlc.arg(check_id) AND name <> sqlc.arg(name);

-- name: ListCheckArtifacts :many
SELECT * FROM check_artifacts WHERE check_id = $1 ORDER BY name;

-- name: SetCheckArtifacts :exec
UPDATE checks SET artifacts = $2 WHERE id = $1;

-- name: GetCheckArtifact :one
-- STU-575: an artifact of a check of a Change Set in the project.
SELECT a.*, c.changeset_id, c.artifacts_expired_at FROM check_artifacts a
JOIN checks c ON c.id = a.check_id JOIN changesets cs ON cs.id = c.changeset_id
WHERE a.check_id = sqlc.arg(check_id) AND a.name = sqlc.arg(name) AND c.changeset_id = sqlc.arg(changeset_id)
  AND cs.project_id = sqlc.arg(project_id);

-- name: ExpiringArtifactChecks :many
-- STU-583: checks of final Change Sets or of replaced content, finished long enough ago.
SELECT c.id, c.changeset_id, cs.project_id FROM checks c JOIN changesets cs ON cs.id = c.changeset_id
WHERE c.artifacts_expired_at IS NULL AND c.finished_at < sqlc.arg(before)::timestamptz
  AND (cs.state IN ('merged', 'abandoned') OR c.content_hash IS DISTINCT FROM cs.content_hash)
  AND EXISTS (SELECT 1 FROM check_artifacts a WHERE a.check_id = c.id)
ORDER BY c.finished_at, c.id LIMIT 100;

-- name: MarkArtifactsExpired :exec
UPDATE checks SET artifacts_expired_at = now() WHERE id = $1;
