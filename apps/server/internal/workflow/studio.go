package workflow

import (
	"context"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// RequireExpectedSeq protects explicit decisions made against an older editor snapshot.
// Omission preserves existing API clients; Studio always supplies it.
func RequireExpectedSeq(cs store.Changeset, expected *int32) error {
	if expected != nil && (*expected < 0 || cs.Seq != *expected) {
		return commandbus.NewError(http.StatusConflict, "CHANGESET_SEQ_CONFLICT", "Change Set изменился", "Перечитайте изменения перед решением")
	}
	return nil
}

type ReviewObject struct {
	ID      uuid.UUID `json:"id"`
	Kind    string    `json:"kind"`
	Path    *string   `json:"path"`
	Deleted bool      `json:"deleted"`
}

// A null reason enables an action. Reasons are advisory; commands repeat authorization.
type StudioReview struct {
	ReviewDetail
	Objects        []ReviewObject     `json:"objects"`
	Actions        map[string]*string `json:"actions"`
	NeedsAttention bool               `json:"needsAttention"`
	CanEdit        bool               `json:"canEdit"`
}

func GetStudioReview(ctx context.Context, q *store.Queries, actor auth.Actor, id uuid.UUID) (StudioReview, error) {
	detail, err := GetReview(ctx, q, actor.ProjectID, id)
	if err != nil {
		return StudioReview{}, err
	}
	out := StudioReview{ReviewDetail: detail, Objects: []ReviewObject{}, Actions: map[string]*string{}}
	_, out.NeedsAttention, err = validation.Diagnostics(ctx, q, actor.ProjectID, id)
	if err != nil {
		return StudioReview{}, err
	}
	objects, err := q.ReviewChangesetObjects(ctx, id)
	if err != nil {
		return StudioReview{}, err
	}
	for _, item := range objects {
		out.Objects = append(out.Objects, ReviewObject{ID: item.ID, Kind: item.Kind, Path: item.Path, Deleted: item.Deleted})
	}
	authors, err := q.ChangesetAuthors(ctx, id)
	if err != nil {
		return StudioReview{}, err
	}
	cs := detail.Changeset
	out.CanEdit = cs.OwnerID == actor.ID && cs.State == "open" && (actor.Rights.Has(auth.ContentWrite) || actor.Rights.Has(auth.DesignCompose))
	set := func(action, reason string) {
		if reason == "" {
			out.Actions[action] = nil
		} else {
			out.Actions[action] = &reason
		}
	}
	for _, action := range []string{"submit", "reopen", "approve", "request-changes", "publish"} {
		reason := ""
		switch action {
		case "submit", "reopen":
			if cs.OwnerID != actor.ID {
				reason = "CHANGESET_NOT_OWNER"
			}
			if action == "submit" {
				if cs.State != "open" {
					reason = "CHANGESET_NOT_OPEN"
				} else if cs.HasConflicts {
					reason = "CHANGESET_HAS_CONFLICTS"
				} else if len(out.Objects) == 0 {
					reason = "CHANGESET_EMPTY"
				}
			} else if !slices.Contains([]string{"failed", "changes_requested", "in_review", "approved"}, cs.State) {
				reason = "CHANGESET_STATE_INVALID"
			}
		case "approve", "request-changes":
			if cs.State != "in_review" {
				reason = "CHANGESET_STATE_INVALID"
			} else if !actor.Rights.Has(auth.ContentPublish) {
				reason = "FORBIDDEN"
			} else if actor.Kind != auth.ActorHuman {
				reason = "APPROVAL_FORBIDDEN_ACTOR"
			} else if cs.OwnerID == actor.ID || slices.Contains(authors, actor.ID) {
				reason = "APPROVAL_SELF"
			}
		case "publish":
			if cs.State != "approved" {
				reason = "CHANGESET_STATE_INVALID"
			} else if !actor.Rights.Has(auth.ContentPublish) {
				reason = "FORBIDDEN"
			}
		}
		set(action, reason)
	}
	return out, nil
}
