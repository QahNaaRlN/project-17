package ops

import (
	"fmt"
	"slices"
	"strings"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
)

// ExtractCandidate — свойство поддерева, которое может стать входом (§4.4).
type ExtractCandidate struct {
	NodeID        string         `json:"nodeId"`
	NodeType      string         `json:"nodeType"`
	Prop          string         `json:"prop"`
	Type          map[string]any `json:"type"`
	Current       string         `json:"current"` // none | literal | local | content | data | item | context | binding
	Content       bool           `json:"content"`
	External      bool           `json:"external"` // вход обязателен
	SuggestedName string         `json:"suggestedName"`
	Restricted    bool           `json:"restricted,omitempty"` // выражение скрыто без content.read
	Expression    any            `json:"expression,omitempty"`
}

// ExtractLocalKey — ключ $local поддерева.
type ExtractLocalKey struct {
	Key         string   `json:"key"`
	NodeIDs     []string `json:"nodeIds"`
	UsedOutside bool     `json:"usedOutside"`
}

// ExtractSlotRange — допустимый диапазон соседей для слота.
type ExtractSlotRange struct {
	ParentID string   `json:"parentId"`
	List     string   `json:"list,omitempty"`
	NodeIDs  []string `json:"nodeIds"`
	Types    []string `json:"types"`
}

// ExtractSlotPosition — контейнер для пустого слота.
type ExtractSlotPosition struct {
	ParentID string `json:"parentId"`
	MaxIndex int    `json:"maxIndex"`
}

// ExtractPlan — read-only план извлечения: подсказка Studio и MCP.
type ExtractPlan struct {
	NodeID          string                `json:"nodeId"`
	Blockers        []ir.Diagnostic       `json:"blockers"`
	Candidates      []ExtractCandidate    `json:"candidates"`
	LocalKeys       []ExtractLocalKey     `json:"localKeys"`
	SlotRanges      []ExtractSlotRange    `json:"slotRanges"`
	SlotPositions   []ExtractSlotPosition `json:"slotPositions"`
	SourceListTypes []map[string]any      `json:"sourceListTypes"`
	EffectiveMode   string                `json:"effectiveMode"`
}

