-- +goose Up
CREATE TABLE asset_files (
 project_id uuid NOT NULL REFERENCES projects(id),
 sha256 bytea NOT NULL CHECK(octet_length(sha256)=32),
 storage_key text NOT NULL,
 mime_type text NOT NULL,
 size_bytes bigint NOT NULL CHECK(size_bytes > 0),
 width int,
 height int,
 duration_ms int,
 blurhash text,
 status text NOT NULL CHECK(status IN ('pending','ready','rejected')),
 preview_key text,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(project_id,sha256)
);
CREATE TABLE asset_uploads (
 id uuid PRIMARY KEY,
 project_id uuid NOT NULL REFERENCES projects(id),
 actor_id uuid NOT NULL REFERENCES actors(id),
 filename text NOT NULL,
 mime_type text NOT NULL,
 size_bytes bigint NOT NULL CHECK(size_bytes > 0),
 source_sha256 bytea NOT NULL CHECK(octet_length(source_sha256)=32),
 storage_key text NOT NULL UNIQUE,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','ready','rejected')),
 file_sha256 bytea,
 error_code text,
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(project_id,file_sha256) REFERENCES asset_files(project_id,sha256)
);
-- +goose Down
DROP TABLE asset_uploads;
DROP TABLE asset_files;
