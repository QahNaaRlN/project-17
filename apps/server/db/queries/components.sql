-- name: ComponentTitleTaken :one
-- Q-6: имя компонента уникально в проекте без учёта регистра (head и рабочие версии этого CS).
SELECT EXISTS (
  SELECT 1 FROM objects o
  JOIN object_versions v ON v.id = COALESCE(
    (SELECT co.working_version_id FROM changeset_objects co
      WHERE co.changeset_id = sqlc.arg(changeset_id) AND co.object_id = o.id),
    o.head_version_id)
  WHERE o.project_id = sqlc.arg(project_id) AND o.kind = 'document' AND o.doc_kind = 'component'
    AND o.deleted_at IS NULL AND o.id <> sqlc.arg(exclude_id)
    AND lower(v.body -> 'meta' ->> 'title') = lower(sqlc.arg(title)::text)
) AS taken;
