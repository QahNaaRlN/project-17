-- name: InsertCodeIndexUpload :exec
INSERT INTO code_index_uploads(id,project_id,manifest_hash,index_hash,repo_commit,body,created_by)
VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(project_id,manifest_hash,index_hash) DO NOTHING;

-- name: FindCodeIndexUpload :one
SELECT * FROM code_index_uploads WHERE project_id=$1 AND manifest_hash=$2 AND index_hash=$3;

-- name: GetCodeIndexUpload :one
SELECT * FROM code_index_uploads WHERE project_id=$1 AND id=$2;

-- name: LinkCodeIndexUpload :exec
UPDATE code_index_uploads SET linked=true WHERE project_id=$1 AND id=$2;

-- name: DeleteExpiredCodeIndexes :execrows
DELETE FROM code_index_uploads WHERE NOT linked AND created_at < now()-interval '24 hours'
AND NOT EXISTS(SELECT 1 FROM capability_context_packs p WHERE p.index_id=code_index_uploads.id);

-- name: InsertCapabilityRequest :one
INSERT INTO capability_requests(id,project_id,environment_id,environment,source_manifest_id,key,title,description,proposed_contract,acceptance,changeset_id,document_id,node_id,event,creator_id,owner_id,agent_session_id,kind,placement,initial_props)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
ON CONFLICT DO NOTHING RETURNING *;

-- name: GetCapabilityRequest :one
SELECT * FROM capability_requests WHERE project_id=$1 AND id=$2;

-- name: LockCapabilityRequest :one
SELECT * FROM capability_requests WHERE project_id=$1 AND id=$2 FOR UPDATE;

-- name: UpdateCapabilityRequest :one
UPDATE capability_requests SET title=$3,description=$4,proposed_contract=$5,acceptance=$6,status=$7,
 external_refs=$8,fulfilled_manifest_id=$9,revision=revision+1,updated_at=now()
WHERE project_id=$1 AND id=$2 RETURNING *;

-- name: ListCapabilityRequests :many
SELECT * FROM capability_requests WHERE project_id=sqlc.arg(project_id)
 AND (owner_id=sqlc.arg(actor_id) OR (sqlc.arg(engineer)::boolean AND status<>'draft'))
 AND (sqlc.arg(environment)::text='' OR environment=sqlc.arg(environment))
 AND (sqlc.arg(status)::text='' OR status=sqlc.arg(status))
 AND (sqlc.arg(key)::text='' OR key=sqlc.arg(key))
 AND (sqlc.narg(before_time)::timestamptz IS NULL OR (updated_at,id)<(sqlc.narg(before_time),sqlc.narg(before_id)::uuid))
ORDER BY updated_at DESC,id DESC LIMIT sqlc.arg(page_limit);

-- name: InsertCapabilityAudit :exec
INSERT INTO capability_request_audit(id,project_id,request_id,revision,actor_id,on_behalf_of,agent_session_id,event,reason,data)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10);

-- name: CapabilityAudit :many
SELECT * FROM capability_request_audit WHERE project_id=$1 AND request_id=$2 ORDER BY revision;

-- name: InsertCapabilityPack :exec
INSERT INTO capability_context_packs(project_id,request_id,revision,manifest_id,index_id,changeset_seq,page_hash,hash,body)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);

-- name: GetCapabilityPack :one
SELECT * FROM capability_context_packs WHERE project_id=$1 AND request_id=$2 AND revision=$3;

-- name: CapabilityPacks :many
SELECT revision,manifest_id,index_id,changeset_seq,page_hash,hash,created_at FROM capability_context_packs
WHERE project_id=$1 AND request_id=$2 ORDER BY revision DESC;
