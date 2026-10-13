package agents

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

func (s *Service) Config(ctx context.Context, a auth.Actor) (map[string]any, error) {
	if err := commandbus.Require(a, auth.DesignRead); err != nil {
		return nil, err
	}
	cfg, err := s.settings(ctx, store.New(s.Pool), a.ProjectID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"provider": "openrouter", "model": cfg.Model, "configured": s.Provider != nil && s.Provider.Ready() && validModel(cfg.Model), "canDelegate": a.Kind == auth.ActorHuman && a.Rights.Has(auth.AgentDelegate), "monthlyTokens": cfg.MonthlyTokens, "userMonthlyTokens": cfg.UserMonthlyTokens}, nil
}
func (s *Service) Detail(ctx context.Context, a auth.Actor, id uuid.UUID) (RunDetail, error) {
	if err := commandbus.Require(a, auth.DesignRead); err != nil {
		return RunDetail{}, err
	}
	r, err := loadRun(ctx, s.Pool, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return RunDetail{}, denied()
	}
	if err != nil {
		return RunDetail{}, err
	}
	if r.OwnerID != a.ID || r.ProjectID != a.ProjectID {
		return RunDetail{}, denied()
	}
	result := RunDetail{Run: r, Steps: []Step{}}
	rows, err := s.Pool.Query(ctx, `SELECT seq,kind,data FROM agent_steps WHERE run_id=$1 ORDER BY seq LIMIT 32`, r.ID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var step Step
		if err = rows.Scan(&step.Seq, &step.Kind, &step.Data); err != nil {
			return result, err
		}
		result.Steps = append(result.Steps, step)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	err = s.Pool.QueryRow(ctx, `SELECT COALESCE(sum(prompt_tokens::bigint+completion_tokens) FILTER(WHERE state='settled'),0),COALESCE(sum(reserved) FILTER(WHERE state<>'settled'),0),COALESCE(bool_and(state='settled'),true) FROM agent_usage WHERE run_id=$1`, r.ID).Scan(&result.Tokens, &result.Reserved, &result.UsageKnown)
	return result, err
}
func (s *Service) List(ctx context.Context, a auth.Actor, scope Scope) ([]RunDetail, error) {
	if err := commandbus.Require(a, auth.DesignRead); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT id FROM agent_runs WHERE project_id=$1 AND owner_id=$2 AND document_id=$3 AND changeset_id=$4 AND environment=$5 ORDER BY created_at DESC LIMIT 10`, a.ProjectID, a.ID, scope.DocumentID, scope.ChangesetID, scope.Environment)
	if err != nil {
		return nil, err
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := []RunDetail{}
	for _, id := range ids {
		value, err := s.Detail(ctx, a, id)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

// Recover never repeats an LLM call with an uncertain outcome. An expired run
// becomes interrupted and pending reservations become explicitly unknown.
func (s *Service) Recover(ctx context.Context) error {
	rows, err := s.Pool.Query(ctx, `SELECT id FROM agent_runs WHERE deadline<=now() AND state IN ('queued','running') LIMIT 100`)
	if err != nil {
		return err
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		r, err := loadRun(ctx, s.Pool, id)
		if err != nil {
			return err
		}
		err = s.mutate(ctx, r, update{State: "interrupted", Code: "AGENT_INTERRUPTED", Kind: "finished", Data: mustJSON(map[string]string{"code": "AGENT_INTERRUPTED"})})
		var ce *commandbus.Error
		if err != nil && (!errors.As(err, &ce) || ce.Code != "AGENT_RUN_CLOSED") {
			return err
		}
	}
	return nil
}
