package bindingdoc

import (
	"regexp"
	"strings"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifestdoc"
)

// Validate accepts L1-valid documents and a validated manifest. Its diagnostics are
// publication errors; editing commands may expose them as warnings without rejecting edits.
func Validate(document, app any, options Options) ir.Result {
	c := checker{doc: obj(document), app: obj(app), options: options}
	s := scopes{"context": fields(object{"locale": typ("string"), "route": fields(object{"path": typ("string"), "params": fields(routeFields(options.Path))}), "viewer": fields(object{"authenticated": typ("boolean")})})}
	if c.doc["kind"] == "component" {
		s["props"] = fields(obj(c.doc["inputs"]))
	}
	if content := obj(c.doc["content"]); content != nil {
		name := stringValue(content["schema"])
		schema := obj(obj(c.app["schemas"])[name])
		if schema == nil {
			c.add("BINDING_PATH_UNRESOLVED", "/content/schema", "", object{"schema": name})
		} else {
			s["content"] = fields(obj(schema["fields"]))
		}
	}
	local := object{}
	for k, v := range obj(c.doc["localContent"]) {
		t := copyMap(obj(v))
		t["required"] = true
		local[k] = t
	}
	s["local"] = fields(local)
	data := object{}
	for _, key := range keys(obj(c.doc["dataSources"])) {
		d := obj(obj(c.doc["dataSources"])[key])
		def := obj(obj(c.app["dataSources"])[stringValue(d["source"])])
		p := ir.Pointer("dataSources", key)
		if def == nil {
			c.add("BINDING_PATH_UNRESOLVED", p+"/source", "", object{"source": d["source"]})
			continue
		}
		c.arguments(obj(d["params"]), obj(def["params"]), p+"/params", "", s, "BINDING_TYPE_MISMATCH")
		result := copyMap(obj(def["result"]))
		result["required"] = true
		if def["paginated"] == true {
			result = fields(object{"items": result, "hasMore": typ("boolean")})
		}
		data[key] = result
	}
	s["data"] = fields(data)
	seen := map[string]bool{}
	var visit func(string, scopes, int)
	visit = func(id string, s scopes, depth int) {
		if seen[id] || depth > 128 {
			return
		}
		seen[id] = true
		n := obj(obj(c.doc["nodes"])[id])
		if n == nil {
			return
		}
		p := ir.Pointer("nodes", id)
		def := manifest.BuiltinContract(stringValue(n["type"]))
		if def == nil {
			def = obj(obj(c.app["primitives"])[stringValue(n["type"])])
		}
		if def == nil {
			def = obj(obj(c.app["components"])[stringValue(n["type"])])
		}
		if n["type"] == "Composed" && options.ResolveComponent != nil {
			child, ok := options.ResolveComponent(obj(n["ref"]))
			if ok {
				def = object{"props": child["inputs"]}
			}
		}
		c.predicate(obj(n["when"]), p+"/when", id, s, 0)
		var items object
		for _, k := range keys(obj(n["bindings"])) {
			source := c.binding(obj(n["bindings"])[k], obj(obj(def["props"])[k]), p+ir.Pointer("bindings", k), id, s, 0)
			if k == "items" && n["type"] == "Repeat" {
				items = source
			}
		}
		for _, event := range keys(obj(n["on"])) {
			raw := obj(n["on"])[event]
			actions := arr(raw)
			if actions == nil {
				actions = []any{raw}
			}
			for i, a := range actions {
				ap := p + ir.Pointer("on", event)
				if arr(raw) != nil {
					ap += ir.Pointer(i)
				}
				c.action(obj(a), ap, id, s)
			}
		}
		childScopes := clone(s)
		contextFields := copyMap(obj(s["context"]["fields"]))
		for k, v := range obj(def["provides"]) {
			contextFields[k] = v
		}
		childScopes["context"] = fields(contextFields)
		if n["type"] == "Repeat" && items != nil && items["type"] == "list" {
			childScopes["item"] = copyMap(obj(items["of"]))
			childScopes["item"]["required"] = true
			childScopes["index"] = typ("number")
			aliases := copyMap(obj(s["each"]["fields"]))
			if alias := stringValue(obj(n["props"])["as"]); alias != "" {
				aliases[alias] = childScopes["item"]
			}
			childScopes["each"] = fields(aliases)
			if key := stringValue(obj(n["props"])["key"]); key != "" {
				c.path("$item."+key, p+"/props/key", id, childScopes)
			}
		}
		for _, child := range arr(n["children"]) {
			visit(stringValue(child), childScopes, depth+1)
		}
		for _, slot := range keys(obj(n["slots"])) {
			slotScopes := childScopes
			if n["type"] == "Repeat" && slot == "empty" {
				slotScopes = s
			}
			for _, child := range arr(obj(n["slots"])[slot]) {
				visit(stringValue(child), slotScopes, depth+1)
			}
		}
	}
	visit(stringValue(c.doc["root"]), s, 0)
	return ir.Result{Valid: len(c.out) == 0, Diagnostics: c.out}
}