// PlanExtract вычисляет план тем же кодом, что проверяет операция.
func PlanExtract(page map[string]any, nodeID string, env ExtractEnv, contentRead bool) ExtractPlan {
	plan := ExtractPlan{NodeID: nodeID, Blockers: []ir.Diagnostic{}, Candidates: []ExtractCandidate{}, LocalKeys: []ExtractLocalKey{},
		SlotRanges: []ExtractSlotRange{}, SlotPositions: []ExtractSlotPosition{}, SourceListTypes: env.SourceTypes}
	if plan.SourceListTypes == nil {
		plan.SourceListTypes = []map[string]any{}
	}
	block := func(err error) {
		if e, ok := err.(*Error); ok {
			if len(e.Diagnostics) > 0 {
				plan.Blockers = append(plan.Blockers, e.Diagnostics...)
				return
			}
			plan.Blockers = append(plan.Blockers, ir.Diagnostic{Code: ir.Code(e.Code), Severity: ir.SeverityError, Message: e.Message})
		}
	}
	x, err := analyse(page, nodeID)
	if err != nil {
		block(err)
		return plan
	}
	plan.EffectiveMode = effectiveMode(page, x.loc.ParentID, env.Project)
	if err := x.protected(); err != nil {
		block(err)
	}
	ids := sortedKeys(boolMap(x.part))
	names := map[string]int{}
	for _, id := range ids {
		n := x.node(id)
		props, _ := env.PropTypes(n)
		fields := map[string]bool{}
		for _, f := range []string{"props", "bindings"} {
			for k := range asMap(n[f]) {
				fields[k] = true
			}
		}
		for _, k := range sortedKeys(props) {
			t := asMap(props[k])
			if t == nil || structuralProp(n, k) {
				continue
			}
			c := ExtractCandidate{NodeID: id, NodeType: fmt.Sprint(n["type"]), Prop: k, Type: typeOnly(t), Current: "none"}
			c.Content, _ = t["content"].(bool)
			if b, ok := asMap(n["bindings"])[k]; ok {
				c.Current, c.External = classify(x, id, b)
				c.Expression = b
				if strings.Contains(string(mustRaw(b)), "$content") && !contentRead {
					c.Restricted, c.Expression = true, nil
				}
			} else if v, ok := asMap(n["props"])[k]; ok {
				c.Current = "literal"
				if _, ext := classify(x, id, v); ext {
					c.External = true
				}
				c.Expression = v
			} else if !fields[k] && !c.Content {
				continue // незаданные неконтентные свойства не предлагаются
			}
			base := k
			if n["type"] == "Repeat" && k == "items" {
				base = "items"
			}
			names[base]++
			c.SuggestedName = base
			if names[base] > 1 {
				c.SuggestedName = fmt.Sprintf("%s%d", base, names[base])
			}
			plan.Candidates = append(plan.Candidates, c)
		}
	}
	// $local и обязательные внешние зависимости в when/on.
	locals := map[string]string{}
	for _, id := range ids {
		n := x.node(id)
		for _, field := range []string{"when", "on"} {
			if err := x.scopes(id, n[field], ir.Pointer("nodes", id, field), true, locals); err != nil {
				block(err)
			}
		}
	}
	outside := map[string]bool{}
	for id, raw := range nodes(page) {
		if x.part[id] {
			continue
		}
		for _, field := range []string{"props", "bindings", "when", "on"} {
			strs(asMap(raw)[field], func(s string) {
				if m := scopePattern.FindStringSubmatch(s); m != nil && m[1] == "local" && m[2] != "" {
					outside[m[2]] = true
				}
			})
		}
	}
	keyNodes := map[string][]string{}
	for _, id := range ids {
		n := x.node(id)
		for _, field := range []string{"props", "bindings", "when", "on"} {
			strs(n[field], func(s string) {
				if m := scopePattern.FindStringSubmatch(s); m != nil && m[1] == "local" && m[2] != "" && !slices.Contains(keyNodes[m[2]], id) {
					keyNodes[m[2]] = append(keyNodes[m[2]], id)
				}
			})
		}
	}
	for _, key := range sortedKeys(anyMap(keyNodes)) {
		plan.LocalKeys = append(plan.LocalKeys, ExtractLocalKey{Key: key, NodeIDs: keyNodes[key], UsedOutside: outside[key]})
	}
	all := map[string]bool{}
	for id := range x.part {
		all[id] = true
	}
	if err := x.nodeRefs(all); err != nil {
		block(err)
	}
	// Диапазоны слотов: любые непрерывные группы соседей внутри поддерева (кроме корня).
	for _, id := range ids {
		n := x.node(id)
		lists := []string{""}
		for _, s := range sortedKeys(asMap(n["slots"])) {
			lists = append(lists, s)
		}
		for _, name := range lists {
			l := list(n, name)
			if len(l) == 0 {
				continue
			}
			r := ExtractSlotRange{ParentID: id, List: name}
			for _, c := range l {
				r.NodeIDs = append(r.NodeIDs, fmt.Sprint(c))
				r.Types = append(r.Types, fmt.Sprint(x.node(fmt.Sprint(c))["type"]))
			}
			plan.SlotRanges = append(plan.SlotRanges, r)
		}
		if env.Container != nil && env.Container(n) {
			plan.SlotPositions = append(plan.SlotPositions, ExtractSlotPosition{ParentID: id, MaxIndex: len(list(n, ""))})
		}
	}
	return plan
}

// classify — вид значения и признак внешней области.
func classify(x *extraction, id string, v any) (string, bool) {
	kind := "binding"
	external := false
	strs(v, func(s string) {
		m := scopePattern.FindStringSubmatch(s)
		if m == nil {
			return
		}
		switch m[1] {
		case "local":
			if kind == "binding" {
				kind = "local"
			}
		case "content", "data":
			kind, external = m[1], true
		case "item", "index":
			if !x.itemScoped(id, "") {
				kind, external = "item", true
			} else if kind == "binding" {
				kind = "item"
			}
		case "each":
			if !x.itemScoped(id, m[2]) {
				kind, external = "item", true
			}
		case "context":
			if kind == "binding" {
				kind = "context"
			}
		}
	})
	return kind, external
}

// nodeRefs — проверка ссылок на узлы через границу множества inside.
func (x *extraction) nodeRefs(inside map[string]bool) error {
	for _, id := range sortedKeys(nodes(x.page)) {
		in := inside[id]
		n := x.node(id)
		for _, field := range []string{"props", "bindings", "when", "on"} {
			var cross string
			allStrs(n[field], func(s string) {
				if cross != "" || s == id {
					return
				}
				if _, isNode := nodes(x.page)[s]; isNode && inside[s] != in {
					cross = s
				}
			})
			if cross != "" {
				return extractFail("COMPONENT_EXTRACT_NODE_REF", ir.Pointer("nodes", id, field), id, "ссылка на узел %s пересекает границу компонента", cross)
			}
		}
	}
	return nil
}
