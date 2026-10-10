-- +goose Up
-- Ключ окружения для подписи preview-токенов (docs/spec/08-api.md API-041): 32 случайных байта.
ALTER TABLE environments
  ADD COLUMN preview_key bytea NOT NULL DEFAULT (uuid_send(gen_random_uuid()) || uuid_send(gen_random_uuid()));

-- +goose Down
ALTER TABLE environments DROP COLUMN preview_key;
