package validation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"pgregory.net/rapid"
)

const minimalApp = `{"manifestVersion":"1.0","app":{"id":"test","version":"1.0.0","framework":"react"},"irVersions":["1.0"],"breakpoints":{},"tokens":{}}`

func testContext() *Context {
	var app any
	_ = json.Unmarshal([]byte(minimalApp), &app)
	return &Context{q: store.New(missingDB{}), app: app, Overrides: map[uuid.UUID]Document{}}
}
func rawDoc(id uuid.UUID, kind string, nodes map[string]any) Document {
	raw, _ := json.Marshal(map[string]any{"irVersion": "1.0", "kind": kind, "root": "n_root", "nodes": nodes})
	return Document{ObjectID: id, VersionID: uuid.New(), Body: raw}
}
func composed(id uuid.UUID) map[string]any {
	return map[string]any{"id": "n_root", "type": "Composed", "ref": map[string]any{"component": id.String(), "version": "live"}}
}
func includes(r ir.Result, code string) bool {
	for _, d := range r.Diagnostics {
		if string(d.Code) == code {
			return true
		}
	}
	return false
}

// The store boundary returns an absent component, just as a scoped database read does.
type missingDB struct{}
type missingRow struct{}

func (missingRow) Scan(...any) error                               { return pgx.ErrNoRows }
func (missingDB) QueryRow(context.Context, string, ...any) pgx.Row { return missingRow{} }
func (missingDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unavailable database")
}
func (missingDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unavailable database")
}

// IR-062/NFR: expansion is bounded, and malformed roots never escape as panics.
func TestExpansionLimitsAndInvalidBodies(t *testing.T) {
	c := testContext()
	ids := make([]uuid.UUID, 131)
	for i := range ids {
		ids[i] = uuid.New()
	}
	for i := len(ids) - 1; i >= 0; i-- {
		node := map[string]any{"id": "n_root", "type": "Box"}
		if i+1 < len(ids) {
			node = composed(ids[i+1])
		}
		c.Overrides[ids[i]] = rawDoc(ids[i], "component", map[string]any{"n_root": node})
	}
	r, err := c.Validate(context.Background(), c.Overrides[ids[0]])
	if err != nil || !includes(r, "LIMIT_EXCEEDED") {
		t.Fatal(r, err)
	}
	// Invalid nested bodies and a page masquerading as a component fail closed.
	child := uuid.New()
	root := rawDoc(uuid.New(), "page", map[string]any{"n_root": composed(child)})
	c.Overrides[child] = Document{ObjectID: child, Body: []byte(`bad json`)}
	if _, err := c.Validate(context.Background(), root); err == nil {
		t.Fatal("invalid dependency body")
	}
	c.Overrides[child] = rawDoc(child, "page", map[string]any{"n_root": map[string]any{"id": "n_root", "type": "Box"}})
	if r, err := c.Validate(context.Background(), root); err != nil || !includes(r, "COMPONENT_NOT_FOUND") {
		t.Fatal(r, err)
	}
	if r, err := c.Validate(context.Background(), Document{Body: []byte(`[]`)}); err != nil || !includes(r, "IR_SCHEMA_VIOLATION") {
		t.Fatal(r, err)
	}
	for _, ref := range []map[string]any{{"component": "bad"}, {"component": child.String(), "version": float64(-1)}, {"component": child.String(), "version": float64(1.5)}, {"component": child.String(), "version": float64(2147483648)}} {
		if _, ok, err := c.resolve(context.Background(), ref); ok || err != nil {
			t.Fatal(ref, ok, err)
		}
	}
	a, b := uuid.New(), uuid.New()
	if !sameID(&a, &a) || sameID(&a, &b) || sameID(nil, &a) {
		t.Fatal("manifest identity")
	}
}

// IR-062: an indirect A -> B -> A cycle is reported at the referencing node.
func TestIndirectCycle(t *testing.T) {
	c := testContext()
	a, b := uuid.New(), uuid.New()
	c.Overrides[a] = rawDoc(a, "component", map[string]any{"n_root": composed(b)})
	c.Overrides[b] = rawDoc(b, "component", map[string]any{"n_root": composed(a)})
	r, err := c.Validate(context.Background(), c.Overrides[a])
	if err != nil || !includes(r, "COMPONENT_CYCLE") || r.Diagnostics[0].Pointer != "/nodes/n_root/ref" {
		t.Fatal(r, err)
	}
}

func TestArbitraryBodiesAreDeterministic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		raw := rapid.SliceOfN(rapid.Byte(), 0, 2048).Draw(t, "body")
		c := testContext()
		d := Document{Body: raw}
		a, err := c.Validate(context.Background(), d)
		if err != nil {
			t.Fatal(err)
		}
		b, err := c.Validate(context.Background(), d)
		if err != nil || !reflect.DeepEqual(a, b) {
			t.Fatal(a, b, err)
		}
	})
}
func FuzzValidate(f *testing.F) {
	f.Add([]byte(`null`))
	f.Add([]byte(`{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Box"}}}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = testContext().Validate(context.Background(), Document{Body: raw})
	})
}
