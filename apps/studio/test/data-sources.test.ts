import { expect, it } from "vitest";
import { readFileSync } from "node:fs";
import type { Operation } from "../src/types.js";
import type { Type } from "@cms/manifest";
import {
  compatible,
  walkFields,
  resultFields,
  itemScope,
  bindingFields,
  parameterFields,
  literal,
  supportsLiteral,
  sourceSupported,
  sourceOperation,
  sourceChange,
  dataBinding,
  repeatTree,
  repeatPlaces,
  insertRepeat,
  safeKey,
  scopeProblems,
} from "../src/data-sources.js";
import { applyOperations } from "../src/operations.js";
import { documentDiff } from "../src/json-diff.js";
import { destinations, subtree } from "../src/structure.js";
import { dataFixture } from "./data-sources-fixture.js";
it("STU-220/223/224: shared Go/TS fixtures preserve compatibility and declaration operations", () => {
  const fixture = JSON.parse(
    readFileSync(
      new URL("../../../packages/ir/fixtures/studio-data-sources.json", import.meta.url),
      "utf8",
    ),
  ) as {
    compatibility: { name: string; from: Type; to: Type; valid: boolean }[];
    operations: { payload: Record<string, unknown>; expected: unknown }[];
  };
  for (const tc of fixture.compatibility)
    expect(compatible(tc.from, tc.to), tc.name).toBe(tc.valid);
  const session = dataFixture();
  let doc = session.record.body;
  delete doc.dataSources;
  for (const step of fixture.operations) {
    doc = applyOperations(doc, [
      {
        type: "document.setDataSource",
        target: session.record.id,
        payload: step.payload,
      } as Operation,
    ]);
    expect(doc.dataSources ?? null).toEqual(step.expected);
  }
});

