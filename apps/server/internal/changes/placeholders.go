package changes

import (
	"errors"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
)

// liveStatus — an active request may hold a placeholder.
func liveStatus(s string) bool {
	return s == "submitted" || s == "accepted" || s == "in_progress" || s == "pr_open"
}

// designPaths maps design paths (relative to the node's design, as anchors
// store them: "padding", "padding/lg", "states/hover/padding") to string values.
func designPaths(design map[string]any) map[string]string {
	out := map[string]string{}
	add := func(path string, v any) {
		if s, ok := v.(string); ok {
			out[path] = s
			return
		}
		if m, ok := v.(map[string]any); ok && m["raw"] == nil {
			for bp, value := range m {
				if s, ok := value.(string); ok {
					out[path+"/"+bp] = s
				}
			}
		}
	}
	for key, value := range design {
		if key == "states" {
			states, _ := value.(map[string]any)
			for state, props := range states {
				m, _ := props.(map[string]any)
				for prop, v := range m {
					add("states/"+state+"/"+prop, v)
				}
			}
			continue
		}
		add(key, value)
	}
	return out
}

func nodeDesign(nodes map[string]any, id string) map[string]any {
	n, _ := nodes[id].(map[string]any)
	d, _ := n["design"].(map[string]any)
	return d
}

func sortedIDs(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// validateDataAndTokens — STU-377/378/385: a data source or token placeholder
// appears only at a live anchor of an active request of that key; a data
// source marker is removed only once the request is fulfilled. Replacing a
// token placeholder by another value is ordinary editing.
func (s *session) validateDataAndTokens(target uuid.UUID, before, after map[string]any) error {
	oldSources, _ := before["dataSources"].(map[string]any)
	sources, _ := after["dataSources"].(map[string]any)
	for _, key := range sortedIDs(sources) {
		use, _ := sources[key].(map[string]any)
		old, _ := oldSources[key].(map[string]any)
		marker, _ := use["pending"].(string)
		previous, _ := old["pending"].(string)
		if marker == previous && (marker == "" || use["source"] == old["source"]) {
			continue
		}
		check, active := marker, true
		if marker == "" {
			check, active = previous, false
		}
		a, err := s.anchorAt(target, check, key, "")
		if err != nil {
			return err
		}
		if a.RequestKey != use["source"] || active != liveStatus(a.RequestStatus) || !active && a.RequestStatus != "fulfilled" {
			return pendingError()
		}
	}
	oldNodes, _ := before["nodes"].(map[string]any)
	nodes, _ := after["nodes"].(map[string]any)
	for _, id := range sortedIDs(nodes) {
		previous := designPaths(nodeDesign(oldNodes, id))
		for path, value := range designPaths(nodeDesign(nodes, id)) {
			marker, ok := strings.CutPrefix(value, "pending:")
			if !ok || previous[path] == value {
				continue
			}
			a, err := s.anchorAt(target, marker, id, path)
			if err != nil {
				return err
			}
			if !liveStatus(a.RequestStatus) {
				return pendingError()
			}
		}
	}
	return nil
}

// systemDataAndTokens — STU-390: cms-system may only drop the marker of a data
// source placeholder or write the token name over a token placeholder, at a
// live anchor of a fulfilled request. It returns the before-nodes with those
// token leaves already replaced, so the remaining comparison is exact.
func (s *session) systemDataAndTokens(target uuid.UUID, before, after map[string]any) (map[string]any, error) {
	oldSources, _ := before["dataSources"].(map[string]any)
	sources, _ := after["dataSources"].(map[string]any)
	if len(oldSources) != len(sources) {
		return nil, systemForbidden()
	}
	for key, raw := range sources {
		use, _ := raw.(map[string]any)
		old, _ := oldSources[key].(map[string]any)
		if sameJSON(use, mustJSON(old)) {
			continue
		}
		marker, _ := old["pending"].(string)
		stripped := map[string]any{}
		for k, v := range old {
			if k != "pending" {
				stripped[k] = v
			}
		}
		if marker == "" || !sameJSON(use, mustJSON(stripped)) {
			return nil, systemForbidden()
		}
		if err := s.systemAnchor(target, marker, key, "", func(requestKey string) bool { return requestKey == use["source"] }); err != nil {
			return nil, err
		}
	}
	oldNodes, _ := before["nodes"].(map[string]any)
	nodes, _ := after["nodes"].(map[string]any)
	normalized := map[string]any{}
	for id, raw := range oldNodes {
		normalized[id] = raw
	}
	for _, id := range sortedIDs(nodes) {
		current := designPaths(nodeDesign(nodes, id))
		for path, value := range designPaths(nodeDesign(oldNodes, id)) {
			marker, ok := strings.CutPrefix(value, "pending:")
			if !ok || current[path] == value {
				continue
			}
			name := current[path]
			if err := s.systemAnchor(target, marker, id, path, func(requestKey string) bool { return strings.HasSuffix(requestKey, "."+name) }); err != nil {
				return nil, err
			}
			normalized[id] = withDesignValue(oldNodes[id], path, name)
		}
	}
	return normalized, nil
}

// systemAnchor checks a live anchor of a fulfilled request whose key matches.
func (s *session) systemAnchor(target uuid.UUID, marker, node, event string, key func(string) bool) error {
	a, err := s.anchorAt(target, marker, node, event)
	if err != nil && errors.As(err, new(*commandbus.Error)) {
		return systemForbidden()
	}
	if err != nil {
		return err
	}
	if a.RequestStatus != "fulfilled" || !key(a.RequestKey) {
		return systemForbidden()
	}
	return nil
}

// withDesignValue returns a copy of the node with one design path replaced.
func withDesignValue(raw any, path, value string) map[string]any {
	n := deepCopy(raw).(map[string]any)
	m, _ := n["design"].(map[string]any)
	parts := strings.Split(path, "/")
	for _, p := range parts[:len(parts)-1] {
		next, _ := m[p].(map[string]any)
		m = next
	}
	if m != nil {
		m[parts[len(parts)-1]] = value
	}
	return n
}

func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = deepCopy(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = deepCopy(val)
		}
		return out
	}
	return v
}
