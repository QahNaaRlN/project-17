// Package commandbus — единая точка изменения состояния CMS (FR-001, API-012):
// авторизация → разбор и проверка payload → идемпотентность → исполнение в транзакции.
// Studio, внешний API, агент и миграции вызывают одни и те же команды.
package commandbus

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"github.com/qahnaarln/project-17/apps/server/internal/securityevents"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Command описывает команду с payload типа P и результатом типа R.
type Command[P, R any] struct {
	Name string
	// Right — право, необходимое для команды. Если задан Authorize, он решает вместо Right.
	Right auth.Right
	// Authorize — проверка прав, зависящая от payload (например, вид окружения).
	Authorize func(actor auth.Actor, p P) error
	// Validate проверяет payload до открытия транзакции.
	Validate func(p P) error
	// Handle исполняет команду в транзакции.
	Handle func(ctx context.Context, tx pgx.Tx, actor auth.Actor, p P) (R, error)
}

type handler struct {
	name   string
	decode func(raw json.RawMessage) (any, error)
	auth   func(actor auth.Actor, p any) error
	valid  func(p any) error
	handle func(ctx context.Context, tx pgx.Tx, actor auth.Actor, p any) (any, error)
}

// Bus — реестр команд и их исполнитель.
type Bus struct {
	pool     *pgxpool.Pool
	handlers map[string]handler
	contexts []func(context.Context) context.Context
}

// New создаёт шину команд.
func New(pool *pgxpool.Pool) *Bus {
	return &Bus{pool: pool, handlers: map[string]handler{}}
}

// WithContext добавляет обогащение контекста обработчиков (например, клиент очереди задач
// для постановки задач в транзакции команды — OPS-010).
func (b *Bus) WithContext(fn func(context.Context) context.Context) {
	b.contexts = append(b.contexts, fn)
}

// Register добавляет команду в шину. Повторное имя — ошибка программирования (паника).
func Register[P, R any](b *Bus, c Command[P, R]) {
	if _, dup := b.handlers[c.Name]; dup {
		panic("commandbus: duplicate command " + c.Name)
	}
	if c.Handle == nil || (c.Right == "" && c.Authorize == nil) {
		panic("commandbus: command " + c.Name + " needs Handle and Right or Authorize")
	}
	b.handlers[c.Name] = handler{
		name: c.Name,
		decode: func(raw json.RawMessage) (any, error) {
			var p P
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&p); err != nil {
				return nil, err
			}
			if _, err := dec.Token(); !errors.Is(err, io.EOF) {
				return nil, errors.New("лишние данные после объекта payload")
			}
			return p, nil
		},
		auth: func(actor auth.Actor, p any) error {
			if c.Authorize != nil {
				return c.Authorize(actor, p.(P))
			}
			return Require(actor, c.Right)
		},
		valid: func(p any) error {
			if c.Validate == nil {
				return nil
			}
			return c.Validate(p.(P))
		},
		handle: func(ctx context.Context, tx pgx.Tx, actor auth.Actor, p any) (any, error) {
			return c.Handle(ctx, tx, actor, p.(P))
		},
	}
}

