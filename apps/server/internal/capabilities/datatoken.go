package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/bindingdoc"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifestdoc"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/policydoc"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Token categories a request may add (STU-375); typography and themes are out.
var tokenKey = regexp.MustCompile(`^(spacing|colors|radius|shadow|borderWidth|container|layer|transition)\.[a-z][A-Za-z0-9]{0,63}$`)

// tokenParts — category and name of a token request key.
func tokenParts(key string) (string, string) {
	category, name, _ := strings.Cut(key, ".")
	return category, name
}

// validDataSourceContract — STU-376: params, result and paginated, checked by
// the manifest validator as a data source definition.
func validDataSourceContract(contract object) error {
	params := obj(contract["params"])
	if contract["result"] == nil || len(params) > 16 || contract["params"] != nil && params == nil {
		return fail("CAPABILITY_CONTRACT_INVALID")
	}
	for k, v := range contract {
		if k == "paginated" {
			if _, ok := v.(bool); !ok {
				return fail("CAPABILITY_CONTRACT_INVALID")
			}
		} else if k != "params" && k != "result" {
			return fail("CAPABILITY_CONTRACT_INVALID")
		}
	}
	if strings.Contains(string(raw(contract)), `"content":true`) {
		return fail("CAPABILITY_CONTRACT_INVALID")
	}
	schemas := object{}
	collectSchemas(contract, schemas, 0)
	m := object{"manifestVersion": "1.0", "app": object{"id": "capability", "version": "1.0.0", "framework": "react"}, "irVersions": []any{"1.0"}, "breakpoints": object{}, "tokens": object{}, "dataSources": object{"capability.proposed": contract}}
	if len(schemas) > 0 {
		m["schemas"] = schemas
	}
	if !manifest.Validate(m).Valid {
		return fail("CAPABILITY_CONTRACT_INVALID")
	}
	return nil
}

// collectSchemas adds a placeholder schema for every reference; the real
// manifest is checked at create/submit (CAPABILITY_SCHEMA_MISSING).
func collectSchemas(v any, out object, depth int) {
	if depth > 16 {
		return
	}
	if m := obj(v); m != nil {
		if m["type"] == "reference" {
			if s, ok := m["schema"].(string); ok {
				out[s] = object{"version": json.Number("1"), "fields": object{"title": object{"type": "string"}}}
			}
		}
		for _, child := range m {
			collectSchemas(child, out, depth+1)
		}
	}
}

// validTokenContract — STU-376: a category and a value of that category.
func validTokenContract(contract object) error {
	category, _ := contract["category"].(string)
	if len(contract) != 2 || contract["value"] == nil || !tokenKey.MatchString(category+".probe") {
		return fail("CAPABILITY_CONTRACT_INVALID")
	}
	m := object{"manifestVersion": "1.0", "app": object{"id": "capability", "version": "1.0.0", "framework": "react"}, "irVersions": []any{"1.0"}, "breakpoints": object{}, "tokens": object{category: object{"probe": contract["value"]}}}
	if !manifest.Validate(m).Valid {
		return fail("CAPABILITY_CONTRACT_INVALID")
	}
	return nil
}

// dataSourceCompatible — STU-389 for a manifest data source.
func dataSourceCompatible(r store.CapabilityRequest, actual object) bool {
	if actual == nil {
		return false
	}
	contract := parse(r.ProposedContract)
	proposed := obj(contract["params"])
	params := obj(actual["params"])
	for key, field := range proposed {
		if !bindingdoc.SimpleContentCompatible(obj(field), obj(params[key])) {
			return false
		}
	}
	for key, field := range params {
		if proposed[key] == nil && obj(field)["required"] == true && obj(field)["default"] == nil {
			return false
		}
	}
	return (contract["paginated"] == true) == (actual["paginated"] == true) && bindingdoc.SimpleContentCompatible(obj(contract["result"]), obj(actual["result"]))
}

// designPath — the anchor path of a token placement in the node's current
// design: "padding" for a plain value, "padding/<bp>" in a responsive value.
func designPath(node object, p Placement) (string, bool) {
	value := obj(node["design"])[p.Property]
	if responsive := obj(value); responsive != nil && responsive["raw"] == nil {
		bp := p.Breakpoint
		if bp == "" {
			bp = "base"
		}
		return p.Property + "/" + bp, true
	}
	if p.Breakpoint != "" {
		// A breakpoint value needs a base: the plain value becomes the base.
		return p.Property + "/" + p.Breakpoint, value != nil
	}
	return p.Property, true
}

// designValue — the property value with the leaf at path replaced.
func designValue(node object, path, leaf string) (string, any) {
	property, bp, responsive := strings.Cut(path, "/")
	if !responsive {
		return property, leaf
	}
	current := obj(node["design"])[property]
	m := object{}
	if existing := obj(current); existing != nil {
		for k, v := range existing {
			m[k] = v
		}
	} else if current != nil {
		m["base"] = current
	}
	m[bp] = leaf
	return property, m
}

