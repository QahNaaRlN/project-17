package policydoc

// NodeMode resolves the same inherited policy as Validate. Invalid inheritance
// fails closed; a local CODE label cannot escape a parent's maximum mode.
func NodeMode(doc map[string]any, id string, project map[string]any) (Mode, bool) {
	c := checker{out: Result{}}
	c.out.Valid = true
	settings := map[string]any{}
	for k, v := range project {
		settings[k] = v
	}
	if v, ok := settings["defaultMode"]; ok {
		settings["mode"] = v
	}
	p := c.level(policy{mode: System, max: System}, settings, "/project/settings", "")
	p = c.child(p, obj(doc["policy"]), "/policy", "")
	seen := map[string]bool{}
	var walk func(string, policy) (Mode, bool)
	walk = func(current string, inherited policy) (Mode, bool) {
		if seen[current] {
			return Strict, false
		}
		seen[current] = true
		n := obj(obj(doc["nodes"])[current])
		if n == nil {
			return Strict, false
		}
		next := c.child(inherited, obj(n["zone"]), "/nodes/"+current+"/zone", current)
		if current == id {
			return next.mode, c.out.Valid
		}
		for _, child := range array(n["children"]) {
			if key, ok := child.(string); ok {
				if m, found := walk(key, next); found {
					return m, true
				}
			}
		}
		for _, slot := range keys(obj(n["slots"])) {
			for _, child := range array(obj(n["slots"])[slot]) {
				if key, ok := child.(string); ok {
					if m, found := walk(key, next); found {
						return m, true
					}
				}
			}
		}
		return Strict, false
	}
	root, _ := doc["root"].(string)
	return walk(root, p)
}
