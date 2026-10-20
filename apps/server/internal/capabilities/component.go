package capabilities

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/bindingdoc"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifestdoc"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/policydoc"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Kinds of capability requests (STU-355).
const (
	KindAction     = "action"
	KindComponent  = "component"
	KindDataSource = "dataSource"
	KindToken      = "token"
)

// anchorKind — the place an anchor of the kind points at (STU-362/383).
func anchorKind(kind string) string {
	switch kind {
	case KindComponent:
		return "node"
	case KindDataSource:
		return "source"
	case KindToken:
		return "design"
	}
	return "event"
}

var componentPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{0,63}$`)

// Placement — where a placeholder goes: a component's parent, slot and index
// (STU-359), a page data source key (STU-380) or a node design property with
// an optional breakpoint (STU-380).
type Placement struct {
	ParentID      string `json:"parentId,omitempty"`
	Slot          string `json:"slot,omitempty"`
	Index         int    `json:"index,omitempty"`
	DataSourceKey string `json:"dataSourceKey,omitempty"`
	NodeID        string `json:"nodeId,omitempty"`
	Property      string `json:"property,omitempty"`
	Breakpoint    string `json:"breakpoint,omitempty"`
}

func validPlacement(p *Placement) bool {
	return validPlacementFor(KindComponent, p)
}

// validPlacementFor checks the placement shape of a kind.
func validPlacementFor(kind string, p *Placement) bool {
	if p == nil {
		return false
	}
	switch kind {
	case KindComponent:
		return nodeIDPattern.MatchString(p.ParentID) && (p.Slot == "" || identifierPattern.MatchString(p.Slot)) && p.Index >= 0 && p.Index <= 1000 && p.DataSourceKey == "" && p.NodeID == "" && p.Property == "" && p.Breakpoint == ""
	case KindDataSource:
		return identifierPattern.MatchString(p.DataSourceKey) && len(p.DataSourceKey) <= 64 && *p == Placement{DataSourceKey: p.DataSourceKey}
	case KindToken:
		return nodeIDPattern.MatchString(p.NodeID) && fieldPattern.MatchString(p.Property) && (p.Breakpoint == "" || fieldPattern.MatchString(p.Breakpoint)) && p.ParentID == "" && p.Slot == "" && p.Index == 0 && p.DataSourceKey == ""
	}
	return false
}

var (
	nodeIDPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{4,32}$`)
	identifierPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
)

// validComponentContract — STU-356: props in the manifest type system, a
// container flag and slots; no events or provides in v1.
func validComponentContract(contract object) error {
	props := obj(contract["props"])
	slots := obj(contract["slots"])
	if props == nil || len(props) > 32 || len(slots) > 8 {
		return fail("CAPABILITY_CONTRACT_INVALID")
	}
	for k, v := range contract {
		switch k {
		case "props", "slots":
			if obj(v) == nil {
				return fail("CAPABILITY_CONTRACT_INVALID")
			}
		case "container":
			if _, ok := v.(bool); !ok {
				return fail("CAPABILITY_CONTRACT_INVALID")
			}
		default:
			return fail("CAPABILITY_CONTRACT_INVALID")
		}
	}
	schemas := object{}
	for name, value := range props {
		f := obj(value)
		if !fieldPattern.MatchString(name) || f == nil || f["content"] != nil {
			return fail("CAPABILITY_CONTRACT_INVALID")
		}
		if f["type"] == "reference" {
			if s, ok := f["schema"].(string); ok {
				schemas[s] = object{"version": json.Number("1"), "fields": object{"title": object{"type": "string"}}}
			}
		}
	}
	for name, value := range slots {
		s := obj(value)
		maxItems, _ := s["max"].(json.Number)
		n, err := maxItems.Int64()
		if !fieldPattern.MatchString(name) || s == nil || s["max"] != nil && (err != nil || n < 1 || n > 50) {
			return fail("CAPABILITY_CONTRACT_INVALID")
		}
	}
	m := object{"manifestVersion": "1.0", "app": object{"id": "capability", "version": "1.0.0", "framework": "react"}, "irVersions": []any{"1.0"}, "breakpoints": object{}, "tokens": object{}, "components": object{"CapabilityProposed": contract}}
	if len(schemas) > 0 {
		m["schemas"] = schemas
	}
	if !manifest.Validate(m).Valid {
		return fail("CAPABILITY_CONTRACT_INVALID")
	}
	return nil
}

// definition — the manifest element a request asks for, or nil.
func definition(app object, r store.CapabilityRequest) object {
	switch r.Kind {
	case KindComponent:
		return obj(obj(app["components"])[r.Key])
	case KindDataSource:
		return obj(obj(app["dataSources"])[r.Key])
	case KindToken:
		category, name := tokenParts(r.Key)
		if value, ok := obj(obj(app["tokens"])[category])[name]; ok {
			return object{"value": value}
		}
		return nil
	}
	return obj(obj(app["actions"])[r.Key])
}

