package ops

import (
	"encoding/json"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
)

const (
	NodeSetZone       = "node.setZone"
	NodeSetLocked     = "node.setLocked"
	DocumentSetPolicy = "document.setPolicy"
)

func init() {
	registry[NodeSetZone] = spec{auth.DesignZonesManage, fieldSetter("zone")}
	registry[NodeSetLocked] = spec{auth.DesignZonesManage, fieldSetter("locked")}
	registry[DocumentSetPolicy] = spec{auth.DesignZonesManage, setPolicy}
}
func setPolicy(doc map[string]any, raw json.RawMessage, _ ir.IDGenerator) (Result, error) {
	var p struct {
		Policy json.RawMessage `json:"policy"`
	}
	if err := decode(raw, &p); err != nil {
		return Result{}, err
	}
	if p.Policy == nil {
		return Result{}, fail("PAYLOAD_INVALID", "policy обязателен")
	}
	prev := doc["policy"]
	if string(p.Policy) == "null" {
		delete(doc, "policy")
	} else {
		doc["policy"] = fromJSON(p.Policy)
	}
	return Result{Before: map[string]any{"policy": prev}, After: map[string]any{"policy": doc["policy"]}, Inverse: Op{Type: DocumentSetPolicy, Payload: mustRaw(map[string]any{"policy": prev})}}, nil
}
