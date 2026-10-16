import { expect, it, vi } from "vitest";
import type { IrDocument } from "@cms/ir";
import { preparePage } from "../src/plan.js";
import { loadSources } from "../src/sources.js";
const doc: IrDocument = {
  irVersion: "1.0",
  kind: "page",
  root: "root",
  nodes: {
    root: { id: "root", type: "Stack", children: ["static", "repeat"] },
    static: { id: "static", type: "Text", props: { text: "Independent" } },
    repeat: {
      id: "repeat",
      type: "Repeat",
      props: { key: "$item.id" },
      bindings: { items: "$data.featured.items" },
      children: ["item"],
      slots: { empty: ["empty"] },
    },
    item: { id: "item", type: "Text", bindings: { text: "$item.title" } },
    empty: { id: "empty", type: "Text", props: { text: "Empty" } },
  },
};
it("STU-226: bare/canonical keys have identical identity; bad keys preserve siblings and safe codes", () => {
  const data = { featured: { items: [{ id: "one", title: "One" }] } };
  const canonical = preparePage(doc, { scope: { data } });
  const legacy = structuredClone(doc);
  legacy.nodes.repeat!.props = { key: "id" };
  expect(preparePage(legacy, { scope: { data } })).toEqual(canonical);
  for (const [items, code] of [
    [[{ id: "same" }, { id: "same" }], "CMS_REPEAT_KEY_DUPLICATE"],
    [[{ id: null }], "CMS_REPEAT_KEY_INVALID"],
    [[], null],
  ] as const) {
    const plan = preparePage(doc, { scope: { data: { featured: { items } } } });
    expect(plan.roots[0]!.children[0]!.props.text).toBe("Independent");
    if (code) expect(plan.diagnostics).toEqual([{ code, nodeId: "repeat" }]);
    else expect(plan.roots[0]!.children[1]!.props.text).toBe("Empty");
  }
});
it("STU-232: source errors/missing/null are not empty lists and expose no raw exception", () => {
  for (const featured of [
    null,
    {},
    { items: null },
    { error: { code: "CMS_SOURCE_TIMEOUT" } },
    { error: { code: "private-message" } },
  ]) {
    const plan = preparePage(doc, { scope: { data: { featured } } });
    expect(plan.roots[0]!.children.map((n) => n.props.text)).toEqual([
      "Independent",
      "Data unavailable",
    ]);
    expect(plan.diagnostics).toHaveLength(1);
    expect(JSON.stringify(plan)).not.toContain("private-message");
  }
});
it("STU-221/230: reserved data keys never invoke a loader", async () => {
  const loader = vi.fn(() => "private");
  const result = await loadSources(
    { content: { source: "test.source" } },
    [{ name: "test.source", params: {}, result: { type: "string" }, loader }],
    {},
  );
  expect(loader).not.toHaveBeenCalled();
  expect(result.content).toEqual({ error: { code: "CMS_SOURCE_KEY_INVALID" } });
});
it("STU-224/232: a direct data binding reports the source failure without losing independent sections", () => {
  const direct = structuredClone(doc);
  direct.nodes.root!.children = ["static", "title"];
  direct.nodes.title = { id: "title", type: "Heading", bindings: { text: "$data.featured.title" } };
  const plan = preparePage(direct, {
    scope: { data: { featured: { error: { code: "CMS_SOURCE_TIMEOUT" } } } },
  });
  expect(plan.roots[0]!.children.map((node) => node.props.text)).toEqual([
    "Independent",
    "Data unavailable",
  ]);
  expect(plan.diagnostics).toEqual([{ nodeId: "title", code: "CMS_SOURCE_TIMEOUT" }]);
  const missing = preparePage(direct, { scope: { data: { featured: { title: null } } } });
  expect(missing.roots[0]!.children[0]!.props.text).toBe("Independent");
  expect(missing.diagnostics).toEqual([{ nodeId: "title", code: "CMS_DATA_VALUE_INVALID" }]);
  const businessResult = preparePage(direct, {
    scope: { data: { featured: { error: null, title: "Available" } } },
  });
  expect(businessResult.diagnostics).toEqual([]);
  expect(businessResult.roots[0]!.children[1]!.props.text).toBe("Available");
  const businessList = preparePage(doc, {
    scope: { data: { featured: { error: null, items: [{ id: "one", title: "One" }] } } },
  });
  expect(businessList.diagnostics).toEqual([]);
  expect(businessList.roots[0]!.children[1]!.props.text).toBe("One");
});
