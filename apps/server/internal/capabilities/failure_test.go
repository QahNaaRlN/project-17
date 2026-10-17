package capabilities

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

var storageFailure = errors.New("injected storage failure")

type faultDB struct {
	store.DBTX
	needle string
}
type errorRow struct{}
type faultTx struct {
	pgx.Tx
	needle string
}

func (d faultTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, d.needle) {
		return errorRow{}
	}
	return d.Tx.QueryRow(ctx, sql, args...)
}
func (d faultTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, d.needle) {
		return nil, storageFailure
	}
	return d.Tx.Query(ctx, sql, args...)
}
func (d faultTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, d.needle) {
		return pgconn.CommandTag{}, storageFailure
	}
	return d.Tx.Exec(ctx, sql, args...)
}

func (errorRow) Scan(...any) error { return storageFailure }
func (d faultDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, d.needle) {
		return errorRow{}
	}
	return d.DBTX.QueryRow(ctx, sql, args...)
}
func (d faultDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, d.needle) {
		return nil, storageFailure
	}
	return d.DBTX.Query(ctx, sql, args...)
}
func (d faultDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, d.needle) {
		return pgconn.CommandTag{}, storageFailure
	}
	return d.DBTX.Exec(ctx, sql, args...)
}

func TestQueryStorageFailuresNeverBecomeEmptyOrStaleSuccess(t *testing.T) {
	f := setup(t)
	r := f.submit()
	ctx := context.Background()
	for _, needle := range []string{"FROM capability_requests", "FROM capability_context_packs", "FROM capability_request_audit", "FROM changesets", "FROM object_versions", "FROM environments"} {
		q := store.New(faultDB{DBTX: f.e.Pool, needle: needle})
		if _, err := Get(ctx, q, f.a, r.ID); !errors.Is(err, storageFailure) {
			t.Fatalf("Get %s: %v", needle, err)
		}
	}
	for _, needle := range []string{"FROM capability_requests", "FROM changesets", "FROM environments", "FROM manifests", "FROM projects", "FROM code_index_uploads"} {
		q := store.New(faultDB{DBTX: f.e.Pool, needle: needle})
		if _, err := Preview(ctx, q, f.a, r.ID); !errors.Is(err, storageFailure) {
			t.Fatalf("Preview %s: %v", needle, err)
		}
	}
	q := store.New(faultDB{DBTX: f.e.Pool, needle: "FROM capability_context_packs"})
	if _, err := Download(ctx, q, f.a, r.ID, 2); !errors.Is(err, storageFailure) {
		t.Fatal(err)
	}
	q = store.New(faultDB{DBTX: f.e.Pool, needle: "FROM capability_requests"})
	if _, err := List(ctx, q, f.a, Filter{}); !errors.Is(err, storageFailure) {
		t.Fatal(err)
	}
	if _, err := List(ctx, f.e.Q, auth.Actor{Kind: auth.ActorAgent}, Filter{}); err == nil {
		t.Fatal("agent queue exposed")
	}
}

func TestCommandValidationAndPermissionFailures(t *testing.T) {
	f := setup(t)
	r := f.create()
	ctx := context.Background()
	for _, p := range []Action{{}, {ID: r.ID, ExpectedRevision: 1, ExpectedSeq: 1, ExpectedManifestHash: f.hash, Handlers: raw([]any{})}} {
		_, err := call(f.e, f.a, "submit-capability-request", p, "")
		expectCode(t, err, "CAPABILITY_REQUEST_INVALID")
	}
	_, err := call(f.e, f.a, "refresh-capability-context", Action{ID: r.ID, ExpectedRevision: 1, ExpectedSeq: 1, ExpectedManifestHash: f.hash, ConfirmReplaceExisting: true}, "")
	expectCode(t, err, "CAPABILITY_REQUEST_INVALID")
	for _, p := range []Update{{}, {ID: r.ID, ExpectedRevision: 1, Transition: "accepted", PRURL: "http://example.test"}, {ID: r.ID, ExpectedRevision: 1, Transition: "rejected", Reason: strings.Repeat("x", 2001)}} {
		_, err = call(f.e, f.a, "update-capability-request", p, "")
		expectCode(t, err, "CAPABILITY_REQUEST_INVALID")
	}
	for _, a := range []auth.Actor{f.e.Admin, {ID: f.a.ID, ProjectID: f.a.ProjectID, Kind: auth.ActorHuman, Rights: auth.RightSet{auth.CapabilityRequest: true}}} {
		_, err = call(f.e, a, "create-capability-request", Create{}, "")
		expectCode(t, err, "FORBIDDEN")
	}
	_, err = call(f.e, f.engineer, "submit-capability-request", Action{}, "")
	expectCode(t, err, "FORBIDDEN")
	none := f.a
	none.Rights = auth.RightSet{}
	_, err = call(f.e, none, "update-capability-request", Update{ID: r.ID, ExpectedRevision: 1, Transition: "cancelled"}, "")
	expectCode(t, err, "FORBIDDEN")
	if _, err = create(ctx, nil, f.a, Create{}); err == nil {
		t.Fatal("missing reason accepted")
	}
	if _, err = update(ctx, nil, f.a, Update{}); err == nil {
		t.Fatal("missing reason accepted")
	}
	if _, err = act(ctx, nil, f.a, Action{}, "submit-capability-request"); err == nil {
		t.Fatal("missing reason accepted")
	}
	request := commandbus.Request{Name: "submit-capability-request", IdempotencyKey: uuid.NewString(), Reason: "", Payload: raw(Action{ID: r.ID, ExpectedRevision: 1, ExpectedSeq: 1, ExpectedManifestHash: f.hash})}
	if _, err = f.e.Bus.Dispatch(ctx, f.a, request); err == nil {
		t.Fatal("reason missing")
	}
	_, err = call(f.e, f.a, "update-capability-request", Update{ID: r.ID, ExpectedRevision: 1, Transition: "accepted"}, "")
	expectCode(t, err, "FORBIDDEN")
	if descriptionText("bad\x00value") {
		t.Fatal("control accepted")
	}
	if !descriptionText("Paragraph one\nParagraph two") {
		t.Fatal("paragraphs rejected")
	}
}