// designLeaf — the string at a design path of a node.
func designLeaf(node object, path string) string {
	property, bp, responsive := strings.Cut(path, "/")
	value := obj(node["design"])[property]
	if responsive {
		value = obj(value)[bp]
	}
	s, _ := value.(string)
	return s
}

// dataTokenMarker — the IR gesture writing a data source or token placeholder.
func dataTokenMarker(r store.CapabilityRequest, doc object) ops.Op {
	if r.Kind == KindDataSource {
		use := object{"source": r.Key, "pending": r.ID.String()}
		if params := obj(jsonValue(r.InitialProps)); len(params) > 0 {
			use["params"] = params
		}
		return ops.Op{Type: ops.DocumentSetDataSource, Payload: raw(object{"key": r.NodeID, "use": use})}
	}
	return setLeaf(r.NodeID, r.Event, doc, "pending:"+r.ID.String())
}

func setLeaf(nodeID, path string, doc object, leaf string) ops.Op {
	property, value := designValue(obj(obj(doc["nodes"])[nodeID]), path, leaf)
	return ops.Op{Type: ops.NodeSetDesign, Payload: raw(object{"nodeId": nodeID, "set": object{property: value}})}
}

// dataTokenPending — the anchor holds exactly this request's placeholder.
func dataTokenPending(r store.CapabilityRequest, doc object) bool {
	if r.Kind == KindDataSource {
		use := obj(obj(doc["dataSources"])[r.NodeID])
		return use["pending"] == r.ID.String() && use["source"] == r.Key
	}
	return designLeaf(obj(obj(doc["nodes"])[r.NodeID]), r.Event) == "pending:"+r.ID.String()
}

// dataTokenClear — the replacement: drop the source marker or write the
// token name (STU-390). The anchor's place is given by r.NodeID/r.Event.
func dataTokenClear(r store.CapabilityRequest, doc object) ops.Op {
	if r.Kind == KindDataSource {
		use := object{}
		for k, v := range obj(obj(doc["dataSources"])[r.NodeID]) {
			if k != "pending" {
				use[k] = v
			}
		}
		return ops.Op{Type: ops.DocumentSetDataSource, Payload: raw(object{"key": r.NodeID, "use": use})}
	}
	_, name := tokenParts(r.Key)
	return setLeaf(r.NodeID, r.Event, doc, name)
}

// dataTokenDetach removes the owner's placeholder by ordinary editing (STU-384).
func dataTokenDetach(r store.CapabilityRequest, doc object) ops.Op {
	if r.Kind == KindDataSource {
		return ops.Op{Type: ops.DocumentSetDataSource, Payload: raw(object{"key": r.NodeID, "use": nil})}
	}
	property, bp, responsive := strings.Cut(r.Event, "/")
	if !responsive || bp == "base" {
		return ops.Op{Type: ops.NodeSetDesign, Payload: raw(object{"nodeId": r.NodeID, "unset": []string{property}})}
	}
	m := object{}
	for k, v := range obj(obj(obj(obj(doc["nodes"])[r.NodeID])["design"])[property]) {
		if k != bp {
			m[k] = v
		}
	}
	return ops.Op{Type: ops.NodeSetDesign, Payload: raw(object{"nodeId": r.NodeID, "set": object{property: m}})}
}

// dataTokenContext — STU-380/381: before submit the place must accept the
// placeholder in CODE; afterwards the placeholder must be in place.
func dataTokenContext(s snapshot, r store.CapabilityRequest, settings object) error {
	nodes := obj(s.doc["nodes"])
	target := r.NodeID
	if r.Kind == KindDataSource {
		target, _ = s.doc["root"].(string)
	}
	if r.Status != "draft" {
		if !dataTokenPending(r, s.doc) {
			return fail("CAPABILITY_ANCHOR_CHANGED")
		}
	} else if r.Kind == KindDataSource {
		if !identifierPattern.MatchString(r.NodeID) || r.NodeID == "content" || obj(s.doc["dataSources"])[r.NodeID] != nil {
			return fail("CAPABILITY_PLACEMENT_INVALID")
		}
	} else {
		node := obj(nodes[r.NodeID])
		p := placementOf(r)
		category, _ := tokenParts(r.Key)
		_, known := obj(s.app["breakpoints"])[p.Breakpoint]
		if node == nil || node["locked"] == true || node["pending"] != nil || policydoc.TokenCategory(p.Property) != category || p.Breakpoint != "" && !known {
			return fail("CAPABILITY_PLACEMENT_INVALID")
		}
		if path, ok := designPath(node, p); !ok || r.Event != "" && path != r.Event || strings.HasPrefix(designLeaf(node, path), "pending:") {
			return fail("CAPABILITY_PLACEMENT_INVALID")
		}
	}
	mode, ok := policydoc.NodeMode(s.doc, target, settings)
	if !ok || mode != policydoc.Code {
		return fail("CAPABILITY_CODE_REQUIRED")
	}
	return nil
}