// taken — the key already names a manifest or builtin element of the kind.
func taken(app object, r store.CapabilityRequest) bool {
	if r.Kind == KindDataSource || r.Kind == KindToken {
		return definition(app, r) != nil
	}
	if r.Kind == KindComponent {
		return manifest.BuiltinContract(r.Key) != nil || obj(app["primitives"])[r.Key] != nil || obj(app["components"])[r.Key] != nil
	}
	return obj(app["actions"])[r.Key] != nil
}

// proposedFields — the typed fields of the proposed contract (args or props).
func proposedFields(r store.CapabilityRequest) object {
	contract := parse(r.ProposedContract)
	switch r.Kind {
	case KindComponent:
		return obj(contract["props"])
	case KindDataSource:
		return obj(contract["params"])
	case KindToken:
		return object{}
	}
	return obj(contract["args"])
}

// marker — the IR gesture that writes the request's marker at its anchor.
func marker(r store.CapabilityRequest, doc object) ops.Op {
	if r.Kind == KindDataSource || r.Kind == KindToken {
		return dataTokenMarker(r, doc)
	}
	if r.Kind != KindComponent {
		return behavior(r, pending(r))
	}
	p := placementOf(r)
	subtree := object{"id": r.NodeID, "type": r.Key, "pending": r.ID.String()}
	if props := obj(jsonValue(r.InitialProps)); len(props) > 0 {
		subtree["props"] = props
	}
	payload := object{"parentId": p.ParentID, "index": p.Index, "subtree": subtree}
	if p.Slot != "" {
		payload["slot"] = p.Slot
	}
	return ops.Op{Type: ops.NodeInsert, Payload: raw(payload)}
}

func placementOf(r store.CapabilityRequest) Placement {
	var p Placement
	_ = json.Unmarshal(r.Placement, &p)
	return p
}

func clearMarker(nodeID string) ops.Op {
	return ops.Op{Type: ops.NodeClearPending, Payload: raw(object{"nodeId": nodeID})}
}

// exactPlaceholder — the node holds this request's component placeholder.
func exactPlaceholder(r store.CapabilityRequest, doc object) bool {
	n := obj(obj(doc["nodes"])[r.NodeID])
	return n != nil && n["pending"] == r.ID.String() && n["type"] == r.Key
}

// componentContext checks a component anchor (STU-359/360): before submit the
// placement must accept a new node in a CODE zone; afterwards the placeholder
// itself must be in place and in CODE.
func componentContext(s snapshot, r store.CapabilityRequest, settings object) error {
	nodes := obj(s.doc["nodes"])
	target := r.NodeID
	if r.Status == "draft" {
		if nodes[r.NodeID] != nil {
			return fail("CAPABILITY_PLACEMENT_INVALID")
		}
		p := placementOf(r)
		parent := obj(nodes[p.ParentID])
		if parent == nil || parent["locked"] == true || parent["pending"] != nil || !acceptsChild(s.app, parent, p) {
			return fail("CAPABILITY_PLACEMENT_INVALID")
		}
		target = p.ParentID
	} else if !exactPlaceholder(r, s.doc) {
		return fail("CAPABILITY_ANCHOR_CHANGED")
	}
	mode, ok := policydoc.NodeMode(s.doc, target, settings)
	if !ok || mode != policydoc.Code {
		return fail("CAPABILITY_CODE_REQUIRED")
	}
	return nil
}

// acceptsChild — the parent takes one more child of an application type at
// the placement: a container for children, or a slot without allowedTypes.
func acceptsChild(app object, parent object, p Placement) bool {
	typ := fmt.Sprint(parent["type"])
	def := manifest.BuiltinContract(typ)
	if def == nil {
		def = obj(obj(app["primitives"])[typ])
	}
	if def == nil {
		def = obj(obj(app["components"])[typ])
	}
	if def == nil || typ == "Composed" {
		return false
	}
	if p.Slot == "" {
		return def["container"] == true && p.Index <= len(arr(parent["children"]))
	}
	slot := obj(obj(def["slots"])[p.Slot])
	if slot == nil || slot["allowedTypes"] != nil {
		return false
	}
	used := len(arr(obj(parent["slots"])[p.Slot]))
	if limit, ok := slot["max"].(json.Number); ok {
		if n, err := limit.Int64(); err == nil && int64(used) >= n {
			return false
		}
	}
	return p.Index <= used
}

