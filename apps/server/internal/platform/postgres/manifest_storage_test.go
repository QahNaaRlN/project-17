package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
)

// MF-002, CNT-002, MF-033: настоящая БД сохраняет неизменяемость и изоляцию preview.
func TestManifestStorage(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewDB(t)
	q := store.New(pool)
	p, err := q.CreateProject(ctx, store.CreateProjectParams{ID: uuid.New(), Slug: "manifest", Name: "Manifest"})
	if err != nil {
		t.Fatal(err)
	}
	actor, err := q.CreateActor(ctx, store.CreateActorParams{ID: uuid.New(), Kind: "service", ProjectID: &p.ID, DisplayName: "CI"})
	if err != nil {
		t.Fatal(err)
	}
	insert := func(n int) store.Manifest {
		t.Helper()
		hash := fmt.Sprintf("sha256:%064x", n)
		err := q.InsertManifest(ctx, store.InsertManifestParams{ID: uuid.New(), ProjectID: p.ID, Hash: hash, AppVersion: "1.0", Body: []byte(`{"manifestVersion":"1.0"}`), RegisteredBy: actor.ID})
		if err != nil {
			t.Fatal(err)
		}
		m, err := q.GetManifestByHash(ctx, store.GetManifestByHashParams{ProjectID: p.ID, Hash: hash})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	a, b, standard := insert(1), insert(2), insert(3)
	// Повтор hash не заменяет ни тело, ни данные регистрации.
	if err := q.InsertManifest(ctx, store.InsertManifestParams{ID: uuid.New(), ProjectID: p.ID, Hash: a.Hash, AppVersion: "changed", Body: []byte(`{}`), RegisteredBy: actor.ID}); err != nil {
		t.Fatal(err)
	}
	again, err := q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: p.ID, ID: a.ID})
	if err != nil || again.ID != a.ID || again.AppVersion != a.AppVersion || string(again.Body) != string(a.Body) || !again.CreatedAt.Equal(a.CreatedAt) {
		t.Fatalf("duplicate changed original: %+v: %v", again, err)
	}
	for i, m := range []store.Manifest{a, b} {
		body := []byte(fmt.Sprintf(`{"version":2,"fields":{"variant%d":{"type":"string"}}}`, i))
		if err := q.InsertPreviewSchemaSnapshot(ctx, store.InsertPreviewSchemaSnapshotParams{ManifestID: m.ID, SchemaName: "Product", Version: 2, Body: body}); err != nil {
			t.Fatal(err)
		}
		rows, err := q.ListPreviewSchemaSnapshots(ctx, store.ListPreviewSchemaSnapshotsParams{ProjectID: p.ID, ManifestID: m.ID})
		if err != nil || len(rows) != 1 || !strings.Contains(string(rows[0].Body), fmt.Sprintf("variant%d", i)) {
			t.Fatalf("isolated snapshot: %+v %v", rows, err)
		}
	}
	if _, err := q.GetStandardSchemaVersion(ctx, store.GetStandardSchemaVersionParams{ProjectID: p.ID, SchemaName: "Product", Version: 2}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("preview reserved standard version: %v", err)
	}
	params := store.InsertStandardSchemaVersionParams{ProjectID: p.ID, SchemaName: "Product", Version: 2, Body: []byte(`{"version":2,"fields":{"standard":{"type":"number"}}}`), ManifestID: standard.ID}
	if err := q.InsertStandardSchemaVersion(ctx, params); err != nil {
		t.Fatal(err)
	}
	saved, err := q.LatestStandardSchemaVersion(ctx, store.LatestStandardSchemaVersionParams{ProjectID: p.ID, SchemaName: "Product"})
	if err != nil || saved.ManifestID != standard.ID || saved.Version != 2 {
		t.Fatalf("standard: %+v %v", saved, err)
	}
	if err := q.InsertStandardSchemaVersion(ctx, params); !sqlState(err, "23514") {
		t.Fatalf("standard identity not unique: %v", err)
	}
	previous := params
	previous.Version = 1
	if err := q.InsertStandardSchemaVersion(ctx, previous); !sqlState(err, "23514") {
		t.Fatalf("version decreased: %v", err)
	}
	invalidSnapshot := store.InsertPreviewSchemaSnapshotParams{ManifestID: a.ID, SchemaName: "Other", Version: 0, Body: []byte(`{}`)}
	if err := q.InsertPreviewSchemaSnapshot(ctx, invalidSnapshot); !sqlState(err, "23514") {
		t.Fatalf("nonpositive version: %v", err)
	}
	for _, statement := range []string{
		"UPDATE manifests SET app_version='other'", "DELETE FROM manifests",
		"UPDATE schema_versions SET body='{}'", "DELETE FROM schema_versions",
		"UPDATE preview_schema_snapshots SET version=3", "DELETE FROM preview_schema_snapshots",
	} {
		if _, err := pool.Exec(ctx, statement); !sqlState(err, "23514") {
			t.Fatalf("immutable %s: %v", statement, err)
		}
	}
	// MF-003: до активации NULL; ссылка на manifest чужого проекта запрещена.
	other, err := q.CreateProject(ctx, store.CreateProjectParams{ID: uuid.New(), Slug: "another", Name: "Another"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := q.CreateEnvironment(ctx, store.CreateEnvironmentParams{ID: uuid.New(), ProjectID: other.ID, Name: "production", Kind: "standard"})
	if err != nil || env.ActiveManifestID != nil {
		t.Fatalf("initial environment: %+v %v", env, err)
	}
	if _, err := q.SetActiveManifest(ctx, store.SetActiveManifestParams{ProjectID: other.ID, ID: env.ID, ActiveManifestID: &a.ID}); !sqlState(err, "23503") {
		t.Fatalf("cross project manifest: %v", err)
	}
	if _, err := q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: other.ID, ID: a.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross project query: %v", err)
	}
	rows, err := q.ListPreviewSchemaSnapshots(ctx, store.ListPreviewSchemaSnapshotsParams{ProjectID: other.ID, ManifestID: a.ID})
	if err != nil || len(rows) != 0 {
		t.Fatalf("cross project snapshot query: %+v %v", rows, err)
	}
	params.ProjectID = other.ID
	if err := q.InsertStandardSchemaVersion(ctx, params); !sqlState(err, "23503") {
		t.Fatalf("cross project schema: %v", err)
	}
	if err := q.InsertManifest(ctx, store.InsertManifestParams{ID: uuid.New(), ProjectID: p.ID, Hash: "invalid", AppVersion: "1", Body: []byte(`{}`), RegisteredBy: actor.ID}); !sqlState(err, "23514") {
		t.Fatalf("invalid hash: %v", err)
	}
	// Блокировка и смена указателя работают в транзакции и откатываются вместе с ней.
	own, err := q.CreateEnvironment(ctx, store.CreateEnvironmentParams{ID: uuid.New(), ProjectID: p.ID, Name: "preview/one", Kind: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	tq := q.WithTx(tx)
	locked, err := tq.LockManifestEnvironment(ctx, store.LockManifestEnvironmentParams{ProjectID: p.ID, Name: own.Name})
	if err != nil || locked.ID != own.ID {
		t.Fatalf("lock: %+v %v", locked, err)
	}
	if n, err := tq.SetActiveManifest(ctx, store.SetActiveManifestParams{ProjectID: p.ID, ID: own.ID, ActiveManifestID: &a.ID}); err != nil || n != 1 {
		t.Fatalf("activation: %d %v", n, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	envs, err := q.ListEnvironments(ctx, p.ID)
	if err != nil || len(envs) != 1 || envs[0].ActiveManifestID != nil {
		t.Fatalf("rollback: %+v %v", envs, err)
	}
}

func sqlState(err error, code string) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == code
}
