package capabilities

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/capabilitywebhook"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/riverqueue/river"
)

type deliveryFixture struct {
	fixture
	s     *DeliveryService
	admin auth.Actor
	input SendContext
}

func deliverySetup(t *testing.T) deliveryFixture {
	f := setup(t)
	f.a.Rights[auth.CapabilityDeliver] = true
	f.e.Exec(`UPDATE roles SET capabilities=array_append(capabilities,'capability.deliver') WHERE project_id=$1 AND id IN (SELECT role_id FROM role_bindings WHERE actor_id=$2)`, f.a.ProjectID, f.a.ID)
	f.create()
	must(f.e, f.a, "submit-capability-request", Action{ID: f.request.ID, ExpectedRevision: 1, ExpectedSeq: 1, ExpectedManifestHash: f.hash})
	admin := f.e.Human("delivery-admin", auth.ProjectAdmin)
	config := raw(object{"endpoints": []any{object{"endpointId": "engineering", "projectId": f.a.ProjectID, "environment": "staging", "url": "https://engineering.example/context", "activeKeyId": "key1", "keys": object{"key1": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))}}}})
	catalog, err := capabilitywebhook.Parse(config, false)
	if err != nil {
		t.Fatal(err)
	}
	s := &DeliveryService{Pool: f.e.Pool, Catalog: catalog}
	s.Register(f.e.Bus)
	d := deliveryFixture{fixture: f, s: s, admin: admin}
	d.command(admin, "set-capability-delivery-target", SetDeliveryTarget{Environment: "staging", EndpointID: "engineering", Enabled: true}, "")
	preview, err := s.DeliveryPreview(context.Background(), f.e.Q, f.a, f.request.ID)
	if err != nil {
		t.Fatal(err)
	}
	target := preview["target"].(object)
	d.input = SendContext{RequestID: f.request.ID, ExpectedRequestRevision: preview["requestRevision"].(int64), ExpectedPackRevision: preview["packRevision"].(int64), ExpectedPackHash: preview["packHash"].(string), ExpectedSeq: preview["seq"].(int32), ExpectedManifestHash: preview["manifestHash"].(string), ExpectedTargetRevision: target["revision"].(int64), ExpectedTargetFingerprint: target["fingerprint"].(string), ConfirmExternalShare: true}
	return d
}
func (f deliveryFixture) call(a auth.Actor, name string, p any, key string) (object, error) {
	f.e.T.Helper()
	if key == "" {
		key = uuid.NewString()
	}
	response, err := f.e.Bus.Dispatch(context.Background(), a, commandbus.Request{Name: name, Payload: raw(p), IdempotencyKey: key, Reason: "explicit delivery consent"})
	if err != nil {
		return nil, err
	}
	var result struct{ Result object }
	err = json.Unmarshal(response.Body, &result)
	return result.Result, err
}
func (f deliveryFixture) command(a auth.Actor, name string, p any, key string) object {
	f.e.T.Helper()
	v, err := f.call(a, name, p, key)
	if err != nil {
		f.e.T.Fatalf("%s: %v", name, err)
	}
	return v
}
func (f deliveryFixture) send() store.CapabilityDelivery {
	f.e.T.Helper()
	v := f.command(f.a, "send-capability-context", f.input, "")
	id := uuid.MustParse(v["delivery"].(map[string]any)["id"].(string))
	d, err := f.e.Q.GetCapabilityDelivery(context.Background(), store.GetCapabilityDeliveryParams{ProjectID: f.a.ProjectID, ID: id})
	if err != nil {
		f.e.T.Fatal(err)
	}
	return d
}

type senderFunc func(context.Context, capabilitywebhook.Endpoint, capabilitywebhook.Message) capabilitywebhook.Outcome

