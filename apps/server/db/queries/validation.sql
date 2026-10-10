-- name: ValidationCandidate :one
SELECT b.*, m.hash, m.body, cs.kind FROM changeset_manifest_candidates b
JOIN manifests m ON m.id = b.manifest_id AND m.project_id = b.project_id
JOIN changesets cs ON cs.id = b.changeset_id AND cs.project_id = b.project_id
WHERE b.project_id = $1 AND b.changeset_id = $2;

-- name: ValidationComponent :one
SELECT v.id, v.body, v.path FROM objects o
JOIN object_versions v ON v.object_id = o.id AND v.project_id = o.project_id
WHERE o.project_id = sqlc.arg(project_id) AND o.id = sqlc.arg(object_id)
AND o.kind = 'document' AND o.doc_kind = 'component' AND o.deleted_at IS NULL
AND ((sqlc.arg(number)::int > 0 AND v.state = 'committed' AND v.number = sqlc.arg(number))
 OR (sqlc.arg(number)::int = 0 AND v.id = COALESCE(
   (SELECT co.working_version_id FROM changeset_objects co JOIN changesets cs ON cs.id = co.changeset_id
    WHERE co.object_id = o.id AND co.changeset_id = sqlc.narg(changeset_id)::uuid AND cs.project_id = o.project_id
      AND cs.state NOT IN ('merged', 'abandoned')),
   CASE WHEN sqlc.arg(use_head)::boolean THEN o.head_version_id ELSE
   (SELECT pp.version_id FROM published_pointers pp JOIN environments e ON e.id = pp.environment_id
    WHERE pp.object_id = o.id AND e.id = sqlc.arg(environment_id) AND e.project_id = o.project_id) END)));

-- name: ValidationPublishedVersions :many
SELECT o.id AS object_id, v.id AS version_id, v.body, v.path
FROM published_pointers pp JOIN environments e ON e.id = pp.environment_id
JOIN objects o ON o.id = pp.object_id
JOIN object_versions v ON v.id = pp.version_id AND v.object_id = o.id
WHERE e.project_id = $1 AND e.id = $2 AND o.project_id = e.project_id AND o.kind = 'document'
ORDER BY o.id;

-- name: ValidationPage :one
SELECT v.path FROM objects o
JOIN object_versions v ON v.object_id = o.id AND v.project_id = o.project_id
WHERE o.project_id = sqlc.arg(project_id) AND o.id = sqlc.arg(object_id)
AND o.kind = 'document' AND o.doc_kind = 'page' AND o.deleted_at IS NULL
AND v.id = COALESCE(
 (SELECT co.working_version_id FROM changeset_objects co JOIN changesets cs ON cs.id = co.changeset_id
  WHERE co.object_id = o.id AND co.changeset_id = sqlc.narg(changeset_id)::uuid AND cs.project_id = o.project_id
    AND cs.state NOT IN ('merged', 'abandoned')),
 CASE WHEN sqlc.arg(use_head)::boolean THEN o.head_version_id ELSE
 (SELECT pp.version_id FROM published_pointers pp JOIN environments e ON e.id = pp.environment_id
  WHERE pp.object_id = o.id AND e.id = sqlc.arg(environment_id) AND e.project_id = o.project_id) END);