// validInitialProps — STU-360: names of the proposed props, values of their
// types, every required prop without a default supplied.
func validInitialProps(r store.CapabilityRequest, initial object, app object) bool {
	props := proposedFields(r)
	for name, value := range initial {
		field := obj(props[name])
		if field == nil || !manifestdoc.ValidateValue(nil, app, field, value) {
			return false
		}
	}
	for name, value := range props {
		f := obj(value)
		if f["required"] == true && f["default"] == nil && initial[name] == nil {
			return false
		}
	}
	return true
}

// componentCompatible — STU-368 for one manifest component: every proposed
// prop exists with a compatible type, required props are proposed, and
// children of the placeholder are accepted.
func componentCompatible(r store.CapabilityRequest, actual object) bool {
	if actual == nil {
		return false
	}
	proposed := proposedFields(r)
	props := obj(actual["props"])
	for key, field := range proposed {
		if !bindingdoc.SimpleContentCompatible(obj(field), obj(props[key])) {
			return false
		}
	}
	for key, field := range props {
		if proposed[key] == nil && obj(field)["required"] == true && obj(field)["default"] == nil {
			return false
		}
	}
	contract := parse(r.ProposedContract)
	if contract["container"] == true && actual["container"] != true {
		return false
	}
	for name, slot := range obj(contract["slots"]) {
		real := obj(obj(actual["slots"])[name])
		if real == nil || real["allowedTypes"] != nil && obj(slot)["allowedTypes"] == nil {
			return false
		}
	}
	return true
}

// nodeErrors — error diagnostics of one node, as the replacement check of a
// component instance against the real definition (STU-368).
func nodeErrors(warnings map[string]map[string][]ir.Diagnostic, node string) bool {
	for _, documents := range warnings {
		for _, list := range documents {
			for _, d := range list {
				if d.NodeID == node && d.Severity == ir.SeverityError {
					return true
				}
			}
		}
	}
	return false
}

// similarComponents — STU-365: up to n manifest components ranked by shared
// prop names, then by a shared name prefix, then by name; deterministic.
func similarComponents(app object, r store.CapabilityRequest, n int) []string {
	proposed := proposedFields(r)
	type scored struct {
		name  string
		score int
	}
	list := []scored{}
	for _, name := range keys(obj(app["components"])) {
		score := 0
		for prop := range obj(obj(obj(app["components"])[name])["props"]) {
			if proposed[prop] != nil {
				score += 2
			}
		}
		for i := 1; i <= min(len(name), len(r.Key)); i++ {
			if name[:i] != r.Key[:i] {
				break
			}
			if i >= 3 {
				score++
				break
			}
		}
		if score > 0 {
			list = append(list, scored{name, score})
		}
	}
	slices.SortStableFunc(list, func(a, b scored) int { return b.score - a.score })
	out := []string{}
	for _, v := range list[:min(len(list), n)] {
		out = append(out, v.name)
	}
	return out
}

// anchorContext — the anchor section of a pack: the event, or for a
// component the placeholder id and its placement (STU-365).
func anchorContext(r store.CapabilityRequest, s snapshot, ancestors, structure []any, truncated bool) object {
	out := object{"documentId": r.DocumentID.String(), "nodeId": r.NodeID, "event": r.Event, "mode": "CODE", "pagePath": s.path, "ancestors": ancestors, "subtree": structure, "truncated": truncated}
	if r.Kind == KindComponent {
		p := placementOf(r)
		placement := object{"parentId": p.ParentID, "index": p.Index}
		if p.Slot != "" {
			placement["slot"] = p.Slot
		}
		delete(out, "event")
		out["placement"] = placement
		out["initialProps"] = jsonValue(r.InitialProps)
	}
	switch r.Kind {
	case KindDataSource:
		// STU-386: the page key, initial params and the binding paths that use it.
		delete(out, "event")
		delete(out, "nodeId")
		out["dataSourceKey"] = r.NodeID
		out["initialParams"] = jsonValue(r.InitialProps)
		out["bindings"] = dataPaths(s.doc, r.NodeID)
	case KindToken:
		category, _ := tokenParts(r.Key)
		delete(out, "event")
		out["designPath"] = r.Event
		out["tokens"] = keys(obj(obj(s.app["tokens"])[category]))
	}
	return out
}

// dataPaths — binding paths of the page that read $data.<key>, sorted.
func dataPaths(doc object, key string) []string {
	seen := map[string]bool{}
	var walk func(any, int)
	walk = func(v any, depth int) {
		if depth > 32 {
			return
		}
		switch t := v.(type) {
		case string:
			if t == "$data."+key || strings.HasPrefix(t, "$data."+key+".") {
				seen[t] = true
			}
		case map[string]any:
			for _, child := range t {
				walk(child, depth+1)
			}
		case []any:
			for _, child := range t {
				walk(child, depth+1)
			}
		}
	}
	walk(doc["nodes"], 0)
	return keysBool(seen)
}