func (f senderFunc) Send(ctx context.Context, e capabilitywebhook.Endpoint, m capabilitywebhook.Message) capabilitywebhook.Outcome {
	return f(ctx, e, m)
}
func (f deliveryFixture) deliver(d store.CapabilityDelivery) store.CapabilityDelivery {
	f.e.T.Helper()
	if err := f.s.Deliver(context.Background(), DeliveryArgs{ProjectID: d.ProjectID, DeliveryID: d.ID}); err != nil {
		f.e.T.Fatal(err)
	}
	saved, err := f.e.Q.GetCapabilityDelivery(context.Background(), store.GetCapabilityDeliveryParams{ProjectID: d.ProjectID, ID: d.ID})
	if err != nil {
		f.e.T.Fatal(err)
	}
	return saved
}
func (f deliveryFixture) retryInput(d store.CapabilityDelivery) RetryDelivery {
	return RetryDelivery{DeliveryID: d.ID, ExpectedDeliveryRevision: d.Revision, ExpectedRequestRevision: f.input.ExpectedRequestRevision, ExpectedSeq: f.input.ExpectedSeq, ExpectedManifestHash: f.input.ExpectedManifestHash, ExpectedTargetRevision: f.input.ExpectedTargetRevision, ConfirmExternalShare: true}
}

func TestDeliveryImmutableDedupAndWorkflowIsolation(t *testing.T) {
	f := deliverySetup(t)
	ctx := context.Background()
	if err := f.s.ValidateCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	d := f.send()
	again := f.command(f.a, "send-capability-context", f.input, "second-consent")
	if again["deduplicated"] != true || again["delivery"].(map[string]any)["id"] != d.ID.String() {
		t.Fatal(again)
	}
	if len(d.Body) == 0 || capabilitywebhook.Hash(d.Body) != d.BodyHash || strings.Contains(string(d.Body), "private-secret") {
		t.Fatal("body")
	}
	var envelope object
	_ = json.Unmarshal(d.Body, &envelope)
	if envelope["event"] != "capability.context.delivery.v1" || envelope["deliveryId"] != d.ID.String() {
		t.Fatal(envelope)
	}
	if _, err := f.e.Pool.Exec(ctx, `UPDATE capability_deliveries SET body='changed' WHERE id=$1`, d.ID); err == nil {
		t.Fatal("mutable body")
	}
	f.s.Sender = senderFunc(func(_ context.Context, _ capabilitywebhook.Endpoint, message capabilitywebhook.Message) capabilitywebhook.Outcome {
		if string(message.Body) != string(d.Body) || message.DeliveryID != d.ID.String() || message.KeyID != "key1" {
			t.Error("pins")
		}
		return capabilitywebhook.Outcome{Acknowledged: true, Code: "STORED", HTTPStatus: 202}
	})
	d = f.deliver(d)
	if d.State != "delivered" || d.TotalAttempts != 1 || d.DeliveredAt == nil {
		t.Fatal(d.State)
	}
	r, err := f.e.Q.GetCapabilityRequest(ctx, store.GetCapabilityRequestParams{ProjectID: d.ProjectID, ID: d.RequestID})
	if err != nil || r.Revision != f.input.ExpectedRequestRevision || r.Status != "submitted" {
		t.Fatal("transport mutated request", err, r)
	}
	cs, _ := f.e.Q.GetChangeset(ctx, store.GetChangesetParams{ProjectID: d.ProjectID, ID: f.cs})
	if cs.Seq != f.input.ExpectedSeq {
		t.Fatal("transport mutated CS")
	}
	view, err := f.s.DeliveryGet(ctx, f.e.Q, f.engineer, d.ID)
	if err != nil || view["canRetry"] != false || view["body"] != nil || view["keyId"] != nil || view["leaseToken"] != nil {
		t.Fatal(view, err)
	}
	attempt := view["attempts"].([]any)[0].(object)
	if attempt["receiptDeliveryId"] == nil || attempt["receiptBodyHash"] == nil {
		t.Fatal(attempt)
	}
	if _, err = f.call(f.a, "cancel-capability-delivery", CancelDelivery{d.ID, d.Revision}, ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_STATE_INVALID" {
		t.Fatal(err)
	}
	if f.deliver(d).TotalAttempts != 1 {
		t.Fatal("terminal resent")
	}
	if f.command(f.a, "send-capability-context", f.input, "")["deduplicated"] != true {
		t.Fatal("terminal dedup")
	}
}

