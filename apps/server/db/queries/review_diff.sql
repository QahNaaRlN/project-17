-- name: ListDiffObjects :many
SELECT o.id, o.kind, o.doc_kind, o.schema_name, o.head_version_id,
 co.base_version_id, co.working_version_id
FROM changeset_objects co JOIN objects o ON o.id=co.object_id
JOIN changesets cs ON cs.id=co.changeset_id
WHERE cs.project_id=sqlc.arg(project_id) AND co.changeset_id=sqlc.arg(changeset_id)
 AND o.project_id=sqlc.arg(project_id)
ORDER BY o.id LIMIT 1001;

-- name: GetDiffVersion :one
SELECT * FROM object_versions WHERE project_id=$1 AND object_id=$2 AND id=$3;

-- name: ListDiffPointers :many
SELECT pp.object_id, pp.version_id, e.name AS environment
FROM published_pointers pp JOIN environments e ON e.id=pp.environment_id
JOIN changeset_objects co ON co.object_id=pp.object_id AND co.changeset_id=sqlc.arg(changeset_id)
JOIN objects o ON o.id=pp.object_id
WHERE e.project_id=sqlc.arg(project_id) AND o.project_id=sqlc.arg(project_id)
ORDER BY e.name,pp.object_id;
