-- +goose Up
ALTER TABLE object_versions ADD COLUMN certified boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE object_versions DROP COLUMN certified;
