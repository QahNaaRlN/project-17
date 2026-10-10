package manifestdoc

import "slices"

// RichText v1 shape and the declared block/mark subsets (05 §3).
func (c *checker) richText(n, t object, depth int) bool {
	if depth > 32 {
		return false
	}
	kind, _ := n["type"].(string)
	blocks := []string{"paragraph", "heading", "bulletList", "orderedList", "listItem", "blockquote", "image", "divider"}
	inline := []string{"text", "hardBreak"}
	if kind != "doc" && !slices.Contains(blocks, kind) && !slices.Contains(inline, kind) {
		return false
	}
	if kind != "doc" && slices.Contains(blocks, kind) && t["blocks"] != nil && !slices.Contains(array(t["blocks"]), any(kind)) {
		return false
	}
	attrs := asObject(n["attrs"])
	if kind == "heading" {
		level, ok := numeric(attrs["level"])
		if !ok || level < 2 || level > 4 || level != float64(int(level)) {
			return false
		}
	}
	if kind == "image" {
		id, _ := attrs["assetId"].(string)
		if !uuid.MatchString(id) {
			return false
		}
		if alt, ok := attrs["alt"]; ok {
			if !c.value(object{"type": "text", "required": true}, alt, 0) {
				return false
			}
		}
	}
	if kind == "text" {
		if !c.value(object{"type": "text", "required": true}, n["text"], 0) {
			return false
		}
		if marks, exists := n["marks"]; exists {
			list, ok := marks.([]any)
			if !ok {
				return false
			}
			for _, mark := range list {
				m := asObject(mark)
				name, _ := m["type"].(string)
				if !slices.Contains([]string{"bold", "italic", "underline", "strike", "code", "link"}, name) || (t["marks"] != nil && !slices.Contains(array(t["marks"]), any(name))) {
					return false
				}
				if name == "link" && !c.link(asObject(m["attrs"])["to"]) {
					return false
				}
			}
		}
	}
	leaf := slices.Contains([]string{"text", "hardBreak", "image", "divider"}, kind)
	content, exists := n["content"]
	if !exists {
		return leaf
	}
	children, ok := content.([]any)
	if !ok || (leaf && len(children) > 0) {
		return false
	}
	for _, child := range children {
		obj := asObject(child)
		childKind, _ := obj["type"].(string)
		switch kind {
		case "paragraph", "heading":
			if !slices.Contains(inline, childKind) {
				return false
			}
		case "bulletList", "orderedList":
			if childKind != "listItem" {
				return false
			}
		default:
			if !slices.Contains(blocks, childKind) {
				return false
			}
		}
		if !c.richText(obj, t, depth+1) {
			return false
		}
	}
	return true
}
