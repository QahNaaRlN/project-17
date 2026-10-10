package ops

import (
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"testing"
)

// CHG-010/DS-023: policy edits have canonical before/after and reversible payloads.
func TestPolicyOperations(t *testing.T) {
	for _, tt := range []struct{ typ, payload string }{
		{DocumentSetPolicy, `{"policy":{"mode":"SYSTEM"}}`},
		{NodeSetZone, `{"nodeId":"n_root","zone":{"id":"main","mode":"SYSTEM"}}`},
		{NodeSetLocked, `{"nodeId":"n_root","locked":true}`},
	} {
		t.Run(tt.typ, func(t *testing.T) {
			doc := parse(t, base)
			original := mustRaw(doc)
			r, err := Apply(doc, Op{Type: tt.typ, Payload: []byte(tt.payload)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Apply(doc, r.Inverse, nil); err != nil {
				t.Fatal(err)
			}
			if string(mustRaw(doc)) != string(original) {
				t.Fatal(doc)
			}
			if right, ok := Right(tt.typ); !ok || right != auth.DesignZonesManage {
				t.Fatal(right)
			}
		})
	}
	doc := parse(t, base)
	for _, raw := range []string{`{}`, `{"policy":true}`, `{"policy":{}`, `{"policy":null,"extra":1}`} {
		if _, err := Apply(Clone(doc), Op{Type: DocumentSetPolicy, Payload: []byte(raw)}, nil); err == nil {
			t.Fatal(raw)
		}
	}
	doc["policy"] = map[string]any{"mode": "SYSTEM"}
	if _, err := Apply(doc, Op{Type: DocumentSetPolicy, Payload: []byte(`{"policy":null}`)}, nil); err != nil {
		t.Fatal(err)
	}
	if _, exists := doc["policy"]; exists {
		t.Fatal(doc)
	}
}
