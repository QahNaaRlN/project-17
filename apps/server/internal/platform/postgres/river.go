package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"
)

// RiverMigrationVersion — версия goose, на которой создаются таблицы очереди River
// (docs/spec/07-storage.md §2, D-16). При обновлении River с новыми миграциями добавляется
// следующая Go-миграция, снова вызывающая rivermigrate up.
const RiverMigrationVersion = 7

// riverMigration применяет миграции River через goose: очередь задач и outbox создаются
// и откатываются вместе с остальной схемой. Без общей транзакции: River применяет каждую
// свою версию в отдельной (часть из них расширяет enum, что в одной транзакции нельзя).
func riverMigration() *goose.Migration {
	run := func(direction rivermigrate.Direction, opts *rivermigrate.MigrateOpts) func(context.Context, *sql.DB) error {
		return func(ctx context.Context, db *sql.DB) error {
			migrator, err := rivermigrate.New(riverdatabasesql.New(db), nil)
			if err != nil {
				return err
			}
			if _, err := migrator.Migrate(ctx, direction, opts); err != nil {
				return fmt.Errorf("river %s: %w", direction, err)
			}
			return nil
		}
	}
	return goose.NewGoMigration(RiverMigrationVersion,
		&goose.GoFunc{RunDB: run(rivermigrate.DirectionUp, nil)},
		&goose.GoFunc{RunDB: run(rivermigrate.DirectionDown, &rivermigrate.MigrateOpts{TargetVersion: -1})},
	)
}