func (c *checker) arguments(values, contracts object, p, id string, s scopes, code string) {
	for _, k := range keys(contracts) {
		t := obj(contracts[k])
		if _, ok := values[k]; !ok && t["required"] == true && t["default"] == nil {
			c.add(code, p+ir.Pointer(k), id, object{"required": k})
		}
	}
	for _, k := range keys(values) {
		t := obj(contracts[k])
		if t == nil {
			c.add(code, p+ir.Pointer(k), id, object{"unknown": k})
			continue
		}
		c.value(values[k], t, p+ir.Pointer(k), id, s, code)
	}
}

func (c *checker) binding(v any, target object, p, id string, s scopes, depth int) object {
	if target == nil {
		return nil
	} // L3 reports unknown properties.
	if depth > 32 {
		c.add("BINDING_DEPTH_EXCEEDED", p, id, nil)
		return nil
	}
	if expr, ok := v.(string); ok {
		return c.value(expr, target, p, id, s, "BINDING_TYPE_MISMATCH")
	}
	b := obj(v)
	if _, ok := b["template"]; ok {
		for _, match := range templateVariable.FindAllStringSubmatch(stringValue(b["template"]), -1) {
			if _, ok := obj(b["vars"])[match[1]]; !ok {
				c.add("BINDING_PATH_UNRESOLVED", p+"/template", id, object{"variable": match[1]})
			}
		}
		for _, k := range keys(obj(b["vars"])) {
			c.binding(obj(b["vars"])[k], object{"type": "string"}, p+ir.Pointer("vars", k), id, s, depth+1)
		}
		if !compatible(typ("string"), target, 0) {
			c.add("BINDING_TYPE_MISMATCH", p, id, object{"expected": target})
		}
		return typ("string")
	}
	source := c.path(stringValue(b["expr"]), p+"/expr", id, s)
	if source == nil {
		return nil
	}
	if fallback, ok := b["default"]; ok {
		if !manifestdoc.ValidateValue(c.doc, c.app, target, fallback) {
			c.add("BINDING_TYPE_MISMATCH", p+"/default", id, object{"expected": target})
		} else {
			source = copyMap(source)
			source["required"] = true
		}
	}
	if format := obj(b["format"]); format != nil {
		source = c.format(source, format, p+"/format", id, s)
	}
	if source != nil && !compatible(source, target, 0) {
		c.add("BINDING_TYPE_MISMATCH", p, id, object{"expected": target, "actual": source})
	}
	return source
}