func TestDeliveryRetryBudgetRecoveryAndRotation(t *testing.T) {
	f := deliverySetup(t)
	d := f.send()
	original := append([]byte(nil), d.Body...)
	f.s.Sender = senderFunc(func(context.Context, capabilitywebhook.Endpoint, capabilitywebhook.Message) capabilitywebhook.Outcome {
		return capabilitywebhook.Outcome{Code: "ACK_INVALID", Retryable: true, HTTPStatus: 200}
	})
	for cycle := 1; cycle <= 3; cycle++ {
		for attempt := 1; attempt <= 6; attempt++ {
			f.e.Exec(`UPDATE capability_deliveries SET next_attempt_at=now()-interval '1 second' WHERE id=$1`, d.ID)
			d = f.deliver(d)
			if d.TotalAttempts != int32((cycle-1)*6+attempt) || string(d.Body) != string(original) {
				t.Fatal("attempt or immutable body")
			}
			if attempt < 6 && (d.State != "retry_wait" || time.Until(d.NextAttemptAt) < 25*time.Second) {
				t.Fatal("backoff", d.State)
			}
		}
		if d.State != "failed" || !d.RetryableExhaustion {
			t.Fatal("exhaustion")
		}
		view, err := f.s.DeliveryGet(context.Background(), f.e.Q, f.a, d.ID)
		if err != nil || view["canRetry"] != (cycle < 3) {
			t.Fatal(view, err)
		}
		_, err = f.call(f.a, "retry-capability-delivery", f.retryInput(d), "")
		if cycle == 3 {
			if cmstest.Code(err) != "CAPABILITY_DELIVERY_RETRY_EXHAUSTED" {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if f.deliver(d).TotalAttempts != 18 {
		t.Fatal("budget exceeded")
	}
	for attempt := int32(1); attempt <= 6; attempt++ {
		delay := retryDelay(attempt, 0)
		if delay < 30*time.Second || delay > 144*time.Minute {
			t.Fatal(delay)
		}
	}
	if retryDelay(1, 2*time.Hour) < 2*time.Hour {
		t.Fatal("Retry-After ignored")
	}
}

func TestDeliveryFreshAuthorityAndInFlightCancellation(t *testing.T) {
	for _, mode := range []string{"rights", "disabled_actor", "terminal", "anchor", "target", "disabled_target", "key"} {
		t.Run(mode, func(t *testing.T) {
			f := deliverySetup(t)
			d := f.send()
			calls := 0
			f.s.Sender = senderFunc(func(context.Context, capabilitywebhook.Endpoint, capabilitywebhook.Message) capabilitywebhook.Outcome {
				calls++
				return capabilitywebhook.Outcome{Acknowledged: true, Code: "STORED"}
			})
			switch mode {
			case "rights":
				f.e.Exec(`DELETE FROM role_bindings WHERE project_id=$1 AND actor_id=$2`, f.a.ProjectID, f.a.ID)
			case "disabled_actor":
				f.e.Exec(`UPDATE actors SET disabled_at=now() WHERE id=$1`, f.a.ID)
			case "terminal":
				must(f.e, f.a, "update-capability-request", Update{ID: d.RequestID, ExpectedRevision: 2, Transition: "cancelled"})
			case "anchor":
				f.e.Must(f.a, "undo", object{"changesetId": f.cs, "expectedSeq": 2}, nil)
			case "target", "disabled_target":
				f.command(f.admin, "set-capability-delivery-target", SetDeliveryTarget{Environment: "staging", EndpointID: "engineering", Enabled: mode != "disabled_target", ExpectedRevision: 1}, "")
			case "key":
				f.s.Catalog, _ = capabilitywebhook.Parse(raw(object{"endpoints": []any{object{"endpointId": "engineering", "projectId": f.a.ProjectID, "environment": "staging", "url": "https://engineering.example/context", "activeKeyId": "key2", "keys": object{"key2": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("n", 32)))}}}}), false)
			}
			d = f.deliver(d)
			if calls != 0 || deliveryActive(d.State) || d.LastError == "" {
				t.Fatal("gate", mode, calls, d.State, d.LastError)
			}
		})
	}
	f := deliverySetup(t)
	d := f.send()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	f.s.Sender = senderFunc(func(context.Context, capabilitywebhook.Endpoint, capabilitywebhook.Message) capabilitywebhook.Outcome {
		close(started)
		<-release
		return capabilitywebhook.Outcome{Acknowledged: true, HTTPStatus: 200, Code: "STORED"}
	})
	go func() { done <- f.s.Deliver(context.Background(), DeliveryArgs{d.ProjectID, d.ID}) }()
	<-started
	current, _ := f.e.Q.GetCapabilityDelivery(context.Background(), store.GetCapabilityDeliveryParams{ProjectID: d.ProjectID, ID: d.ID})
	f.command(f.admin, "cancel-capability-delivery", CancelDelivery{d.ID, current.Revision}, "")
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	view, err := f.s.DeliveryGet(context.Background(), f.e.Q, f.admin, d.ID)
	if err != nil || view["state"] != "cancelled" || view["attempts"].([]any)[0].(object)["outcome"] != "acknowledged_after_cancel" {
		t.Fatal(view, err)
	}
	beforeAudit, _ := f.e.Q.CapabilityDeliveryAudit(context.Background(), store.CapabilityDeliveryAuditParams{ProjectID: d.ProjectID, DeliveryID: &d.ID})
	if err = f.s.finishDelivery(context.Background(), current, capabilitywebhook.Outcome{Acknowledged: true, Code: "STORED"}); err != nil {
		t.Fatal(err)
	}
	afterAudit, _ := f.e.Q.CapabilityDeliveryAudit(context.Background(), store.CapabilityDeliveryAuditParams{ProjectID: d.ProjectID, DeliveryID: &d.ID})
	if len(beforeAudit) != len(afterAudit) {
		t.Fatal("duplicate late completion rewrote cancellation audit")
	}
}

func TestDeliveryConcurrentClaimsExpiredLeaseAndRecovery(t *testing.T) {
	f := deliverySetup(t)
	d := f.send()
	ctx := context.Background()
	claimed, _, ok, err := f.s.claimDelivery(ctx, DeliveryArgs{d.ProjectID, d.ID})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	var group sync.WaitGroup
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, _, claimed, err := f.s.claimDelivery(ctx, DeliveryArgs{d.ProjectID, d.ID}); err != nil || claimed {
				t.Error("second live lease", claimed, err)
			}
		}()
	}
	group.Wait()
	f.e.Exec(`UPDATE capability_deliveries SET lease_until=now()-interval '1 second',next_attempt_at=now()-interval '1 second' WHERE id=$1`, d.ID)
	second, _, ok, err := f.s.claimDelivery(ctx, DeliveryArgs{d.ProjectID, d.ID})
	if err != nil || !ok || second.TotalAttempts != 2 {
		t.Fatal(ok, err)
	}
	if err = f.s.finishDelivery(ctx, claimed, capabilitywebhook.Outcome{Acknowledged: true, Code: "STORED"}); err != nil {
		t.Fatal(err)
	}
	current, _ := f.e.Q.GetCapabilityDelivery(ctx, store.GetCapabilityDeliveryParams{ProjectID: d.ProjectID, ID: d.ID})
	if current.State != "sending" || *current.LeaseToken != *second.LeaseToken {
		t.Fatal("late stale lease overwrote state")
	}
	if err = f.s.finishDelivery(ctx, second, capabilitywebhook.Outcome{Retryable: true, Code: "NETWORK_UNCERTAIN"}); err != nil {
		t.Fatal(err)
	}
	f.e.Exec(`UPDATE capability_deliveries SET next_attempt_at=now()-interval '1 second' WHERE id=$1`, d.ID)
	queue, err := jobs.NewInserter(f.e.Pool)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.RecoverDeliveries(ctx, queue); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Deliver(ctx, DeliveryArgs{d.ProjectID, uuid.New()}); err != nil {
		t.Fatal(err)
	}
	// The persisted attempt remains authoritative even if River discarded its job.
	f.e.Exec(`UPDATE river_job SET state='discarded',finalized_at=now() WHERE kind='capability_context_delivery'`)
	if err = f.s.RecoverDeliveries(ctx, queue); err != nil {
		t.Fatal(err)
	}
	if err = f.s.finishDelivery(ctx, second, capabilitywebhook.Outcome{Acknowledged: true}); err != nil {
		t.Fatal(err)
	}
}

