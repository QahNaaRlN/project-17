package assets

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

// OPS-010/API-010: DB faults roll back processing and never hide a failed enqueue.
func TestAssetDatabaseFaults(t *testing.T) {
	e := cmstest.New(t)
	m := &memoryStorage{data: map[string][]byte{}}
	s := &Service{Pool: e.Pool, Storage: m}
	s.Register(e.Bus)
	ctx := context.Background()
	u := begin(t, e, s, m, pngBytes(t, 2, 2), "image/png")
	e.Exec(`CREATE FUNCTION fail_asset_processing() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='processing' THEN RAISE EXCEPTION 'fault'; END IF; RETURN NEW; END $$`)
	e.Exec(`CREATE TRIGGER fault BEFORE UPDATE ON asset_uploads FOR EACH ROW EXECUTE FUNCTION fail_asset_processing()`)
	if e.Do(e.Admin, "complete-asset-upload", Ref{u.AssetID}, nil) == nil {
		t.Fatal("update failure swallowed")
	}
	got, err := s.Get(ctx, e.Admin, u.AssetID)
	if err != nil || got.Status != "pending" {
		t.Fatal(got, err)
	}
	e.Exec(`DROP TRIGGER fault ON asset_uploads`)
	e.Must(e.Admin, "complete-asset-upload", Ref{u.AssetID}, nil)
	e.Exec(`CREATE FUNCTION fail_asset_file() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fault'; END $$`)
	e.Exec(`CREATE TRIGGER fault BEFORE INSERT ON asset_files FOR EACH ROW EXECUTE FUNCTION fail_asset_file()`)
	if s.Process(ctx, ProcessArgs{u.AssetID, e.Admin.ProjectID}) == nil {
		t.Fatal("file failure swallowed")
	}
	got, err = s.Get(ctx, e.Admin, u.AssetID)
	if err != nil || got.Status != "processing" {
		t.Fatal(got, err)
	}
	e.Exec(`DROP TRIGGER fault ON asset_files`)
	if err := s.Process(ctx, ProcessArgs{u.AssetID, e.Admin.ProjectID}); err != nil {
		t.Fatal(err)
	}
	e.Exec(`ALTER TABLE asset_uploads RENAME TO hidden_uploads`)
	if s.Process(ctx, ProcessArgs{u.AssetID, e.Admin.ProjectID}) == nil {
		t.Fatal("query failure swallowed")
	}
	e.Exec(`ALTER TABLE hidden_uploads RENAME TO asset_uploads`)
	e.Exec(`ALTER TABLE projects RENAME COLUMN settings TO hidden_settings`)
	if e.Do(e.Admin, "create-asset-upload", Create{Filename: "f", MIME: "image/png", Size: 1, SHA256: strings.Repeat("a", 64)}, nil) == nil {
		t.Fatal("policy query swallowed")
	}
	e.Exec(`ALTER TABLE projects RENAME COLUMN hidden_settings TO settings`)
	s.Storage = nil
	if e.Do(e.Admin, "create-asset-upload", Create{Filename: "f", MIME: "image/png", Size: 1, SHA256: strings.Repeat("a", 64)}, nil) == nil {
		t.Fatal("missing storage")
	}
}

func TestSVGAndStorageMalformedInputs(t *testing.T) {
	if _, err := sanitizeSVG([]byte(`<svg>` + strings.Repeat(`<g/>`, 100001) + `</svg>`)); err == nil {
		t.Fatal("token limit")
	}
	if _, err := sanitizeSVG([]byte(`<svg><use href=""/></svg>`)); err != nil {
		t.Fatal(err)
	}
	if w, h := svgDimensions(nil); w != 0 || h != 0 {
		t.Fatal(w, h)
	}
	s, err := NewS3("127.0.0.1:1", "test", "test", "bad/bucket", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UploadURL(context.Background(), "key"); err == nil {
		t.Fatal("invalid bucket signing")
	}
	if _, _, err := s.Open(context.Background(), "key"); err == nil {
		t.Fatal("invalid bucket reading")
	}
}

// CNT-040/OPS-002: downgrade cannot erase the immutable file/upload audit.
func TestAssetDowngradePreservesAudit(t *testing.T) {
	e := cmstest.New(t)
	m := &memoryStorage{data: map[string][]byte{}}
	s := &Service{Pool: e.Pool, Storage: m}
	s.Register(e.Bus)
	u := ready(t, e, s, m, pngBytes(t, 2, 2))
	err := postgres.Migrate(context.Background(), e.Pool, "down", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "asset upload or file data prevents downgrade") {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), e.Admin, u.AssetID)
	if err != nil || got.Status != "ready" || got.FileHash == nil {
		t.Fatal(got, err)
	}
}
