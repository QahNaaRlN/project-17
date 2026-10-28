-- +goose Up
-- Load tests (docs/load-tests.md): a published page is found by its literal path through
-- the primary key; only parameterised routes are matched in Go, and this index finds them
-- without reading the whole route table of the environment.
CREATE INDEX routes_patterns ON routes (environment_id) WHERE strpos(path, ':') > 0;

-- +goose Down
DROP INDEX routes_patterns;
