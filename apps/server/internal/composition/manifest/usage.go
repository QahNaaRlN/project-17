package manifest

import (
	"slices"
	"strings"
)

// Usage связывает элемент реестра с местом использования в документе (MF-020).
// Это отбор потенциально затронутых документов, а не диагностика или решение MF-022.
type Usage struct {
	ManifestPointer string `json:"manifestPointer"`
	DocumentPointer string `json:"documentPointer"`
	Indirect        bool   `json:"indirect,omitempty"`
}

// DocumentUses собирает прямые ссылки IR и зависимости типов активного manifest.
// Ссылка на компонент охватывает весь его контракт: точную применимость изменения
// проверяют L2–L7. Литералы, имена узлов и тексты не интерпретируются как ссылки.
// Входы — разобранные JSON; неверная форма безопасно игнорируется, L1 проверяется отдельно.
func DocumentUses(document, active any) []Usage {
	c := usageCollector{manifest: object(active), seen: map[Usage]bool{}, expanded: map[string]bool{}}
	doc := object(document)
	if _, ok := doc["irVersion"]; ok {
		c.add("/irVersions", "/irVersion", false)
	}
	if schema, ok := object(doc["content"])["schema"].(string); ok {
		c.reference("schemas", schema, "/content/schema", false)
	}
	c.types(doc["inputs"], "/inputs", false)
	for name, raw := range object(doc["dataSources"]) {
		p := Pointer("dataSources", name)
		source := object(raw)
		c.reference("dataSources", text(source["source"]), p+"/source", false)
		c.bindings(source["params"], p+"/params")
	}
	for id, raw := range object(doc["nodes"]) {
		node := object(raw)
		p := Pointer("nodes", id)
		typ := text(node["type"])
		registry := "components"
		if _, ok := object(c.manifest["primitives"])[typ]; ok {
			registry = "primitives"
		}
		// Builtin primitives remain usable with an empty manifest and cannot be removed from it.
		if !slices.Contains(BuiltinPrimitives, typ) {
			c.reference(registry, typ, p+"/type", false)
		}
		c.bindings(node["bindings"], p+"/bindings")
		c.bindings(node["when"], p+"/when")
		c.actions(node["on"], p+"/on")
		c.design(node["design"], p+"/design")
		props := object(node["props"])
		def := object(object(c.manifest[registry])[typ])
		for name, value := range props {
			path := p + "/props" + Pointer(name)
			if object(object(def["props"])[name])["responsive"] == true {
				c.responsive(value, path)
			}
			if text(object(object(def["props"])[name])["type"]) == "color" {
				c.tokenValues(value, "colors", path)
			}
		}
		if typ == "Icon" {
			if _, ok := props["name"]; ok {
				c.add("/icons", p+"/props/name", false)
			}
		}
	}
	out := make([]Usage, 0, len(c.seen))
	for usage := range c.seen {
		out = append(out, usage)
	}
	slices.SortFunc(out, func(a, b Usage) int {
		if n := compareUTF16(a.ManifestPointer, b.ManifestPointer); n != 0 {
			return n
		}
		if n := compareUTF16(a.DocumentPointer, b.DocumentPointer); n != 0 {
			return n
		}
		if a.Indirect == b.Indirect {
			return 0
		}
		if a.Indirect {
			return 1
		}
		return -1
	})
	return out
}

// UsedChanges возвращает изменения контрактов используемых элементов, включая
// совместимые и визуальные. Результат не доказывает, что документ стал невалидным.
func UsedChanges(changes []Change, usages []Usage) []Change {
	out := []Change{}
	for _, change := range changes {
		for _, usage := range usages {
			if change.Pointer == usage.ManifestPointer || strings.HasPrefix(change.Pointer, usage.ManifestPointer+"/") || strings.HasPrefix(usage.ManifestPointer, change.Pointer+"/") {
				out = append(out, change)
				break
			}
		}
	}
	slices.SortFunc(out, func(a, b Change) int { return compareUTF16(a.Pointer, b.Pointer) })
	return out
}

type usageCollector struct {
	manifest obj
	seen     map[Usage]bool
	expanded map[string]bool
}

