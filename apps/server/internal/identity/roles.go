package identity

import (
	"context"
	"errors"
	"regexp"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

type createRole struct {
	Name         string       `json:"name"`
	Capabilities []auth.Right `json:"capabilities"`
}
type binding struct {
	ActorID uuid.UUID `json:"actorId"`
	RoleID  uuid.UUID `json:"roleId"`
}

func Register(bus *commandbus.Bus) {
	commandbus.Register(bus, commandbus.Command[createRole, store.Role]{Name: "create-role", Right: auth.ProjectAdmin, Validate: func(p createRole) error {
		if !regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`).MatchString(p.Name) || len(p.Capabilities) > len(auth.AllRights) {
			return commandbus.Validation(map[string]string{"role": "нужны имя и известные права"})
		}
		for _, r := range p.Capabilities {
			if !slices.Contains(auth.AllRights, r) {
				return commandbus.Validation(map[string]string{"capabilities": "неизвестное право"})
			}
		}
		return nil
	}, Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p createRole) (store.Role, error) {
		caps := []string{}
		for _, r := range p.Capabilities {
			caps = append(caps, string(r))
		}
		role, err := store.New(tx).CreateRole(ctx, store.CreateRoleParams{ID: uuid.Must(uuid.NewV7()), ProjectID: a.ProjectID, Name: p.Name, Capabilities: caps})
		if err != nil {
			var pe interface{ SQLState() string }
			if errors.As(err, &pe) && pe.SQLState() == "23505" {
				err = commandbus.NewError(409, "ROLE_EXISTS", "Роль существует", "Выберите другое имя")
			}
		}
		return role, err
	}})
	for _, unbind := range []bool{false, true} {
		name := "bind-human-role"
		if unbind {
			name = "unbind-human-role"
		}
		commandbus.Register(bus, commandbus.Command[binding, map[string]bool]{Name: name, Right: auth.ProjectAdmin, Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p binding) (map[string]bool, error) {
			q := store.New(tx)
			if _, err := q.GetActiveHuman(ctx, p.ActorID); err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					return nil, err
				}
				return nil, commandbus.Validation(map[string]string{"actorId": "нужен активный человек"})
			}
			if _, err := q.GetProjectRole(ctx, store.GetProjectRoleParams{ID: p.RoleID, ProjectID: a.ProjectID}); err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					return nil, err
				}
				return nil, commandbus.Validation(map[string]string{"roleId": "нужна роль этого проекта"})
			}
			var err error
			if unbind {
				err = q.UnbindHumanRole(ctx, store.UnbindHumanRoleParams{ProjectID: a.ProjectID, ActorID: p.ActorID, RoleID: p.RoleID})
			} else {
				err = q.BindHumanRole(ctx, store.BindHumanRoleParams{ProjectID: a.ProjectID, ActorID: p.ActorID, RoleID: p.RoleID})
			}
			return map[string]bool{"ok": true}, err
		}})
	}
}
