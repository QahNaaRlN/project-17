package changes

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
)

const ComponentCertify = "component.certify"

func certification(raw json.RawMessage) (bool, error) {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || len(m) != 1 {
		return false, &ops.Error{Code: "PAYLOAD_INVALID", Message: "certified: bool обязателен"}
	}
	b, ok := m["certified"].(bool)
	if !ok {
		return false, &ops.Error{Code: "PAYLOAD_INVALID", Message: "certified: bool обязателен"}
	}
	return b, nil
}
func certificateResult(before, after bool, body map[string]any) ops.Result {
	_, hash := encodeBody(body)
	return ops.Result{Before: map[string]any{"certified": before, "bodyHash": fmt.Sprintf("%x", hash)}, After: map[string]any{"certified": after}, Inverse: ops.Op{Type: ComponentCertify, Payload: mustJSON(map[string]any{"certified": before})}}
}
func (s *session) certify(target uuid.UUID, raw json.RawMessage) (recordInput, error) {
	d, err := s.load(target)
	if err != nil {
		return recordInput{}, err
	}
	if d.body["kind"] != "component" {
		return recordInput{}, &ops.Error{Code: "COMPONENT_REQUIRED", Message: "Сертифицировать можно только компонент"}
	}
	value, err := certification(raw)
	if err != nil {
		return recordInput{}, err
	}
	r := certificateResult(d.certified, value, d.body)
	d.certified = value
	return recordInput{target: target, opType: ComponentCertify, payload: raw, before: r.Before, after: r.After, inverse: &r.Inverse}, nil
}
