-- name: CreateAssetObject :one
INSERT INTO objects(id,project_id,kind) VALUES($1,$2,'asset') RETURNING *;

-- name: ReadyAssetUpload :one
SELECT u.id, f.* FROM asset_uploads u JOIN asset_files f ON f.project_id=u.project_id AND f.sha256=u.file_sha256
WHERE u.project_id=$1 AND u.id=$2 AND u.actor_id=$3 AND u.status='ready' AND f.status='ready';

-- name: ReadyAssetFile :one
SELECT * FROM asset_files WHERE project_id=$1 AND sha256=$2 AND status='ready';