// Names — зарегистрированные команды в лексикографическом порядке.
func (b *Bus) Names() []string {
	names := make([]string, 0, len(b.handlers))
	for n := range b.handlers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Require возвращает 403 FORBIDDEN, если у актора нет права.
func Require(actor auth.Actor, r auth.Right) error {
	if actor.Rights.Has(r) {
		return nil
	}
	return NewError(http.StatusForbidden, "FORBIDDEN", "Недостаточно прав",
		fmt.Sprintf("Для команды нужно право %s", r)).WithParams(map[string]any{"right": string(r)})
}

// Request — вызов команды.
type Request struct {
	Name           string
	IdempotencyKey string
	Payload        json.RawMessage
	// Reason — причина изменения из конверта команды (API-011); сохраняется в операциях.
	Reason string
}

type reasonKey struct{}

// ReasonFrom возвращает причину изменения текущей команды.
func ReasonFrom(ctx context.Context) string {
	r, _ := ctx.Value(reasonKey{}).(string)
	return r
}

// Response — результат: HTTP-статус и тело {"result": …}. Повтор с тем же ключом
// идемпотентности возвращает сохранённый ответ (Replayed = true).
type Response struct {
	Status   int
	Body     json.RawMessage
	Replayed bool
}

// audited — успешные команды над ролями и политиками в журнале SEC-005 (STU-507, 508).
var audited = map[string]string{
	"create-role":                    "role.created",
	"bind-human-role":                "role.bound",
	"unbind-human-role":              "role.unbound",
	"set-approval-policy":            "policy.changed",
	"set-agent-policy":               "policy.changed",
	"set-agent-config":               "policy.changed",
	"set-environment-app-url":        "policy.changed",
	"set-capability-github-config":   "policy.changed",
	"set-capability-delivery-target": "policy.changed",
}

// Dispatch исполняет команду от имени actor. Отказы доступа любого актора пишутся в
// журнал SEC-005 (STU-505), для агентов — и исчерпание квоты (STU-426).
func (b *Bus) Dispatch(ctx context.Context, actor auth.Actor, req Request) (Response, error) {
	// STU-592, 597: a span and the duration per command; the payload never becomes an attribute.
	start := time.Now()
	name := req.Name
	if _, ok := b.handlers[name]; !ok {
		name = "unknown"
	}
	ctx, span := telemetry.Tracer().Start(ctx, "command "+name, trace.WithAttributes(
		attribute.String("cms.command", name), attribute.String("cms.project_id", actor.ProjectID.String()),
		attribute.String("cms.actor_kind", string(actor.Kind))))
	resp, err := b.dispatch(ctx, actor, req)
	outcome := "ok"
	var ce *Error
	if errors.As(err, &ce) {
		outcome = ce.Code
	} else if err != nil {
		outcome = "INTERNAL"
	}
	if outcome != "ok" {
		span.SetStatus(codes.Error, outcome)
	}
	span.SetAttributes(attribute.String("cms.outcome", outcome))
	span.End()
	commandDuration().Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
		attribute.String("cms.command", name), attribute.String("cms.outcome", outcome)))
	ce = nil
	if err != nil && errors.As(err, &ce) && (ce.Status == 401 || ce.Status == 403 || actor.Kind == auth.ActorAgent && ce.Status == 429) {
		data := map[string]any{"code": ce.Code, "command": req.Name}
		if actor.Kind == auth.ActorAgent {
			// STU-426: the refusal's right, rule and condition, never page data.
			data["params"] = ce.Params
		}
		var project, id *uuid.UUID
		if actor.ProjectID != uuid.Nil {
			project = &actor.ProjectID
		}
		if actor.ID != uuid.Nil {
			id = &actor.ID
		}
		_ = securityevents.Record(context.WithoutCancel(ctx), b.pool, securityevents.Event{ProjectID: project, ActorID: id, Type: "access.denied", Data: data})
	}
	return resp, err
}

