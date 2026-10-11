import { expect, it } from "vitest";
import {
  createRegistry,
  defineComponent,
  nativeDefinitions,
  componentContract,
} from "../src/index.js";
import { buildManifest, manifestArtifact } from "../src/manifest.js";
import type { ComponentDefinition } from "../src/index.js";
const app = { id: "test-app", version: "1.0.0", framework: "other" } as const;
const implementation = () => "rendered";
const definition = () =>
  defineComponent({
    name: "Card",
    origin: "native",
    container: false,
    component: implementation,
    description: "Safe description",
    props: { label: { type: "text", required: true, content: true } },
    validate: (p: Record<string, unknown>) => typeof p.label === "string",
  });
it("MF-010: derives manifest and native plan definitions from the same implementation", () => {
  const component = definition();
  const registry = createRegistry({
    components: [component],
    breakpoints: { sm: 640 },
    tokens: { colors: { surface: "#ffffff" } },
    icons: ["cart"],
  });
  const manifest = buildManifest(registry, { app });
  expect(manifest.components).toEqual({ Card: componentContract(component) });
  expect(registry.components[0]!.component).toBe(implementation);
  expect(registry.components[0]!.validate).toBe(component.validate);
  const native = nativeDefinitions(registry).Card!;
  expect(native).toEqual({ props: { label: {} }, container: false, validate: component.validate });
  expect(native.validate!({ label: "hello" })).toBe(true);
  expect(native.validate!({ label: 12 })).toBe(false);
  expect(manifest.breakpoints).toEqual({ sm: 640 });
  expect(manifest.tokens.colors).toEqual({ surface: "#ffffff" });
  expect(manifest.icons).toEqual(["cart"]);
  expect(manifest.components!.Card).not.toHaveProperty("component");
  expect(manifest.components!.Card).not.toHaveProperty("validate");
});
it("MF-010: preserves responsive declarations, container and absent optional metadata", () => {
  const component = defineComponent({
    name: "ResponsiveCard",
    component: implementation,
    props: {
      config: { type: "object", responsive: true, fields: {} },
      plain: { type: "string", responsive: false },
    },
    container: true,
  });
  const registry = createRegistry({ components: [component] });
  expect(nativeDefinitions(registry).ResponsiveCard).toEqual({
    props: { config: { responsive: true }, plain: { responsive: false } },
    container: true,
  });
  expect(
    nativeDefinitions(
      createRegistry({
        components: [defineComponent({ name: "EmptyCard", component: implementation, props: {} })],
      }),
    ).EmptyCard,
  ).toEqual({ props: {} });
  expect(registry.breakpoints).toEqual({});
  expect(registry.tokens).toEqual({});
  expect(registry.icons).toEqual([]);
});
it("MF-010: snapshots nested JSON metadata and keeps implementation identity", () => {
  const props = {
    config: {
      type: "object" as const,
      fields: { label: { type: "string" as const, maxLength: 200 } },
    },
  };
  const tokens = { colors: { surface: "#ffffff" } };
  const points = { sm: 640 };
  const icons = ["cart"];
  const component = defineComponent({ name: "Card", component: implementation, props });
  const registry = createRegistry({ components: [component], tokens, breakpoints: points, icons });
  props.config.fields.label.maxLength = 1;
  tokens.colors.surface = "#000000";
  points.sm = 800;
  icons.push("search");
  expect(registry.tokens.colors!.surface).toBe("#ffffff");
  expect(registry.breakpoints.sm).toBe(640);
  expect(registry.icons).toEqual(["cart"]);
  expect(component.props).not.toBe(props);
  expect(component.props.config.fields.label.maxLength).toBe(200);
  expect(Object.isFrozen(component.props.config.fields.label)).toBe(true);
  expect(Object.isFrozen(registry.components)).toBe(true);
  expect(() => {
    registry.tokens.colors!.surface = "#000000";
  }).toThrow();
  const natives = nativeDefinitions(registry);
  natives.Card!.props.config!.responsive = true;
  expect(nativeDefinitions(registry).Card!.props.config).toEqual({});
  const manifest = buildManifest(registry, { app });
  manifest.breakpoints.sm = 900;
  expect(registry.breakpoints.sm).toBe(640);
});
it("MF-010: rejects duplicate names, unsafe prop names and invalid names", () => {
  const distinct = createRegistry({
    components: [
      definition(),
      defineComponent({
        name: "SecondCard",
        component: implementation,
        props: {},
      }),
    ],
  });
  expect(distinct.components.map((item) => item.name)).toEqual(["Card", "SecondCard"]);
  expect(Object.keys(buildManifest(distinct, { app }).components!)).toEqual(["Card", "SecondCard"]);
  expect(() => createRegistry({ components: [definition(), definition()] })).toThrow(
    "CMS_REGISTRY_NAME_DUPLICATE",
  );
  for (const name of ["", "card", "__proto__", "Card.Name", "Card-name"])
    expect(() => defineComponent({ ...definition(), name })).toThrow("CMS_REGISTRY_NAME_INVALID");
  for (const key of ["constructor", "prototype", "__proto__"])
    expect(() =>
      defineComponent({ ...definition(), props: Object.fromEntries([[key, { type: "string" }]]) }),
    ).toThrow("CMS_REGISTRY_PROP_INVALID");
});
it("MF-001: rejects non-JSON and cyclic metadata instead of silently changing contracts", () => {
  const cycle: Record<string, unknown> = {};
  cycle.self = cycle;
  const getter = Object.defineProperty({}, "label", {
    get() {
      throw new Error("getter ran");
    },
  });
  for (const value of [
    () => true,
    Symbol("x"),
    1n,
    NaN,
    Infinity,
    cycle,
    new Date(),
    /pattern/,
    { [Symbol("x")]: 1 },
    getter,
    Array(2),
    { nested: undefined },
    { nested: () => true },
  ])
    expect(() =>
      defineComponent({ ...definition(), description: value } as unknown as ComponentDefinition),
    ).toThrow("CMS_REGISTRY_METADATA_INVALID");
  const shared = { since: "1.0" };
  const common = { type: "string" as const };
  expect(
    defineComponent({ ...definition(), props: { first: common, second: common } }).props,
  ).toEqual({ first: common, second: common });
  expect(
    defineComponent({ ...definition(), deprecated: shared, slots: { footer: { max: 2 } } })
      .deprecated,
  ).toEqual(shared);
  expect(defineComponent({ ...definition(), deprecated: null }).deprecated).toBeNull();
  expect(defineComponent({ ...definition(), props: Object.create(null) }).props).toEqual({});
});
it("MF-001: validates reserved names, invalid constraints, slots and unsupported versions before output", () => {
  for (const component of [
    { ...definition(), name: "Box" },
    { ...definition(), props: { label: { type: "string", maxLength: -1 } } },
    { ...definition(), slots: { footer: { allowedTypes: ["UnknownComponent"] } } },
    { ...definition(), props: { label: { type: "object", content: true, fields: {} } } },
  ] as ComponentDefinition[]) {
    expect(() => buildManifest(createRegistry({ components: [component] }), { app })).toThrow(
      "CMS_MANIFEST_INVALID",
    );
  }
  expect(() =>
    buildManifest(createRegistry({ components: [definition()] }), { app, irVersions: ["99.0"] }),
  ).toThrow("MANIFEST_IR_VERSION_UNSUPPORTED");
});
it("MF-002: returns deterministic canonical JSON and SHA-256 without executable values", async () => {
  const a = await manifestArtifact(createRegistry({ components: [definition()] }), { app });
  const b = await manifestArtifact(createRegistry({ components: [definition()] }), {
    app: { framework: "other", version: "1.0.0", id: "test-app" },
    irVersions: ["1.0"],
  });
  expect(a.hash).toMatch(/^sha256:[a-f0-9]{64}$/);
  expect(a.hash).toBe(b.hash);
  expect(a.json).toBe(b.json);
  expect(JSON.parse(a.json)).toEqual(a.manifest);
  const changed = await manifestArtifact(
    createRegistry({ components: [defineComponent({ ...definition(), description: "changed" })] }),
    { app },
  );
  expect(changed.hash).not.toBe(a.hash);
});
