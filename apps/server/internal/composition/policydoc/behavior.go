package policydoc

import (
	"reflect"
	"strings"
)

// Changing the primary declaration can change protected behavior/visibility
// without changing on/when JSON. Ordinary props/content bindings remain editable.
func checkBehaviorPrimaryEdits(before, after map[string]any, fail func(string, string)) {
	if reflect.DeepEqual(before["content"], after["content"]) {
		return
	}
	var uses func(any) bool
	uses = func(value any) bool {
		switch value := value.(type) {
		case string:
			return value == "$content" || strings.HasPrefix(value, "$content.")
		case map[string]any:
			for key, child := range value {
				if key != "lit" && key != "default" && key != "template" && key != "format" && uses(child) {
					return true
				}
			}
		case []any:
			for _, child := range value {
				if uses(child) {
					return true
				}
			}
		}
		return false
	}
	for _, doc := range []map[string]any{before, after} {
		seen := map[string]bool{}
		var visit func(string, bool, bool)
		visit = func(id string, locked, conditional bool) {
			if seen[id] {
				return
			}
			seen[id] = true
			n := obj(obj(doc["nodes"])[id])
			if n == nil {
				return
			}
			locked = locked || n["locked"] == true || obj(n["zone"])["locked"] == true
			conditional = conditional || uses(n["when"])
			if locked && (conditional || uses(n["on"])) {
				fail(id, "/content")
			}
			for _, child := range array(n["children"]) {
				visit(child.(string), locked, conditional)
			}
			for _, list := range obj(n["slots"]) {
				for _, child := range array(list) {
					visit(child.(string), locked, conditional)
				}
			}
		}
		root, _ := doc["root"].(string)
		visit(root, false, false)
	}
}
