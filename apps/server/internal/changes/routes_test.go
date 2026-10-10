package changes_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
)

func pageWithPath(path string) map[string]any {
	return map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": path, "root": map[string]any{"id": "n_root", "type": "Box"}}}
}

func setRoute(doc uuid.UUID, path any) map[string]any {
	return map[string]any{"type": "document.setRoute", "target": doc, "payload": map[string]any{"path": path}}
}

func (e *env) path(doc, cs uuid.UUID) *string {
	e.t.Helper()
	d, err := changes.GetDocument(context.Background(), e.q, e.actor.ProjectID, doc, &cs)
	if err != nil {
		e.t.Fatal(err)
	}
	return d.Path
}

// opCode — код ошибки операции (operationCode у OPERATION_INVALID) или код команды.
func opCode(err error) string {
	var cerr *commandbus.Error
	if errors.As(err, &cerr) && cerr.Code == "OPERATION_INVALID" {
		c, _ := cerr.Params["operationCode"].(string)
		return c
	}
	return code(err)
}

func TestCreateWithPathAndSetRoute(t *testing.T) {
	e := setup(t)
	cs := e.createCS("routes")
	res, err := e.apply(cs.ID, 0, pageWithPath("/collections/:slug"))
	if err != nil {
		t.Fatal(err)
	}
	doc := res.Operations[0].Target
	if p := e.path(doc, cs.ID); p == nil || *p != "/collections/:slug" {
		t.Fatalf("путь: %v", p)
	}
	if _, err := e.apply(cs.ID, 1, setRoute(doc, "/sale")); err != nil {
		t.Fatal(err)
	}
	if p := e.path(doc, cs.ID); *p != "/sale" {
		t.Errorf("после setRoute: %s", *p)
	}
	list, _ := changes.ListOperations(context.Background(), e.q, e.actor.ProjectID, cs.ID, 1)
	var before, after map[string]any
	_ = json.Unmarshal(list[0].Before, &before)
	_ = json.Unmarshal(list[0].After, &after)
	if before["path"] != "/collections/:slug" || after["path"] != "/sale" {
		t.Errorf("до/после: %s %s", list[0].Before, list[0].After)
	}
	// Отмена возвращает прежний маршрут; снятие маршрута — null.
	e.must(e.dispatch(e.actor, "undo", map[string]any{"changesetId": cs.ID, "expectedSeq": 2}, ""))
	if p := e.path(doc, cs.ID); *p != "/collections/:slug" {
		t.Errorf("после undo: %s", *p)
	}
	if _, err := e.apply(cs.ID, 3, setRoute(doc, nil)); err != nil {
		t.Fatal(err)
	}
	if p := e.path(doc, cs.ID); p != nil {
		t.Errorf("маршрут снят: %s", *p)
	}
}

func TestRouteErrors(t *testing.T) {
	e := setup(t)
	headDoc := e.commitHead(`{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Box"}}}`)
	if _, err := e.pool.Exec(context.Background(), "UPDATE objects SET head_path = '/products/:id' WHERE id = $1", headDoc); err != nil {
		t.Fatal(err)
	}
	cs := e.createCS("errors")
	res, err := e.apply(cs.ID, 0, pageWithPath("/about"))
	if err != nil {
		t.Fatal(err)
	}
	doc := res.Operations[0].Target
	for name, tc := range map[string]struct {
		op   map[string]any
		code string
	}{
		"формат":           {setRoute(doc, "/About"), "PATH_INVALID"},
		"хвостовой слэш":   {setRoute(doc, "/about/"), "PATH_INVALID"},
		"пустой параметр":  {setRoute(doc, "/a/:"), "PATH_INVALID"},
		"длина":            {setRoute(doc, "/"+strings.Repeat("a", changes.MaxPathLength)), "PATH_INVALID"},
		"та же форма":      {setRoute(doc, "/products/:slug"), "PATH_TAKEN"},
		"при создании":     {pageWithPath("/products/:sku"), "PATH_TAKEN"},
		"неизвестное поле": {map[string]any{"type": "document.setRoute", "target": doc, "payload": map[string]any{"route": "/x"}}, "PAYLOAD_INVALID"},
		"компонент":        {map[string]any{"type": "document.create", "payload": map[string]any{"kind": "component", "path": "/c", "root": map[string]any{"type": "Box"}}}, "PATH_NOT_ALLOWED"},
		"нет документа":    {setRoute(uuid.New(), "/x"), "NOT_FOUND"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := e.apply(cs.ID, 1, tc.op); opCode(err) != tc.code {
				t.Errorf("ожидался %s: %v", tc.code, err)
			}
		})
	}
	// Корень, собственный маршрут объекта и маршрут предельной длины допустимы.
	longest := "/" + strings.Repeat("a", changes.MaxPathLength-1)
	if _, err := e.apply(cs.ID, 1, setRoute(doc, longest), setRoute(doc, "/"), setRoute(headDoc, "/products/:id")); err != nil {
		t.Errorf("%v", err)
	}
}

