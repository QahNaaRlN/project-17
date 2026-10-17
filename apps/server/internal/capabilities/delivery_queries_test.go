package capabilities

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/capabilitywebhook"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

func TestDeliveryQueryScopesAndHistory(t *testing.T) {
	f := deliverySetup(t)
	ctx := context.Background()
	endpoints, err := f.s.DeliveryEndpoints(ctx, f.e.Q, f.admin, "staging")
	if err != nil || len(endpoints["items"].([]any)) != 1 {
		t.Fatal(endpoints, err)
	}
	for _, actor := range []auth.Actor{f.a, f.engineer, f.e.Admin, {Kind: auth.ActorAgent}} {
		if _, err = f.s.DeliveryEndpoints(ctx, f.e.Q, actor, "staging"); cmstest.Code(err) != "CAPABILITY_DELIVERY_FORBIDDEN" {
			t.Fatal(err)
		}
	}
	view, err := f.s.DeliveryTarget(ctx, f.e.Q, f.engineer, "staging")
	if err != nil || view["ready"] != true || view["url"] != "https://engineering.example/context" {
		t.Fatal(view, err)
	}
	view, err = f.s.DeliveryTarget(ctx, f.e.Q, f.admin, "production")
	if err != nil || view["configured"] != false || view["revision"] != int64(0) {
		t.Fatal(view, err)
	}
	for _, environment := range []string{"missing", ""} {
		if _, err = f.s.DeliveryTarget(ctx, f.e.Q, f.admin, environment); cmstest.Code(err) != "CAPABILITY_DELIVERY_NOT_FOUND" {
			t.Fatal(err)
		}
	}
	if _, err = f.s.DeliveryTarget(ctx, f.e.Q, auth.Actor{Kind: auth.ActorAgent}, "staging"); cmstest.Code(err) != "CAPABILITY_DELIVERY_FORBIDDEN" {
		t.Fatal(err)
	}
	if _, err = f.s.DeliveryPreview(ctx, f.e.Q, f.engineer, f.request.ID); cmstest.Code(err) != "CAPABILITY_DELIVERY_FORBIDDEN" {
		t.Fatal(err)
	}
	if _, err = f.s.DeliveryPreview(ctx, f.e.Q, f.a, uuid.New()); cmstest.Code(err) != "CAPABILITY_DELIVERY_NOT_FOUND" {
		t.Fatal(err)
	}
	if _, err = f.s.DeliveryGet(ctx, f.e.Q, f.a, uuid.New()); cmstest.Code(err) != "CAPABILITY_DELIVERY_NOT_FOUND" {
		t.Fatal(err)
	}
	if _, err = f.s.DeliveryGet(ctx, f.e.Q, auth.Actor{Kind: auth.ActorAgent}, uuid.New()); cmstest.Code(err) != "CAPABILITY_DELIVERY_FORBIDDEN" {
		t.Fatal(err)
	}
	empty, err := f.s.DeliveryList(ctx, f.e.Q, f.a, f.request.ID, 0, "")
	if err != nil || len(empty["items"].([]any)) != 0 {
		t.Fatal(empty, err)
	}
	for range 3 {
		d := f.send()
		f.command(f.a, "cancel-capability-delivery", CancelDelivery{d.ID, d.Revision}, "")
		target, _ := f.e.Q.GetCapabilityTarget(ctx, store.GetCapabilityTargetParams{ProjectID: f.a.ProjectID, Environment: "staging"})
		f.command(f.admin, "set-capability-delivery-target", SetDeliveryTarget{Environment: "staging", EndpointID: "engineering", Enabled: true, ExpectedRevision: target.Revision}, "")
		f.input.ExpectedTargetRevision++
	}
	first, err := f.s.DeliveryList(ctx, f.e.Q, f.engineer, f.request.ID, 1, "")
	if err != nil || len(first["items"].([]any)) != 1 || first["nextCursor"] == nil {
		t.Fatal(first, err)
	}
	second, err := f.s.DeliveryList(ctx, f.e.Q, f.engineer, f.request.ID, 2, first["nextCursor"].(string))
	if err != nil || len(second["items"].([]any)) != 2 || second["nextCursor"] != nil {
		t.Fatal(second, err)
	}
	if _, err = f.s.DeliveryList(ctx, f.e.Q, f.a, f.request.ID, 1, first["nextCursor"].(string)); cmstest.Code(err) != "CAPABILITY_DELIVERY_STATE_INVALID" {
		t.Fatal("cursor crossed actor", err)
	}
	for _, input := range []struct {
		limit  int
		cursor string
	}{{-1, ""}, {101, ""}, {1, "bad"}, {1, base64.RawURLEncoding.EncodeToString([]byte(`{}`))}} {
		if _, err = f.s.DeliveryList(ctx, f.e.Q, f.a, f.request.ID, input.limit, input.cursor); cmstest.Code(err) != "CAPABILITY_DELIVERY_STATE_INVALID" {
			t.Fatal(err)
		}
	}
	foreign := f.a
	foreign.ProjectID = uuid.New()
	if _, err = f.s.DeliveryList(ctx, f.e.Q, foreign, f.request.ID, 1, ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_NOT_FOUND" {
		t.Fatal("project leak", err)
	}
	if _, err = f.s.DeliveryList(ctx, f.e.Q, auth.Actor{Kind: auth.ActorAgent}, f.request.ID, 1, ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_FORBIDDEN" {
		t.Fatal(err)
	}
}

