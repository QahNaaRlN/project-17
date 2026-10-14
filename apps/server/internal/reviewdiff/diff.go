package reviewdiff

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

type Value struct {
	Present  bool    `json:"present"`
	Value    any     `json:"value,omitempty"`
	RawJSON  *string `json:"rawJson,omitempty"`
	Redacted bool    `json:"redacted,omitempty"`
	Reason   string  `json:"reason,omitempty"`
}

// MarshalJSON preserves a present JSON null independently of absent values.
func (v Value) MarshalJSON() ([]byte, error) {
	if !v.Present {
		return []byte(`{"present":false}`), nil
	}
	if v.Redacted {
		return []byte(`{"present":true,"redacted":true,"reason":"service-field"}`), nil
	}
	if v.RawJSON != nil {
		return json.Marshal(map[string]any{"present": true, "rawJson": *v.RawJSON})
	}
	return json.Marshal(map[string]any{"present": true, "value": v.Value})
}

type Change struct {
	Key      string `json:"key"`
	Category string `json:"category"`
	Kind     string `json:"kind"`
	Pointer  string `json:"pointer"`
	NodeID   string `json:"nodeId,omitempty"`
	Locale   string `json:"locale,omitempty"`
	Before   Value  `json:"before"`
	After    Value  `json:"after"`
}
type Summary struct {
	Effect      string `json:"effect"`
	ChangeCount int    `json:"changeCount"`
}
type Image struct {
	Body    map[string]any
	Meta    map[string]any
	Deleted bool
}

