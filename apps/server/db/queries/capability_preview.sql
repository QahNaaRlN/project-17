-- name: CreatePreviewEnvironment :one
INSERT INTO environments (id, project_id, name, kind, app_url, expires_at, source_base_environment_id, source_config_id, source_repository_id, source_pull_number)
VALUES ($1, $2, $3, 'preview', $4, now() + interval '14 days', $5, $6, $7, $8)
RETURNING *;

-- name: GetEnvironmentByID :one
SELECT * FROM environments WHERE project_id = $1 AND id = $2;

-- name: LockEnvironmentByID :one
SELECT * FROM environments WHERE project_id = $1 AND id = $2 FOR UPDATE;

-- name: LatestArchivedEnvironment :one
SELECT * FROM environments WHERE project_id = $1 AND archived_name = sqlc.arg(name)::text
ORDER BY archived_at DESC, id DESC LIMIT 1;

-- name: RecordPreviewRegistration :one
UPDATE environments SET registration_seq = registration_seq + 1, last_registered_at = now(), expires_at = now() + interval '14 days'
WHERE project_id = $1 AND id = $2 AND archived_at IS NULL
RETURNING *;

-- name: InsertPreviewRegistration :one
INSERT INTO preview_registrations (id, project_id, environment_id, seq, manifest_id, commit, app_url, registered_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: CurrentPreviewRegistration :one
SELECT r.* FROM preview_registrations r JOIN environments e ON e.id = r.environment_id AND e.registration_seq = r.seq
WHERE r.project_id = $1 AND r.environment_id = $2;

-- name: ArchiveEnvironment :one
UPDATE environments SET archived_name = name, name = sqlc.arg(tombstone)::text, archived_at = now(), archive_reason = sqlc.arg(reason)::text,
  active_manifest_id = NULL, preview_key = (uuid_send(gen_random_uuid()) || uuid_send(gen_random_uuid()))
WHERE project_id = $1 AND id = $2 AND kind = 'preview' AND archived_at IS NULL
RETURNING *;

-- name: DeleteEnvironmentPresence :exec
DELETE FROM presence_leases WHERE project_id = $1 AND environment = $2;

-- name: ExpiredPreviewEnvironments :many
SELECT project_id, id FROM environments
WHERE kind = 'preview' AND archived_at IS NULL AND expires_at <= now()
ORDER BY expires_at, id LIMIT 100;

-- name: ActivePreviewBySource :many
SELECT * FROM environments
WHERE project_id = $1 AND archived_at IS NULL AND source_config_id = $2 AND source_repository_id = $3 AND source_pull_number = $4
ORDER BY name;

-- name: PreviewEnvironmentsForSource :many
SELECT * FROM environments
WHERE project_id = $1 AND source_config_id = $2 AND source_pull_number = $3
  AND (archived_at IS NULL OR archived_at > now() - interval '14 days')
ORDER BY archived_at NULLS FIRST, created_at DESC, id DESC LIMIT 10;

-- name: InsertPreviewEnvironmentAudit :exec
INSERT INTO preview_environment_audit (id, project_id, environment_id, actor_id, event, reason, data)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetCapabilityPreviewArgs :one
SELECT * FROM capability_preview_args WHERE project_id = $1 AND request_id = $2 AND environment_id = $3;

-- name: SetCapabilityPreviewArgs :one
INSERT INTO capability_preview_args (project_id, request_id, environment_id, manifest_id, args, set_by)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (project_id, request_id, environment_id) DO UPDATE
SET manifest_id = EXCLUDED.manifest_id, args = EXCLUDED.args, set_by = EXCLUDED.set_by, set_at = now(),
    revision = capability_preview_args.revision + 1
RETURNING *;
