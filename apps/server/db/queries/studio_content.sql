-- name: ListStudioContent :many
SELECT o.id, o.kind, o.schema_name, v.deleted,
 LEFT(COALESCE(v.body->'title'->>(p.settings->>'defaultLocale'),v.body->'title'->>'ru', v.body->'title'->>'en',
 CASE WHEN jsonb_typeof(v.body->'title')='string' THEN v.body->>'title' END, ''),200)::text AS title,
 COALESCE(v.body->>'assetKind','')::text AS asset_kind,
 COALESCE(v.body->>'mimeType','')::text AS mime_type
FROM objects o
JOIN projects p ON p.id=o.project_id
LEFT JOIN changeset_objects co ON co.object_id=o.id AND co.changeset_id=sqlc.arg(changeset_id)
JOIN object_versions v ON v.id=COALESCE(co.working_version_id,o.head_version_id)
WHERE o.project_id=sqlc.arg(project_id) AND o.kind=sqlc.arg(kind)::text
 AND (sqlc.arg(schema_name)::text='' OR o.schema_name=sqlc.arg(schema_name))
 AND (sqlc.arg(search)::text='' OR strpos(lower((v.body->'title')::text),lower(sqlc.arg(search)::text))>0)
 AND (NOT v.deleted OR co.object_id IS NOT NULL)
 AND o.id>sqlc.arg(after_id)::uuid
ORDER BY o.id LIMIT 51;
