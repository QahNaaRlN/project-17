package changes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/policydoc"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Извлечение компонента (docs/studio-component-extract.md §4): страница меняется в
// журнале операцией над ней, документ компонента создаётся в том же Change Set.

// extractApp — manifest для вывода типов: первое окружение CS (или проекта по имени)
// с активным manifest. Канонический payload хранит выведенные типы, поэтому повтор
// и rebase от выбора окружения не зависят.
func extractApp(ctx context.Context, q *store.Queries, project uuid.UUID, targets []string, environment string) (map[string]any, error) {
	envs, err := q.ListEnvironments(ctx, project)
	if err != nil {
		return nil, err
	}
	order := slices.Clone(targets)
	if environment != "" {
		order = append([]string{environment}, order...)
	}
	for _, e := range envs {
		order = append(order, e.Name)
	}
	for _, name := range order {
		for _, e := range envs {
			if e.Name != name || e.ActiveManifestID == nil {
				continue
			}
			m, err := q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: project, ID: *e.ActiveManifestID})
			if err != nil {
				return nil, err
			}
			v, err := manifest.ParseJSON(m.Body)
			if err != nil {
				return nil, err
			}
			app, _ := v.(map[string]any)
			return app, nil
		}
	}
	return nil, nil
}

func asObject(v any) map[string]any { m, _ := v.(map[string]any); return m }

// ExtractEnvironment собирает окружение извлечения: типы свойств по builtin, manifest
// и входам вложенных Composed (рабочая версия этого CS или head).
func ExtractEnvironment(ctx context.Context, q *store.Queries, project, cs uuid.UUID, targets []string, environment string) (ops.ExtractEnv, error) {
	app, err := extractApp(ctx, q, project, targets, environment)
	if err != nil {
		return ops.ExtractEnv{}, err
	}
	settings, err := q.GetProjectSettings(ctx, project)
	if err != nil {
		return ops.ExtractEnv{}, err
	}
	var projectPolicy map[string]any
	_ = json.Unmarshal(settings, &projectPolicy)
	definition := func(n map[string]any) map[string]any {
		name := fmt.Sprint(n["type"])
		if c := manifest.BuiltinContract(name); c != nil && name != "Composed" {
			return c
		}
		if d := asObject(asObject(app["primitives"])[name]); d != nil {
			return d
		}
		if d := asObject(asObject(app["components"])[name]); d != nil {
			return d
		}
		if name == "Composed" {
			id, err := uuid.Parse(fmt.Sprint(asObject(n["ref"])["component"]))
			if err != nil {
				return nil
			}
			row, err := q.ValidationComponent(ctx, store.ValidationComponentParams{ProjectID: project, ObjectID: id, ChangesetID: &cs, UseHead: true})
			if err != nil {
				return nil
			}
			body := decodeBody(row.Body)
			return map[string]any{"props": body["inputs"], "container": false}
		}
		return nil
	}
	return ops.ExtractEnv{
		PropTypes: func(n map[string]any) (map[string]any, bool) {
			d := definition(n)
			if d == nil {
				return nil, false
			}
			return asObject(d["props"]), true
		},
		Container: func(n map[string]any) bool {
			d := definition(n)
			b, _ := d["container"].(bool)
			return b
		},
		SourceTypes: ops.SourceTypes(app),
		Project:     projectPolicy,
	}, nil
}

func extractError(err error) error {
	var e *ops.Error
	if errors.As(err, &e) {
		return e
	}
	return err
}