func TestRebaseRoutes(t *testing.T) {
	e := setup(t)
	base := `{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Box"}}}`
	ctx := context.Background()
	newHead := func(doc uuid.UUID, path string) {
		e.moveHead(doc, base)
		if _, err := e.pool.Exec(ctx, `UPDATE object_versions SET path = $2 WHERE id = (SELECT head_version_id FROM objects WHERE id = $1)`, doc, path); err != nil {
			t.Fatal(err)
		}
	}
	// Маршрут на базе не менялся — операция переигрывается.
	moved, changed, same := e.commitHead(base), e.commitHead(base), e.commitHead(base)
	cs := e.createCS("b")
	if _, err := e.apply(cs.ID, 0, setRoute(moved, "/mine"), setRoute(changed, "/mine-2"), setRoute(same, "/same")); err != nil {
		t.Fatal(err)
	}
	e.moveHead(moved, base)
	newHead(changed, "/theirs")
	newHead(same, "/same")
	res := e.rebase(cs.ID, nil)
	if len(res.Conflicts) != 1 || res.Conflicts[0].Target != changed || res.Conflicts[0].Code != "BEFORE_MISMATCH" {
		t.Fatalf("%+v", res)
	}
	if res.Conflicts[0].Current.(map[string]any)["path"] != "/theirs" {
		t.Errorf("текущий маршрут в конфликте: %+v", res.Conflicts[0])
	}
	res = e.rebase(cs.ID, map[uuid.UUID]string{res.Conflicts[0].OperationID: changes.ResolutionMine})
	if len(res.Conflicts) != 0 || len(res.Rebased) != 3 {
		t.Fatalf("%+v", res)
	}
	for doc, want := range map[uuid.UUID]string{moved: "/mine", changed: "/mine-2", same: "/same"} {
		if p := e.path(doc, cs.ID); p == nil || *p != want {
			t.Errorf("%s: %v, ожидался %s", doc, p, want)
		}
	}
	// На новой базе маршрут снят, а операция рассчитывала на прежний — конфликт.
	cs3 := e.createCS("d")
	if _, err := e.apply(cs3.ID, 0, setRoute(same, "/elsewhere")); err != nil {
		t.Fatal(err)
	}
	e.moveHead(same, base)
	if res := e.rebase(cs3.ID, nil); len(res.Conflicts) != 1 || res.Conflicts[0].Current.(map[string]any)["path"] != nil {
		t.Errorf("маршрут снят на базе: %+v", res)
	}
	// theirs исключает смену маршрута: остаётся маршрут новой базы.
	cs2 := e.createCS("c")
	if _, err := e.apply(cs2.ID, 0, setRoute(changed, "/other"), rename(changed, "x")); err != nil {
		t.Fatal(err)
	}
	newHead(changed, "/again")
	res = e.rebase(cs2.ID, nil)
	res = e.rebase(cs2.ID, map[uuid.UUID]string{res.Conflicts[0].OperationID: changes.ResolutionTheirs})
	if p := e.path(changed, cs2.ID); len(res.Conflicts) != 0 || p == nil || *p != "/again" {
		t.Errorf("%+v %v", res, p)
	}
}