func TestDeliveryExplicitGatesAndAtomicOutbox(t *testing.T) {
	f := deliverySetup(t)
	ctx := context.Background()
	for _, name := range []string{"send-capability-context", "retry-capability-delivery", "cancel-capability-delivery", "set-capability-delivery-target"} {
		if _, err := f.call(f.a, name, object{}, ""); err == nil {
			t.Fatal("empty payload", name)
		}
	}
	for _, actor := range []auth.Actor{f.e.Admin, {Kind: auth.ActorAgent, ProjectID: f.a.ProjectID, Rights: f.a.Rights}, f.engineer} {
		if _, err := f.call(actor, "send-capability-context", f.input, ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_FORBIDDEN" && cmstest.Code(err) != "UNAUTHENTICATED" {
			t.Fatal(err)
		}
	}
	other := f.e.Human("other-deliverer", auth.CapabilityRequest, auth.DesignRead, auth.CapabilityDeliver)
	if _, err := f.call(other, "send-capability-context", f.input, ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_NOT_FOUND" {
		t.Fatal(err)
	}
	engineerOwner := f.e.Human("engineer-deliverer", auth.CodePropose, auth.CapabilityRequest, auth.DesignRead, auth.CapabilityDeliver)
	if _, err := f.call(engineerOwner, "send-capability-context", f.input, ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_NOT_FOUND" {
		t.Fatal("engineer may not send someone else's CS", err)
	}
	if _, err := f.call(f.admin, "set-capability-delivery-target", SetDeliveryTarget{Environment: "staging", EndpointID: "engineering", ExpectedRevision: -1}, ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_STATE_INVALID" {
		t.Fatal(err)
	}
	if _, err := f.call(f.a, "cancel-capability-delivery", CancelDelivery{DeliveryID: uuid.New(), ExpectedDeliveryRevision: 1}, ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_NOT_FOUND" {
		t.Fatal(err)
	}
	for name, payload := range map[string]any{"retry-capability-delivery": f.retryInput(store.CapabilityDelivery{ID: uuid.New(), Revision: 1}), "cancel-capability-delivery": CancelDelivery{DeliveryID: uuid.New(), ExpectedDeliveryRevision: 1}, "set-capability-delivery-target": SetDeliveryTarget{Environment: "staging", EndpointID: "engineering"}} {
		actor := f.a
		if name == "set-capability-delivery-target" {
			actor = f.admin
		}
		if _, err := f.e.Bus.Dispatch(ctx, actor, commandbus.Request{Name: name, Payload: raw(payload), IdempotencyKey: uuid.NewString()}); cmstest.Code(err) != "REASON_REQUIRED" {
			t.Fatal(name, err)
		}
	}
	for _, change := range []func(*SendContext){func(p *SendContext) { p.ConfirmExternalShare = false }, func(p *SendContext) { p.ExpectedRequestRevision++ }, func(p *SendContext) { p.ExpectedPackRevision++ }, func(p *SendContext) { p.ExpectedPackHash = "sha256:" + strings.Repeat("b", 64) }, func(p *SendContext) { p.ExpectedSeq++ }, func(p *SendContext) { p.ExpectedManifestHash = "sha256:" + strings.Repeat("b", 64) }, func(p *SendContext) { p.ExpectedTargetRevision++ }, func(p *SendContext) { p.ExpectedTargetFingerprint = "sha256:" + strings.Repeat("b", 64) }} {
		input := f.input
		change(&input)
		if _, err := f.call(f.a, "send-capability-context", input, ""); err == nil {
			t.Fatal("stale consent accepted")
		}
	}
	if _, err := f.e.Bus.Dispatch(ctx, f.a, commandbus.Request{Name: "send-capability-context", Payload: raw(f.input), IdempotencyKey: uuid.NewString()}); cmstest.Code(err) != "REASON_REQUIRED" {
		t.Fatal(err)
	}
	// A missing transactional queue must roll back the item and its audit.
	before := f.e.Bus
	f.e.Bus = commandbus.New(f.e.Pool)
	f.s.Register(f.e.Bus)
	if _, err := f.call(f.a, "send-capability-context", f.input, ""); err == nil {
		t.Fatal("outbox absent")
	}
	f.e.Bus = before
	var count int
	if err := f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM capability_deliveries`).Scan(&count); err != nil || count != 0 {
		t.Fatal("rollback", count, err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := f.call(f.a, "send-capability-context", f.input, "")
			if err != nil {
				t.Error(err)
				return
			}
			ids <- result["delivery"].(map[string]any)["id"].(string)
		}()
	}
	wg.Wait()
	close(ids)
	var id string
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("dedup race")
		}
		id = got
	}
	d, _ := f.e.Q.GetCapabilityDelivery(ctx, store.GetCapabilityDeliveryParams{ProjectID: f.a.ProjectID, ID: uuid.MustParse(id)})
	if _, err := f.call(other, "cancel-capability-delivery", CancelDelivery{d.ID, d.Revision}, ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_NOT_FOUND" {
		t.Fatal(err)
	}
	if _, err := f.call(f.a, "cancel-capability-delivery", CancelDelivery{d.ID, d.Revision + 1}, ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_REVISION_CONFLICT" {
		t.Fatal(err)
	}
	if _, err := f.call(f.a, "retry-capability-delivery", f.retryInput(d), ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_STATE_INVALID" {
		t.Fatal(err)
	}
	stale := f.retryInput(d)
	stale.ExpectedDeliveryRevision++
	if _, err := f.call(f.a, "retry-capability-delivery", stale, ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_REVISION_CONFLICT" {
		t.Fatal(err)
	}
	if _, err := f.call(other, "retry-capability-delivery", f.retryInput(d), ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_NOT_FOUND" {
		t.Fatal(err)
	}
	foreign := f.a
	foreign.ProjectID = uuid.New()
	if _, err := f.call(foreign, "retry-capability-delivery", f.retryInput(d), ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_NOT_FOUND" {
		t.Fatal(err)
	}
	if _, err := f.s.DeliveryGet(ctx, f.e.Q, other, d.ID); cmstest.Code(err) != "CAPABILITY_DELIVERY_NOT_FOUND" {
		t.Fatal(err)
	}
	f.command(f.a, "cancel-capability-delivery", CancelDelivery{d.ID, d.Revision}, "")
}

func TestDeliveryTargetRotationAndFreshPack(t *testing.T) {
	f := deliverySetup(t)
	ctx := context.Background()
	d := f.send()
	for _, payload := range []SetDeliveryTarget{{"missing", "engineering", true, 0}, {"staging", "absent", true, 1}, {"staging", "engineering", true, 0}} {
		if _, err := f.call(f.admin, "set-capability-delivery-target", payload, ""); err == nil {
			t.Fatal(payload)
		}
	}
	var config object
	_ = json.Unmarshal(raw(object{"endpoints": []any{object{"endpointId": "engineering", "projectId": f.a.ProjectID, "environment": "staging", "url": "https://engineering.example/context", "activeKeyId": "key2", "keys": object{"key1": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))), "key2": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("n", 32)))}}}}), &config)
	f.s.Catalog, _ = capabilitywebhook.Parse(raw(config), false)
	if err := f.s.ValidateCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	f.s.Sender = senderFunc(func(_ context.Context, _ capabilitywebhook.Endpoint, m capabilitywebhook.Message) capabilitywebhook.Outcome {
		if m.KeyID != "key1" {
			t.Error("rotated pinned key")
		}
		return capabilitywebhook.Outcome{Code: "HTTP_PERMANENT", HTTPStatus: 403}
	})
	d = f.deliver(d)
	if d.State != "failed" || d.RetryableExhaustion {
		t.Fatal("permanent error retryable")
	}
	entry := config["endpoints"].([]any)[0].(map[string]any)
	entry["keys"].(map[string]any)["key1"] = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	f.s.Catalog, _ = capabilitywebhook.Parse(raw(config), false)
	if err := f.s.ValidateCatalog(ctx); err != capabilitywebhook.ErrConfig {
		t.Fatal("reused key ID", err)
	}
	entry["environment"] = "unknown"
	f.s.Catalog, _ = capabilitywebhook.Parse(raw(config), false)
	if err := f.s.ValidateCatalog(ctx); err != capabilitywebhook.ErrConfig {
		t.Fatal("unknown scope", err)
	}
	f.s.Catalog = nil
	if err := f.s.ValidateCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := f.s.DeliveryTarget(ctx, f.e.Q, f.a, "staging")
	if err != nil || view["ready"] != false {
		t.Fatal(view, err)
	}
	if _, err := f.s.DeliveryPreview(ctx, f.e.Q, f.a, f.request.ID); cmstest.Code(err) != "CAPABILITY_DELIVERY_UNAVAILABLE" {
		t.Fatal(err)
	}
	// Engineering transitions do not invalidate an older saved pack revision.
	f = deliverySetup(t)
	must(f.e, f.engineer, "update-capability-request", Update{ID: f.request.ID, ExpectedRevision: 2, Transition: "accepted"})
	f.input.ExpectedRequestRevision = 3
	if f.send().PackRevision != 2 {
		t.Fatal("engineering status rewrote pack")
	}
	// A fresh page hash is required even while the exact pending call remains.
	f.e.Must(f.a, "apply-operations", object{"changesetId": f.cs, "expectedSeq": 2, "operations": []any{object{"type": "node.rename", "target": f.doc, "payload": object{"nodeId": "n_root", "name": "Changed"}}}}, nil)
	if _, err := f.s.DeliveryPreview(ctx, f.e.Q, f.a, f.request.ID); cmstest.Code(err) != "CAPABILITY_DELIVERY_CONTEXT_STALE" {
		t.Fatal(err)
	}
	f.e.Exec(`DELETE FROM role_bindings WHERE actor_id=$1`, f.a.ID)
	if _, err := f.call(f.a, "send-capability-context", f.input, ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_FORBIDDEN" {
		t.Fatal("stale caller rights", err)
	}
}

func TestDeliveryFreshPackAndRetryRequireTheSavedContext(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"terminal", "missing_pack", "page_hash", "policy"} {
		t.Run(mode, func(t *testing.T) {
			f := deliverySetup(t)
			switch mode {
			case "terminal":
				must(f.e, f.a, "update-capability-request", Update{ID: f.request.ID, ExpectedRevision: 2, Transition: "cancelled"})
			case "missing_pack":
				f.e.Exec(`DELETE FROM capability_context_packs WHERE request_id=$1`, f.request.ID)
			case "page_hash":
				f.e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{nodes,n_root,name}','"Legacy drift"') WHERE object_id=$1 AND state='working'`, f.doc)
			case "policy":
				f.e.Exec(`UPDATE projects SET settings=settings || '{"defaultMode":"SYSTEM","maxMode":"SYSTEM"}' WHERE id=$1`, f.a.ProjectID)
			}
			if _, err := f.s.DeliveryPreview(ctx, f.e.Q, f.a, f.request.ID); err == nil {
				t.Fatal("unreviewed context accepted")
			}
		})
	}
	f := deliverySetup(t)
	d := f.send()
	f.e.Exec(`UPDATE capability_deliveries SET state='failed',retryable_exhaustion=true,attempts_in_cycle=6,total_attempts=6 WHERE id=$1`, d.ID)
	originalCatalog := f.s.Catalog
	f.s.Catalog = nil
	if _, err := f.call(f.a, "retry-capability-delivery", f.retryInput(d), ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_UNAVAILABLE" {
		t.Fatal(err)
	}
	f.s.Catalog = originalCatalog
	must(f.e, f.a, "refresh-capability-context", Action{ID: f.request.ID, ExpectedRevision: 2, ExpectedSeq: 2, ExpectedManifestHash: f.hash})
	f.input.ExpectedRequestRevision = 3
	if _, err := f.call(f.a, "retry-capability-delivery", f.retryInput(d), ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_CONTEXT_STALE" {
		t.Fatal(err)
	}
	f.e.Exec(`UPDATE capability_deliveries SET state='queued',attempts_in_cycle=0,total_attempts=0 WHERE id=$1`, d.ID)
	if f.deliver(d).LastError != "ANCHOR_CHANGED" {
		t.Fatal("superseded pack sent")
	}
}