// extract применяет component.extract: новый объект компонента и правка страницы.
func (s *session) extract(target uuid.UUID, raw json.RawMessage) (recordInput, error) {
	if s.cs.Kind != "standard" {
		return recordInput{}, commandbus.Validation(map[string]string{"changesetId": "нужен standard Change Set"})
	}
	if err := commandbus.Require(s.actor, auth.DesignCompose); err != nil {
		return recordInput{}, err
	}
	p, err := ops.DecodeExtract(raw)
	if err != nil {
		return recordInput{}, err
	}
	if p.Canonical() != s.historical {
		return recordInput{}, &ops.Error{Code: "PAYLOAD_INVALID", Message: "componentId, instanceNodeId и slotNodeIds назначает сервер"}
	}
	d, err := s.load(target)
	if err != nil {
		return recordInput{}, err
	}
	env := ops.ExtractEnv{}
	componentID := uuid.Must(uuid.NewV7())
	if p.Canonical() {
		if componentID, err = uuid.Parse(p.ComponentID); err != nil {
			return recordInput{}, &ops.Error{Code: "PAYLOAD_INVALID", Message: "componentId"}
		}
		var settings []byte
		if settings, err = s.q.GetProjectSettings(s.ctx, s.actor.ProjectID); err != nil {
			return recordInput{}, err
		}
		_ = json.Unmarshal(settings, &env.Project)
	} else {
		if env, err = ExtractEnvironment(s.ctx, s.q, s.actor.ProjectID, s.cs.ID, s.cs.Targets, ""); err != nil {
			return recordInput{}, err
		}
		env.ComponentID = componentID.String()
	}
	taken, err := s.q.ComponentTitleTaken(s.ctx, store.ComponentTitleTakenParams{ChangesetID: s.cs.ID, ProjectID: s.actor.ProjectID, ExcludeID: componentID, Title: trimmed(p.Name)})
	if err != nil {
		return recordInput{}, err
	}
	if taken {
		return recordInput{}, &ops.Error{Code: "COMPONENT_NAME_TAKEN", Message: "имя компонента занято", Diagnostics: []ir.Diagnostic{{
			Code: "COMPONENT_NAME_TAKEN", Severity: ir.SeverityError, Pointer: "/payload/name", Message: "имя компонента занято в проекте"}}}
	}
	next := ops.Clone(d.body)
	r, err := ops.Extract(next, p, env)
	if err != nil {
		return recordInput{}, extractError(err)
	}
	if res := policydoc.CheckEdit(d.body, next, s.actor.Rights.Has(auth.DesignZonesManage)); !res.Valid {
		return recordInput{}, &ops.Error{Code: "POLICY_ZONES_MANAGE_REQUIRED", Message: "Недостаточно прав на защищённую зону", Diagnostics: res.Diagnostics}
	}
	if err := s.validatePending(target, d.body, next); err != nil {
		return recordInput{}, err
	}
	instance := mustJSON(asObject(asObject(next["nodes"])[r.Payload.InstanceNodeID])["bindings"])
	if bytes.Contains(instance, []byte("$content")) {
		if err := commandbus.Require(s.actor, auth.ContentRead); err != nil {
			return recordInput{}, err
		}
		d.primaryChanged = true
	}
	if err := s.createComponent(componentID, r.Component); err != nil {
		return recordInput{}, err
	}
	d.body = next
	after := r.After
	if len(r.Warnings) > 0 {
		after["diagnostics"] = r.Warnings
	}
	return recordInput{target: target, opType: ops.ComponentExtract, payload: mustJSON(r.Payload), before: r.Before, after: after, inverse: &r.Inverse}, nil
}

func trimmed(s string) string {
	return string(bytes.TrimSpace([]byte(s)))
}

// createComponent создаёт объект (или переиспользует его при повторе) и рабочую версию.
func (s *session) createComponent(id uuid.UUID, body map[string]any) error {
	obj, err := s.q.ContentIdentity(s.ctx, store.ContentIdentityParams{ProjectID: s.actor.ProjectID, ID: id})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		kind := "component"
		if _, err := s.q.CreateObject(s.ctx, store.CreateObjectParams{ID: id, ProjectID: s.actor.ProjectID, DocKind: &kind}); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		// Повтор извлечения: объект без head, исключённый из CS отменой.
		if obj.Kind != "document" || obj.DocKind == nil || *obj.DocKind != "component" || obj.HeadVersionID != nil {
			return commandbus.NewError(http.StatusConflict, "REDO_STATE_MISMATCH", "Компонент нельзя восстановить", "Объект компонента изменился")
		}
		if _, err := s.q.GetChangesetObject(s.ctx, store.GetChangesetObjectParams{ChangesetID: s.cs.ID, ObjectID: id}); err == nil {
			return commandbus.NewError(http.StatusConflict, "REDO_STATE_MISMATCH", "Компонент нельзя восстановить", "Компонент уже есть в Change Set")
		}
	}
	_, err = s.createWorking(id, nil, nil, body)
	return err
}