func TestNestedCommandStorageFailuresPreserveTheTransaction(t *testing.T) {
	f := setup(t)
	p := Create{Draft: Draft{Title: "New", Description: "A task", Acceptance: []string{"works"}, ProposedContract: raw(object{"args": object{}})}, Environment: "staging", ExpectedManifestHash: f.hash, ChangesetID: f.cs, ExpectedSeq: 1, DocumentID: f.doc, NodeID: "n_root", Event: "click", Key: "store.favorite"}
	for _, needle := range []string{"LockManifestEnvironment", "LockChangeset", "GetChangeset", "GetEnvironmentByName", "GetManifestByID", "GetWorkingDocument", "GetVersion", "GetProjectSettings", "InsertCapabilityRequest", "InsertCapabilityAudit"} {
		name := "test-create-" + needle
		commandbus.Register(f.e.Bus, commandbus.Command[Create, Result]{Name: name, Authorize: func(a auth.Actor, _ Create) error { return author(a) }, Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p Create) (Result, error) {
			return create(ctx, faultTx{Tx: tx, needle: needle}, a, p)
		}})
		if _, err := call(f.e, f.a, name, p, ""); !errors.Is(err, storageFailure) {
			t.Fatalf("create %s: %v", needle, err)
		}
	}
	r := f.create()
	for _, needle := range []string{"GetCapabilityRequest", "LockManifestEnvironment", "LockChangeset", "GetChangeset", "LockCapabilityRequest", "UpdateCapabilityRequest", "InsertCapabilityAudit"} {
		name := "test-update-" + needle
		commandbus.Register(f.e.Bus, commandbus.Command[Update, Result]{Name: name, Right: auth.CapabilityRequest, Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p Update) (Result, error) {
			return update(ctx, faultTx{Tx: tx, needle: needle}, a, p)
		}})
		d := p.Draft
		if _, err := call(f.e, f.a, name, Update{ID: r.ID, ExpectedRevision: 1, Draft: &d}, ""); !errors.Is(err, storageFailure) {
			t.Fatalf("update %s: %v", needle, err)
		}
	}
	for _, needle := range []string{"GetCapabilityRequest", "UpdateCapabilityRequest", "GetCodeIndexUpload", "InsertCapabilityPack", "InsertCapabilityAudit"} {
		name := "test-submit-" + needle
		commandbus.Register(f.e.Bus, commandbus.Command[Action, Result]{Name: name, Right: auth.CapabilityRequest, Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p Action) (Result, error) {
			return act(ctx, faultTx{Tx: tx, needle: needle}, a, p, "submit-capability-request")
		}})
		if _, err := call(f.e, f.a, name, Action{ID: r.ID, ExpectedRevision: 1, ExpectedSeq: 1, ExpectedManifestHash: f.hash}, ""); !errors.Is(err, storageFailure) {
			t.Fatalf("submit %s: %v", needle, err)
		}
	}
	saved, err := f.e.Q.GetCapabilityRequest(context.Background(), store.GetCapabilityRequestParams{ProjectID: f.a.ProjectID, ID: r.ID})
	if err != nil || saved.Revision != 1 || saved.Status != "draft" {
		t.Fatal(saved, err)
	}
}
