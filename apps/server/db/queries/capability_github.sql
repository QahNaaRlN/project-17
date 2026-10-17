-- name: GetCapabilityGithubConfig :one
SELECT * FROM capability_github_configs WHERE project_id=$1 AND environment=$2;
-- name: LockCapabilityGithubConfig :one
SELECT * FROM capability_github_configs WHERE project_id=$1 AND environment=$2 FOR UPDATE;
-- name: SetCapabilityGithubConfig :one
INSERT INTO capability_github_configs(id,project_id,environment_id,environment,config_id,fingerprint,credential_id,enabled,changed_by)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(project_id,environment) DO UPDATE SET config_id=EXCLUDED.config_id,fingerprint=EXCLUDED.fingerprint,credential_id=EXCLUDED.credential_id,enabled=EXCLUDED.enabled,changed_by=EXCLUDED.changed_by,changed_at=now(),revision=capability_github_configs.revision+1
RETURNING *;
-- name: InvalidateCapabilityGithubLinks :exec
UPDATE capability_github_links SET generation=generation+1,revision=revision+1,state='stopped',last_error='config_changed',lease_token=NULL,lease_until=NULL,updated_at=now() WHERE project_id=$1 AND config_id=$2;
-- name: GetCapabilityGithubLink :one
SELECT * FROM capability_github_links WHERE project_id=$1 AND request_id=$2;
-- name: LockCapabilityGithubLink :one
SELECT * FROM capability_github_links WHERE project_id=$1 AND request_id=$2 FOR UPDATE;
-- name: SetCapabilityGithubLink :one
INSERT INTO capability_github_links(id,project_id,request_id,config_id,config_revision,fingerprint,pull_number,initiated_by,sync_id)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(project_id,request_id) DO UPDATE SET config_id=EXCLUDED.config_id,config_revision=EXCLUDED.config_revision,fingerprint=EXCLUDED.fingerprint,pull_number=EXCLUDED.pull_number,initiated_by=EXCLUDED.initiated_by,sync_id=EXCLUDED.sync_id,revision=capability_github_links.revision+1,generation=capability_github_links.generation+1,active=true,state='queued',last_error='',attempts=0,polls_left=5,window_until=now()+interval '30 minutes',next_at=now(),refresh_after=now()+interval '60 seconds',lease_token=NULL,lease_until=NULL,updated_at=now()
RETURNING *;
-- name: UnlinkCapabilityGithub :one
UPDATE capability_github_links SET active=false,state='stopped',generation=generation+1,revision=revision+1,lease_token=NULL,lease_until=NULL,last_error='unlinked',updated_at=now() WHERE project_id=$1 AND request_id=$2 RETURNING *;
-- name: SaveCapabilityGithubSync :one
UPDATE capability_github_links SET state=$3,last_error=$4,attempts=$5,polls_left=$6,next_at=$7,lease_token=$8,lease_until=$9,sync_id=$10,updated_at=now() WHERE project_id=$1 AND request_id=$2 RETURNING *;
-- name: DueCapabilityGithubLinks :many
SELECT project_id,request_id FROM capability_github_links WHERE active AND ((state IN ('queued','retry_wait') AND next_at<=now()) OR (state='sending' AND lease_until<=now())) ORDER BY next_at,id LIMIT 100;
-- name: InsertCapabilityGithubObservation :exec
INSERT INTO capability_github_observations(id,project_id,request_id,link_id,generation,config_revision,fingerprint,sync_id,body,digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(link_id,sync_id) DO NOTHING;
-- name: LatestCapabilityGithubObservation :one
SELECT * FROM capability_github_observations WHERE project_id=$1 AND request_id=$2 ORDER BY observed_at DESC,id DESC LIMIT 1;
-- name: ListCapabilityGithubObservations :many
SELECT * FROM capability_github_observations WHERE project_id=$1 AND request_id=$2 AND (sqlc.narg(before_time)::timestamptz IS NULL OR (observed_at,id)<(sqlc.narg(before_time)::timestamptz,sqlc.narg(before_id)::uuid)) ORDER BY observed_at DESC,id DESC LIMIT sqlc.arg(page_limit);
-- name: InsertCapabilityGithubAudit :exec
INSERT INTO capability_github_audit(id,project_id,request_id,actor_id,event,generation,sync_id,attempt,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);
-- name: CapabilityGithubRefreshCount :one
SELECT count(*) FROM capability_github_audit WHERE project_id=$1 AND event='refresh' AND created_at>now()-interval '1 hour';
-- name: EnsureCapabilityGithubCredential :exec
INSERT INTO capability_github_credentials(credential_id) VALUES($1) ON CONFLICT DO NOTHING;
-- name: LockCapabilityGithubCredential :one
SELECT * FROM capability_github_credentials WHERE credential_id=$1 FOR UPDATE;
-- name: SaveCapabilityGithubCredential :exec
UPDATE capability_github_credentials SET blocked_until=$2,lease_token=$3,lease_until=$4 WHERE credential_id=$1;
