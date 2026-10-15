package bindingdoc

// SimpleContentCompatible does not introduce implicit scalar or asset coercion.
// Complex legacy expressions keep the existing formatter/template contract.
func SimpleContentCompatible(from, to map[string]any) bool {
	f, t := from["type"], to["type"]
	if (t == "text" || t == "string") && f != "text" && f != "string" {
		return false
	}
	if !compatible(from, to, 0) {
		return false
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
	return true
}
