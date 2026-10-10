package changes

import (
	"encoding/json"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"testing"
)

// CHG-010/FR-003: path mutations are atomic and their inverse restores missing/null containers exactly.
func FuzzContentFieldsRoundTrip(f *testing.F) {
	for _, v := range [][2]string{
		{`{}`, `{"set":{"nested.a":1,"nested.b":2}}`},
		{`{"nested":null}`, `{"set":{"nested.a":1}}`},
		{`{"nested":{"a":null}}`, `{"set":{"nested.a":"x"},"unset":["missing.child"]}`},
		{`{"nested":{"a":"old"}}`, `{"unset":["nested.a"]}`},
		{`{"a":1}`, `{"set":{"a.b":2}}`},
		{`{}`, `{"set":{"":1}}`}, {`{}`, `{"set":{"a..b":1}}`},
		{`{}`, `{"unexpected":true}`}, {`{}`, `[`},
		{`{}`, `{"set":{"x":1},"unset":["x"]}`},
		{`{}`, `{"unset":["a","a.b"]}`},
	} {
		f.Add(v[0], v[1])
	}
	f.Fuzz(func(t *testing.T, bodyJSON, payload string) {
		var body map[string]any
		if json.Unmarshal([]byte(bodyJSON), &body) != nil || body == nil {
			return
		}
		before := ops.Clone(body)
		r, err := contentFields(body, ops.Op{Type: EntitySetFields, Payload: json.RawMessage(payload)})
		if err != nil {
			if !sameJSON(body, mustJSON(before)) {
				t.Fatal("error mutated body")
			}
			return
		}
		if _, err := contentFields(body, r.Inverse); err != nil {
			t.Fatal("inverse rejected", err)
		}
		if !sameJSON(body, mustJSON(before)) {
			t.Fatal("inverse changed body", body, before)
		}
	})
}
