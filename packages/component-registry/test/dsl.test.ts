import { expect, it } from "vitest";
import { createRegistry, defineComponent, t } from "../src/index.js";
import { buildManifest } from "../src/manifest.js";
import type { InferProps, ResolveFields } from "../src/index.js";
const app = { id: "dsl-app", version: "1.0.0", framework: "other" } as const;

it("MF-010: all 17 field kinds and slots produce validated schema JSON", () => {
  const props = {
    title: t.text().required().content().max(100).multiline().description("Title"),
    slug: t.string().min(1).max(80).pattern("^[a-z]+$").default("one"),
    body: t.richText().content().marks("bold").blocks("paragraph"),
    count: t.number().int().min(0).max(100).default(2),
    visible: t.boolean().default(true),
    choice: t.enum(["one", "two"]).default("one").responsive(),
    date: t.date(),
    time: t.datetime(),
    url: t.url().schemes("https", "mailto"),
    link: t.link().content(),
    image: t.asset("image").mimeTypes("image/png", "image/jpeg").content(),
    file: t.asset(),
    product: t.reference("Product"),
    items: t
      .list(t.object({ id: t.string().required() }))
      .min(1)
      .max(5),
    literalItems: t.list({ type: "number" }),
    config: t.object({ base: t.boolean(), raw: { type: "text" } }).responsive(),
    color: t.color().default("surface"),
    node: t.nodeRef("Card"),
    anyNode: t.nodeRef(),
  };
  const c = defineComponent({
    name: "Card",
    component: () => null,
    props,
    slots: {
      footer: t.slot().allow("Card", "Text").min(0).max(2).description("Footer"),
      plain: {},
    },
    provides: { name: t.string().required() },
  });
  const schemas = {
    Product: {
      version: 1,
      fields: {
        name: t.string().required().unique().localized().schema,
        title: t.text().unique().schema,
        code: t.number().unique().schema,
      },
    },
  };
  const capabilities = { "catalog.read": { description: "Read catalog" } };
  const registry = createRegistry({
    components: [c],
    schemas,
    capabilities,
    tokens: { colors: { surface: "#ffffff" } },
  });
  const manifest = buildManifest(registry, { app });
  expect(manifest.components!.Card!.props).toEqual({
    title: {
      type: "text",
      required: true,
      content: true,
      maxLength: 100,
      multiline: true,
      description: "Title",
    },
    slug: { type: "string", minLength: 1, maxLength: 80, pattern: "^[a-z]+$", default: "one" },
    body: { type: "richText", content: true, marks: ["bold"], blocks: ["paragraph"] },
    count: { type: "number", integer: true, min: 0, max: 100, default: 2 },
    visible: { type: "boolean", default: true },
    choice: { type: "enum", values: ["one", "two"], default: "one", responsive: true },
    date: { type: "date" },
    time: { type: "datetime" },
    url: { type: "url", schemes: ["https", "mailto"] },
    link: { type: "link", content: true },
    image: {
      type: "asset",
      assetKind: "image",
      mimeTypes: ["image/png", "image/jpeg"],
      content: true,
    },
    file: { type: "asset" },
    product: { type: "reference", schema: "Product" },
    items: {
      type: "list",
      of: { type: "object", fields: { id: { type: "string", required: true } } },
      min: 1,
      max: 5,
    },
    literalItems: { type: "list", of: { type: "number" } },
    config: {
      type: "object",
      fields: { base: { type: "boolean" }, raw: { type: "text" } },
      responsive: true,
    },
    color: { type: "color", default: "surface" },
    node: { type: "nodeRef", nodeType: "Card" },
    anyNode: { type: "nodeRef" },
  });
  expect(manifest.components!.Card!.props!.slug).toEqual({
    type: "string",
    minLength: 1,
    maxLength: 80,
    pattern: "^[a-z]+$",
    default: "one",
  });
  expect(manifest.components!.Card!.slots!.footer).toEqual({
    allowedTypes: ["Card", "Text"],
    min: 0,
    max: 2,
    description: "Footer",
  });
  expect(manifest.components!.Card!.provides).toEqual({ name: { type: "string", required: true } });
  expect(manifest.schemas).toEqual({
    Product: {
      version: 1,
      fields: {
        name: { type: "string", required: true, unique: true, localized: true },
        title: { type: "text", unique: true },
        code: { type: "number", unique: true },
      },
    },
  });
  expect(manifest.capabilities).toEqual(capabilities);
  schemas.Product.version = 99;
  capabilities["catalog.read"].description = "Changed";
  expect(registry.schemas.Product!.version).toBe(1);
  expect(registry.capabilities["catalog.read"]!.description).toBe("Read catalog");
});
it("MF-010: builders snapshot input and do not mutate earlier descriptors", () => {
  const initial = t.text();
  const required = initial.required();
  expect(initial.schema).toEqual({ type: "text" });
  expect(required.schema).toEqual({ type: "text", required: true });
  const values: [string, string] = ["a", "b"];
  const choice = t.enum(values);
  values[0] = "mutated";
  expect(choice.schema.values).toEqual(["a", "b"]);
  expect(Object.isFrozen(choice.schema.values)).toBe(true);
  expect(Object.isFrozen(initial)).toBe(true);
  const slot = t.slot();
  expect(Object.isFrozen(slot)).toBe(true);
  expect(Object.isFrozen(slot.schema)).toBe(true);
  expect(slot.max(2).schema).toEqual({ max: 2 });
  expect(slot.schema).toEqual({});
});
it("MF-001: descriptors retain authoritative validation, including RE2 and defaults", () => {
  for (const field of [
    t.string().min(-1),
    t.number().min(3).max(1),
    t.string().pattern("(?=x)"),
    t.enum(["one"]).default("bad" as "one"),
  ])
    expect(() =>
      buildManifest(
        createRegistry({
          components: [
            defineComponent({
              name: "Card",
              props: { value: field },
              component: () => null,
            }),
          ],
        }),
        { app },
      ),
    ).toThrow("CMS_MANIFEST_INVALID");
});

