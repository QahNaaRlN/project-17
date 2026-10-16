package policydoc

import (
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"reflect"
	"strings"
)

// A declaration can change locked rendered content without changing the node JSON.
func checkDataEdits(before, after map[string]any, fail func(string, string)) {
	changed := map[string]bool{}
	for key := range obj(before["dataSources"]) {
		changed[key] = !reflect.DeepEqual(obj(before["dataSources"])[key], obj(after["dataSources"])[key])
	}
	for key := range obj(after["dataSources"]) {
		changed[key] = !reflect.DeepEqual(obj(before["dataSources"])[key], obj(after["dataSources"])[key])
	}
	anyChanged := false
	for _, value := range changed {
		anyChanged = anyChanged || value
	}
	if !anyChanged {
		return
	}
	var refs func(any, map[string]bool)
	refs = func(value any, result map[string]bool) {
		switch value := value.(type) {
		case string:
			if strings.HasPrefix(value, "$data.") {
				key := strings.Split(strings.TrimPrefix(value, "$data."), ".")[0]
				if changed[key] {
					result[key] = true
				}
			}
		case map[string]any:
			for key, child := range value {
				if key != "lit" && key != "default" && key != "template" && key != "format" {
					refs(child, result)
				}
			}
		case []any:
			for _, child := range value {
				refs(child, result)
			}
		}
	}
	for _, doc := range []map[string]any{before, after} {
		seen := map[string]bool{}
		var walk func(string, bool, map[string]bool)
		walk = func(id string, locked bool, inherited map[string]bool) {
			if seen[id] {
				return
			}
			seen[id] = true
			node := obj(obj(doc["nodes"])[id])
			if node == nil {
				return
			}
			locked = locked || node["locked"] == true || obj(node["zone"])["locked"] == true
			used := map[string]bool{}
			refs(node["bindings"], used)
			refs(node["when"], used)
			refs(node["on"], used)
			for key := range inherited {
				used[key] = true
			}
			if locked {
				for key := range used {
					fail(id, ir.Pointer("dataSources", key))
				}
				if id == doc["root"] {
					for key, change := range changed {
						if change {
							fail(id, ir.Pointer("dataSources", key))
						}
					}
				}
			}
			childSources := inherited
			if node["type"] == "Repeat" {
				childSources = used
			}
			for _, child := range array(node["children"]) {
				walk(child.(string), locked, childSources)
			}
			for _, slot := range obj(node["slots"]) {
				for _, child := range array(slot) {
					walk(child.(string), locked, childSources)
				}
			}
		}
		walk(doc["root"].(string), false, map[string]bool{})
	}
}