// validInitialParams — STU-381: literal or expression params by the proposed
// contract, every required param without a default supplied.
func validInitialParams(r store.CapabilityRequest, params object, app object) bool {
	proposed := obj(parse(r.ProposedContract)["params"])
	for name, value := range params {
		field := obj(proposed[name])
		if field == nil {
			return false
		}
		if lit, ok := obj(value)["lit"]; ok {
			if len(obj(value)) != 1 || !manifestdoc.ValidateValue(nil, app, field, lit) {
				return false
			}
		} else if s, ok := value.(string); !ok || !strings.HasPrefix(s, "$") {
			return false
		}
	}
	for name, value := range proposed {
		f := obj(value)
		if f["required"] == true && f["default"] == nil && params[name] == nil {
			return false
		}
	}
	return true
}

// validInitial — initial props of a component or params of a data source.
func validInitial(r store.CapabilityRequest, app object) bool {
	switch r.Kind {
	case KindComponent:
		return validInitialProps(r, obj(jsonValue(r.InitialProps)), app)
	case KindDataSource:
		return validInitialParams(r, obj(jsonValue(r.InitialProps)), app)
	}
	return true
}

// placeErrors — error diagnostics at a placeholder's place once replaced:
// the component node, the page's data source, or the design property.
func placeErrors(r store.CapabilityRequest, nodeID, event string, warnings map[string]map[string][]ir.Diagnostic) bool {
	for _, documents := range warnings {
		for _, list := range documents {
			for _, d := range list {
				if d.Severity != ir.SeverityError {
					continue
				}
				switch r.Kind {
				case KindComponent:
					if d.NodeID == nodeID {
						return true
					}
				case KindDataSource:
					if strings.HasPrefix(d.Pointer, ir.Pointer("dataSources", nodeID)) {
						return true
					}
				case KindToken:
					property, _, _ := strings.Cut(event, "/")
					if d.NodeID == nodeID && strings.HasPrefix(d.Pointer, ir.Pointer("nodes", nodeID, "design", property)) {
						return true
					}
				}
			}
		}
	}
	return false
}

// sortedLeaves — string design values of a node in stable order.
func sortedLeaves(design object) []string {
	out := []string{}
	add := func(v any) {
		if s, ok := v.(string); ok {
			out = append(out, s)
		} else if m := obj(v); m != nil && m["raw"] == nil {
			for _, bp := range keys(m) {
				if s, ok := m[bp].(string); ok {
					out = append(out, s)
				}
			}
		}
	}
	for _, key := range keys(design) {
		if key == "states" {
			for _, state := range keys(obj(design[key])) {
				for _, prop := range keys(obj(obj(design[key])[state])) {
					add(obj(obj(design[key])[state])[prop])
				}
			}
			continue
		}
		add(design[key])
	}
	return out
}

// proposedTokens — STU-387: in a draft preview a token placeholder renders
// its proposed value as a raw value; stored versions keep the placeholder.
func proposedTokens(ctx context.Context, q *store.Queries, project uuid.UUID, doc object) (bool, error) {
	changed := false
	for _, nodeID := range keys(obj(doc["nodes"])) {
		design := obj(obj(obj(doc["nodes"])[nodeID])["design"])
		replace := func(v any) (any, error) {
			s, _ := v.(string)
			marker, ok := strings.CutPrefix(s, "pending:")
			id, err := uuid.Parse(marker)
			if !ok || err != nil {
				return v, nil
			}
			r, e := q.GetCapabilityRequest(ctx, store.GetCapabilityRequestParams{ProjectID: project, ID: id})
			if errors.Is(e, pgx.ErrNoRows) || e == nil && r.Kind != KindToken {
				return v, nil
			}
			if e != nil {
				return nil, e
			}
			changed = true
			return object{"raw": parse(r.ProposedContract)["value"]}, nil
		}
		for _, key := range keys(design) {
			value := design[key]
			targets := []object{design}
			names := []string{key}
			if m := obj(value); m != nil && m["raw"] == nil && key != "states" {
				targets, names = nil, nil
				for _, bp := range keys(m) {
					targets, names = append(targets, m), append(names, bp)
				}
			}
			if key == "states" {
				continue // states are out of the token placement (§1)
			}
			for i, t := range targets {
				v, e := replace(t[names[i]])
				if e != nil {
					return false, e
				}
				t[names[i]] = v
			}
		}
	}
	return changed, nil
}
