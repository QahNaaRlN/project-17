-- name: ListStudioPages :many
SELECT o.id, COALESCE(h.path, w.path)::text AS path,
       COALESCE(h.body->'nodes'->(h.body->>'root')->>'name',
                w.body->'nodes'->(w.body->>'root')->>'name', '')::text AS title,
       (h.id IS NOT NULL AND h.path IS NOT NULL)::boolean AS has_head
FROM objects o
LEFT JOIN object_versions h ON h.id = o.head_version_id AND NOT h.deleted
LEFT JOIN LATERAL (
  SELECT v.path, v.body FROM changeset_objects co
  JOIN changesets c ON c.id = co.changeset_id
  JOIN object_versions v ON v.id = co.working_version_id
  WHERE co.object_id = o.id AND c.project_id = sqlc.arg(project_id)
    AND c.owner_id = sqlc.arg(owner_id) AND c.state = 'open' AND c.kind = 'standard'
    AND sqlc.arg(environment)::text = ANY(c.targets) AND NOT v.deleted
    AND v.path IS NOT NULL
  ORDER BY c.updated_at DESC, c.id DESC LIMIT 1
) w ON true
WHERE o.project_id = sqlc.arg(project_id) AND o.kind = 'document' AND o.doc_kind = 'page'
  AND o.deleted_at IS NULL AND COALESCE(h.path, w.path) IS NOT NULL
  AND o.id > sqlc.arg(after_id)::uuid
ORDER BY o.id LIMIT 51;

-- name: ListStudioDrafts :many
SELECT c.* FROM changesets c
JOIN objects o ON o.id = sqlc.arg(document_id) AND o.project_id = c.project_id
LEFT JOIN changeset_objects co ON co.changeset_id = c.id AND co.object_id = o.id
LEFT JOIN object_versions w ON w.id = co.working_version_id
LEFT JOIN object_versions h ON h.id = o.head_version_id
WHERE c.project_id = sqlc.arg(project_id) AND c.owner_id = sqlc.arg(owner_id)
  AND c.state = 'open' AND c.kind = 'standard'
  AND sqlc.arg(environment)::text = ANY(c.targets)
  AND o.kind = 'document' AND o.doc_kind = 'page' AND o.deleted_at IS NULL
  AND NOT COALESCE(w.deleted, h.deleted, true)
  AND (CASE WHEN w.id IS NOT NULL THEN w.path ELSE h.path END) IS NOT NULL
  AND c.id > sqlc.arg(after_id)::uuid
ORDER BY c.id LIMIT 51;
