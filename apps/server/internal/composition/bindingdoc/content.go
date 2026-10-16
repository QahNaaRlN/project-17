package bindingdoc

// SimpleContentCompatible does not introduce implicit scalar or asset coercion.
// Complex legacy expressions keep the existing formatter/template contract.
func SimpleContentCompatible(from, to map[string]any) bool {
	return simpleCompatible(from, to, 0)
}

func simpleCompatible(from, to map[string]any, depth int) bool {
	if depth > 8 || from == nil || to == nil {
		return false
	}
	f, t := from["type"], to["type"]
	if f != t && !((t == "text" || t == "string") && (f == "text" || f == "string")) {
		return false
	}
	if !compatible(from, to, 0) {
		return false
	}
	if t == "object" && f == "object" {
		for key := range obj(from["fields"]) {
			if _, ok := obj(to["fields"])[key]; !ok {
				return false
			}
		}
		for key, value := range obj(to["fields"]) {
			target := obj(value)
			source := obj(obj(from["fields"])[key])
			if source == nil && (target["required"] != true || target["default"] != nil) {
				continue
			}
			if !simpleCompatible(source, target, depth+1) {
				return false
			}
		}
	}
	if t == "list" && f == "list" {
		for _, bound := range []string{"min", "max"} {
			if limit, ok := number(to[bound]); ok {
				value, known := number(from[bound])
				if !known || (bound == "min" && value < limit) || (bound == "max" && value > limit) {
					return false
				}
			}
		}
		if to["of"] != nil {
			return simpleCompatible(obj(from["of"]), obj(to["of"]), depth+1)
		}
	}
	if t == "text" || t == "string" {
		if limit, ok := number(to["minLength"]); ok && limit > 0 {
			value, known := number(from["minLength"])
			if !known || value < limit {
				return false
			}
		}
		if limit, ok := number(to["maxLength"]); ok {
			value, known := number(from["maxLength"])
			if !known || value > limit {
				return false
			}
		}
		if to["pattern"] != nil && to["pattern"] != from["pattern"] {
			return false
		}
	}
	if t == "url" || t == "richText" {
		bounds := []string{"schemes"}
		if t == "richText" {
			bounds = []string{"marks", "blocks"}
		}
		for _, key := range bounds {
			if to[key] == nil {
				continue
			}
			provided := arr(from[key])
			if provided == nil {
				return false
			}
			for _, value := range provided {
				found := false
				for _, allowed := range arr(to[key]) {
					found = found || value == allowed
				}
				if !found {
					return false
				}
			}
		}
	}
	return true
}
