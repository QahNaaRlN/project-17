package ops

import (
	"encoding/json"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
)

// DocumentSetContent changes the primary source without pinning an entity version.
const DocumentSetContent = "document.setContent"

func setContent(doc map[string]any, raw json.RawMessage, _ ir.IDGenerator) (Result, error) {
	var p struct {
		Content json.RawMessage `json:"content"`
	}
	if err := decode(raw, &p); err != nil {
		return Result{}, err
	}
	if p.Content == nil {
		return Result{}, fail("PAYLOAD_INVALID", "content обязателен")
	}
	if doc["kind"] != "page" {
		return Result{}, fail("CONTENT_NOT_ALLOWED", "источник бывает только у страницы")
	}
	prev := cloneValue(doc["content"])
	if string(p.Content) == "null" {
		delete(doc, "content")
	} else {
		doc["content"] = fromJSON(p.Content)
	}
	return Result{Before: map[string]any{"content": prev}, After: map[string]any{"content": doc["content"]}, Inverse: Op{Type: DocumentSetContent, Payload: mustRaw(map[string]any{"content": prev})}}, nil
}
