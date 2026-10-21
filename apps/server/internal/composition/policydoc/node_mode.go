package policydoc

// NodeMode resolves the same inherited policy as Validate. Invalid inheritance
// fails closed; a local CODE label cannot escape a parent's maximum mode.
func NodeMode(doc map[string]any, id string, project map[string]any) (Mode, bool) {
	mode, _, ok := NodeZone(doc, id, project)
	return mode, ok
}

// NodeZone — the effective mode of a node and the id of the nearest node
// declaring a zone ("" — the document or project level).
func NodeZone(doc map[string]any, id string, project map[string]any) (Mode, string, bool) {
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
	var walk func(string, policy, string) (Mode, string, bool)
	walk = func(current string, inherited policy, zone string) (Mode, string, bool) {
		if seen[current] {
			return Strict, "", false
		}
		seen[current] = true
		n := obj(obj(doc["nodes"])[current])
		if n == nil {
			return Strict, "", false
		}
		next := c.child(inherited, obj(n["zone"]), "/nodes/"+current+"/zone", current)
		if obj(n["zone"]) != nil {
			zone = current
		}
		if current == id {
			return next.mode, zone, c.out.Valid
		}
		for _, child := range array(n["children"]) {
			if key, ok := child.(string); ok {
				if m, z, found := walk(key, next, zone); found {
					return m, z, true
				}
			}
		}
		for _, slot := range keys(obj(n["slots"])) {
			for _, child := range array(obj(n["slots"])[slot]) {
				if key, ok := child.(string); ok {
					if m, z, found := walk(key, next, zone); found {
						return m, z, true
					}
				}
			}
		}
		return Strict, "", false
	}
	root, _ := doc["root"].(string)
	return walk(root, p, "")
}