func decode(raw []byte) (map[string]any, error) {
	var out map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	err := d.Decode(&out)
	return out, err
}
func rat(n json.Number) *big.Rat { r, _ := new(big.Rat).SetString(string(n)); return r }
func canonical(v any) string {
	switch x := v.(type) {
	case json.Number:
		if r := rat(x); r != nil {
			return "n:" + r.RatString()
		}
	case map[string]any:
		keys := sortedKeys(x)
		var b strings.Builder
		b.WriteString("o:")
		for _, k := range keys {
			raw, _ := json.Marshal(k)
			b.Write(raw)
			b.WriteString(canonical(x[k]))
			b.WriteByte(';')
		}
		return b.String()
	case []any:
		var b strings.Builder
		b.WriteString("a:")
		for _, item := range x {
			b.WriteString(canonical(item))
			b.WriteByte(';')
		}
		return b.String()
	}
	raw, _ := json.Marshal(v)
	return "v:" + string(raw)
}
func hash(v any) string {
	s := sha256.Sum256([]byte(canonical(v)))
	return "sha256:" + hex.EncodeToString(s[:])
}
func sortedKeys(v map[string]any) []string {
	keys := make([]string, 0, len(v))
	for key := range v {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func unsafeNumber(v any) bool {
	switch x := v.(type) {
	case json.Number:
		f, err := strconv.ParseFloat(string(x), 64)
		if err != nil {
			return true
		}
		r := rat(json.Number(strconv.FormatFloat(f, 'g', -1, 64)))
		return r == nil || rat(x) == nil || rat(x).Cmp(r) != 0
	case map[string]any:
		for _, y := range x {
			if unsafeNumber(y) {
				return true
			}
		}
	case []any:
		for _, y := range x {
			if unsafeNumber(y) {
				return true
			}
		}
	}
	return false
}
func value(v any, present bool, redact bool) Value {
	out := Value{Present: present, Value: v}
	if present && redact {
		out.Value = nil
		out.Redacted = true
		out.Reason = "service-field"
	} else if present && unsafeNumber(v) {
		raw, _ := json.Marshal(v)
		text := string(raw)
		out.Value = nil
		out.RawJSON = &text
	}
	return out
}
func pointerPart(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}
func category(kind, path string) string {
	if strings.HasPrefix(path, "/@version") {
		return "metadata"
	}
	if kind == "asset" {
		if strings.Contains(strings.ToLower(path), "file") || strings.Contains(strings.ToLower(path), "hash") || strings.Contains(strings.ToLower(path), "storage") {
			return "file"
		}
		return "metadata"
	}
	if kind == "entity" {
		return "content"
	}
	for _, p := range []struct{ part, cat string }{{"/design", "design"}, {"/bindings", "binding"}, {"/when", "condition"}, {"/behavior", "behavior"}, {"/on/", "behavior"}, {"/localContent", "localContent"}, {"/children", "structure"}, {"/slots", "structure"}, {"/nodes", "property"}} {
		if strings.Contains(path, p.part) {
			return p.cat
		}
	}
	return "metadata"
}
func secretKey(key string) bool {
	k := strings.ToLower(strings.NewReplacer("_", "", "-", "", "~1", "/").Replace(key))
	return strings.Contains(k, "storage") || strings.Contains(k, "objectkey") || strings.Contains(k, "bucket") || strings.Contains(k, "credential") || strings.Contains(k, "password") || strings.Contains(k, "secret") || strings.Contains(k, "token") || strings.Contains(k, "url") || strings.Contains(k, "authorization")
}
func redactAsset(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, y := range x {
			if secretKey(k) {
				out[k] = "[service-field]"
			} else {
				out[k] = redactAsset(y)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, y := range x {
			out[i] = redactAsset(y)
		}
		return out
	}
	return v
}
func positions(body map[string]any) map[string]any {
	out := map[string]any{}
	nodes, _ := body["nodes"].(map[string]any)
	for _, parent := range sortedKeys(nodes) {
		node, _ := nodes[parent].(map[string]any)
		attach := func(slot string, v any) {
			items, _ := v.([]any)
			for i, child := range items {
				if id, ok := child.(string); ok {
					out[id] = map[string]any{"parentId": parent, "slot": slot, "index": i}
				}
			}
		}
		attach("children", node["children"])
		slots, _ := node["slots"].(map[string]any)
		for _, slot := range sortedKeys(slots) {
			attach(slot, slots[slot])
		}
	}
	return out
}
func Diff(kind string, before, after *Image) ([]Change, Summary) {
	result := []Change{}
	add := func(path, cat, node, locale, op string, a any, ap bool, b any, bp bool, redact bool) {
		if kind == "asset" && !redact {
			a = redactAsset(a)
			b = redactAsset(b)
		}
		result = append(result, Change{Category: cat, Kind: op, Pointer: path, NodeID: node, Locale: locale, Before: value(a, ap, redact), After: value(b, bp, redact)})
	}
	var walk func(string, any, bool, any, bool, string, string)
	walk = func(path string, a any, ap bool, b any, bp bool, node, locale string) {
		if ap == bp && canonical(a) == canonical(b) {
			return
		}
		if kind == "asset" && secretKey(path[strings.LastIndex(path, "/")+1:]) {
			add(path, category(kind, path), node, locale, "replace", a, ap, b, bp, true)
			return
		}
		ma, aMap := a.(map[string]any)
		mb, bMap := b.(map[string]any)
		if aMap && bMap {
			union := map[string]any{}
			for k := range ma {
				union[k] = nil
			}
			for k := range mb {
				union[k] = nil
			}
			for _, k := range sortedKeys(union) {
				av, aok := ma[k]
				bv, bok := mb[k]
				loc := locale
				if strings.HasPrefix(path, "/localContent/") && strings.HasSuffix(path, "/value") {
					loc = k
				}
				walk(path+"/"+pointerPart(k), av, aok, bv, bok, node, loc)
			}
			return
		}
		op := "replace"
		if !ap {
			op = "add"
		} else if !bp {
			op = "remove"
		}
		redact := kind == "asset" && secretKey(path[strings.LastIndex(path, "/")+1:])
		add(path, category(kind, path), node, locale, op, a, ap, b, bp, redact)
	}
	aBody, bBody := map[string]any{}, map[string]any{}
	aMeta, bMeta := map[string]any{}, map[string]any{}
	if before != nil {
		aBody = before.Body
		aMeta = before.Meta
	}
	if after != nil {
		bBody = after.Body
		bMeta = after.Meta
	}
	// Deleted bodies are retained for inspection, not reconstructed from delivery/head.
	if kind == "document" {
		an, _ := aBody["nodes"].(map[string]any)
		bn, _ := bBody["nodes"].(map[string]any)
		pa, pb := positions(aBody), positions(bBody)
		union := map[string]any{}
		for id := range an {
			union[id] = nil
		}
		for id := range bn {
			union[id] = nil
		}
		for _, id := range sortedKeys(union) {
			a, ap := an[id]
			b, bp := bn[id]
			path := "/nodes/" + pointerPart(id)
			if !ap || !bp {
				add(path, "structure", id, "", map[bool]string{true: "add", false: "remove"}[bp], a, ap, b, bp, false)
			} else {
				walk(path, a, true, b, true, id, "")
			}
			av, aok := pa[id]
			bv, bok := pb[id]
			if ap && bp && (aok != bok || canonical(av) != canonical(bv)) {
				add(path+"/@position", "structure", id, "", "move", av, aok, bv, bok, false)
			}
		}
		aCopy, bCopy := map[string]any{}, map[string]any{}
		for k, v := range aBody {
			if k != "nodes" {
				aCopy[k] = v
			}
		}
		for k, v := range bBody {
			if k != "nodes" {
				bCopy[k] = v
			}
		}
		walk("", aCopy, true, bCopy, true, "", "")
	} else {
		walk("", aBody, before != nil, bBody, after != nil, "", "")
	}
	walk("/@version", aMeta, before != nil, bMeta, after != nil, "", "")
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		return a.Category+"\x00"+a.NodeID+"\x00"+a.Locale+"\x00"+a.Pointer+"\x00"+a.Kind < b.Category+"\x00"+b.NodeID+"\x00"+b.Locale+"\x00"+b.Pointer+"\x00"+b.Kind
	})
	for i := range result {
		result[i].Key = strconv.Itoa(i)
	}
	aliveA, aliveB := before != nil && !before.Deleted, after != nil && !after.Deleted
	effect := "unchanged"
	if aliveA && !aliveB {
		effect = "deleted"
	} else if !aliveA && aliveB {
		effect = "added"
		if before != nil {
			effect = "restored"
		}
	} else if len(result) > 0 {
		effect = "modified"
		if !aliveA && !aliveB || canonical(aBody) == canonical(bBody) {
			effect = "metadata-only"
		}
	}
	return result, Summary{Effect: effect, ChangeCount: len(result)}
}
