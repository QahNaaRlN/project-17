// Package db содержит SQL-миграции (goose) и запросы (sqlc) сервера.
package db

import "embed"

//go:generate go tool sqlc generate -f ../sqlc.yaml

// Migrations — миграции схемы БД, встроенные в бинарник.
//
//go:embed migrations/*.sql
var Migrations embed.FS
