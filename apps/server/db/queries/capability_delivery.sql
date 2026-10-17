-- name: GetCapabilityTarget :one
SELECT * FROM capability_delivery_targets WHERE project_id=$1 AND environment=$2;

-- name: LockCapabilityTarget :one
SELECT * FROM capability_delivery_targets WHERE project_id=$1 AND environment=$2 FOR UPDATE;

-- name: SetCapabilityTarget :one
INSERT INTO capability_delivery_targets(id,project_id,environment_id,environment,endpoint_id,url,fingerprint,enabled,revision,changed_by)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,1,$9)
ON CONFLICT(project_id,environment) DO UPDATE SET endpoint_id=excluded.endpoint_id,url=excluded.url,fingerprint=excluded.fingerprint,enabled=excluded.enabled,revision=capability_delivery_targets.revision+1,changed_by=excluded.changed_by,changed_at=now()
RETURNING *;

-- name: InsertCapabilityDelivery :one
INSERT INTO capability_deliveries(id,project_id,request_id,request_revision,pack_revision,pack_hash,target_id,target_revision,target_fingerprint,destination_url,endpoint_id,key_id,key_fingerprint,initiated_by,body,body_hash,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING *;

-- name: PinnedCapabilityDeliveryKeys :many
SELECT DISTINCT d.key_id,d.key_fingerprint FROM capability_deliveries d JOIN capability_requests r ON r.project_id=d.project_id AND r.id=d.request_id
WHERE d.project_id=$1 AND d.endpoint_id=$2 AND r.environment=$3;

-- name: FindCapabilityDelivery :one
SELECT * FROM capability_deliveries WHERE project_id=$1 AND request_id=$2 AND pack_revision=$3 AND target_id=$4 AND target_revision=$5;

-- name: GetCapabilityDelivery :one
SELECT * FROM capability_deliveries WHERE project_id=$1 AND id=$2;

-- name: LockCapabilityDelivery :one
SELECT * FROM capability_deliveries WHERE project_id=$1 AND id=$2 FOR UPDATE;

-- name: SaveCapabilityDelivery :one
UPDATE capability_deliveries SET state=$3,revision=revision+1,updated_at=now(),cycle=$4,attempts_in_cycle=$5,total_attempts=$6,next_attempt_at=$7,lease_token=$8,lease_until=$9,cancelled_at=$10,delivered_at=$11,last_error=$12,last_http_status=$13,retryable_exhaustion=$14
WHERE project_id=$1 AND id=$2 RETURNING *;

-- name: DueCapabilityDeliveries :many
SELECT project_id,id FROM capability_deliveries WHERE (state IN ('queued','retry_wait') AND next_attempt_at<=now()) OR (state='sending' AND lease_until<=now()) ORDER BY next_attempt_at,id LIMIT 100;

-- name: CapabilityDeliveryQueueStats :one
SELECT count(*)::bigint AS depth, EXTRACT(EPOCH FROM COALESCE(now()-min(created_at),interval '0 second'))::bigint AS oldest_age_seconds
FROM capability_deliveries WHERE state IN ('queued','sending','retry_wait');

-- name: InsertCapabilityDeliveryAttempt :exec
INSERT INTO capability_delivery_attempts(id,project_id,delivery_id,ordinal,cycle,lease_token,started_at)
VALUES($1,$2,$3,$4,$5,$6,$7);

-- name: FinishCapabilityDeliveryAttempt :exec
UPDATE capability_delivery_attempts SET finished_at=now(),http_status=$4,outcome=$5,retryable=$6,receipt_delivery_id=$7,receipt_body_hash=$8,next_attempt_at=$9
WHERE project_id=$1 AND delivery_id=$2 AND lease_token=$3 AND finished_at IS NULL;

-- name: CapabilityDeliveryAttempts :many
SELECT * FROM capability_delivery_attempts WHERE project_id=$1 AND delivery_id=$2 ORDER BY ordinal DESC LIMIT 50;

-- name: InsertCapabilityDeliveryAudit :exec
INSERT INTO capability_delivery_audit(project_id,delivery_id,target_id,event,source,actor_id,initiated_by,reason,revision)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);

-- name: CapabilityDeliveryAudit :many
SELECT * FROM capability_delivery_audit WHERE project_id=$1 AND delivery_id=$2 ORDER BY id DESC LIMIT 100;

-- name: ListCapabilityDeliveries :many
SELECT * FROM capability_deliveries WHERE project_id=sqlc.arg(project_id) AND request_id=sqlc.arg(request_id)
AND (sqlc.narg(before_time)::timestamptz IS NULL OR (created_at,id)<(sqlc.narg(before_time),sqlc.narg(before_id)::uuid))
ORDER BY created_at DESC,id DESC LIMIT sqlc.arg(page_limit);
