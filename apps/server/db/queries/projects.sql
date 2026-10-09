-- name: CreateProject :one
INSERT INTO projects (id, slug, name) VALUES ($1, $2, $3)
RETURNING *;

-- name: GetProjectBySlug :one
SELECT * FROM projects WHERE slug = $1;

-- name: CreateEnvironment :one
INSERT INTO environments (id, project_id, name, kind, app_url)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListEnvironments :many
SELECT * FROM environments WHERE project_id = $1 ORDER BY name;