// revertExtract — undo извлечения: страница восстанавливается, компонент выходит из CS.
func (s *session) revertExtract(target uuid.UUID, raw json.RawMessage) (recordInput, error) {
	// Клиент не передаёт этот тип: validateApply отклоняет его, он приходит только из истории.
	d, err := s.load(target)
	if err != nil {
		return recordInput{}, err
	}
	next := ops.Clone(d.body)
	r, p, err := ops.RevertExtract(next, raw)
	if err != nil {
		return recordInput{}, extractError(err)
	}
	componentID, err := uuid.Parse(p.ComponentID)
	if err != nil {
		return recordInput{}, &ops.Error{Code: "PAYLOAD_INVALID", Message: "componentId"}
	}
	// Компонент должен совпадать с результатом извлечения (без политики места).
	redo, err := ops.DecodeExtract(p.Redo)
	if err != nil {
		return recordInput{}, err
	}
	replay := ops.Clone(next)
	expected, err := ops.Extract(replay, redo, ops.ExtractEnv{})
	if err != nil {
		return recordInput{}, extractError(err)
	}
	comp, err := s.load(componentID)
	if err != nil {
		return recordInput{}, err
	}
	current := ops.Clone(comp.body)
	delete(current, "policy")
	delete(expected.Component, "policy")
	if !sameJSON(expected.Component, mustJSON(current)) {
		return recordInput{}, &ops.Error{Code: "UNDO_STATE_MISMATCH", Message: "компонент изменён после извлечения"}
	}
	if res := policydoc.CheckEdit(d.body, next, s.actor.Rights.Has(auth.DesignZonesManage)); !res.Valid {
		return recordInput{}, &ops.Error{Code: "POLICY_ZONES_MANAGE_REQUIRED", Message: "Недостаточно прав на защищённую зону", Diagnostics: res.Diagnostics}
	}
	if err := s.q.RemoveChangesetObject(s.ctx, store.RemoveChangesetObjectParams{ChangesetID: s.cs.ID, ObjectID: componentID}); err != nil {
		return recordInput{}, err
	}
	if err := s.q.DeleteWorkingVersion(s.ctx, comp.versionID); err != nil {
		return recordInput{}, err
	}
	delete(s.docs, componentID)
	d.body = next
	if bytes.Contains(mustJSON(p.Instance), []byte("$content")) {
		d.primaryChanged = true
	}
	return recordInput{target: target, opType: ops.ComponentRevertExtract, payload: raw, before: r.Before, after: r.After, inverse: &r.Inverse}, nil
}

// ExtractPlan — read-only план извлечения (STU-401) по рабочей версии документа в CS.
func ExtractPlan(ctx context.Context, q *store.Queries, actor auth.Actor, documentID, changesetID uuid.UUID, nodeID, environment string) (ops.ExtractPlan, error) {
	if err := commandbus.Require(actor, auth.DesignRead); err != nil {
		return ops.ExtractPlan{}, err
	}
	cs, err := q.GetChangeset(ctx, store.GetChangesetParams{ID: changesetID, ProjectID: actor.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ops.ExtractPlan{}, notFound("Change Set", changesetID)
	}
	if err != nil {
		return ops.ExtractPlan{}, err
	}
	d, err := GetDocument(ctx, q, actor.ProjectID, documentID, &changesetID)
	if err != nil {
		return ops.ExtractPlan{}, err
	}
	env, err := ExtractEnvironment(ctx, q, actor.ProjectID, changesetID, cs.Targets, environment)
	if err != nil {
		return ops.ExtractPlan{}, err
	}
	plan := ops.PlanExtract(decodeBody(d.Body), nodeID, env, actor.Rights.Has(auth.ContentRead))
	// Причины, не зависящие от документа: права и состояние Change Set.
	reason := func(code, message string) {
		plan.Blockers = append(plan.Blockers, ir.Diagnostic{Code: ir.Code(code), Severity: ir.SeverityError, Message: message})
	}
	if !actor.Rights.Has(auth.ComponentWrite) || !actor.Rights.Has(auth.DesignCompose) {
		reason("FORBIDDEN", "нужны права component.write и design.compose")
	}
	if cs.Kind != "standard" {
		reason("CHANGESET_KIND", "нужен standard Change Set")
	}
	if RequireOwner(cs, actor) != nil || cs.State != "open" {
		reason("CHANGESET_READ_ONLY", "Change Set закрыт для записи этим пользователем")
	}
	return plan, nil
}