func (c *checker) format(source, format object, p, id string, s scopes) object {
	fn := stringValue(format["fn"])
	def := obj(obj(c.app["formatters"])[fn])
	if def == nil {
		def = builtinFormatter(fn)
	}
	if def == nil {
		c.add("BINDING_PATH_UNRESOLVED", p+"/fn", id, object{"formatter": fn})
		return nil
	}
	accepted := false
	for _, t := range arr(def["input"]) {
		if t == source["type"] {
			accepted = true
		}
	}
	if !accepted {
		c.add("BINDING_TYPE_MISMATCH", p, id, object{"formatter": fn, "actual": source})
	}
	// Formatter arguments are literal JSON, unlike action/data-source operands.
	args := object{}
	for k, v := range obj(format["args"]) {
		args[k] = object{"lit": v}
	}
	c.arguments(args, obj(def["args"]), p+"/args", id, s, "BINDING_TYPE_MISMATCH")
	result := typ("string")
	if source["required"] != true && source["default"] == nil {
		delete(result, "required")
	}
	return result
}
func builtinFormatter(fn string) object {
	def := object{"input": []any{"string", "text"}, "args": object{}}
	enum := func(values ...any) object { return object{"type": "enum", "values": values} }
	switch fn {
	case "uppercase", "lowercase":
	case "date":
		def["input"] = []any{"date", "datetime"}
		def["args"] = object{"style": enum("short", "medium", "long", "iso")}
	case "number":
		def["input"] = []any{"number"}
		def["args"] = object{"style": enum("decimal", "percent"), "minFraction": object{"type": "number", "integer": true, "min": 0.0, "max": 100.0}, "maxFraction": object{"type": "number", "integer": true, "min": 0.0, "max": 100.0}}
	case "currency":
		def["input"] = []any{"number"}
		def["args"] = object{"code": typ("string")}
	case "truncate":
		def["args"] = object{"length": object{"type": "number", "integer": true, "min": 0.0, "required": true}}
	default:
		return nil
	}
	return def
}

var templateVariable = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func (c *checker) predicate(pred object, p, id string, s scopes, depth int) {
	if pred == nil {
		return
	}
	if depth > 8 {
		c.add("BINDING_DEPTH_EXCEEDED", p, id, nil)
		return
	}
	op := stringValue(pred["op"])
	args := arr(pred["args"])
	switch op {
	case "and", "or":
		for i, a := range args {
			c.predicate(obj(a), p+ir.Pointer("args", i), id, s, depth+1)
		}
	case "not":
		c.predicate(obj(pred["arg"]), p+"/arg", id, s, depth+1)
	case "exists", "empty":
		c.path(stringValue(pred["arg"]), p+"/arg", id, s)
	default:
		if len(args) != 2 {
			return
		}
		a := c.operand(args[0], p+"/args/0", id, s)
		b := c.operand(args[1], p+"/args/1", id, s)
		if op == "eq" || op == "neq" {
			for _, v := range args {
				if literal, ok := obj(v)["lit"]; ok && literal == nil {
					return
				}
			}
		}
		if literal, ok := obj(args[1])["lit"]; ok {
			b = c.literalType(literal, a)
		}
		if literal, ok := obj(args[0])["lit"]; ok {
			a = c.literalType(literal, b)
		}
		if op == "in" {
			if a != nil {
				for i, value := range arr(obj(args[1])["lit"]) {
					left := copyMap(a)
					left["required"] = true
					if !sameType(left, c.literalType(value, left)) {
						c.add("BINDING_TYPE_MISMATCH", p+ir.Pointer("args", 1, "lit", i), id, object{"op": op})
					}
				}
			}
			b = obj(b["of"])
			if values := arr(obj(args[1])["lit"]); len(values) > 0 {
				b = c.literalType(values[0], a)
			}
		}
		if a == nil || b == nil {
			return
		}
		a = copyMap(a)
		b = copyMap(b)
		a["required"] = true
		b["required"] = true
		ordered := op == "gt" || op == "gte" || op == "lt" || op == "lte"
		if (ordered && a["type"] != "number" && a["type"] != "date" && a["type"] != "datetime") || !sameType(a, b) {
			c.add("BINDING_TYPE_MISMATCH", p, id, object{"op": op})
		}
	}
}
func (c *checker) literalType(value any, other object) object {
	if other["type"] == "date" || other["type"] == "datetime" {
		if manifestdoc.ValidateValue(c.doc, c.app, other, value) {
			return typ(stringValue(other["type"]))
		}
	}
	return infer(value)
}
func sameType(a, b object) bool {
	if a["type"] == "number" && b["type"] == "number" {
		return true
	}
	if (a["type"] == "enum" && b["type"] == "string") || (a["type"] == "string" && b["type"] == "enum") {
		return true
	}
	if a["type"] == b["type"] {
		return compatible(a, b, 0) && compatible(b, a, 0)
	}
	return (a["type"] == "string" && b["type"] == "text") || (a["type"] == "text" && b["type"] == "string")
}
func (c *checker) operand(v any, p, id string, s scopes) object {
	if expr, ok := v.(string); ok {
		return c.path(expr, p, id, s)
	}
	return infer(obj(v)["lit"])
}