function repeated() {
  const session = dataFixture(),
    doc = session.record.body;
  const list = resultFields(doc, session.manifest).fields.find(
    (f) => f.path === "$data.featured.items",
  )!;
  const tree = repeatTree(list, 12, "$item.id", new Set(Object.keys(doc.nodes)));
  const after = applyOperations(doc, [
    insertRepeat(session.record.id, { parentId: doc.root, slot: "", index: 1 }, tree),
  ]);
  const repeat = after.nodes[tree.id!]!,
    template = after.nodes[repeat.children![0]!]!,
    empty = after.nodes[repeat.slots!.empty![0]!]!;
  template.children = ["n_item"];
  after.nodes.n_item = { id: "n_item", type: "Text", bindings: { text: "$item.title" } };
  return { session, before: doc, doc: after, repeat, template, empty };
}
it("STU-221/223: result contracts, paginated paths, safe keys and bounded traversal", () => {
  const { record, manifest } = dataFixture();
  record.body.dataSources!.pages = { source: "test.pages" };
  record.body.dataSources!.missing = { source: "missing" };
  expect(resultFields(record.body, manifest).fields.map((f) => f.path)).toContain(
    "$data.pages.items",
  );
  expect(resultFields(record.body, manifest).fields.map((f) => f.path)).not.toContain(
    "$data.missing",
  );
  expect(safeKey("featured_2")).toBe(true);
  for (const key of ["content", "constructor", "prototype", "__proto__", "bad-key", ""])
    expect(safeKey(key)).toBe(false);
  const fields = Object.fromEntries(
    Array.from({ length: 205 }, (_, i) => ["field" + i, { type: "string" }]),
  );
  expect(walkFields("$data.test", { type: "object", fields } as Type)).toMatchObject({
    limited: true,
    fields: expect.any(Array),
  });
  let deep: Type = { type: "string" };
  for (let i = 0; i < 12; i++) deep = { type: "object", fields: { nested: deep } };
  expect(walkFields("$data.test", deep).limited).toBe(true);
  const paths = walkFields("$data.test", {
    type: "object",
    fields: { value: { type: "string", required: true }, constructor: { type: "string" as const } },
  });
  expect(paths.fields.at(-1)?.schema.required).toBe(false);
});
it("STU-222/224: exact scalar/object/list compatibility has no implicit coercions", () => {
  expect(compatible({ type: "number" }, { type: "text" })).toBe(false);
  expect(compatible({ type: "string" }, { type: "string", required: true })).toBe(false);
  expect(
    compatible(
      { type: "object", fields: {} },
      { type: "object", fields: { optional: { type: "string" } } },
    ),
  ).toBe(true);
  expect(
    compatible(
      { type: "object", fields: { count: { type: "number" } } },
      { type: "object", fields: { count: { type: "string" } } },
    ),
  ).toBe(false);
  expect(compatible({ type: "list", of: { type: "string" } }, { type: "list" } as Type)).toBe(true);
  expect(compatible({ type: "list" } as Type, { type: "list", of: { type: "string" } })).toBe(
    false,
  );
  expect(compatible({ type: "string" }, { type: "string" }, 9)).toBe(false);
});
it("STU-222: parameters preserve null/false/0, validate constraints and expose only document scopes", () => {
  const session = dataFixture();
  session.record.body.localContent = {
    t_title: { type: "text", value: { ru: "Text" } },
    t_image: { type: "asset", value: {} },
  };
  expect(parameterFields(session.record.body, session.manifest).map((f) => f.path)).toEqual([
    "$content.handle",
    "$local.t_title",
    "$context.locale",
  ]);
  expect(
    parameterFields({ ...session.record.body, content: undefined } as never, session.manifest).some(
      (f) => f.path.startsWith("$content"),
    ),
  ).toBe(false);
  expect(literal({ type: "boolean" }, "false")).toBe(false);
  expect(literal({ type: "number" }, "0")).toBe(0);
  expect(literal({ type: "text" }, "")).toBe("");
  expect(literal({ type: "enum", values: ["one"] }, "one")).toBe("one");
  for (const [schema, raw] of [
    [{ type: "boolean" }, "yes"],
    [{ type: "enum", values: ["one"] }, "two"],
    [{ type: "number", integer: true }, "1.1"],
    [{ type: "number", min: 1 }, "0"],
    [{ type: "number", max: 1 }, "2"],
    [{ type: "number" }, ""],
    [{ type: "string", maxLength: 2 }, "long"],
    [{ type: "string", minLength: 2 }, "x"],
    [{ type: "string", pattern: "^a$" }, "b"],
    [{ type: "object" }, "{}"],
  ] as [Type, string][])
    expect(() => literal(schema, raw)).toThrow();
  expect(supportsLiteral({ type: "asset" })).toBe(false);
  expect(sourceSupported(session.manifest.dataSources!["test.items"]!)).toBe(true);
  expect(sourceSupported(session.manifest.dataSources!["test.unsupported"]!)).toBe(false);
});
it("STU-225/226/227/228: Repeat creates one template, stable key, empty outside item scope", () => {
  const { session, doc, repeat, template, empty } = repeated();
  expect(itemScope(doc, session.manifest, "n_item")?.repeat).toBe(repeat.id);
  expect(
    bindingFields(doc, session.manifest, "n_item").fields.some((f) => f.path === "$item.title"),
  ).toBe(true);
  expect(itemScope(doc, session.manifest, empty.id)).toBeNull();
  expect(itemScope(doc, session.manifest, doc.root)).toBeNull();
  const list = resultFields(doc, session.manifest).fields.find(
    (f) => f.path === "$data.featured.items",
  )!;
  expect(
    repeatPlaces(
      doc,
      session.manifest,
      repeatTree(list, 2, "", new Set(Object.keys(doc.nodes))),
    ).some((p) => p.parentId === template.id),
  ).toBe(false);
  for (const [limit, key] of [
    [0, ""],
    [201, ""],
    [1, "$item.missing"],
  ] as [number, string][])
    expect(() => repeatTree(list, limit, key, new Set())).toThrow();
  expect(() => repeatTree({ path: "x", schema: { type: "string" } }, 1, "", new Set())).toThrow();
  expect(
    destinations(doc, session.manifest, subtree(doc, "n_item"), "n_item").some(
      (p) => p.parentId === doc.root,
    ),
  ).toBe(false);
});
it("STU-220/224/235: dataSources diff uses public per-key operations, binding/props exclude each other", () => {
  const { record, manifest } = dataFixture(),
    target = record.id;
  const operations = [sourceOperation(target, "pages", { source: "test.pages" })];
  const after = applyOperations(record.body, operations);
  expect(documentDiff(record.body, after, target)).toEqual(operations);
  const removed = applyOperations(after, [
    sourceOperation(target, "featured", null),
    sourceOperation(target, "pages", null),
  ]);
  expect(removed.dataSources).toBeUndefined();
  expect(documentDiff(after, removed, target)).toHaveLength(2);
  const node = record.body.nodes.n_heading!;
  node.props = { text: "Old" };
  expect(dataBinding(target, node, "text", "$data.pages")).toHaveLength(2);
  expect(dataBinding(target, node, "text", null)).toHaveLength(1);
  for (const path of ["$context.secret", "$data.featured.constructor"])
    expect(() => dataBinding(target, node, "text", path)).toThrow();
  expect(() => sourceOperation(target, "content", null)).toThrow();
  expect(() =>
    applyOperations(record.body, [
      { type: "document.setDataSource", target, payload: { key: "constructor", use: null } },
    ]),
  ).toThrow();
  expect(scopeProblems(record.body, after, manifest)).toEqual([]);
});
it("STU-229: deletion explicitly clears supported dependencies, blocks complex expressions and keys", () => {
  const { session, doc, repeat } = repeated(),
    target = session.record.id;
  const change = sourceChange(doc, session.manifest, target, "featured", null);
  expect(change.affected).toHaveLength(2);
  expect(change.blocked).toEqual([]);
  expect(change.operations.map((o) => o.type)).toEqual([
    "node.setBinding",
    "node.setBinding",
    "document.setDataSource",
  ]);
  doc.nodes.n_item!.when = { op: "exists", arg: "$item.title" };
  doc.nodes.n_item!.bindings = { text: { expr: "$item.title" } };
  expect(
    sourceChange(doc, session.manifest, target, "featured", null).blocked.length,
  ).toBeGreaterThan(0);
  delete doc.nodes.n_item!.when;
  doc.nodes.n_item!.bindings = { text: "$item.title" };
  session.manifest.dataSources!["test.other"] = {
    params: {},
    result: {
      type: "object",
      fields: {
        items: {
          type: "list",
          of: { type: "object", fields: { title: { type: "text", required: true } } },
        },
      },
    },
  };
  expect(
    sourceChange(doc, session.manifest, target, "featured", { source: "test.other" }).blocked,
  ).toContain(`/nodes/${repeat.id}/props/key`);
  const after = structuredClone(doc);
  after.nodes.n_item!.bindings = { text: "$item.count" };
  expect(scopeProblems(doc, after, session.manifest)).toHaveLength(1);
});
