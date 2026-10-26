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
  AND (sqlc.arg(search)::text = ''
       OR strpos(lower(COALESCE(h.path, w.path)), lower(sqlc.arg(search)::text)) > 0
       OR strpos(lower(COALESCE(h.body->'nodes'->(h.body->>'root')->>'name',
                               w.body->'nodes'->(w.body->>'root')->>'name', '')),
                 lower(sqlc.arg(search)::text)) > 0)
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

-- name: ListStudioWork :many
SELECT c.id,c.title,c.owner_id,a.display_name AS owner_name,c.state,
       page.object_id AS document_id,COALESCE(page.path,'')::text AS path
FROM changesets c JOIN actors a ON a.id=c.owner_id
JOIN LATERAL (
 SELECT co.object_id,v.path FROM changeset_objects co
 JOIN objects o ON o.id=co.object_id AND o.project_id=c.project_id AND o.doc_kind='page'
 JOIN object_versions v ON v.id=co.working_version_id
 WHERE co.changeset_id=c.id AND v.path IS NOT NULL AND NOT v.deleted
 ORDER BY co.object_id LIMIT 1
) page ON true
WHERE c.project_id=sqlc.arg(project_id) AND c.kind='standard'
AND sqlc.arg(environment)::text=ANY(c.targets) AND c.id>sqlc.arg(after_id)
AND ((c.owner_id=sqlc.arg(actor_id) AND c.state IN ('open','checking','failed','changes_requested','in_review','approved'))
 OR (c.owner_id<>sqlc.arg(actor_id) AND sqlc.arg(can_review)::boolean AND c.state IN ('in_review','approved')))
ORDER BY c.id LIMIT 51;
