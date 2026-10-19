package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

func TestCapabilityQueriesAreScopedBoundedAndNoStore(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	q := store.New(e.pool)
	admin, err := auth.Authenticate(ctx, q, "Bearer "+e.token)
	if err != nil {
		t.Fatal(err)
	}
	command := func(name string, payload any) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"payload": payload, "reason": "HTTP capability acceptance"})
		response, value := e.do(t, "POST", "/api/v1/commands/"+name, string(raw), e.authed(map[string]string{"Idempotency-Key": uuid.NewString()}))
		if response.StatusCode != 200 {
			t.Fatalf("%s: %d %v", name, response.StatusCode, value)
		}
		return value["result"].(map[string]any)
	}
	index := map[string]any{"repo": map[string]any{"root": ".", "commit": strings.Repeat("a", 40)}, "elements": map[string]any{}, "conventions": map[string]any{"registry": "src/registry.ts", "testCommand": "pnpm test", "manifestCommand": "pnpm cms build"}}
	indexHash, _ := manifest.Hash(index)
	m, _ := manifest.ParseJSON([]byte(cmstest.DefaultManifest))
	app := m.(map[string]any)
	app["codeIndex"] = map[string]any{"hash": indexHash, "uploaded": false}
	hash, _ := manifest.Hash(app)
	upload := command("upload-code-index", map[string]any{"manifestHash": hash, "indexHash": indexHash, "index": index})
	command("register-manifest", map[string]any{"manifest": app, "environment": "staging", "codeIndexUploadId": upload["uploadId"]})
	fixture := cmstest.Env{T: t, Pool: e.pool, Q: q, Admin: admin}
	human := fixture.Human("request-author", auth.DesignRead, auth.CapabilityRequest, auth.CapabilityDeliver, auth.ProjectAdmin, auth.DesignCompose, auth.BehaviorUse)
	secret, digest := auth.NewToken(auth.ServiceTokenPrefix)
	caps := []string{string(auth.DesignRead), string(auth.CapabilityRequest), string(auth.CapabilityDeliver), string(auth.ProjectAdmin), string(auth.DesignCompose), string(auth.BehaviorUse)}
	if _, err = q.CreateAPIToken(ctx, store.CreateAPITokenParams{ID: uuid.New(), ProjectID: admin.ProjectID, ActorID: human.ID, TokenHash: digest, Scopes: caps, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	e.token = secret
	fixture.Exec(`UPDATE projects SET settings=settings || '{"defaultMode":"CODE","maxMode":"CODE"}' WHERE id=$1`, human.ProjectID)
	cs := command("create-changeset", map[string]any{"title": "missing", "targets": []string{"staging"}})
	made := command("apply-operations", map[string]any{"changesetId": cs["id"], "expectedSeq": 0, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": "/missing", "root": map[string]any{"id": "n_root", "type": "Button", "props": map[string]any{"label": "private-value"}}}}}})
	doc := made["operations"].([]any)[0].(map[string]any)["target"]
	created := command("create-capability-request", map[string]any{"environment": "staging", "expectedManifestHash": hash, "changesetId": cs["id"], "expectedSeq": 1, "documentId": doc, "nodeId": "n_root", "event": "click", "key": "store.favorite", "title": "Favorite", "description": "Save favorite", "proposedContract": map[string]any{"args": map[string]any{}}, "acceptance": []string{"Explicit only"}})
	id := created["request"].(map[string]any)["id"].(string)
	root := "/api/v1/capability-requests/" + id
	for _, path := range []string{"/api/v1/capability-requests?environment=staging&kind=action&limit=1", root + "?environment=staging", root + "/context-preview?environment=staging"} {
		response, value := e.do(t, "GET", path, "", e.authed(nil))
		if response.StatusCode != 200 || response.Header.Get("Cache-Control") != "private, no-store" {
			t.Fatal(path, response.StatusCode, value)
		}
	}
	command("submit-capability-request", map[string]any{"id": id, "expectedRevision": 1, "expectedSeq": 1, "expectedManifestHash": hash})
	// STU-308/310: GitHub queries never fetch remotely and repeat scope/filters.
	for _, path := range []string{"/api/v1/capability-github-config?environment=staging", root + "/github?environment=staging", root + "/github-observations?environment=staging&limit=1"} {
		response, value := e.do(t, "GET", path, "", e.authed(nil))
		if response.StatusCode != 200 || response.Header.Get("Cache-Control") != "private, no-store" {
			t.Fatal(path, response.StatusCode, value)
		}
	}
	for _, path := range []string{"/api/v1/capability-github-config", root + "/github?environment=production", root + "/github?environment=staging&unknown=x", root + "/github-observations?limit=bad", root + "/github-observations?limit=101", root + "/github-observations?cursor=bad", root + "/github?environment=staging&environment=staging", "/api/v1/capability-requests/invalid/github"} {
		response, value := e.do(t, "GET", path, "", e.authed(nil))
		if response.StatusCode < 400 || response.StatusCode >= 500 {
			t.Fatal(path, response.StatusCode, value)
		}
	}
	// STU-318/323: preview queries are scoped, bounded and never cached.
	for _, path := range []string{root + "/preview?environment=staging", root + "/preview"} {
		response, value := e.do(t, "GET", path, "", e.authed(nil))
		if response.StatusCode != 200 || response.Header.Get("Cache-Control") != "private, no-store" || value["items"] == nil {
			t.Fatal(path, response.StatusCode, value)
		}
	}
	for path, status := range map[string]int{root + "/preview?environment=production": 404, root + "/preview?x=1": 422, "/api/v1/changesets/" + cs["id"].(string) + "/preview?environment=staging": 404, "/api/v1/changesets/" + cs["id"].(string) + "/preview": 422, "/api/v1/changesets/invalid/preview?environment=preview/pr-1": 0} {
		response, value := e.do(t, "GET", path, "", e.authed(nil))
		if status == 0 && (response.StatusCode < 400 || response.StatusCode >= 500) || status != 0 && response.StatusCode != status {
			t.Fatal(path, response.StatusCode, value)
		}
	}
	response, pack := e.do(t, "GET", root+"/context-pack?version=2&environment=staging", "", e.authed(nil))
	if response.StatusCode != 200 || !strings.Contains(response.Header.Get("Content-Disposition"), "attachment;") || response.Header.Get("X-Context-Hash") == "" {
		t.Fatal(response.StatusCode, pack)
	}
	bytes, _ := json.Marshal(pack)
	if strings.Contains(string(bytes), "private-value") {
		t.Fatal("content exported")
	}
	for _, path := range []string{root + "?environment=production", "/api/v1/capability-requests/" + uuid.NewString(), root + "/context-pack?version=1"} {
		response, value := e.do(t, "GET", path, "", e.authed(nil))
		if response.StatusCode != 404 || value["code"] != "NOT_FOUND" {
			t.Fatal(path, response.StatusCode, value)
		}
	}
	for _, path := range []string{root + "/context-pack?version=0", root + "/context-pack?version=unknown", root + "?unknown=1", root + "?environment=staging&environment=staging", "/api/v1/capability-requests?limit=bad", "/api/v1/capability-requests?limit=101", "/api/v1/capability-requests/invalid"} {
		response, value := e.do(t, "GET", path, "", e.authed(nil))
		if response.StatusCode < 400 || response.StatusCode >= 500 {
			t.Fatal(path, response.StatusCode, value)
		}
	}
	// Delivery uses separate consent and does not change the engineering request.
	for _, route := range []string{"capability-delivery-endpoints", "capability-delivery-target"} {
		response, value := e.do(t, "GET", "/api/v1/"+route+"?environment=staging", "", e.authed(nil))
		if response.StatusCode != 200 || response.Header.Get("Cache-Control") != "private, no-store" {
			t.Fatal(value)
		}
	}
	command("set-capability-delivery-target", map[string]any{"environment": "staging", "endpointId": "engineering", "enabled": true, "expectedRevision": 0})
	response, preview := e.do(t, "GET", root+"/delivery-preview?environment=staging", "", e.authed(nil))
	if response.StatusCode != 200 {
		t.Fatal(preview)
	}
	target := preview["target"].(map[string]any)
	sent := command("send-capability-context", map[string]any{"requestId": id, "expectedRequestRevision": preview["requestRevision"], "expectedPackRevision": preview["packRevision"], "expectedPackHash": preview["packHash"], "expectedSeq": preview["seq"], "expectedManifestHash": preview["manifestHash"], "expectedTargetRevision": target["revision"], "expectedTargetFingerprint": target["fingerprint"], "confirmExternalShare": true})
	item := sent["delivery"].(map[string]any)
	deliveryRoot := "/api/v1/capability-deliveries/" + item["id"].(string)
	for _, path := range []string{root + "/deliveries?environment=staging&limit=1", deliveryRoot + "?environment=staging"} {
		response, value := e.do(t, "GET", path, "", e.authed(nil))
		if response.StatusCode != 200 || value["body"] != nil || value["keyId"] != nil {
			t.Fatal(path, value)
		}
	}
	for _, path := range []string{root + "/delivery-preview?environment=production", root + "/deliveries?environment=production", deliveryRoot + "?environment=production", "/api/v1/capability-deliveries/" + uuid.NewString()} {
		response, value := e.do(t, "GET", path, "", e.authed(nil))
		if response.StatusCode != 404 || value["code"] != "CAPABILITY_DELIVERY_NOT_FOUND" {
			t.Fatal(path, response.StatusCode, value)
		}
	}
	for _, path := range []string{"/api/v1/capability-delivery-target", "/api/v1/capability-delivery-target?environment=staging&unknown=1", root + "/deliveries?environment=staging&limit=bad", root + "/deliveries?limit=101", deliveryRoot + "?environment=staging&environment=staging", "/api/v1/capability-deliveries/invalid"} {
		response, value := e.do(t, "GET", path, "", e.authed(nil))
		if response.StatusCode < 400 || response.StatusCode >= 500 {
			t.Fatal(path, response.StatusCode, value)
		}
	}
	command("cancel-capability-delivery", map[string]any{"deliveryId": item["id"], "expectedDeliveryRevision": item["revision"]})
	// Upload's larger envelope does not expand ordinary commands' body limits.
	large := strings.Repeat(" ", 1024*1024+1) + `{"payload":{}}`
	response, _ = e.do(t, "POST", "/api/v1/commands/create-capability-request", large, e.authed(map[string]string{"Idempotency-Key": "large"}))
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatal(response.StatusCode)
	}
	fixture.Exec(`UPDATE roles SET capabilities='{}' WHERE name='role-request-author'`)
	response, _ = e.do(t, "GET", root+"/context-pack?version=2", "", e.authed(nil))
	if response.StatusCode != 404 {
		t.Fatal("revoked download", response.StatusCode)
	}
}
