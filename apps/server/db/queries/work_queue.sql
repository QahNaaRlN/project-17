-- name: WorkQueue :many
SELECT c.*, a.display_name AS owner_name,
  counts.page_count, counts.component_count, counts.entity_count, counts.asset_count, counts.deleted_count
FROM changesets c
JOIN actors a ON a.id=c.owner_id
CROSS JOIN LATERAL (
 SELECT
 count(*) FILTER (WHERE o.kind='document' AND o.doc_kind='page')::int AS page_count,
 count(*) FILTER (WHERE o.kind='document' AND o.doc_kind='component')::int AS component_count,
 count(*) FILTER (WHERE o.kind='entity')::int AS entity_count,
 count(*) FILTER (WHERE o.kind='asset')::int AS asset_count,
 count(*) FILTER (WHERE w.deleted)::int AS deleted_count
 FROM changeset_objects co JOIN objects o ON o.id=co.object_id AND o.project_id=c.project_id
 JOIN object_versions w ON w.id=co.working_version_id
 WHERE co.changeset_id=c.id
) counts
WHERE c.project_id=sqlc.arg(project_id) AND c.kind='standard'
AND c.state IN ('open','checking','failed','changes_requested','in_review','approved')
AND sqlc.arg(environment)::text=ANY(c.targets)
AND (sqlc.arg(item_id)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR c.id=sqlc.arg(item_id))
AND ((c.owner_id=sqlc.arg(actor_id) AND c.state IN ('open','checking','failed','changes_requested','in_review','approved'))
 OR (c.owner_id<>sqlc.arg(actor_id) AND sqlc.arg(can_review)::boolean AND c.state IN ('in_review','approved')))
AND (sqlc.arg(state_filter)::text='all' OR c.state=sqlc.arg(state_filter))
AND (sqlc.arg(ownership)::text='all' OR (sqlc.arg(ownership)='mine' AND c.owner_id=sqlc.arg(actor_id)) OR (sqlc.arg(ownership)='others' AND c.owner_id<>sqlc.arg(actor_id)))
AND (sqlc.arg(can_design)::boolean OR counts.page_count+counts.component_count=0)
AND (sqlc.arg(can_content)::boolean OR counts.entity_count+counts.asset_count=0)
AND (sqlc.arg(kind_filter)::text='all'
 OR (sqlc.arg(kind_filter)='page' AND counts.page_count>0)
 OR (sqlc.arg(kind_filter)='component' AND counts.component_count>0)
 OR (sqlc.arg(kind_filter)='entity' AND counts.entity_count>0)
 OR (sqlc.arg(kind_filter)='asset' AND counts.asset_count>0))
AND (sqlc.arg(search)::text='' OR strpos(lower(c.title),lower(sqlc.arg(search)))>0
 OR strpos(lower(a.display_name),lower(sqlc.arg(search)))>0
 OR EXISTS (
 SELECT 1 FROM changeset_objects co JOIN objects o ON o.id=co.object_id AND o.project_id=c.project_id AND o.doc_kind='page'
 LEFT JOIN object_versions b ON b.id=co.base_version_id
 JOIN object_versions w ON w.id=co.working_version_id
 WHERE co.changeset_id=c.id AND (strpos(lower(COALESCE(b.path,'')),lower(sqlc.arg(search)))>0 OR strpos(lower(COALESCE(w.path,'')),lower(sqlc.arg(search)))>0)
 ))
AND (NOT sqlc.arg(has_cursor)::boolean OR (c.created_at,c.id)<(sqlc.arg(after_time)::timestamptz,sqlc.arg(after_id)::uuid))
ORDER BY c.created_at DESC,c.id DESC LIMIT sqlc.arg(page_limit);

-- name: WorkQueueObjects :many
SELECT o.id,o.kind,o.doc_kind,co.base_version_id,co.working_version_id,w.deleted,
 b.path AS base_path,w.path AS working_path,
 COALESCE(w.body->'nodes'->(w.body->>'root')->>'name',b.body->'nodes'->(b.body->>'root')->>'name','')::text AS title
FROM changeset_objects co
JOIN objects o ON o.id=co.object_id AND o.project_id=sqlc.arg(project_id)
LEFT JOIN object_versions b ON b.id=co.base_version_id
JOIN object_versions w ON w.id=co.working_version_id
WHERE co.changeset_id=sqlc.arg(changeset_id)
ORDER BY o.id LIMIT 1001;