func TestDeliveryLostClaimsBudgetAndRiverWorker(t *testing.T) {
	f := deliverySetup(t)
	ctx := context.Background()
	d := f.send()
	f.s.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	for attempt := 1; attempt <= 6; attempt++ {
		_, _, ok, err := f.s.claimDelivery(ctx, DeliveryArgs{d.ProjectID, d.ID})
		if err != nil || !ok {
			t.Fatal(attempt, ok, err)
		}
		f.e.Exec(`UPDATE capability_deliveries SET lease_until=now()-interval '1 second',next_attempt_at=now()-interval '1 second' WHERE id=$1`, d.ID)
	}
	saved, _, claimed, err := f.s.claimDelivery(ctx, DeliveryArgs{d.ProjectID, d.ID})
	if err != nil || claimed || saved.State != "failed" || !saved.RetryableExhaustion || saved.TotalAttempts != 6 {
		t.Fatal("crashed claims bypassed budget", saved.State, claimed, err)
	}
	f.command(f.a, "retry-capability-delivery", f.retryInput(saved), "")
	f.s.Sender = senderFunc(func(context.Context, capabilitywebhook.Endpoint, capabilitywebhook.Message) capabilitywebhook.Outcome {
		return capabilitywebhook.Outcome{Acknowledged: true, Code: "STORED"}
	})
	worker := &DeliveryWorker{Service: f.s}
	if err = worker.Work(ctx, &river.Job[DeliveryArgs]{Args: DeliveryArgs{d.ProjectID, d.ID}}); err != nil {
		t.Fatal(err)
	}
	queue, err := jobs.NewInserter(f.e.Pool)
	if err != nil {
		t.Fatal(err)
	}
	recovery := &DeliveryRecoveryWorker{Service: f.s, Queue: queue}
	if err = recovery.Work(ctx, &river.Job[DeliveryRecoveryArgs]{}); err != nil {
		t.Fatal(err)
	}
	if (DeliveryArgs{}).Kind() == "" || (DeliveryRecoveryArgs{}).Kind() == "" {
		t.Fatal("job kinds")
	}
	if (DeliveryArgs{}).InsertOpts().Queue != DeliveryQueue || (DeliveryRecoveryArgs{}).InsertOpts().MaxAttempts != 1 {
		t.Fatal("worker queue/budget")
	}
}

