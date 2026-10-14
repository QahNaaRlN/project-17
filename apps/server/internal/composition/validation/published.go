package validation

import (
	"context"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"reflect"
)

// PublishedConstraints evaluates singleton/unique against the hypothetical environment,
// independently of head and open drafts. It emits diagnostics without writing content.
func (c *Context) PublishedConstraints(ctx context.Context, docs map[uuid.UUID]Document) (map[string][]ir.Diagnostic, error) {
	out := map[string][]ir.Diagnostic{}
	seen := map[string][]uuid.UUID{}
	values := map[uuid.UUID]map[string]any{}
	for _, id := range sortedUUIDs(docs) {
		d := docs[id]
		o, err := c.q.ContentIdentity(ctx, store.ContentIdentityParams{ProjectID: c.ProjectID, ID: id})
		if err != nil {
			return nil, err
		}
		if o.Kind != "entity" || o.SchemaName == nil || d.Deleted {
			continue
		}
		schema, _, ok := c.Schema(*o.SchemaName)
		if !ok {
			continue
		}
		data, err := decode(d.Body)
		if err != nil {
			return nil, err
		}
		values[id] = data
		if schema["singleton"] == true && len(seen[*o.SchemaName]) > 0 {
			out[id.String()] = append(out[id.String()], problem("CONTENT_SINGLETON", "", "", nil).Diagnostics...)
		}
		for _, other := range seen[*o.SchemaName] {
			for _, key := range sortedKeys(object(schema["fields"])) {
				f := object(object(schema["fields"])[key])
				if f["unique"] != true || data[key] == nil {
					continue
				}
				collision := reflect.DeepEqual(data[key], values[other][key])
				if f["localized"] == true {
					collision = false
					for locale, v := range object(data[key]) {
						collision = collision || (v != nil && reflect.DeepEqual(v, object(values[other][key])[locale]))
					}
				}
				if collision {
					out[id.String()] = append(out[id.String()], problem("CONTENT_UNIQUE", ir.Pointer(key), "", nil).Diagnostics...)
				}
			}
		}
		seen[*o.SchemaName] = append(seen[*o.SchemaName], id)
	}
	return out, nil
}
