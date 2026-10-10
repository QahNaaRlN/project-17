// Package schemaflow prepares and verifies schema.apply plans bound to immutable candidates.
package schemaflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

const Apply = "schema.apply"

type Target struct {
	Kind       string `json:"kind"`
	SchemaName string `json:"schemaName"`
}
type OperationPayload struct {
	Schema  string `json:"schema"`
	Version *int32 `json:"version"`
}
type Change struct {
	Name    string
	Before  any
	After   any
	Version *int32
}

func failure(code string) error {
	return commandbus.NewError(http.StatusConflict, code, "Схемный план не соответствует кандидату", "Подготовьте новый manifest и повторите согласование")
}
func Schema(v any) (int32, []byte, error) {
	number, err := v.(map[string]any)["version"].(json.Number).Float64()
	if err != nil || number < 1 || number > math.MaxInt32 || math.Trunc(number) != number {
		return 0, nil, commandbus.Validation(map[string]string{"schemas": "version вне диапазона PostgreSQL integer"})
	}
	raw, err := manifest.CanonicalJSON(v)
	return int32(number), raw, err
}
func schemas(app any) map[string]any {
	if app == nil {
		return map[string]any{}
	}
	s, _ := app.(map[string]any)["schemas"].(map[string]any)
	return s
}
func equal(a, b any) bool {
	x, _ := manifest.CanonicalJSON(a)
	y, _ := manifest.CanonicalJSON(b)
	return bytes.Equal(x, y)
}
func Plan(ctx context.Context, q *store.Queries, project, cs uuid.UUID) (store.ValidationCandidateRow, []Change, error) {
	candidate, err := q.ValidationCandidate(ctx, store.ValidationCandidateParams{ProjectID: project, ChangesetID: cs})
	if errors.Is(err, pgx.ErrNoRows) {
		return candidate, nil, failure("MANIFEST_CANDIDATE_REQUIRED")
	}
	if err != nil {
		return candidate, nil, err
	}
	after, err := manifest.ParseJSON(candidate.Body)
	if err != nil {
		return candidate, nil, err
	}
	var before any
	if candidate.BaseManifestID != nil {
		m, e := q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: project, ID: *candidate.BaseManifestID})
		if e != nil {
			return candidate, nil, e
		}
		before, err = manifest.ParseJSON(m.Body)
		if err != nil {
			return candidate, nil, err
		}
	}
	old, next := schemas(before), schemas(after)
	names := map[string]bool{}
	for n := range old {
		names[n] = true
	}
	for n := range next {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	changes := []Change{}
	for _, n := range sorted {
		if equal(old[n], next[n]) {
			continue
		}
		change := Change{Name: n, Before: old[n], After: next[n]}
		if next[n] != nil {
			v, _, e := Schema(next[n])
			if e != nil {
				return candidate, nil, e
			}
			change.Version = &v
		}
		changes = append(changes, change)
	}
	if candidate.Kind != "schema" || len(changes) == 0 {
		return candidate, nil, failure("SCHEMA_PLAN_MISMATCH")
	}
	return candidate, changes, nil
}
func Record(ctx context.Context, q *store.Queries, actor auth.Actor, cs uuid.UUID, seq int32, source string, change Change) error {
	payload, _ := json.Marshal(OperationPayload{change.Name, change.Version})
	before, _ := json.Marshal(change.Before)
	after, _ := json.Marshal(change.After)
	return q.InsertSchemaOperation(ctx, store.InsertSchemaOperationParams{ID: uuid.Must(uuid.NewV7()), ProjectID: actor.ProjectID, ChangesetID: cs, Seq: seq, ActorID: actor.ID, Source: source, TargetSchemaName: &change.Name, Payload: payload, Before: before, After: after})
}

