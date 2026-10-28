package manifestregistry_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/manifestregistry"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

func indexFixture(t *testing.T) (map[string]any, map[string]any, string, string) {
	t.Helper()
	raw, err := os.ReadFile("../../../../packages/manifest/fixtures/code-index.json")
	if err != nil {
		t.Fatal(err)
	}
	v, err := manifest.ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	index := v.(map[string]any)
	indexHash, _ := manifest.Hash(index)
	m, _ := manifest.ParseJSON([]byte(base))
	app := m.(map[string]any)
	app["codeIndex"] = map[string]any{"hash": indexHash, "uploaded": false}
	app["actions"] = map[string]any{"store.favorite": map[string]any{"args": map[string]any{}, "sourceRef": map[string]any{"file": "src/actions.ts", "export": "favorite", "line": json.Number("12")}}}
	hash, _ := manifest.Hash(app)
	return index, app, hash, indexHash
}

// FR-053: the code index carries no file contents.
func TestCodeIndexPairingImmutabilityAndIsolation(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	index, app, hash, indexHash := indexFixture(t)
	raw, _ := json.Marshal(index)
	p := manifestregistry.UploadIndex{ManifestHash: hash, IndexHash: indexHash, Index: raw}
	var first, again manifestregistry.UploadedIndex
	e.Must(e.Admin, "upload-code-index", p, &first)
	e.Must(e.Admin, "upload-code-index", p, &again)
	if !first.Ready || first.UploadID != again.UploadID || first.Hash != indexHash {
		t.Fatal(first, again)
	}
	human := e.Human("index-human", auth.ManifestRegister)
	if err := e.Do(human, "upload-code-index", p, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		p    manifestregistry.UploadIndex
		code string
	}{{manifestregistry.UploadIndex{ManifestHash: "wrong", IndexHash: indexHash, Index: raw}, "CODE_INDEX_INVALID"}, {manifestregistry.UploadIndex{ManifestHash: hash, IndexHash: hash, Index: raw}, "CODE_INDEX_HASH_MISMATCH"}, {manifestregistry.UploadIndex{ManifestHash: hash, IndexHash: indexHash, Index: json.RawMessage(`{"sourceContents":"private"}`)}, "CODE_INDEX_INVALID"}} {
		if err := e.Do(e.Admin, "upload-code-index", tc.p, nil); cmstest.Code(err) != tc.code {
			t.Fatal(tc.code, err)
		}
	}
	manifestBody, _ := json.Marshal(app)
	id := first.UploadID.String()
	e.Must(e.Admin, "register-manifest", manifestregistry.Payload{Manifest: manifestBody, Environment: "staging", CodeIndexUploadID: &id}, nil)
	active, err := manifestregistry.GetActive(ctx, e.Q, e.Admin.ProjectID, "staging")
	if err != nil || !active.CodeIndexAvailable || active.ManifestHash != hash {
		t.Fatal(active, err)
	}
	e.Must(e.Admin, "register-manifest", manifestregistry.Payload{Manifest: manifestBody, Environment: "production", CodeIndexUploadID: &id}, nil)
	stored, err := e.Q.GetManifestByHash(ctx, store.GetManifestByHashParams{ProjectID: e.Admin.ProjectID, Hash: hash})
	if err != nil || stored.CodeIndexKey == nil || *stored.CodeIndexKey != id {
		t.Fatal(stored, err)
	}
	if count(e, "code_index_uploads") != 1 {
		t.Fatal("content deduplication failed")
	}
	// A separate project cannot register another project's upload.
	app["app"].(map[string]any)["version"] = "2.0.0"
	otherBody, _ := json.Marshal(app)
	if err = e.Do(e.Admin, "register-manifest", manifestregistry.Payload{Manifest: otherBody, Environment: "staging", CodeIndexUploadID: &id}, nil); cmstest.Code(err) != "CODE_INDEX_HASH_MISMATCH" {
		t.Fatal(err)
	}
	missing := uuid.NewString()
	if err = e.Do(e.Admin, "register-manifest", manifestregistry.Payload{Manifest: manifestBody, Environment: "staging", CodeIndexUploadID: &missing}, nil); cmstest.Code(err) != "CODE_INDEX_UPLOAD_NOT_READY" {
		t.Fatal(err)
	}
	// Linked metadata survives TTL cleanup and the stored build still says false.
	e.Exec(`UPDATE code_index_uploads SET created_at=now()-interval '2 days' WHERE id=$1`, first.UploadID)
	e.Must(e.Admin, "upload-code-index", p, &again)
	if again.UploadID != first.UploadID {
		t.Fatal("linked index expired")
	}
	var saved map[string]any
	_ = json.Unmarshal(stored.Body, &saved)
	if saved["codeIndex"].(map[string]any)["uploaded"] != false {
		t.Fatal("deployed manifest mutated")
	}
}

func TestCodeIndexLegacyAndExpiredUploads(t *testing.T) {
	e := setup(t)
	index, app, hash, indexHash := indexFixture(t)
	body, _ := json.Marshal(app)
	raw, _ := json.Marshal(index)
	// Legacy registration stays readable and cannot be enriched under the same hash.
	e.Must(e.Admin, "register-manifest", manifestregistry.Payload{Manifest: body, Environment: "staging"}, nil)
	var upload manifestregistry.UploadedIndex
	e.Must(e.Admin, "upload-code-index", manifestregistry.UploadIndex{ManifestHash: hash, IndexHash: indexHash, Index: raw}, &upload)
	id := upload.UploadID.String()
	if err := e.Do(e.Admin, "register-manifest", manifestregistry.Payload{Manifest: body, Environment: "staging", CodeIndexUploadID: &id}, nil); cmstest.Code(err) != "CODE_INDEX_IMMUTABLE" {
		t.Fatal(err)
	}
	active, err := manifestregistry.GetActive(context.Background(), e.Q, e.Admin.ProjectID, "staging")
	if err != nil || active.CodeIndexAvailable {
		t.Fatal(active, err)
	}
	e.Exec(`UPDATE code_index_uploads SET created_at=now()-interval '2 days' WHERE id=$1`, upload.UploadID)
	if err = e.Do(e.Admin, "register-manifest", manifestregistry.Payload{Manifest: body, Environment: "staging", CodeIndexUploadID: &id}, nil); cmstest.Code(err) != "CODE_INDEX_UPLOAD_NOT_READY" {
		t.Fatal(err)
	}
	var renewed manifestregistry.UploadedIndex
	e.Must(e.Admin, "upload-code-index", manifestregistry.UploadIndex{ManifestHash: hash, IndexHash: indexHash, Index: raw}, &renewed)
	if renewed.UploadID == upload.UploadID {
		t.Fatal("expired unlinked index reused")
	}
	// A metadata record may only describe elements in this exact manifest.
	app["actions"] = map[string]any{}
	badHash, _ := manifest.Hash(app)
	badBody, _ := json.Marshal(app)
	e.Must(e.Admin, "upload-code-index", manifestregistry.UploadIndex{ManifestHash: badHash, IndexHash: indexHash, Index: raw}, &renewed)
	id = renewed.UploadID.String()
	if err = e.Do(e.Admin, "register-manifest", manifestregistry.Payload{Manifest: badBody, Environment: "staging", CodeIndexUploadID: &id}, nil); cmstest.Code(err) != "CODE_INDEX_INVALID" {
		t.Fatal(err)
	}
}
