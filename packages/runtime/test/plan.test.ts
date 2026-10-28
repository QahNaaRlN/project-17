import { describe, expect, it } from "vitest";
import type { IrDocument, Node } from "@cms/ir";
import { linkHref, preparePage, type PlanOptions } from "../src/plan.js";
function document(nodes: Partial<Node>[], kind: "page" | "component" = "page"): IrDocument {
  return {
    irVersion: "1.0",
    kind,
    root: "root",
    nodes: Object.fromEntries(
      nodes.map((node, index) => {
        const id = node.id ?? (index ? `node${index}` : "root");
        return [id, { id, type: "Text", props: { text: "Hello" }, ...node }];
      }),
    ),
  };
}
function invalid(node: Partial<Node>, options: PlanOptions = {}) {
  const plan = preparePage(document([node]), options);
  expect(plan.roots).toEqual([]);
  expect(plan.diagnostics).toHaveLength(1);
  return plan.diagnostics[0]!.code;
}
describe("SDK-001/013, IR-040/045: render plan", () => {
  it.each([undefined, null])(
    "STU-211/212/216: missing Image source %s retains its placeholder",
    (src) => {
      const plan = preparePage(document([{ type: "Image", props: { src, alt: "Cover" } }]));
      expect(plan.roots).toHaveLength(1);
      expect(plan.roots[0]!.type).toBe("Image");
      expect(plan.roots[0]!.props.alt).toBe("Cover");
    },
  );
  it.each(["https://private.test/image", {}, { assetId: "invalid" }])(
    "STU-212: malformed Image source %s remains invalid",
    (src) => {
      expect(invalid({ type: "Image", props: { src } })).toBe("CMS_IMAGE_INVALID");
    },
  );
  it("resolves bindings/conditions/localization and serializes only used values", () => {
    const doc = document([
      {
        type: "Stack",
        props: { direction: { base: "vertical", sm: "horizontal" } },
        children: ["node1", "node2"],
      },
      { bindings: { text: "$local.t_title" }, props: {} },
      { when: { op: "exists", arg: "$content.missing" }, props: { text: "hidden" } },
    ]);
    doc.localContent = { t_title: { type: "text", value: { ru: "Русский", en: "English" } } };
    const plan = preparePage(doc, {
      scope: { content: { missing: null, secret: "never serialize" }, context: { locale: "ru" } },
      breakpoints: { sm: 640 },
    });
    expect(plan.diagnostics).toEqual([]);
    expect(plan.roots[0]!.children.map((n) => n.props.text)).toEqual(["Русский"]);
    expect(plan.css).toContain("display:flex");
    expect(plan.css).toContain("flex-direction:column");
    expect(plan.css).toContain("@media(min-width:640px)");
    expect(JSON.stringify(plan)).not.toContain("secret");
    expect(preparePage(doc).diagnostics[0]!.nodeId).toBe("root");
    expect(
      preparePage(document([{ bindings: { text: "$local.t_title" }, props: {} }]), {
        scope: { context: { locale: "fr" } },
      }).roots,
    ).toEqual([]);
  });
  it("keeps native object props and unwraps explicitly responsive base values", () => {
    const natives = {
      Widget: {
        props: { config: { responsive: true }, title: {} },
        validate: (p: Record<string, unknown>) => p.title === "yes",
      },
    };
    const doc = document([
      { type: "Widget", props: { config: { base: { nested: true } }, title: "yes" } },
    ]);
    expect(preparePage(doc, { natives }).roots[0]!.props).toEqual({
      config: { nested: true },
      title: "yes",
    });
    for (const config of [{ nested: true }, { base: 1, sm: 2 }, [1]])
      expect(invalid({ type: "Widget", props: { config, title: "yes" } }, { natives })).toBe(
        "CMS_NODE_INVALID",
      );
    expect(invalid({ type: "Widget", props: { config: 1, title: "no" } }, { natives })).toBe(
      "CMS_PROP_INVALID",
    );
    expect(
      preparePage(document([{ type: "Widget", props: { title: "yes" } }]), { natives }).roots[0]!
        .props,
    ).toEqual({ title: "yes" });
    expect(
      preparePage(document([{ type: "Widget", props: { config: null, title: "yes" } }]), {
        natives,
      }).roots[0]!.props.config,
    ).toBeNull();
    expect(
      invalid(
        { type: "Widget", props: { title: "yes" } },
        {
          natives: {
            Widget: {
              props: { title: {} },
              validate: () => {
                throw new Error("secret");
              },
            },
          },
        },
      ),
    ).toBe("CMS_NODE_INVALID");
  });
  // MF-024: unknown node types are skipped with a diagnostic.
  it("rejects invalid nodes, props, actions, unsupported slots and leaf children", () => {
    expect(invalid({ type: "Unknown" })).toBe("CMS_NODE_UNKNOWN");
    expect(invalid({ type: "constructor" })).toBe("CMS_NODE_UNKNOWN");
    expect(invalid({ props: { text: "yes", dangerous: "no" } })).toBe("CMS_PROP_INVALID");
    expect(invalid({ props: { constructor: "unsafe" } })).toBe("CMS_PROP_INVALID");
    expect(invalid({ bindings: { text: "$item" } })).toBe("CMS_PROP_CONFLICT");
    expect(invalid({ props: {}, bindings: { constructor: "$item" } })).toBe("CMS_PROP_INVALID");
    expect(invalid({ on: { click: { action: "navigate" } } })).toBe("CMS_ACTION_UNSUPPORTED");
    expect(invalid({ slots: { extra: [] } })).toBe("CMS_SLOT_UNSUPPORTED");
    expect(invalid({ children: ["nope"] })).toBe("CMS_CHILDREN_INVALID");
    const cyclic: Record<string, unknown> = {};
    cyclic.self = cyclic;
    expect(
      invalid(
        { type: "Widget", props: { value: cyclic } },
        { natives: { Widget: { props: { value: {} } } } },
      ),
    ).toBe("CMS_NODE_INVALID");
    const doc = document([
      { type: "Box", props: {}, children: ["root", "nope", "node1"] },
      { id: "node1", props: { text: "valid" } },
    ]);
    const plan = preparePage(doc);
    expect(plan.roots[0]!.children).toHaveLength(1);
    expect(plan.diagnostics.map((d) => d.code)).toEqual(["CMS_NODE_CYCLE", "CMS_NODE_MISSING"]);
    expect(preparePage({ ...doc, nodes: {} }).diagnostics[0]!.code).toBe("CMS_NODE_MISSING");
  });
  it.each([
    ["Heading", { text: "h", level: 0 }],
    ["Heading", { text: "h", level: 7 }],
    ["Heading", { text: "h", level: 1.5 }],
    ["Text", { text: 1 }],
    ["Text", { text: "t", as: "script" }],
    ["Box", { as: "script" }],
    ["Divider", { orientation: "vertical" }],
    ["Link", { to: { kind: "url", url: "javascript:alert(1)" } }],
    ["Link", { to: { kind: "url", url: "/safe" }, label: 1 }],
    ["Link", { to: { kind: "url", url: "/safe" }, newTab: 1 }],
  ])("rejects invalid %s primitive", (type, props) =>
    expect(invalid({ type, props })).toBe("CMS_NODE_INVALID"),
  );
  it("prepares valid semantics and safe links", () => {
    for (const [type, props] of [
      ["Heading", { text: "h", level: 6 }],
      ["Text", { text: "t", as: "span" }],
      ["Box", { as: "section" }],
      ["Divider", { orientation: "horizontal" }],
      ["Label", { text: "label" }],
      ["Link", { to: { kind: "url", url: "/safe" }, label: "Go", newTab: true }],
      ["Flex", { wrap: { base: true, sm: false } }],
      ["Grid", {}],
    ] as const)
      expect(
        preparePage(document([{ type, props }]), { breakpoints: { sm: 640 } }).diagnostics,
      ).toEqual([]);
    const doc = document([
      { type: "Box", props: {}, children: ["node1", "node2"] },
      { type: "Link", props: { to: { kind: "anchor", node: "node2" } } },
      { props: { text: "target" } },
    ]);
    expect(preparePage(doc).roots[0]!.children[0]!.props.href).toBe("#node2");
  });
});
describe("SDK-011: Repeat", () => {
  const doc = document([
    {
      type: "Repeat",
      props: { key: "$item.id", as: "outer", limit: 2 },
      bindings: { items: "$content.rows" },
      children: ["node1"],
      slots: { empty: ["node2"] },
    },
    {
      props: {},
      bindings: {
        text: {
          template: "{id}/{i}/{outer}",
          vars: { id: "$item.id", i: "$index", outer: "$each.outer.id" },
        },
      },
    },
    { props: { text: "Empty" } },
  ]);
  it("uses stable item keys, limits work and invokes empty slots", () => {
    const run = (rows: unknown) => preparePage(doc, { scope: { content: { rows } } });
    const plan = run([{ id: "b" }, { id: "a" }, { id: "c" }]);
    expect(plan.roots.map((n) => n.props.text)).toEqual(["b/0/b", "a/1/a"]);
    expect(plan.roots[0]!.key).toContain("root:string%3Ab/");
    expect(run([{ id: "a" }, { id: "b" }]).roots[1]!.key).toBe(plan.roots[0]!.key);
    expect(run([]).roots[0]!.props.text).toBe("Empty");
    expect(run(null).roots[0]!.key).toContain("/empty/");
    expect(run(undefined).roots).toHaveLength(1);
    expect(run("not-list").diagnostics[0]!.code).toBe("CMS_REPEAT_INVALID");
    for (const rows of [
      [{ id: "a" }, { id: "a" }],
      [{}],
      [{ id: "" }],
      [{ id: null }],
      [{ id: Infinity }],
      [{ id: "a".repeat(257) }],
    ])
      expect(run(rows).roots).toEqual([]);
  });
  it("supports index keys and nested aliases without overwriting outer scopes", () => {
    const nested = document([
      {
        type: "Repeat",
        props: { as: "outer" },
        bindings: { items: "$content.rows" },
        children: ["node1"],
      },
      { type: "Repeat", props: {}, bindings: { items: "$item.children" }, children: ["node2"] },
      {
        props: {},
        bindings: { text: { template: "{o}:{i}", vars: { o: "$each.outer.id", i: "$item" } } },
      },
    ]);
    const plan = preparePage(nested, {
      scope: { content: { rows: [{ id: "a", children: ["x", "y"] }] } },
    });
    expect(plan.roots.map((n) => n.props.text)).toEqual(["a:x", "a:y"]);
    expect(new Set(plan.roots.map((n) => n.key)).size).toBe(2);
    expect(preparePage(document([{ type: "Repeat", props: {}, children: [] }])).roots).toEqual([]);
  });
  it.each([
    { limit: 0 },
    { limit: 201 },
    { limit: 1.5 },
    { as: 1 },
    { as: "constructor" },
    { as: "bad-alias" },
    { key: 1 },
    { key: "$content.id" },
  ])("rejects invalid Repeat %j", (props) =>
    expect(invalid({ type: "Repeat", props })).toBe("CMS_REPEAT_INVALID"),
  );
  it("rejects unsupported repeat/slot designs", () => {
    expect(invalid({ type: "Repeat", props: {}, design: { hidden: true } })).toBe(
      "CMS_REPEAT_INVALID",
    );
    expect(invalid({ type: "Repeat", props: {}, slots: { other: [] } })).toBe("CMS_REPEAT_INVALID");
    expect(invalid({ type: "Slot", props: { name: "main" }, design: { hidden: true } })).toBe(
      "CMS_DESIGN_UNSUPPORTED",
    );
    expect(invalid({ type: "Slot", props: { name: 1 } })).toBe("CMS_SLOT_INVALID");
  });
});
describe("Composed", () => {
  const ref = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11";
  const component = document(
    [
      { type: "Box", props: {}, children: ["node1", "node2", "node3"] },
      { props: {}, bindings: { text: "$props.title" } },
      { type: "Slot", props: { name: "main" }, children: ["node4"] },
      { props: {}, bindings: { text: "$local.t_label" } },
      { props: { text: "Fallback" } },
    ],
    "component",
  );
  component.inputs = { title: { type: "text", required: true, default: "Default" } };
  component.slotDefs = { main: { allowedTypes: ["Text"], max: 1 } };
  component.localContent = { t_label: { type: "text", value: { en: "Component" } } };
  const options: PlanOptions = {
    components: { [ref]: component },
    scope: { props: { title: "Caller" }, context: { locale: "en" } },
  };
  const doc = document([
    {
      type: "Composed",
      props: { title: "Input" },
      ref: { component: ref, version: "live" },
      slots: { main: ["node1"] },
    },
    { props: {}, bindings: { text: "$props.title" } },
  ]);
  it("keeps caller slot scope, component local scope, inputs and instance keys", () => {
    const plan = preparePage(doc, options);
    expect(plan.diagnostics).toEqual([]);
    expect(plan.roots[0]!.children.map((n) => n.props.text)).toEqual([
      "Input",
      "Caller",
      "Component",
    ]);
    expect(plan.roots[0]!.key).toBe("root/root");
    expect(plan.roots[0]!.children[1]!.key).toContain("slot:main");
    expect(
      preparePage(
        document([{ type: "Composed", props: {}, ref: { component: ref } }]),
        options,
      ).roots[0]!.children.map((n) => n.props.text),
    ).toEqual(["Default", "Fallback", "Component"]);
  });
  it("validates missing inputs, responsive inputs, versions, recursion and slots", () => {
    expect(invalid({ type: "Composed", props: {}, ref: { component: ref } })).toBe(
      "CMS_COMPONENT_MISSING",
    );
    expect(invalid({ type: "Composed", props: {} })).toBe("CMS_COMPONENT_VERSION_UNSUPPORTED");
    expect(
      invalid({ type: "Composed", props: {}, ref: { component: ref, version: 2 } }, options),
    ).toBe("CMS_COMPONENT_VERSION_UNSUPPORTED");
    expect(
      invalid({ type: "Composed", props: { unknown: 1 }, ref: { component: ref } }, options),
    ).toBe("CMS_PROP_INVALID");
    expect(
      invalid(
        { type: "Composed", props: {}, ref: { component: ref }, design: { width: "full" } },
        options,
      ),
    ).toBe("CMS_COMPONENT_DESIGN_UNSUPPORTED");
    const required = structuredClone(component);
    delete required.inputs!.title!.default;
    expect(
      invalid(
        { type: "Composed", props: {}, ref: { component: ref } },
        { components: { [ref]: required } },
      ),
    ).toBe("CMS_INPUT_MISSING");
    required.inputs!.title!.responsive = true;
    expect(
      preparePage(
        document([
          { type: "Composed", props: { title: { base: "Base" } }, ref: { component: ref } },
        ]),
        { components: { [ref]: required } },
      ).roots[0]!.children[0]!.props.text,
    ).toBe("Base");
    const recursion = document(
      [{ type: "Composed", props: {}, ref: { component: ref } }],
      "component",
    );
    expect(
      preparePage(document([{ type: "Composed", props: {}, ref: { component: ref } }]), {
        components: { [ref]: recursion },
      }).diagnostics[0]!.code,
    ).toBe("CMS_COMPONENT_CYCLE");
    expect(
      invalid(
        { type: "Composed", props: {}, ref: { component: ref } },
        { components: { [ref]: document([{}]) } },
      ),
    ).toBe("CMS_COMPONENT_MISSING");
    expect(
      invalid(
        { type: "Composed", props: {}, ref: { component: ref }, slots: { unknown: [] } },
        options,
      ),
    ).toBe("CMS_SLOT_INVALID");
    const strict = structuredClone(component);
    strict.slotDefs!.main!.min = 1;
    expect(
      invalid(
        { type: "Composed", props: {}, ref: { component: ref } },
        { components: { [ref]: strict } },
      ),
    ).toBe("CMS_SLOT_INVALID");
    for (const ids of [[], ["node1", "node1"]])
      expect(
        invalid(
          { type: "Composed", props: {}, ref: { component: ref }, slots: { main: ids } },
          { components: { [ref]: strict } },
        ),
      ).toBe("CMS_SLOT_INVALID");
    const wrong = document([
      { type: "Composed", props: {}, ref: { component: ref }, slots: { main: ["node1"] } },
      { type: "Box", props: {} },
    ]);
    expect(preparePage(wrong, options).diagnostics[0]!.code).toBe("CMS_SLOT_INVALID");
  });
});
describe("CSS and expansion limits", () => {
  it("deduplicates atoms, sorts breakpoints after every base atom and drops rejected styles", () => {
    const doc = document([
      {
        type: "Grid",
        props: {},
        design: { hidden: { base: true, sm: false } },
        children: ["node1", "node2", "node3"],
      },
      { design: { padding: { base: { raw: 1 }, sm: { raw: 2 } } } },
      { design: { padding: { sm: { raw: 2 }, base: { raw: 3 } } } },
      { design: { padding: { raw: 4 }, position: "fixed" } },
    ]);
    const plan = preparePage(doc, { breakpoints: { sm: 640 } });
    expect(plan.roots[0]!.children).toHaveLength(2);
    expect(plan.css).not.toContain("padding:4px");
    expect(plan.css.indexOf("padding:3px")).toBeLessThan(plan.css.indexOf("@media"));
    expect(plan.css.match(/padding:2px/g)).toHaveLength(1);
    expect(plan.css).toContain("display:grid");
    expect(
      preparePage(document([{ type: "Flex", props: {}, design: { hidden: false } }])).css,
    ).toContain("display:flex");
    expect(
      preparePage(document([{ type: "Box", props: {}, design: { hidden: false } }])).css,
    ).toContain("display:revert");
  });
  it("fails the entire plan when expansion crosses the node budget", () => {
    const doc = document([
      { type: "Repeat", props: { items: Array(200).fill(1) }, children: ["node1"] },
      { type: "Repeat", props: { items: Array(200).fill(1) }, children: ["node2"] },
      { props: { text: "large" } },
    ]);
    expect(preparePage(doc)).toEqual({
      roots: [],
      css: "",
      diagnostics: [{ code: "CMS_EXPANSION_LIMIT", nodeId: "root" }],
    });
    const deep = Array.from({ length: 35 }, (_, i) => ({
      id: i === 0 ? "root" : `node${i}`,
      type: "Box",
      props: {},
      children: i === 34 ? [] : [`node${i + 1}`],
    }));
    expect(preparePage(document(deep)).diagnostics[0]!.code).toBe("CMS_EXPANSION_LIMIT");
    const huge = document([{ type: "Text", props: { text: "я".repeat(1024 * 1024 + 1) } }]);
    expect(preparePage(huge).diagnostics[0]!.code).toBe("CMS_EXPANSION_LIMIT");
  });
});
describe("link allowlist", () => {
  it.each([
    "https://example.test/a",
    "./a",
    "../a",
    "catalog/item",
    "http://example.test",
    "mailto:a@example.test",
    "tel:+123",
    "/a",
    "?q=ok",
    "#collection",
  ])("allows %s", (url) => expect(linkHref({ kind: "url", url })).toBe(url));
  it.each([
    "javascript:alert(1)",
    "data:text/html,x",
    "//evil.test",
    "https://user:pass@example.test",
    "https://",
    "https://example.test/\n",
    " /path",
    "/\\evil.test",
    "ftp://example.test",
    "https://example.test/\u007f",
  ])("rejects %s", (url) => expect(linkHref({ kind: "url", url })).toBeUndefined());
  it("rejects wrong shapes and validates anchors", () => {
    for (const value of [
      null,
      1,
      "/a",
      { kind: "url", url: 1 },
      { kind: "page", page: "id" },
      { kind: "anchor", node: "bad!" },
    ])
      expect(linkHref(value)).toBeUndefined();
    expect(linkHref({ kind: "anchor", node: "node1" })).toBe("#node1");
  });
});