// Prepare is registration's declaration of the immutable candidate, not activation.
func Prepare(ctx context.Context, q *store.Queries, actor auth.Actor, cs store.Changeset) error {
	_, plan, err := Plan(ctx, q, actor.ProjectID, cs.ID)
	if err != nil {
		return err
	}
	for _, change := range plan {
		cs.Seq++
		if err := Record(ctx, q, actor, cs.ID, cs.Seq, "import", change); err != nil {
			return err
		}
	}
	return q.SetChangesetSeq(ctx, store.SetChangesetSeqParams{ID: cs.ID, Seq: cs.Seq})
}
func Verify(ctx context.Context, q *store.Queries, project, cs uuid.UUID) error {
	_, plan, err := Plan(ctx, q, project, cs)
	if err != nil {
		return err
	}
	expected := map[string]Change{}
	for _, c := range plan {
		expected[c.Name] = c
	}
	rows, err := q.SchemaOperations(ctx, cs)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, row := range rows {
		var p OperationPayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return failure("SCHEMA_PLAN_MISMATCH")
		}
		change, ok := expected[p.Schema]
		if !ok || row.TargetSchemaName == nil || *row.TargetSchemaName != p.Schema || !sameVersion(change.Version, p.Version) {
			return failure("SCHEMA_PLAN_MISMATCH")
		}
		seen[p.Schema] = true
	}
	if len(seen) != len(expected) {
		return failure("SCHEMA_PLAN_MISMATCH")
	}
	return nil
}
func sameVersion(a, b *int32) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
func CheckVersions(ctx context.Context, q *store.Queries, project uuid.UUID, app any) error {
	values := schemas(app)
	names := make([]string, 0, len(values))
	for n := range values {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		version, raw, err := Schema(values[name])
		if err != nil {
			return err
		}
		row, err := q.GetStandardSchemaVersion(ctx, store.GetStandardSchemaVersionParams{ProjectID: project, SchemaName: name, Version: version})
		if err == nil {
			stored, e := manifest.ParseJSON(row.Body)
			if e != nil {
				return e
			}
			canonical, e := manifest.CanonicalJSON(stored)
			if e != nil {
				return e
			}
			if bytes.Equal(raw, canonical) {
				continue
			}
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if errors.Is(err, pgx.ErrNoRows) {
			latest, e := q.LatestStandardSchemaVersion(ctx, store.LatestStandardSchemaVersionParams{ProjectID: project, SchemaName: name})
			if errors.Is(e, pgx.ErrNoRows) {
				continue
			}
			if e != nil {
				return e
			}
			if version > latest.Version {
				continue
			}
		}
		return commandbus.NewError(http.StatusConflict, "SCHEMA_VERSION_CONFLICT", "Версия схемы уже закреплена", "Изменённое тело требует новой возрастающей версии").WithParams(map[string]any{"schema": name, "version": version})
	}
	return nil
}
func StoreVersions(ctx context.Context, q *store.Queries, candidate store.ValidationCandidateRow) error {
	app, err := manifest.ParseJSON(candidate.Body)
	if err != nil {
		return err
	}
	if err := CheckVersions(ctx, q, candidate.ProjectID, app); err != nil {
		return err
	}
	values := schemas(app)
	names := make([]string, 0, len(values))
	for n := range values {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		v, body, err := Schema(values[name])
		if err != nil {
			return err
		}
		_, err = q.GetStandardSchemaVersion(ctx, store.GetStandardSchemaVersionParams{ProjectID: candidate.ProjectID, SchemaName: name, Version: v})
		if err == nil {
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := q.InsertStandardSchemaVersion(ctx, store.InsertStandardSchemaVersionParams{ProjectID: candidate.ProjectID, SchemaName: name, Version: v, Body: body, ManifestID: candidate.ManifestID}); err != nil {
			return err
		}
	}
	return nil
}
func RequireOwnerRight(ctx context.Context, q *store.Queries, cs store.Changeset) error {
	caps, err := q.ActorCapabilities(ctx, store.ActorCapabilitiesParams{ProjectID: cs.ProjectID, ActorID: cs.OwnerID})
	if err != nil {
		return err
	}
	return commandbus.Require(auth.Actor{Rights: auth.EffectiveRights(caps, []string{auth.ScopeAll})}, auth.SchemaApply)
}

type claimPayload struct {
	ChangesetID uuid.UUID `json:"changesetId"`
	ExpectedSeq int32     `json:"expectedSeq"`
}

func Register(bus *commandbus.Bus) {
	commandbus.Register(bus, commandbus.Command[claimPayload, store.Changeset]{Name: "claim-schema-changeset", Right: auth.SchemaApply, Handle: claim})
}
func claim(ctx context.Context, tx pgx.Tx, actor auth.Actor, p claimPayload) (store.Changeset, error) {
	if actor.Kind != auth.ActorHuman {
		return store.Changeset{}, commandbus.NewError(http.StatusForbidden, "SCHEMA_CLAIM_HUMAN_REQUIRED", "Кандидат принимает человек", "Владение передаётся ответственному автору с schema.apply")
	}
	q := store.New(tx)
	if err := validation.LockProjectEnvironments(ctx, q, actor.ProjectID); err != nil {
		return store.Changeset{}, err
	}
	cs, err := q.LockChangeset(ctx, store.LockChangesetParams{ProjectID: actor.ProjectID, ID: p.ChangesetID})
	if errors.Is(err, pgx.ErrNoRows) {
		return cs, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Change Set не найден", "")
	}
	if err != nil {
		return cs, err
	}
	if cs.Kind != "schema" || cs.State != "open" {
		return cs, failure("CHANGESET_STATE_INVALID")
	}
	if cs.Seq != p.ExpectedSeq {
		return cs, failure("CHANGESET_SEQ_CONFLICT")
	}
	if cs.OwnerID == actor.ID {
		return cs, nil
	}
	kind, err := q.SchemaOwner(ctx, cs.ID)
	if err != nil {
		return cs, err
	}
	if kind != "service" {
		return cs, commandbus.NewError(http.StatusForbidden, "CHANGESET_NOT_OWNER", "Кандидат уже принят", "Нельзя забрать Change Set другого человека")
	}
	candidate, _, err := Plan(ctx, q, actor.ProjectID, cs.ID)
	if err != nil {
		return cs, err
	}
	envs, err := q.ListEnvironments(ctx, actor.ProjectID)
	if err != nil {
		return cs, err
	}
	for _, env := range envs {
		if env.ID == candidate.EnvironmentID {
			if _, err := validation.Load(ctx, q, actor.ProjectID, env.Name, &cs.ID); err != nil {
				return cs, err
			}
		}
	}
	rows, err := q.SchemaOperations(ctx, cs.ID)
	if err != nil {
		return cs, err
	}
	// Candidates made before schema journaling are upgraded on explicit human acceptance.
	if len(rows) == 0 {
		ci := actor
		ci.ID = cs.OwnerID
		if err := Prepare(ctx, q, ci, cs); err != nil {
			return cs, err
		}
	}
	if err := q.RecordSchemaClaim(ctx, store.RecordSchemaClaimParams{ID: uuid.Must(uuid.NewV7()), ChangesetID: cs.ID, PreviousOwnerID: cs.OwnerID, OwnerID: actor.ID}); err != nil {
		return cs, err
	}
	return q.ClaimSchemaChangeset(ctx, store.ClaimSchemaChangesetParams{ID: cs.ID, OwnerID: actor.ID})
}