func TestDeliveryRealSignedHTTPReceiptAndJournal(t *testing.T) {
	f := deliverySetup(t)
	var mu sync.Mutex
	var messages []capabilitywebhook.Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		if !capabilitywebhook.Verify([]byte(strings.Repeat("k", 32)), r.Header, body, time.Now()) {
			t.Error("signature")
		}
		messages = append(messages, capabilitywebhook.Message{DeliveryID: r.Header.Get("X-CMS-Delivery-Id"), BodyHash: r.Header.Get("X-CMS-Body-Hash"), KeyID: r.Header.Get("X-CMS-Key-Id"), Body: body})
		if len(messages) == 1 {
			w.WriteHeader(204)
			return
		}
		_ = json.NewEncoder(w).Encode(object{"deliveryId": r.Header.Get("X-CMS-Delivery-Id"), "bodyHash": r.Header.Get("X-CMS-Body-Hash"), "status": "stored"})
	}))
	defer server.Close()
	f.s.Catalog, _ = capabilitywebhook.Parse(raw(object{"endpoints": []any{object{"endpointId": "engineering", "projectId": f.a.ProjectID, "environment": "staging", "url": server.URL, "activeKeyId": "key1", "keys": object{"key1": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))}}}}), true)
	f.s.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	configured := f.command(f.admin, "set-capability-delivery-target", SetDeliveryTarget{Environment: "staging", EndpointID: "engineering", Enabled: true, ExpectedRevision: 1}, "")
	f.input.ExpectedTargetRevision = 2
	f.input.ExpectedTargetFingerprint = configured["fingerprint"].(string)
	d := f.deliver(f.send())
	if d.State != "retry_wait" || d.LastError != "ACK_INVALID" {
		t.Fatal(d.State, d.LastError)
	}
	f.e.Exec(`UPDATE capability_deliveries SET next_attempt_at=now()-interval '1 second' WHERE id=$1`, d.ID)
	d = f.deliver(d)
	mu.Lock()
	defer mu.Unlock()
	if d.State != "delivered" || len(messages) != 2 || string(messages[0].Body) != string(messages[1].Body) || messages[0].DeliveryID != messages[1].DeliveryID {
		t.Fatal("HTTP receipt and immutable retry")
	}
	if _, err := f.s.DeliveryGet(context.Background(), f.e.Q, f.engineer, d.ID); err != nil {
		t.Fatal(err)
	}
}
