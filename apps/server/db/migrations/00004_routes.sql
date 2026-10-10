-- +goose Up
-- Маршруты страниц (docs/spec/07-storage.md §2, 06 §2.2: document.create path?, document.setRoute).
--
-- Форма маршрута — путь с параметрами, заменёнными на «:»: '/a/:x' и '/a/:y' совпадают по форме
-- и не могут сосуществовать, иначе выбор страницы был бы неоднозначен.

-- +goose StatementBegin
CREATE FUNCTION route_shape(path text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
AS $$ SELECT regexp_replace(path, ':[^/]+', ':', 'g') $$;
-- +goose StatementEnd

DROP INDEX objects_head_path;
CREATE UNIQUE INDEX objects_head_route ON objects (project_id, route_shape(head_path))
  WHERE head_path IS NOT NULL AND deleted_at IS NULL;

CREATE TABLE routes (
  environment_id  uuid NOT NULL REFERENCES environments(id),
  path            text NOT NULL,                       -- '/products/:slug'
  object_id       uuid NOT NULL REFERENCES objects(id),
  PRIMARY KEY (environment_id, path)
);
CREATE UNIQUE INDEX routes_shape ON routes (environment_id, route_shape(path));
CREATE UNIQUE INDEX routes_object ON routes (environment_id, object_id);

-- +goose Down
DROP TABLE routes;
DROP INDEX objects_head_route;
CREATE UNIQUE INDEX objects_head_path ON objects (project_id, head_path)
  WHERE head_path IS NOT NULL AND deleted_at IS NULL;
DROP FUNCTION route_shape(text);
