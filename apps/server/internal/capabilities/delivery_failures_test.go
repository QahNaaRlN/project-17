package capabilities

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/capabilitywebhook"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

func TestDeliveryQueriesPropagateStorageFailures(t *testing.T) {
	f := deliverySetup(t)
	d := f.send()
	ctx := context.Background()
	for _, needle := range []string{"FROM capability_requests", "FROM capability_context_packs", "FROM changesets", "FROM environments", "FROM manifests", "FROM object_versions", "FROM projects", "FROM capability_delivery_targets"} {
		q := store.New(faultDB{DBTX: f.e.Pool, needle: needle})
		if _, err := f.s.DeliveryPreview(ctx, q, f.a, f.request.ID); !errors.Is(err, storageFailure) {
			t.Fatalf("preview %s: %v", needle, err)
		}
	}
	for _, needle := range []string{"FROM capability_requests", "FROM capability_deliveries", "FROM capability_delivery_attempts", "FROM capability_delivery_audit"} {
		q := store.New(faultDB{DBTX: f.e.Pool, needle: needle})
		if _, err := f.s.DeliveryGet(ctx, q, f.a, d.ID); !errors.Is(err, storageFailure) {
			t.Fatalf("detail %s: %v", needle, err)
		}
	}
	for _, needle := range []string{"FROM capability_requests", "FROM capability_deliveries"} {
		q := store.New(faultDB{DBTX: f.e.Pool, needle: needle})
		if _, err := f.s.DeliveryList(ctx, q, f.a, f.request.ID, 1, ""); !errors.Is(err, storageFailure) {
			t.Fatal(err)
		}
	}
	q := store.New(faultDB{DBTX: f.e.Pool, needle: "FROM environments"})
	if _, err := f.s.DeliveryEndpoints(ctx, q, f.admin, "staging"); !errors.Is(err, storageFailure) {
		t.Fatal(err)
	}
	q = store.New(faultDB{DBTX: f.e.Pool, needle: "FROM capability_delivery_targets"})
	if _, err := f.s.DeliveryTarget(ctx, q, f.admin, "staging"); !errors.Is(err, storageFailure) {
		t.Fatal(err)
	}
	for _, needle := range []string{"FROM actors", "FROM role_bindings", "FROM capability_context_packs", "FROM changesets"} {
		q := store.New(faultDB{DBTX: f.e.Pool, needle: needle})
		r, _ := f.e.Q.GetCapabilityRequest(ctx, store.GetCapabilityRequestParams{ProjectID: d.ProjectID, ID: d.RequestID})
		target, _ := f.e.Q.GetCapabilityTarget(ctx, store.GetCapabilityTargetParams{ProjectID: d.ProjectID, Environment: r.Environment})
		if _, _, err := f.s.workerGate(ctx, q, d, r, target); !errors.Is(err, storageFailure) {
			t.Fatal(needle, err)
		}
	}
}