// MF-012: checked by the compiler, never invoked at runtime.
function typeChecks() {
  const props = {
    choice: t.enum(["one", "two"]).required(),
    items: t.list(t.object({ id: t.string().required() })).required(),
    node: t.nodeRef().required(),
    image: t.asset().required(),
    ref: t.reference("Product").required(),
    body: t.richText().required(),
    link: t.link().required(),
    maybe: t.number(),
  };
  type Values = InferProps<ResolveFields<typeof props>>;
  defineComponent({
    name: "Card",
    props,
    component: (p) => {
      const choice: "one" | "two" = p.choice;
      const id: string = p.items[0]!.id;
      const node: string = p.node;
      const asset: string = p.image.assetId;
      const entity: string = p.ref.entityId;
      const doc: "doc" = p.body.type;
      const link: "url" | "page" | "anchor" = p.link.kind;
      const optional: number | null | undefined = p.maybe;
      // @ts-expect-error MF-012: enum choices remain literal.
      const bad: "three" = p.choice;
      return [choice, id, node, asset, entity, doc, link, optional, bad];
    },
  });
  defineComponent({
    name: "BadCard",
    props: { value: t.string().required() },
    // @ts-expect-error MF-012: implementation must accept declared string values.
    component: (_p: { value: number }) => null,
  });
  // @ts-expect-error MF-012: required nested field is missing.
  const missing: Values = { choice: "one", items: [{}] };
  // @ts-expect-error MF-012: defaults match the value kind.
  t.number().default("2");
  // @ts-expect-error MF-012: enum default must be declared.
  t.enum(["a", "b"]).default("c");
  // @ts-expect-error MF-012: manifest does not support object defaults.
  t.object({}).default({});
  // @ts-expect-error MF-012: string is not a content field.
  t.string().content();
  // @ts-expect-error MF-012: lengths are unavailable on booleans.
  t.boolean().max(1);
  return missing;
}
void typeChecks;