func (c *checker) action(a object, p, id string, s scopes) {
	name := stringValue(a["action"])
	args := obj(a["args"])
	if strings.HasPrefix(name, "pending:") {
		c.add("ACTION_PENDING_CAPABILITY", p+"/action", id, nil)
		return
	}
	contracts := builtinAction(name)
	if contracts == nil {
		def := obj(obj(c.app["actions"])[name])
		if def == nil {
			c.add("ACTION_UNKNOWN", p+"/action", id, object{"action": name})
			return
		}
		contracts = obj(def["args"])
	}
	// Link literals contain expression-valued route params; validate separately.
	values := copyMap(args)
	if name == "navigate" {
		if to := obj(args["to"]); to != nil {
			if lit, ok := to["lit"]; ok {
				to = obj(lit)
			}
			c.link(to, p+"/args/to", id, s)
			delete(values, "to")
			contracts = copyMap(contracts)
			delete(contracts, "to")
		}
	}
	if name == "track" {
		if props, ok := values["props"]; ok {
			if expr, ok := props.(string); ok {
				c.value(expr, typ("object"), p+"/args/props", id, s, "ACTION_ARGS_INVALID")
			} else {
				if obj(obj(props)["lit"]) == nil {
					c.add("ACTION_ARGS_INVALID", p+"/args/props", id, nil)
				}
			}
			delete(values, "props")
		}
	}
	c.arguments(values, contracts, p+"/args", id, s, "ACTION_ARGS_INVALID")
	if name == "data.loadMore" {
		source := stringValue(obj(args["source"])["lit"])
		d := obj(obj(c.doc["dataSources"])[source])
		def := obj(obj(c.app["dataSources"])[stringValue(d["source"])])
		if source == "" || def["paginated"] != true {
			c.add("ACTION_ARGS_INVALID", p+"/args/source", id, nil)
		}
	}
}
func builtinAction(name string) object {
	switch name {
	case "navigate":
		return object{"to": typ("link")}
	case "openModal", "closeModal":
		t := typ("nodeRef")
		t["nodeType"] = "Modal"
		return object{"target": t}
	case "scrollTo":
		return object{"target": typ("nodeRef")}
	case "track":
		return object{"event": typ("string"), "props": object{"type": "object"}}
	case "data.loadMore":
		return object{"source": typ("string")}
	default:
		return nil
	}
}
func (c *checker) link(link object, p, id string, s scopes) {
	clean := copyMap(link)
	delete(clean, "params")
	if !manifestdoc.ValidateValue(c.doc, c.app, typ("link"), clean) {
		c.add("ACTION_ARGS_INVALID", p, id, nil)
		return
	}
	if link["kind"] == "page" {
		if c.options.ResolvePage == nil {
			c.add("ACTION_ARGS_INVALID", p+"/page", id, nil)
			return
		}
		path, ok := c.options.ResolvePage(stringValue(link["page"]))
		if !ok {
			c.add("ACTION_ARGS_INVALID", p+"/page", id, nil)
			return
		}
		c.arguments(obj(link["params"]), routeFields(path), p+"/params", id, s, "ACTION_ARGS_INVALID")
	} else if len(obj(link["params"])) > 0 {
		c.add("ACTION_ARGS_INVALID", p+"/params", id, nil)
	}
}
