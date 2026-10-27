-- name: ValidationCandidate :one
SELECT b.*, m.hash, m.body, cs.kind FROM changeset_manifest_candidates b
JOIN manifests m ON m.id = b.manifest_id AND m.project_id = b.project_id
JOIN changesets cs ON cs.id = b.changeset_id AND cs.project_id = b.project_id
WHERE b.project_id = $1 AND b.changeset_id = $2;

-- name: ValidationComponent :one
SELECT v.id, v.body, v.path, v.certified FROM objects o
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
SELECT o.id AS object_id, v.id AS version_id, v.body, v.path, v.certified, v.deleted
FROM published_pointers pp JOIN environments e ON e.id = pp.environment_id
JOIN objects o ON o.id = pp.object_id
JOIN object_versions v ON v.id = pp.version_id AND v.object_id = o.id
WHERE e.project_id = $1 AND e.id = $2 AND o.project_id = e.project_id
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

-- name: ValidationDependents :many
-- Objects of the environment whose check depends on the given ones (STU-563): published
-- versions that reference them, and through components — every committed component version,
-- since pinned versions are validated inside their consumers. Only components propagate.
WITH RECURSIVE affected(object_id, depth, component) AS (
  SELECT t, 0, true FROM unnest(sqlc.arg(objects)::uuid[]) t
  UNION
  SELECT v.object_id, a.depth + 1, COALESCE(o.doc_kind = 'component', false)
  FROM affected a
  JOIN version_references r ON r.target_id = a.object_id
  JOIN object_versions v ON v.id = r.version_id AND v.state = 'committed'
  JOIN objects o ON o.id = v.object_id AND o.project_id = sqlc.arg(project_id)
  WHERE a.component AND a.depth < 128
    AND (o.doc_kind = 'component' OR EXISTS (SELECT 1 FROM published_pointers pp
      WHERE pp.environment_id = sqlc.arg(environment_id) AND pp.version_id = v.id))
)
SELECT DISTINCT a.object_id::uuid AS object_id FROM affected a
JOIN published_pointers pp ON pp.environment_id = sqlc.arg(environment_id) AND pp.object_id = a.object_id
WHERE a.depth > 0
LIMIT 10001;

-- name: ValidationPublishedVersionsByID :many
SELECT o.id AS object_id, v.id AS version_id, v.body, v.path, v.certified, v.deleted
FROM published_pointers pp JOIN environments e ON e.id = pp.environment_id
JOIN objects o ON o.id = pp.object_id
JOIN object_versions v ON v.id = pp.version_id AND v.object_id = o.id
WHERE e.project_id = sqlc.arg(project_id) AND e.id = sqlc.arg(environment_id) AND o.project_id = e.project_id
AND pp.object_id = ANY(sqlc.arg(objects)::uuid[])
ORDER BY o.id;

-- name: ValidationPublishedDeleted :many
SELECT pp.object_id, pp.version_id FROM published_pointers pp
JOIN object_versions v ON v.id = pp.version_id
WHERE pp.environment_id = sqlc.arg(environment_id) AND v.deleted
ORDER BY pp.object_id;

-- name: ValidationPublishedEntities :many
SELECT o.id AS object_id, o.schema_name, v.body, v.deleted
FROM published_pointers pp JOIN objects o ON o.id = pp.object_id
JOIN object_versions v ON v.id = pp.version_id
WHERE pp.environment_id = sqlc.arg(environment_id) AND o.project_id = sqlc.arg(project_id)
AND o.kind = 'entity' AND o.schema_name = ANY(sqlc.arg(schemas)::text[])
ORDER BY o.id;

-- name: ContentHeadsBySchema :many
SELECT o.id, o.schema_name, v.body, v.deleted FROM objects o
JOIN object_versions v ON v.id = o.head_version_id
WHERE o.project_id = sqlc.arg(project_id) AND o.kind = 'entity' AND o.schema_name = ANY(sqlc.arg(schemas)::text[])
ORDER BY o.id;

-- name: SetEnvironmentValidationBasis :exec
UPDATE environments SET validation_basis = $2 WHERE id = $1;