func (b *Bus) dispatch(ctx context.Context, actor auth.Actor, req Request) (Response, error) {
	h, ok := b.handlers[req.Name]
	if !ok {
		return Response{}, NewError(http.StatusNotFound, "NOT_FOUND", "Команда не найдена",
			fmt.Sprintf("Неизвестная команда %q", req.Name))
	}
	if req.IdempotencyKey == "" || len(req.IdempotencyKey) > 200 {
		return Response{}, NewError(http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "Нужен ключ идемпотентности",
			"Передайте заголовок Idempotency-Key длиной 1…200 символов (API-010)")
	}
	payload, err := h.decode(req.Payload)
	if err != nil {
		return Response{}, NewError(http.StatusBadRequest, "PAYLOAD_INVALID", "Некорректный payload", err.Error())
	}
	if actor.Kind != auth.ActorAgent {
		if err := h.auth(actor, payload); err != nil {
			return Response{}, err
		}
	}
	if err := h.valid(payload); err != nil {
		return Response{}, err
	}

	hash := requestHash(req)
	ctx = context.WithValue(ctx, reasonKey{}, req.Reason)
	for _, fn := range b.contexts {
		ctx = fn(ctx)
	}
	var resp Response
	err = postgres.InTx(ctx, b.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		var session store.AgentSession
		var charge int32
		if actor.Kind == auth.ActorAgent {
			var gate error
			actor, session, charge, gate = auth.GuardAgent(ctx, q, actor, req.Name, req.Reason, req.Payload)
			if gate != nil {
				if errors.Is(gate, auth.ErrUnauthenticated) {
					return NewError(401, "UNAUTHENTICATED", "Сессия агента недействительна", "Создайте новую сессию")
				}
				if errors.Is(gate, auth.ErrAgentPolicy) {
					return NewError(403, "POLICY_DENIED", "Действие агента запрещено", "Проверьте право, reason и разрешённые CS")
				}
				return gate
			}
			if err := h.auth(actor, payload); err != nil {
				return err
			}
			// The same agent can have distinct delegations. A key replay must never
			// return another session's response or bypass its operation quota.
			keyHash := sha256.Sum256([]byte(req.IdempotencyKey))
			req.IdempotencyKey = fmt.Sprintf("%s:%x", session.ID, keyHash)
		}
		if err := q.LockIdempotencyKey(ctx, store.LockIdempotencyKeyParams{ActorID: actor.ID.String(), Key: req.IdempotencyKey}); err != nil {
			return err
		}
		prev, err := q.GetIdempotencyKey(ctx, store.GetIdempotencyKeyParams{ActorID: actor.ID, Key: req.IdempotencyKey})
		switch {
		case err == nil:
			if prev.Command != req.Name || !bytes.Equal(prev.RequestHash, hash) {
				return NewError(http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED", "Ключ идемпотентности уже использован",
					"Этот Idempotency-Key уже применён к другому запросу")
			}
			resp = Response{Status: int(prev.Status), Body: prev.Response, Replayed: true}
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		if actor.Kind == auth.ActorAgent && session.OpsUsed+charge > session.OpsQuota {
			return NewError(429, "AGENT_QUOTA_EXCEEDED", "Квота операций исчерпана", "Создайте новую сессию с допустимой квотой")
		}

		result, err := h.handle(ctx, tx, actor, payload)
		if err != nil {
			return err
		}
		if actor.Kind == auth.ActorAgent && charge > 0 {
			if err := q.ChargeAgentOperations(ctx, store.ChargeAgentOperationsParams{ID: session.ID, OpsUsed: charge}); err != nil {
				return err
			}
		}
		body, err := json.Marshal(map[string]any{"result": result})
		if err != nil {
			return fmt.Errorf("commandbus: marshal result: %w", err)
		}
		resp = Response{Status: http.StatusOK, Body: body}
		if typ := audited[req.Name]; typ != "" {
			if err := securityevents.Record(ctx, tx, securityevents.Event{ProjectID: &actor.ProjectID, ActorID: &actor.ID, Type: typ,
				Data: map[string]any{"command": req.Name, "payloadSha256": hex.EncodeToString(hash), "reason": req.Reason}}); err != nil {
				return err
			}
		}
		if err := q.SaveIdempotencyKey(ctx, store.SaveIdempotencyKeyParams{
			ActorID: actor.ID, Key: req.IdempotencyKey, Command: req.Name,
			RequestHash: hash, Status: int32(resp.Status), Response: body,
		}); err != nil {
			return err
		}
		// Ephemeral leases and preview credentials do not invalidate saved content.
		if req.Name == "presence-heartbeat" || req.Name == "presence-leave" || req.Name == "create-preview-token" || strings.HasPrefix(req.Name, "agent-internal-") {
			return nil
		}
		// UPSERT holds the project row until commit: concurrent commands cannot
		// expose revision N+1 before N commits. Rollback and replay emit nothing.
		_, err = tx.Exec(ctx, `INSERT INTO project_revisions(project_id,revision) VALUES($1,1)
		 ON CONFLICT(project_id) DO UPDATE SET revision=project_revisions.revision+1`, actor.ProjectID)
		return err
	})
	if err != nil {
		return Response{}, err
	}
	return resp, nil
}

// requestHash — SHA-256 имени команды и компактного JSON payload: пробелы и переводы
// строк не делают запрос «другим».
func requestHash(req Request) []byte {
	var compact bytes.Buffer
	if err := json.Compact(&compact, req.Payload); err != nil {
		compact.Write(req.Payload)
	}
	h := sha256.New()
	h.Write([]byte(req.Name))
	h.Write([]byte{0})
	h.Write(compact.Bytes())
	return h.Sum(nil)
}

var commandHistogram struct {
	sync.Once
	h metric.Float64Histogram
}

// commandDuration — cms.command.duration (STU-597), created once on first use.
func commandDuration() metric.Float64Histogram {
	commandHistogram.Do(func() {
		commandHistogram.h, _ = telemetry.Meter().Float64Histogram("cms.command.duration",
			metric.WithUnit("s"), metric.WithDescription("Command duration by type and outcome"))
	})
	return commandHistogram.h
}
