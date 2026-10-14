package httpapi_test

import (
	"fmt"
	"testing"
)

func TestStudioLiteralTextSaveUndo(t *testing.T) {
	// STU-141: Studio converts inline text atomically; undo restores its literal form.
	e := setup(t)
	cs := e.command(t, "create-changeset", `{"title":"Preview","targets":["staging"]}`)["id"].(string)
	created := e.command(t, "apply-operations", fmt.Sprintf(`{"changesetId":%q,"expectedSeq":0,"operations":[{"type":"document.create","payload":{"kind":"page","path":"/preview","root":{"id":"n_title","type":"Heading","props":{"level":2,"text":"Literal text"}}}}]}`, cs))
	doc := created["operations"].([]any)[0].(map[string]any)["target"].(string)
	e.command(t, "apply-operations", fmt.Sprintf(`{"changesetId":%q,"expectedSeq":1,"group":true,"operations":[{"type":"localContent.set","target":%q,"payload":{"key":"t_editor_text","type":"text","value":{"ru":"Новый текст"}}},{"type":"node.setProps","target":%q,"payload":{"nodeId":"n_title","unset":["text"]}},{"type":"node.setBinding","target":%q,"payload":{"nodeId":"n_title","prop":"text","binding":"$local.t_editor_text"}}]}`, cs, doc, doc, doc))
	path := "/api/v1/documents/" + doc + "?changesetId=" + cs
	response, result := e.do(t, "GET", path, "", e.authed(nil))
	if response.StatusCode != 200 {
		t.Fatal(result)
	}
	body := result["body"].(map[string]any)
	node := body["nodes"].(map[string]any)["n_title"].(map[string]any)
	if node["props"].(map[string]any)["text"] != nil || node["bindings"].(map[string]any)["text"] != "$local.t_editor_text" {
		t.Fatal(node)
	}
	local := body["localContent"].(map[string]any)["t_editor_text"].(map[string]any)
	if local["value"].(map[string]any)["ru"] != "Новый текст" {
		t.Fatal(local)
	}
	e.command(t, "undo", fmt.Sprintf(`{"changesetId":%q,"expectedSeq":4}`, cs))
	response, result = e.do(t, "GET", path, "", e.authed(nil))
	if response.StatusCode != 200 {
		t.Fatal(result)
	}
	body = result["body"].(map[string]any)
	node = body["nodes"].(map[string]any)["n_title"].(map[string]any)
	if node["props"].(map[string]any)["text"] != "Literal text" || node["bindings"] != nil {
		t.Fatal(node)
	}
}
