// Package capabilities implements the explicit, project-scoped engineering handoff.
package capabilities

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/codeindex"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
)

type object = map[string]any

func obj(v any) object          { m, _ := v.(map[string]any); return m }
func arr(v any) []any           { a, _ := v.([]any); return a }
func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func parse(b []byte) object     { v, _ := manifest.ParseJSON(b); return obj(v) }
func fail(code string) error {
	return commandbus.NewError(http.StatusConflict, code, "Запрос недоступен", code)
}
func forbidden() error {
	return commandbus.NewError(http.StatusForbidden, "FORBIDDEN", "Недостаточно прав", "Операция недоступна этому актору")
}
func notFound() error {
	return commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Запрос не найден", "Запрос недоступен в этом проекте")
}
func terminal(s string) bool { return s == "fulfilled" || s == "rejected" || s == "cancelled" }

var keyPattern = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*(\.[a-z][a-zA-Z0-9]*)+$`)
var fieldPattern = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)

type Draft struct {
	Title            string          `json:"title"`
	Description      string          `json:"description"`
	ProposedContract json.RawMessage `json:"proposedContract"`
	Acceptance       []string        `json:"acceptance"`
}
type Create struct {
	Draft
	Environment          string    `json:"environment"`
	ExpectedManifestHash string    `json:"expectedManifestHash"`
	ChangesetID          uuid.UUID `json:"changesetId"`
	ExpectedSeq          int32     `json:"expectedSeq"`
	DocumentID           uuid.UUID `json:"documentId"`
	NodeID               string    `json:"nodeId,omitempty"`
	Event                string    `json:"event,omitempty"`
	Key                  string    `json:"key"`
	// Kind — action (default) or component (STU-355); a component request has
	// a placement and initial props instead of a node event.
	Kind         string          `json:"kind,omitempty"`
	Placement    *Placement      `json:"placement,omitempty"`
	InitialProps json.RawMessage `json:"initialProps,omitempty"`
}
type Update struct {
	ID               uuid.UUID `json:"id"`
	ExpectedRevision int64     `json:"expectedRevision"`
	Draft            *Draft    `json:"draft,omitempty"`
	Transition       string    `json:"transition,omitempty"`
	Reason           string    `json:"reason,omitempty"`
	PRURL            string    `json:"prUrl,omitempty"`
}
type Action struct {
	ID                     uuid.UUID       `json:"id"`
	ExpectedRevision       int64           `json:"expectedRevision"`
	ExpectedSeq            int32           `json:"expectedSeq"`
	ExpectedManifestHash   string          `json:"expectedManifestHash"`
	ConfirmReplaceExisting bool            `json:"confirmReplaceExisting,omitempty"`
	Handlers               json.RawMessage `json:"handlers,omitempty"`
}

func author(a auth.Actor) error {
	if a.Kind != auth.ActorHuman && a.Kind != auth.ActorAgent {
		return forbidden()
	}
	if a.Kind == auth.ActorAgent && (a.OnBehalfOf == nil || a.DelegatorID == nil || *a.OnBehalfOf != *a.DelegatorID) {
		return forbidden()
	}
	if err := commandbus.Require(a, auth.CapabilityRequest); err != nil {
		return err
	}
	return commandbus.Require(a, auth.DesignRead)
}
func human(a auth.Actor) error {
	if a.Kind != auth.ActorHuman {
		return forbidden()
	}
	return author(a)
}

// validDraft checks the texts and a contract of either kind; handlers repeat
// the contract check for the request's own kind.
func validDraft(d Draft) error {
	if e := validTexts(d); e != nil {
		return e
	}
	contract := parse(d.ProposedContract)
	if validActionContract(contract) != nil && validComponentContract(contract) != nil && validDataSourceContract(contract) != nil && validTokenContract(contract) != nil {
		return fail("CAPABILITY_CONTRACT_INVALID")
	}
	return nil
}
func validContract(kind string, raw json.RawMessage) error {
	switch kind {
	case KindComponent:
		return validComponentContract(parse(raw))
	case KindDataSource:
		return validDataSourceContract(parse(raw))
	case KindToken:
		return validTokenContract(parse(raw))
	}
	return validActionContract(parse(raw))
}
func validTexts(d Draft) error {
	if !codeindex.Text(d.Title, 200) || strings.TrimSpace(d.Title) == "" || !descriptionText(d.Description) || len(d.Acceptance) < 1 || len(d.Acceptance) > 16 {
		return fail("CAPABILITY_REQUEST_INVALID")
	}
	for _, line := range d.Acceptance {
		if !codeindex.Text(line, 500) || strings.TrimSpace(line) == "" {
			return fail("CAPABILITY_REQUEST_INVALID")
		}
	}
	return nil
}
func validActionContract(contract object) error {
	args := obj(contract["args"])
	if len(contract) != 1 || args == nil || len(args) > 32 {
		return fail("CAPABILITY_CONTRACT_INVALID")
	}
	for name, value := range args {
		if !fieldPattern.MatchString(name) {
			return fail("CAPABILITY_CONTRACT_INVALID")
		}
		f := obj(value)
		typ, _ := f["type"].(string)
		allowed := map[string]bool{"type": true, "required": true}
		switch typ {
		case "string", "text":
			allowed["minLength"] = true
			allowed["maxLength"] = true
		case "number":
			allowed["min"] = true
			allowed["max"] = true
		case "boolean", "link":
		case "reference":
			allowed["schema"] = true
		case "enum":
			allowed["values"] = true
		default:
			return fail("CAPABILITY_CONTRACT_INVALID")
		}
		for k := range f {
			if !allowed[k] {
				return fail("CAPABILITY_CONTRACT_INVALID")
			}
		}
	}
	// Reuse the manifest schema and semantic validator for bounds and types.
	m := object{"manifestVersion": "1.0", "app": object{"id": "capability", "version": "1.0.0", "framework": "react"}, "irVersions": []any{"1.0"}, "breakpoints": object{}, "tokens": object{}, "actions": object{"capability.proposed": object{"args": args}}}
	// Reference schemas are checked against the real manifest at create/submit.
	schemas := object{}
	for _, v := range args {
		if f := obj(v); f["type"] == "reference" {
			if s, ok := f["schema"].(string); ok {
				schemas[s] = object{"version": json.Number("1"), "fields": object{"title": object{"type": "string"}}}
			}
		}
	}
	if len(schemas) > 0 {
		m["schemas"] = schemas
	}
	if !manifest.Validate(m).Valid {
		return fail("CAPABILITY_CONTRACT_INVALID")
	}
	return nil
}
func descriptionText(s string) bool {
	if strings.TrimSpace(s) == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > 4000 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}
func validURL(s string) bool {
	u, e := url.Parse(s)
	return e == nil && len(s) <= 2048 && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && !strings.ContainsAny(s, "\r\n\t")
}
