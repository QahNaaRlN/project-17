package policydoc

import (
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"testing"
)

func TestDataDeclarationsRespectLockedTemplatesSTU234(t *testing.T) {
	base := parse(`{"root":"n_root","nodes":{"n_root":{"type":"Stack","children":["n_repeat"]},"n_repeat":{"type":"Repeat","bindings":{"items":"$data.featured.items"},"children":["n_text"],"slots":{"empty":["n_empty"]}},"n_text":{"type":"Text","locked":true,"bindings":{"text":"$item.title"}},"n_empty":{"type":"Text","when":{"op":"exists","arg":"$data.featured"},"on":{"click":{"action":"test.run","args":{"key":{"lit":"$data.other"}}}}}},"dataSources":{"featured":{"source":"test.items","params":{}}}}`)
	after := ops.Clone(base)
	obj(after["dataSources"])["featured"] = parse(`{"source":"test.other"}`)
	if r := CheckEdit(base, after, false); r.Valid {
		t.Fatal("locked template changed through source")
	}
	if r := CheckEdit(base, after, true); !r.Valid {
		t.Fatal(r)
	}
	if r := CheckEdit(base, ops.Clone(base), false); !r.Valid {
		t.Fatal("unchanged source blocked", r)
	}
	delete(obj(obj(base["nodes"])["n_text"]), "locked")
	after = ops.Clone(base)
	obj(after["dataSources"])["other"] = parse(`{"source":"test.other"}`)
	if r := CheckEdit(base, after, false); !r.Valid {
		t.Fatal("unrelated declaration blocked", r)
	}
	obj(obj(base["nodes"])["n_root"])["locked"] = true
	after = ops.Clone(base)
	delete(after, "dataSources")
	if r := CheckEdit(base, after, false); r.Valid {
		t.Fatal("locked root source removed")
	}
}
