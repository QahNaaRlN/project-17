package changes

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/schemaflow"
)

// UUID targets remain accepted for document operations; schema targets are a tagged union.
func (in OperationInput) MarshalJSON() ([]byte, error) {
	type alias OperationInput
	var target any = in.Target
	if in.SchemaTarget != nil {
		target = in.SchemaTarget
	}
	return json.Marshal(struct {
		alias
		Target any `json:"target,omitempty"`
	}{alias(in), target})
}
func (in *OperationInput) UnmarshalJSON(raw []byte) error {
	type alias OperationInput
	value := struct {
		*alias
		Target json.RawMessage `json:"target"`
	}{alias: (*alias)(in)}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	in.Target = nil
	in.SchemaTarget = nil
	if len(value.Target) == 0 || bytes.Equal(value.Target, []byte("null")) {
		return nil
	}
	if value.Target[0] == '{' {
		decoder := json.NewDecoder(bytes.NewReader(value.Target))
		decoder.DisallowUnknownFields()
		in.SchemaTarget = &schemaflow.Target{}
		return decoder.Decode(in.SchemaTarget)
	}
	return json.Unmarshal(value.Target, &in.Target)
}
func (op AppliedOperation) MarshalJSON() ([]byte, error) {
	type alias AppliedOperation
	var target any = op.Target
	if op.SchemaTarget != nil {
		target = op.SchemaTarget
	}
	return json.Marshal(struct {
		alias
		Target any `json:"target"`
	}{alias(op), target})
}
func (op *AppliedOperation) UnmarshalJSON(raw []byte) error {
	type alias AppliedOperation
	value := struct {
		*alias
		Target json.RawMessage `json:"target"`
	}{alias: (*alias)(op)}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	op.Target = uuid.Nil
	op.SchemaTarget = nil
	if len(value.Target) > 0 && value.Target[0] == '{' {
		return json.Unmarshal(value.Target, &op.SchemaTarget)
	}
	return json.Unmarshal(value.Target, &op.Target)
}
func (op Operation) MarshalJSON() ([]byte, error) {
	type alias Operation
	var target any = op.Target
	if op.SchemaTarget != nil {
		target = op.SchemaTarget
	}
	return json.Marshal(struct {
		alias
		Target any `json:"target"`
	}{alias(op), target})
}
func (s *session) applySchema(in OperationInput) (recordInput, error) {
	var p schemaflow.OperationPayload
	decoder := json.NewDecoder(bytes.NewReader(in.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return recordInput{}, commandbus.Validation(map[string]string{"payload": err.Error()})
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(in.Payload, &fields); err != nil {
		return recordInput{}, err
	}
	if _, ok := fields["version"]; !ok {
		return recordInput{}, commandbus.Validation(map[string]string{"payload.version": "нужна версия или null для удаления"})
	}
	if p.Schema != in.SchemaTarget.SchemaName {
		return recordInput{}, commandbus.Validation(map[string]string{"target": "schemaName должен совпадать с payload.schema"})
	}
	_, plan, err := schemaflow.Plan(s.ctx, s.q, s.actor.ProjectID, s.cs.ID)
	if err != nil {
		return recordInput{}, err
	}
	for _, change := range plan {
		if change.Name != p.Schema {
			continue
		}
		if (p.Version == nil) != (change.Version == nil) || (p.Version != nil && *p.Version != *change.Version) {
			break
		}
		return recordInput{schemaName: &change.Name, opType: schemaflow.Apply, payload: mustJSON(p), before: change.Before, after: change.After}, nil
	}
	return recordInput{}, commandbus.Validation(map[string]string{"payload": "schema и version должны совпадать с кандидатом"})
}

func (op *Operation) UnmarshalJSON(raw []byte) error {
	type alias Operation
	value := struct {
		*alias
		Target json.RawMessage `json:"target"`
	}{alias: (*alias)(op)}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	op.Target = uuid.Nil
	op.SchemaTarget = nil
	if len(value.Target) > 0 && value.Target[0] == '{' {
		return json.Unmarshal(value.Target, &op.SchemaTarget)
	}
	return json.Unmarshal(value.Target, &op.Target)
}
