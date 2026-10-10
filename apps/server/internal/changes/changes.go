// Package changes — Change Set, рабочие версии документов и журнал операций
// (docs/spec/06-changes-publishing.md §1–3).
//
// Изменения документов накапливаются в рабочих версиях открытого Change Set; head и
// опубликованные версии не меняются до слияния (FR-030). Каждая операция записывается
// неизменяемой записью с актором, причиной, данными «до» и «после» и обратной операцией (FR-003).
package changes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// DocumentCreate — операция создания документа IR в Change Set.
const DocumentCreate = "document.create"

// MaxOperationsPerCommand — предел операций в одной команде apply-operations (08-api.md §3.2).
const MaxOperationsPerCommand = 200

// writeRights — права, любое из которых позволяет создать Change Set (08-api.md §3.2).
var writeRights = []auth.Right{
	auth.ContentWrite, auth.DesignCompose, auth.ComponentWrite, auth.BehaviorUse,
	auth.SchemaPropose, auth.DesignZonesManage,
}

// Changeset — Change Set в ответах API.
type Changeset struct {
	ID           uuid.UUID `json:"id"`
	Title        string    `json:"title"`
	Description  *string   `json:"description"`
	OwnerID      uuid.UUID `json:"ownerId"`
	State        string    `json:"state"`
	Seq          int32     `json:"seq"`
	HasConflicts bool      `json:"hasConflicts"` // после rebase остались неразрешённые конфликты (§4.2)
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ToChangeset — представление Change Set для ответов API.
func ToChangeset(c store.Changeset) Changeset { return toChangeset(c) }

func toChangeset(c store.Changeset) Changeset {
	return Changeset{ID: c.ID, Title: c.Title, Description: c.Description, OwnerID: c.OwnerID,
		State: c.State, Seq: c.Seq, HasConflicts: c.HasConflicts, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

// Register регистрирует команды модуля.
func Register(bus *commandbus.Bus) {
	commandbus.Register(bus, commandbus.Command[createPayload, Changeset]{
		Name:      "create-changeset",
		Authorize: func(actor auth.Actor, _ createPayload) error { return requireAnyWrite(actor) },
		Validate:  validateCreate,
		Handle:    handleCreate,
	})
	commandbus.Register(bus, commandbus.Command[applyPayload, ApplyResult]{
		Name:      "apply-operations",
		Authorize: authorizeApply,
		Validate:  validateApply,
		Handle:    handleApply,
	})
	commandbus.Register(bus, commandbus.Command[seqPayload, ApplyResult]{
		Name:      "undo",
		Authorize: func(actor auth.Actor, _ seqPayload) error { return requireAnyWrite(actor) },
		Handle:    handleUndo,
	})
	commandbus.Register(bus, commandbus.Command[changesetRef, Changeset]{
		Name:      "abandon-changeset",
		Authorize: func(actor auth.Actor, _ changesetRef) error { return requireAnyWrite(actor) },
		Handle:    handleAbandon,
	})
}

func requireAnyWrite(actor auth.Actor) error {
	for _, r := range writeRights {
		if actor.Rights.Has(r) {
			return nil
		}
	}
	return commandbus.NewError(http.StatusForbidden, "FORBIDDEN", "Недостаточно прав",
		"Нужно хотя бы одно право записи (content.write, design.compose …)")
}

// --- create-changeset --------------------------------------------------------------

type createPayload struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
}

func validateCreate(p createPayload) error {
	if l := len([]rune(p.Title)); l < 1 || l > 200 {
		return commandbus.Validation(map[string]string{"title": "1…200 символов"})
	}
	return nil
}

func handleCreate(ctx context.Context, tx pgx.Tx, actor auth.Actor, p createPayload) (Changeset, error) {
	cs, err := store.New(tx).CreateChangeset(ctx, store.CreateChangesetParams{
		ID: uuid.Must(uuid.NewV7()), ProjectID: actor.ProjectID, Title: p.Title, Description: p.Description, OwnerID: actor.ID,
	})
	if err != nil {
		return Changeset{}, err
	}
	return toChangeset(cs), nil
}

// --- общие проверки -----------------------------------------------------------------

type changesetRef struct {
	ChangesetID uuid.UUID `json:"changesetId"`
}

// Lock блокирует Change Set проекта до конца транзакции.
func Lock(ctx context.Context, q *store.Queries, projectID, id uuid.UUID) (store.Changeset, error) {
	cs, err := q.LockChangeset(ctx, store.LockChangesetParams{ID: id, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return cs, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Change Set не найден",
			fmt.Sprintf("Change Set %s не найден в проекте", id))
	}
	return cs, err
}

// RequireOwner — изменять Change Set может только его владелец.
func RequireOwner(cs store.Changeset, actor auth.Actor) error {
	if cs.OwnerID != actor.ID {
		return commandbus.NewError(http.StatusForbidden, "CHANGESET_NOT_OWNER", "Чужой Change Set",
			"Изменять Change Set может только его владелец")
	}
	return nil
}

// RequireState — Change Set должен быть в одном из состояний.
func RequireState(cs store.Changeset, action string, states ...string) error {
	for _, s := range states {
		if cs.State == s {
			return nil
		}
	}
	return commandbus.NewError(http.StatusConflict, "CHANGESET_STATE_INVALID", "Недопустимое состояние Change Set",
		fmt.Sprintf("%s: допустимо в состояниях %v; текущее состояние %s", action, states, cs.State)).
		WithParams(map[string]any{"state": cs.State, "allowed": states})
}

// lockOpen блокирует Change Set и проверяет, что он открыт и принадлежит актору (CHG-030).
func lockOpen(ctx context.Context, q *store.Queries, actor auth.Actor, id uuid.UUID) (store.Changeset, error) {
	cs, err := Lock(ctx, q, actor.ProjectID, id)
	if err != nil {
		return cs, err
	}
	if err := RequireOwner(cs, actor); err != nil {
		return cs, err
	}
	return cs, RequireState(cs, "Операции", "open")
}

// OperationRight — право, необходимое для операции данного типа.
func OperationRight(opType string) (auth.Right, bool) {
	if opType == DocumentCreate || opType == DocumentSetRoute {
		return auth.DesignCompose, true
	}
	return ops.Right(opType)
}

func checkSeq(cs store.Changeset, expected int32) error {
	if cs.Seq != expected {
		return commandbus.NewError(http.StatusConflict, "CHANGESET_SEQ_CONFLICT", "Change Set изменился",
			fmt.Sprintf("Ожидался seq %d, текущий %d; перечитайте Change Set (CHG-031)", expected, cs.Seq)).
			WithParams(map[string]any{"expectedSeq": expected, "seq": cs.Seq})
	}
	return nil
}

// --- apply-operations ---------------------------------------------------------------

// OperationInput — операция в команде apply-operations.
type OperationInput struct {
	ClientOpID *string         `json:"clientOpId"`
	Type       string          `json:"type"`
	Target     *uuid.UUID      `json:"target"` // документ; для document.create не задаётся
	Payload    json.RawMessage `json:"payload"`
}

type applyPayload struct {
	ChangesetID uuid.UUID        `json:"changesetId"`
	ExpectedSeq int32            `json:"expectedSeq"`
	Operations  []OperationInput `json:"operations"`
}

// AppliedOperation — запись о применённой операции в ответе.
type AppliedOperation struct {
	ID         uuid.UUID `json:"id"`
	Seq        int32     `json:"seq"`
	Type       string    `json:"type"`
	Target     uuid.UUID `json:"target"`
	ClientOpID *string   `json:"clientOpId,omitempty"`
	After      any       `json:"after"`
}

// ApplyResult — ответ apply-operations и undo.
type ApplyResult struct {
	Changeset  Changeset          `json:"changeset"`
	Operations []AppliedOperation `json:"operations"`
}

func authorizeApply(actor auth.Actor, p applyPayload) error {
	for _, op := range p.Operations {
		right, ok := OperationRight(op.Type)
		if !ok {
			continue // неизвестный тип отклонит Validate
		}
		if err := commandbus.Require(actor, right); err != nil {
			return err
		}
	}
	return nil
}

func validateApply(p applyPayload) error {
	if n := len(p.Operations); n == 0 || n > MaxOperationsPerCommand {
		return commandbus.Validation(map[string]string{"operations": fmt.Sprintf("1…%d операций", MaxOperationsPerCommand)})
	}
	for i, op := range p.Operations {
		field := fmt.Sprintf("operations[%d]", i)
		if _, ok := OperationRight(op.Type); !ok {
			return commandbus.Validation(map[string]string{field + ".type": "неизвестный тип операции " + op.Type})
		}
		if (op.Type == DocumentCreate) != (op.Target == nil) {
			return commandbus.Validation(map[string]string{field + ".target": "target обязателен для всех операций, кроме document.create, и запрещён для неё"})
		}
		if len(op.Payload) == 0 {
			return commandbus.Validation(map[string]string{field + ".payload": "обязательно"})
		}
	}
	return nil
}

// session — рабочие версии документов, затронутых командой, в пределах одной транзакции.
type session struct {
	ctx   context.Context
	q     *store.Queries
	actor auth.Actor
	cs    store.Changeset
	docs  map[uuid.UUID]*workingDoc
}

type workingDoc struct {
	versionID uuid.UUID
	path      *string // маршрут страницы
	body      map[string]any
}

// load возвращает рабочую версию документа в Change Set, при первом обращении копируя head.
func (s *session) load(objectID uuid.UUID) (*workingDoc, error) {
	if d, ok := s.docs[objectID]; ok {
		return d, nil
	}
	co, err := s.q.GetChangesetObject(s.ctx, store.GetChangesetObjectParams{ChangesetID: s.cs.ID, ObjectID: objectID})
	if err == nil {
		d := &workingDoc{versionID: co.WorkingVersionID, path: co.WorkingPath, body: decodeBody(co.WorkingBody)}
		s.docs[objectID] = d
		return d, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	obj, err := s.q.GetObject(s.ctx, store.GetObjectParams{ID: objectID, ProjectID: s.actor.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && obj.HeadVersionID == nil) {
		// Объект без head существует только в чужом открытом Change Set — для этого он не виден.
		return nil, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Документ не найден",
			fmt.Sprintf("Документ %s не найден", objectID))
	}
	if err != nil {
		return nil, err
	}
	head, err := s.q.GetVersion(s.ctx, *obj.HeadVersionID)
	if err != nil {
		return nil, err
	}
	d, err := s.createWorking(objectID, obj.HeadVersionID, head.Path, decodeBody(head.Body))
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (s *session) createWorking(objectID uuid.UUID, base *uuid.UUID, path *string, body map[string]any) (*workingDoc, error) {
	raw, hash := encodeBody(body)
	irVersion := "1.0"
	v, err := s.q.CreateWorkingVersion(s.ctx, store.CreateWorkingVersionParams{
		ID: uuid.Must(uuid.NewV7()), ProjectID: s.actor.ProjectID, ObjectID: objectID, ChangesetID: &s.cs.ID,
		ParentVersionID: base, IrVersion: &irVersion, Path: path, Body: raw, BodyHash: hash, CreatedBy: s.actor.ID,
	})
	if err != nil {
		return nil, err
	}
	if err := s.q.AddChangesetObject(s.ctx, store.AddChangesetObjectParams{
		ChangesetID: s.cs.ID, ObjectID: objectID, BaseVersionID: base, WorkingVersionID: v.ID,
	}); err != nil {
		return nil, err
	}
	d := &workingDoc{versionID: v.ID, path: path, body: body}
	s.docs[objectID] = d
	return d, nil
}

// flush сохраняет рабочие версии всех документов, затронутых командой.
func (s *session) flush() error {
	for _, d := range s.docs {
		raw, hash := encodeBody(d.body)
		if err := s.q.UpdateWorkingVersion(s.ctx, store.UpdateWorkingVersionParams{ID: d.versionID, Path: d.path, Body: raw, BodyHash: hash}); err != nil {
			return err
		}
	}
	return nil
}

type recordInput struct {
	target     uuid.UUID
	opType     string
	payload    json.RawMessage
	before     any
	after      any
	inverse    *ops.Op
	clientOpID *string
	undoOf     *uuid.UUID
}

func (s *session) record(in recordInput) (AppliedOperation, error) {
	s.cs.Seq++
	reason := commandbus.ReasonFrom(s.ctx)
	var reasonPtr *string
	if reason != "" {
		reasonPtr = &reason
	}
	var inverse []byte
	if in.inverse != nil {
		inverse = mustJSON(in.inverse)
	}
	row, err := s.q.InsertOperation(s.ctx, store.InsertOperationParams{
		ID: uuid.Must(uuid.NewV7()), ProjectID: s.actor.ProjectID, ChangesetID: s.cs.ID, Seq: s.cs.Seq,
		ActorID: s.actor.ID, Source: "api", TargetObjectID: in.target, Type: in.opType, Payload: in.payload,
		Before: nullableJSON(in.before), After: nullableJSON(in.after), Inverse: inverse,
		Reason: reasonPtr, ClientOpID: in.clientOpID, UndoOf: in.undoOf,
	})
	if postgres.IsUniqueViolation(err) && in.clientOpID != nil {
		return AppliedOperation{}, commandbus.NewError(http.StatusConflict, "CLIENT_OP_ID_REUSED", "Повтор clientOpId",
			fmt.Sprintf("Операция с clientOpId %q уже есть в Change Set", *in.clientOpID))
	}
	if err != nil {
		return AppliedOperation{}, err
	}
	return AppliedOperation{ID: row.ID, Seq: row.Seq, Type: row.Type, Target: in.target, ClientOpID: in.clientOpID, After: in.after}, nil
}

func handleApply(ctx context.Context, tx pgx.Tx, actor auth.Actor, p applyPayload) (ApplyResult, error) {
	q := store.New(tx)
	cs, err := lockOpen(ctx, q, actor, p.ChangesetID)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := checkSeq(cs, p.ExpectedSeq); err != nil {
		return ApplyResult{}, err
	}
	s := &session{ctx: ctx, q: q, actor: actor, cs: cs, docs: map[uuid.UUID]*workingDoc{}}
	applied := make([]AppliedOperation, 0, len(p.Operations))

	for i, in := range p.Operations {
		var rec recordInput
		var err error
		if in.Type == DocumentCreate {
			rec, err = s.createDocument(in)
		} else {
			rec, err = s.applyToDocument(*in.Target, ops.Op{Type: in.Type, Payload: in.Payload})
		}
		if err != nil {
			return ApplyResult{}, operationError(i, err)
		}
		rec.clientOpID = in.ClientOpID
		op, err := s.record(rec)
		if err != nil {
			return ApplyResult{}, operationError(i, err)
		}
		applied = append(applied, op)
	}
	return s.finish(applied)
}

func (s *session) finish(applied []AppliedOperation) (ApplyResult, error) {
	if err := s.flush(); err != nil {
		return ApplyResult{}, err
	}
	if err := s.q.SetChangesetSeq(s.ctx, store.SetChangesetSeqParams{ID: s.cs.ID, Seq: s.cs.Seq}); err != nil {
		return ApplyResult{}, err
	}
	s.cs.UpdatedAt = time.Now()
	return ApplyResult{Changeset: toChangeset(s.cs), Operations: applied}, nil
}

func (s *session) applyToDocument(target uuid.UUID, op ops.Op) (recordInput, error) {
	if op.Type == DocumentSetRoute {
		return s.setRoute(target, op.Payload)
	}
	d, err := s.load(target)
	if err != nil {
		return recordInput{}, err
	}
	next := ops.Clone(d.body)
	res, err := ops.Apply(next, op, nil)
	if err != nil {
		return recordInput{}, err
	}
	d.body = next
	payload := op.Payload
	if res.Payload != nil {
		payload = res.Payload
	}
	return recordInput{target: target, opType: op.Type, payload: payload, before: res.Before, after: res.After, inverse: &res.Inverse}, nil
}

type createDocumentPayload struct {
	Kind    string          `json:"kind"`
	Path    *string         `json:"path,omitempty"`
	Root    json.RawMessage `json:"root"`
	Meta    json.RawMessage `json:"meta,omitempty"`
	Policy  json.RawMessage `json:"policy,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
}

// createDocument создаёт документ из вложенной формы (02-ir.md §10): объект, рабочая версия
// и запись в Change Set. Отмена создания не поддерживается — Change Set можно закрыть.
func (s *session) createDocument(in OperationInput) (recordInput, error) {
	var p createDocumentPayload
	dec := json.NewDecoder(bytes.NewReader(in.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return recordInput{}, &ops.Error{Code: "PAYLOAD_INVALID", Message: "payload document.create: " + err.Error()}
	}
	// kind проверяет валидатор IR ниже (допустимы page и component).
	nested := map[string]any{"irVersion": "1.0", "kind": p.Kind}
	for key, raw := range map[string]json.RawMessage{"root": p.Root, "meta": p.Meta, "policy": p.Policy, "content": p.Content} {
		if raw != nil {
			nested[key] = decodeAny(raw)
		}
	}
	doc, err := ir.Normalize(nested, nil)
	if err != nil {
		var nerr *ir.NormalizeError
		errors.As(err, &nerr)
		return recordInput{}, &ops.Error{Code: "PAYLOAD_INVALID", Message: err.Error(), Diagnostics: nerr.Diagnostics}
	}
	doc = decodeBody(mustJSON(doc)) // привести значения к виду разбора JSON
	if v := ir.ValidateDocument(doc); !v.Valid {
		return recordInput{}, &ops.Error{Code: "OPERATION_INVALID", Message: "документ не проходит валидацию", Diagnostics: v.Diagnostics}
	}

	id := uuid.Must(uuid.NewV7())
	if err := s.checkRoute(id, doc, p.Path); err != nil {
		return recordInput{}, err
	}
	obj, err := s.q.CreateObject(s.ctx, store.CreateObjectParams{ID: id, ProjectID: s.actor.ProjectID, DocKind: &p.Kind})
	if err != nil {
		return recordInput{}, err
	}
	if _, err := s.createWorking(obj.ID, nil, p.Path, doc); err != nil {
		return recordInput{}, err
	}
	return recordInput{target: obj.ID, opType: DocumentCreate, payload: in.Payload,
		after: map[string]any{"documentId": obj.ID, "root": doc["root"], "path": p.Path}}, nil
}

// operationError превращает ошибку операции в ошибку команды с номером операции.
func operationError(index int, err error) error {
	var oerr *ops.Error
	if errors.As(err, &oerr) {
		status := http.StatusUnprocessableEntity
		if oerr.Code == "PAYLOAD_INVALID" || oerr.Code == "OPERATION_UNKNOWN" {
			status = http.StatusBadRequest
		}
		params := map[string]any{"operationIndex": index, "operationCode": oerr.Code, "diagnostics": oerr.Diagnostics}
		return commandbus.NewError(status, "OPERATION_INVALID", "Операция отклонена",
			fmt.Sprintf("Операция %d: %s", index, oerr.Message)).WithParams(params)
	}
	var cerr *commandbus.Error
	if errors.As(err, &cerr) {
		if cerr.Params == nil {
			cerr.Params = map[string]any{}
		}
		cerr.Params["operationIndex"] = index
	}
	return err
}

// --- undo ---------------------------------------------------------------------------

type seqPayload struct {
	ChangesetID uuid.UUID `json:"changesetId"`
	ExpectedSeq int32     `json:"expectedSeq"`
}

// handleUndo отменяет последнюю ещё не отменённую операцию добавлением обратной (§2.4):
// история не переписывается.
func handleUndo(ctx context.Context, tx pgx.Tx, actor auth.Actor, p seqPayload) (ApplyResult, error) {
	q := store.New(tx)
	cs, err := lockOpen(ctx, q, actor, p.ChangesetID)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := checkSeq(cs, p.ExpectedSeq); err != nil {
		return ApplyResult{}, err
	}
	last, err := q.LastUndoableOperation(ctx, cs.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApplyResult{}, commandbus.NewError(http.StatusConflict, "NOTHING_TO_UNDO", "Нечего отменять",
			"В Change Set нет операций, которые можно отменить")
	}
	if err != nil {
		return ApplyResult{}, err
	}
	if last.Inverse == nil {
		return ApplyResult{}, commandbus.NewError(http.StatusConflict, "UNDO_NOT_SUPPORTED", "Отмена не поддерживается",
			fmt.Sprintf("Операцию %s нельзя отменить; закройте Change Set (abandon-changeset)", last.Type)).
			WithParams(map[string]any{"operationId": last.ID, "type": last.Type})
	}
	var inverse ops.Op
	if err := json.Unmarshal(last.Inverse, &inverse); err != nil {
		return ApplyResult{}, err
	}
	s := &session{ctx: ctx, q: q, actor: actor, cs: cs, docs: map[uuid.UUID]*workingDoc{}}
	rec, err := s.applyToDocument(last.TargetObjectID, inverse)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("undo %s: %w", last.ID, err)
	}
	rec.undoOf = &last.ID
	op, err := s.record(rec)
	if err != nil {
		return ApplyResult{}, err
	}
	return s.finish([]AppliedOperation{op})
}

// --- abandon-changeset ---------------------------------------------------------------

func handleAbandon(ctx context.Context, tx pgx.Tx, actor auth.Actor, p changesetRef) (Changeset, error) {
	q := store.New(tx)
	cs, err := lockOpen(ctx, q, actor, p.ChangesetID)
	if err != nil {
		return Changeset{}, err
	}
	if err := q.SetChangesetState(ctx, store.SetChangesetStateParams{ID: cs.ID, State: "abandoned"}); err != nil {
		return Changeset{}, err
	}
	cs.State = "abandoned"
	return toChangeset(cs), nil
}

// --- JSON ----------------------------------------------------------------------------

func decodeAny(raw []byte) any {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		panic(fmt.Sprintf("changes: invalid stored JSON: %v", err))
	}
	return v
}

func decodeBody(raw []byte) map[string]any {
	m, _ := decodeAny(raw).(map[string]any)
	return m
}

// encodeBody — тело версии и SHA-256 его канонического JSON (ключи по алфавиту).
func encodeBody(body map[string]any) ([]byte, []byte) {
	raw := mustJSON(body)
	sum := sha256.Sum256(raw)
	return raw, sum[:]
}

func mustJSON(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}

func nullableJSON(v any) []byte {
	if v == nil {
		return nil
	}
	return mustJSON(v)
}
