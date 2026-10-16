package ops

import (
	"encoding/json"
	"regexp"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
)

const DocumentSetDataSource = "document.setDataSource"

var sourceKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// setDataSource edits one declaration. Loading remains an application concern.
func setDataSource(doc map[string]any, raw json.RawMessage, _ ir.IDGenerator) (Result, error) {
	var p struct {
		Key string          `json:"key"`
		Use json.RawMessage `json:"use"`
	}
	if err := decode(raw, &p); err != nil {
		return Result{}, err
	}
	if !sourceKey.MatchString(p.Key) || p.Key == "content" || p.Key == "constructor" || p.Key == "prototype" || p.Key == "__proto__" || p.Use == nil {
		return Result{}, fail("PAYLOAD_INVALID", "нужны безопасный key и use")
	}
	if doc["kind"] != "page" {
		return Result{}, fail("DATA_SOURCE_NOT_ALLOWED", "источник доступен только странице")
	}
	uses, _ := doc["dataSources"].(map[string]any)
	previous := cloneValue(uses[p.Key])
	if string(p.Use) == "null" {
		delete(uses, p.Key)
		if len(uses) == 0 {
			delete(doc, "dataSources")
		}
	} else {
		if uses == nil {
			uses = map[string]any{}
			doc["dataSources"] = uses
		}
		uses[p.Key] = fromJSON(p.Use)
		if len(uses) > 64 {
			return Result{}, fail("LIMIT_EXCEEDED", "не больше 64 источников")
		}
	}
	return Result{
		Before:  map[string]any{"key": p.Key, "use": previous},
		After:   map[string]any{"key": p.Key, "use": uses[p.Key]},
		Inverse: Op{Type: DocumentSetDataSource, Payload: mustRaw(map[string]any{"key": p.Key, "use": previous})},
	}, nil
}