func TestDeliveryWriteFailuresRollbackOutboxAndAudit(t *testing.T) {
	for _, stage := range []string{"target", "send", "cancel", "retry"} {
		for _, table := range []string{"capability_delivery_audit", "capability_deliveries"} {
			t.Run(stage+"/"+table, func(t *testing.T) {
				f := deliverySetup(t)
				name := "send-capability-context"
				payload := any(f.input)
				actor := f.a
				var d store.CapabilityDelivery
				if stage == "target" {
					name = "set-capability-delivery-target"
					payload = SetDeliveryTarget{Environment: "staging", EndpointID: "engineering", Enabled: false, ExpectedRevision: 1}
					actor = f.admin
					if table == "capability_deliveries" {
						table = "capability_delivery_targets"
					}
				}
				if stage == "cancel" || stage == "retry" {
					d = f.send()
					name = "cancel-capability-delivery"
					payload = CancelDelivery{d.ID, d.Revision}
				}
				if stage == "retry" {
					f.e.Exec(`UPDATE capability_deliveries SET state='failed',retryable_exhaustion=true,attempts_in_cycle=6,total_attempts=6 WHERE id=$1`, d.ID)
					name = "retry-capability-delivery"
					payload = f.retryInput(d)
				}
				op := "UPDATE"
				if table == "capability_delivery_audit" || stage == "send" {
					op = "INSERT"
				}
				f.e.Break(table, op)
				if _, err := f.call(actor, name, payload, ""); err == nil {
					t.Fatal("write failure committed")
				}
				if stage == "send" {
					var count int
					_ = f.e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM capability_deliveries`).Scan(&count)
					if count != 0 {
						t.Fatal("partial item")
					}
				}
				if stage == "cancel" || stage == "retry" {
					saved, _ := f.e.Q.GetCapabilityDelivery(context.Background(), store.GetCapabilityDeliveryParams{ProjectID: d.ProjectID, ID: d.ID})
					if saved.Revision != d.Revision {
						t.Fatal("partial transition")
					}
				}
			})
		}
	}
}

func TestDeliveryCommandReadFailuresAndGates(t *testing.T) {
	f := deliverySetup(t)
	for i, needle := range []string{"FROM environments", "FROM changesets", "FROM capability_requests", "FROM capability_context_packs", "FROM capability_delivery_targets", "FROM capability_deliveries", "FROM actors", "FROM role_bindings"} {
		name := fmt.Sprintf("delivery-send-fault-%d", i)
		commandbus.Register(f.e.Bus, commandbus.Command[SendContext, DeliveryResult]{Name: name, Right: auth.CapabilityDeliver, Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p SendContext) (DeliveryResult, error) {
			return f.s.sendContext(ctx, faultTx{Tx: tx, needle: needle}, a, p)
		}})
		if _, err := f.call(f.a, name, f.input, ""); !errors.Is(err, storageFailure) {
			t.Fatal(needle, err)
		}
	}
	for i, needle := range []string{"FROM capability_delivery_targets", "FROM environments", "FROM actors", "FROM role_bindings"} {
		name := fmt.Sprintf("delivery-target-fault-%d", i)
		commandbus.Register(f.e.Bus, commandbus.Command[SetDeliveryTarget, object]{Name: name, Right: auth.ProjectAdmin, Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p SetDeliveryTarget) (object, error) {
			return f.s.setTarget(ctx, faultTx{Tx: tx, needle: needle}, a, p)
		}})
		if _, err := f.call(f.admin, name, SetDeliveryTarget{Environment: "staging", EndpointID: "engineering", Enabled: true, ExpectedRevision: 1}, ""); !errors.Is(err, storageFailure) {
			t.Fatal(needle, err)
		}
	}
	d := f.send()
	for i, needle := range []string{"FROM capability_deliveries", "FROM environments", "FROM actors", "FROM role_bindings"} {
		name := fmt.Sprintf("delivery-cancel-fault-%d", i)
		commandbus.Register(f.e.Bus, commandbus.Command[CancelDelivery, DeliveryResult]{Name: name, Right: auth.CapabilityDeliver, Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p CancelDelivery) (DeliveryResult, error) {
			return f.s.cancelDelivery(ctx, faultTx{Tx: tx, needle: needle}, a, p)
		}})
		if _, err := f.call(f.a, name, CancelDelivery{d.ID, d.Revision}, ""); !errors.Is(err, storageFailure) {
			t.Fatal(needle, err)
		}
	}
	f.e.Exec(`UPDATE capability_deliveries SET state='failed',retryable_exhaustion=true,attempts_in_cycle=6,total_attempts=6 WHERE id=$1`, d.ID)
	for i, needle := range []string{"FROM capability_deliveries", "FROM capability_requests", "FROM environments", "FROM capability_delivery_targets", "FROM actors", "FROM role_bindings", "FROM capability_context_packs", "FROM manifests"} {
		name := fmt.Sprintf("delivery-retry-fault-%d", i)
		commandbus.Register(f.e.Bus, commandbus.Command[RetryDelivery, DeliveryResult]{Name: name, Right: auth.CapabilityDeliver, Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p RetryDelivery) (DeliveryResult, error) {
			return f.s.retryDelivery(ctx, faultTx{Tx: tx, needle: needle}, a, p)
		}})
		if _, err := f.call(f.a, name, f.retryInput(d), ""); !errors.Is(err, storageFailure) {
			t.Fatal(needle, err)
		}
	}
	f.e.Exec(`UPDATE capability_delivery_targets SET revision=revision+1 WHERE project_id=$1`, d.ProjectID)
	if _, err := f.call(f.a, "retry-capability-delivery", f.retryInput(d), ""); cmstest.Code(err) != "CAPABILITY_DELIVERY_TARGET_CHANGED" {
		t.Fatal(err)
	}
	if view, err := f.s.DeliveryGet(context.Background(), f.e.Q, f.a, d.ID); err != nil || view["retryReason"] != "TARGET_CHANGED" {
		t.Fatal(view, err)
	}
}

func TestDeliveryClaimAndReceiptStorageFailuresRemainRecoverable(t *testing.T) {
	for _, table := range []string{"environments", "capability_deliveries", "capability_requests", "changesets", "capability_delivery_targets", "actors", "role_bindings", "capability_context_packs", "capability_delivery_attempts", "capability_delivery_audit"} {
		t.Run("claim/"+table, func(t *testing.T) {
			f := deliverySetup(t)
			d := f.send()
			calls := 0
			f.s.Sender = senderFunc(func(context.Context, capabilitywebhook.Endpoint, capabilitywebhook.Message) capabilitywebhook.Outcome {
				calls++
				return capabilitywebhook.Outcome{Acknowledged: true}
			})
			op := ""
			if table == "capability_delivery_attempts" || table == "capability_delivery_audit" {
				op = "INSERT"
			}
			f.e.Break(table, op)
			if err := f.s.Deliver(context.Background(), DeliveryArgs{d.ProjectID, d.ID}); err == nil || calls != 0 {
				t.Fatal("HTTP before a durable claim", err, calls)
			}
		})
	}
	for _, table := range []string{"capability_deliveries", "capability_delivery_attempts", "capability_delivery_audit"} {
		t.Run("receipt/"+table, func(t *testing.T) {
			f := deliverySetup(t)
			d := f.send()
			ctx := context.Background()
			claimed, _, ok, err := f.s.claimDelivery(ctx, DeliveryArgs{d.ProjectID, d.ID})
			if err != nil || !ok {
				t.Fatal(ok, err)
			}
			op := "UPDATE"
			if table == "capability_delivery_audit" {
				op = "INSERT"
			}
			f.e.Break(table, op)
			if err = f.s.finishDelivery(ctx, claimed, capabilitywebhook.Outcome{Acknowledged: true, Code: "STORED"}); err == nil {
				t.Fatal("partial receipt committed")
			}
			current, err := f.e.Q.GetCapabilityDelivery(ctx, store.GetCapabilityDeliveryParams{ProjectID: d.ProjectID, ID: d.ID})
			if err != nil || current.State != "sending" || current.Revision != claimed.Revision {
				t.Fatal("receipt lost its recoverable claim", err)
			}
		})
	}
	// Recovery must report storage/queue failures rather than claiming success.
	f := deliverySetup(t)
	_ = f.send()
	f.e.Break("capability_deliveries", "")
	queue, err := jobs.NewInserter(f.e.Pool)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.RecoverDeliveries(context.Background(), queue); err == nil {
		t.Fatal("recovery masked storage failure")
	}
}

func TestDeliveryFailedRetryProjectionAndPoolFailure(t *testing.T) {
	f := deliverySetup(t)
	d := f.send()
	ctx := context.Background()
	f.a.Rights[auth.ProjectAdmin] = true
	f.e.Exec(`UPDATE roles SET capabilities=array_append(capabilities,'project.admin') WHERE project_id=$1 AND id IN (SELECT role_id FROM role_bindings WHERE actor_id=$2)`, f.a.ProjectID, f.a.ID)
	f.e.Exec(`UPDATE capability_deliveries SET state='failed',retryable_exhaustion=true,attempts_in_cycle=6,total_attempts=6 WHERE id=$1`, d.ID)
	for _, needle := range []string{"FROM capability_requests", "FROM capability_delivery_targets", "FROM role_bindings", "FROM capability_context_packs"} {
		q := store.New(faultDB{DBTX: f.e.Pool, needle: needle})
		if _, err := f.s.DeliveryGet(ctx, q, f.a, d.ID); !errors.Is(err, storageFailure) {
			t.Fatal(needle, err)
		}
	}
	f.e.Pool.Close()
	if err := f.s.Deliver(ctx, DeliveryArgs{d.ProjectID, d.ID}); err == nil {
		t.Fatal("closed pool claim")
	}
	if err := f.s.finishDelivery(ctx, d, capabilitywebhook.Outcome{}); err == nil {
		t.Fatal("closed pool receipt")
	}
}