func text(value any) string                              { s, _ := value.(string); return s }
func (c *usageCollector) add(m, d string, indirect bool) { c.seen[Usage{m, d, indirect}] = true }
func (c *usageCollector) reference(registry, name, path string, indirect bool) {
	if name == "" {
		return
	}
	m := Pointer(registry, name)
	c.add(m, path, indirect)
	key := m + "\x00" + path
	if c.expanded[key] {
		return
	}
	c.expanded[key] = true
	def := object(object(c.manifest[registry])[name])
	for _, field := range []string{"props", "provides", "args", "params", "fields"} {
		c.types(def[field], path, true)
	}
	c.typeValue(def["result"], path, true)
	for _, event := range object(def["events"]) {
		c.types(object(event)["payload"], path, true)
	}
	for _, capability := range array(def["capabilities"]) {
		c.reference("capabilities", text(capability), path, true)
	}
}
func array(value any) []any { a, _ := value.([]any); return a }
func (c *usageCollector) types(value any, path string, indirect bool) {
	for name, field := range object(value) {
		p := path
		if !indirect {
			p += Pointer(name)
		}
		c.typeValue(field, p, indirect)
	}
}
func (c *usageCollector) typeValue(value any, path string, indirect bool) {
	def := object(value)
	switch def["type"] {
	case "reference":
		c.reference("schemas", text(def["schema"]), path, indirect)
	case "object":
		fieldPath := path
		if !indirect {
			fieldPath += "/fields"
		}
		c.types(def["fields"], fieldPath, indirect)
	case "list":
		itemPath := path
		if !indirect {
			itemPath += "/of"
		}
		c.typeValue(def["of"], itemPath, indirect)
	}
}
func (c *usageCollector) bindings(value any, path string) {
	// Only actual binding objects can declare formatters; lit/default/template are content.
	switch v := value.(type) {
	case []any:
		for i, item := range v {
			c.bindings(item, path+Pointer(i))
		}
	case map[string]any:
		if _, literal := v["lit"]; literal {
			return
		}
		if format := object(v["format"]); format != nil {
			c.reference("formatters", text(format["fn"]), path+"/format/fn", false)
		}
		for key, item := range v {
			if key != "default" && key != "template" && key != "format" {
				c.bindings(item, path+Pointer(key))
			}
		}
	}
}
func (c *usageCollector) actions(value any, path string) {
	switch v := value.(type) {
	case []any:
		for i, item := range v {
			c.actions(item, path+Pointer(i))
		}
	case map[string]any:
		if action, ok := v["action"].(string); ok {
			c.reference("actions", action, path+"/action", false)
			c.bindings(v["args"], path+"/args")
			return
		}
		for key, item := range v {
			c.actions(item, path+Pointer(key))
		}
	}
}
func (c *usageCollector) responsive(value any, path string) {
	v := object(value)
	if _, ok := v["base"]; !ok {
		return
	}
	for bp := range v {
		if bp != "base" {
			c.add(Pointer("breakpoints", bp), path+Pointer(bp), false)
		}
	}
}
func (c *usageCollector) tokenValues(value any, category, path string) {
	if name, ok := value.(string); ok {
		c.add(Pointer("tokens", category, name), path, false)
		if category == "typography" {
			token := object(object(object(c.manifest["tokens"])[category])[name])
			sizes := object(token["fontSize"])
			if _, ok := sizes["base"]; ok {
				for bp := range sizes {
					if bp != "base" {
						c.add(Pointer("breakpoints", bp), path, true)
					}
				}
			}
		}
		return
	}
	v := object(value)
	if _, raw := v["raw"]; raw {
		return
	}
	for bp, item := range v {
		c.tokenValues(item, category, path+Pointer(bp))
	}
}
func (c *usageCollector) design(value any, path string) {
	categories := map[string]string{"gap": "spacing", "rowGap": "spacing", "columnGap": "spacing", "marginTop": "spacing", "marginBottom": "spacing", "marginLeft": "spacing", "marginRight": "spacing", "marginX": "spacing", "top": "spacing", "right": "spacing", "bottom": "spacing", "left": "spacing", "background": "colors", "color": "colors", "borderColor": "colors", "outline": "colors", "typography": "typography", "radius": "radius", "borderWidth": "borderWidth", "shadow": "shadow", "zIndex": "layer", "transition": "transition", "maxWidth": "container"}
	for name, val := range object(value) {
		p := path + Pointer(name)
		if name == "states" {
			for state, style := range object(val) {
				c.design(style, p+Pointer(state))
			}
			continue
		}
		if name == "fontWeight" {
			c.add("/tokens/typography", p, false)
		}
		c.responsive(val, p)
		category := categories[name]
		if strings.HasPrefix(name, "padding") {
			category = "spacing"
		}
		if text(val) == "auto" && slices.Contains([]string{"marginLeft", "marginRight", "marginX"}, name) {
			continue
		}
		if category != "" {
			c.tokenValues(val, category, p)
		}
	}
}
