-- name: ContentIdentity :one
SELECT * FROM objects WHERE project_id=$1 AND id=$2;

-- name: CreateEntityObject :one
INSERT INTO objects (id, project_id, kind, schema_name) VALUES ($1,$2,'entity',$3) RETURNING *;

-- name: SetWorkingContent :exec
UPDATE object_versions SET ir_version=NULL, schema_version=$2, deleted=$3 WHERE id=$1 AND state='working';

-- name: ContentVersion :one
SELECT v.*, o.kind, o.schema_name FROM objects o JOIN object_versions v ON v.object_id=o.id
WHERE o.project_id=sqlc.arg(project_id) AND o.id=sqlc.arg(object_id)
AND v.id=COALESCE(
 (SELECT co.working_version_id FROM changeset_objects co WHERE co.object_id=o.id AND co.changeset_id=sqlc.narg(changeset_id)::uuid),
 CASE WHEN sqlc.arg(use_head)::boolean THEN o.head_version_id ELSE
 (SELECT pp.version_id FROM published_pointers pp WHERE pp.object_id=o.id AND pp.environment_id=sqlc.arg(environment_id)) END);

-- name: ContentHeads :many
SELECT o.id, o.kind, o.schema_name, v.body, v.deleted FROM objects o
JOIN object_versions v ON v.id=o.head_version_id WHERE o.project_id=$1 AND o.kind='entity' ORDER BY o.id;

-- name: SetContentTargets :exec
UPDATE changesets SET targets=$2 WHERE id=$1;
