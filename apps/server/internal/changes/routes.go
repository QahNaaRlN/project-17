package changes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// DocumentSetRoute — операция смены маршрута страницы (06 §2.2).
const DocumentSetRoute = "document.setRoute"

// MaxPathLength — предел длины маршрута.
const MaxPathLength = 512

// routePattern — маршрут: «/» или сегменты из строчных букв, цифр и «-._~»
// либо параметры «:имя».
var routePattern = regexp.MustCompile(`^(/|(/([a-z0-9][a-z0-9._~-]*|:[a-zA-Z][a-zA-Z0-9_]*))+)$`)

// ValidatePath проверяет формат маршрута страницы.
func ValidatePath(path string) error {
	if len(path) > MaxPathLength || !routePattern.MatchString(path) {
		return &ops.Error{Code: "PATH_INVALID", Message: fmt.Sprintf(
			"маршрут %q: «/» или сегменты из строчных латинских букв, цифр и «-._~» либо параметры «:имя», до %d символов", path, MaxPathLength)}
	}
	return nil
}

// checkRoute проверяет маршрут документа: только у страниц, верный формат и не занят
// head другого объекта (с точностью до имён параметров).
func (s *session) checkRoute(objectID uuid.UUID, body map[string]any, path *string) error {
	if path == nil {
		return nil
	}
	if body["kind"] != "page" {
		return &ops.Error{Code: "PATH_NOT_ALLOWED", Message: "маршрут бывает только у страниц (kind = page)"}
	}
	if err := ValidatePath(*path); err != nil {
		return err
	}
	other, err := s.q.RouteTaken(s.ctx, store.RouteTakenParams{ProjectID: s.actor.ProjectID, ID: objectID, Path: *path})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return &ops.Error{Code: "PATH_TAKEN", Message: fmt.Sprintf("маршрут %q занят страницей %s", *path, other)}
}

type routePayload struct {
	Path *string `json:"path"` // null — снять маршрут
}

func decodeRoute(raw json.RawMessage) (routePayload, error) {
	var p routePayload
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, &ops.Error{Code: "PAYLOAD_INVALID", Message: "payload document.setRoute: " + err.Error()}
	}
	return p, nil
}

// setRoute — document.setRoute: маршрут хранится в рабочей версии, а не в IR (02 §1).
func (s *session) setRoute(target uuid.UUID, raw json.RawMessage) (recordInput, error) {
	p, err := decodeRoute(raw)
	if err != nil {
		return recordInput{}, err
	}
	d, err := s.load(target)
	if err != nil {
		return recordInput{}, err
	}
	if err := s.checkRoute(target, d.body, p.Path); err != nil {
		return recordInput{}, err
	}
	before := d.path
	d.path = p.Path
	return recordInput{target: target, opType: DocumentSetRoute, payload: raw,
		before: map[string]any{"path": before}, after: map[string]any{"path": p.Path},
		inverse: &ops.Op{Type: DocumentSetRoute, Payload: mustJSON(routePayload{Path: before})}}, nil
}
