-- name: CreateSystemActor :exec
INSERT INTO actors (id, kind, project_id, display_name, system)
VALUES ($1, 'service', $2, 'cms-system', true);

-- name: GetSystemActor :one
SELECT * FROM actors WHERE project_id = $1 AND system;

-- name: InsertCapabilityAnchor :one
INSERT INTO capability_request_anchors (id, project_id, request_id, changeset_id, document_id, node_id, event, owner_id, original, anchor_kind)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, CASE WHEN $7::text = '' THEN 'node' ELSE 'event' END)
RETURNING *;

-- name: LockCapabilityAnchor :one
SELECT * FROM capability_request_anchors WHERE project_id = $1 AND id = $2 FOR UPDATE;

-- name: ListCapabilityAnchors :many
SELECT * FROM capability_request_anchors WHERE project_id = $1 AND request_id = $2 ORDER BY created_at, id;

-- name: ChangesetCapabilityAnchors :many
SELECT * FROM capability_request_anchors WHERE project_id = $1 AND changeset_id = $2 AND state = 'queued' ORDER BY created_at, id FOR UPDATE;

-- name: AnchorAt :one
SELECT a.*, r.status AS request_status, r.key AS request_key FROM capability_request_anchors a
JOIN capability_requests r ON r.project_id = a.project_id AND r.id = a.request_id
WHERE a.project_id = $1 AND a.request_id = $2 AND a.changeset_id = $3 AND a.document_id = $4 AND a.node_id = $5 AND a.event = $6
  AND a.state NOT IN ('detached', 'changeset_closed');

-- name: SaveCapabilityAnchor :one
UPDATE capability_request_anchors SET state = $3, revision = revision + 1, fulfillment_args = $4, attempts = $5,
  next_at = $6, last_error = $7, replaced_batch_id = $8, updated_at = now()
WHERE project_id = $1 AND id = $2
RETURNING *;

-- name: QueueRequestAnchors :exec
UPDATE capability_request_anchors SET state = 'queued', revision = revision + 1, attempts = 0, next_at = now(), last_error = '', updated_at = now()
WHERE project_id = $1 AND request_id = $2 AND state = 'active' AND (sqlc.narg(skip)::uuid IS NULL OR id <> sqlc.narg(skip)::uuid);

-- name: CountActiveAnchors :one
SELECT
  (SELECT count(*) FROM capability_request_anchors a WHERE a.project_id = $1 AND a.request_id = $2 AND a.state NOT IN ('detached', 'changeset_closed'))::int AS request_anchors,
  (SELECT count(DISTINCT b.request_id) FROM capability_request_anchors b WHERE b.project_id = $1 AND b.changeset_id = $3 AND b.state NOT IN ('detached', 'changeset_closed'))::int AS changeset_requests;

-- name: DueAnchorChangesets :many
SELECT DISTINCT a.project_id, a.changeset_id FROM capability_request_anchors a
JOIN changesets c ON c.id = a.changeset_id
WHERE (a.state = 'queued' AND a.next_at <= now()) OR (a.state = 'changeset_in_review' AND c.state = 'open')
LIMIT 100;

-- name: RequeueReviewedAnchors :exec
UPDATE capability_request_anchors a SET state = 'queued', revision = revision + 1, next_at = now(), updated_at = now()
FROM changesets c
WHERE c.id = a.changeset_id AND a.project_id = $1 AND a.changeset_id = $2 AND a.state = 'changeset_in_review' AND c.state = 'open';

-- name: EnqueueFulfillmentActivation :exec
INSERT INTO capability_fulfillment_jobs (id, project_id, environment_id, manifest_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (project_id, environment_id, manifest_id) WHERE request_id IS NULL DO NOTHING;

-- name: EnqueueFulfillmentCheck :exec
INSERT INTO capability_fulfillment_jobs (id, project_id, environment_id, manifest_id, request_id)
VALUES ($1, $2, $3, $4, $5);

-- name: ClaimFulfillmentJob :one
UPDATE capability_fulfillment_jobs SET state = 'running', attempts = attempts + 1, lease_until = now() + interval '5 minutes', updated_at = now()
WHERE id = (
  SELECT j.id FROM capability_fulfillment_jobs j
  WHERE j.state <> 'done' AND j.next_at <= now() AND (j.state = 'queued' OR j.lease_until < now())
  ORDER BY j.next_at, j.id LIMIT 1 FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: FinishFulfillmentJob :exec
UPDATE capability_fulfillment_jobs SET state = $2, next_at = $3, last_error = $4, lease_until = NULL, updated_at = now()
WHERE id = $1;

-- name: FulfillmentCandidates :many
SELECT * FROM capability_requests
WHERE project_id = $1 AND environment_id = $2 AND status IN ('submitted', 'accepted', 'in_progress', 'pr_open')
  AND (sqlc.narg(request)::uuid IS NULL OR id = sqlc.narg(request)::uuid)
ORDER BY created_at, id LIMIT 100;

-- name: SetFulfillmentWaiting :exec
UPDATE capability_requests SET fulfillment_reason = $3
WHERE project_id = $1 AND id = $2 AND fulfillment_state = 'waiting';

-- name: FulfillCapabilityRequest :one
UPDATE capability_requests SET status = 'fulfilled', revision = revision + 1, fulfilled_manifest_id = $4,
  fulfillment_state = 'fulfilled', fulfillment_reason = '', fulfillment_mode = $5, fulfillment_commit = $6,
  fulfillment_observation_id = $7, fulfillment_basis = $8, fulfilled_at = now(), updated_at = now()
WHERE project_id = $1 AND id = $2 AND revision = $3 AND status IN ('submitted', 'accepted', 'in_progress', 'pr_open')
RETURNING *;

-- name: InsertFulfillmentAudit :exec
INSERT INTO capability_fulfillment_audit (id, project_id, request_id, anchor_id, actor_id, event, reason, data)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: DetachRequestAnchors :exec
UPDATE capability_request_anchors SET state = 'detached', revision = revision + 1, updated_at = now()
WHERE project_id = $1 AND request_id = $2 AND state NOT IN ('detached', 'changeset_closed', 'replaced');

-- name: MarkManualFulfillment :exec
UPDATE capability_requests SET fulfillment_state = 'fulfilled', fulfillment_reason = '', fulfillment_mode = 'manual', fulfilled_at = now()
WHERE project_id = $1 AND id = $2;

-- name: OriginalCapabilityAnchor :one
SELECT * FROM capability_request_anchors WHERE project_id = $1 AND request_id = $2 AND original;

-- name: OpenCapabilityRequestByKey :one
SELECT id, key, title, status, revision, environment, proposed_contract FROM capability_requests
WHERE project_id = $1 AND environment = $2 AND key = $3 AND status IN ('submitted', 'accepted', 'in_progress', 'pr_open');

-- name: OwnsCapabilityAnchor :one
SELECT EXISTS (SELECT 1 FROM capability_request_anchors WHERE project_id = $1 AND request_id = $2 AND owner_id = $3 AND state <> 'detached')::bool;

-- name: IsSystemActor :one
SELECT EXISTS (SELECT 1 FROM actors WHERE id = $1 AND system)::bool;
