-- name: HistoryPublications :many
SELECT p.*, e.name AS environment_name,
 (SELECT count(*) FROM publication_items i WHERE i.publication_id=p.id)::int AS object_count
FROM publications p JOIN environments e ON e.id=p.environment_id
WHERE p.project_id=sqlc.arg(project_id) AND e.name=sqlc.arg(environment)
 AND (sqlc.narg(before_time)::timestamptz IS NULL OR
 (p.created_at,p.id)<(sqlc.narg(before_time)::timestamptz,sqlc.narg(before_id)::uuid))
ORDER BY p.created_at DESC,p.id DESC LIMIT sqlc.arg(page_limit);

-- name: HistoryItems :many
SELECT i.*, o.kind,o.doc_kind,o.schema_name
FROM publication_items i JOIN publications p ON p.id=i.publication_id
JOIN objects o ON o.id=i.object_id AND o.project_id=p.project_id
WHERE p.project_id=$1 AND p.id=$2 ORDER BY i.object_id LIMIT 1001;

-- name: HistoryKinds :many
SELECT DISTINCT o.kind FROM publication_items i JOIN publications p ON p.id=i.publication_id
JOIN objects o ON o.id=i.object_id AND o.project_id=p.project_id
WHERE p.project_id=$1 AND p.id=$2 ORDER BY o.kind;

-- name: HistoryPointers :many
SELECT pp.* FROM published_pointers pp JOIN environments e ON e.id=pp.environment_id
WHERE e.project_id=$1 AND e.name=$2 ORDER BY pp.object_id;
