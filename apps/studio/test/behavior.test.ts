import { expect, it } from "vitest";
import { readFileSync } from "node:fs";
import type { Type, Fields } from "@cms/manifest";
import {
  behaviorFields,
  builtinBehavior,
  actionFields,
  validOperand,
  editableChain,
  editablePredicate,
  predicateStats,
  conditionLocked,
  behaviorDependencies,
  behaviorOperation,
  conditionOperation,
} from "../src/behavior.js";
import {
  compatible,
  sourceChange as dataChange,
  repeatTree,
  resultFields,
  insertRepeat,
  scopeProblems,
} from "../src/data-sources.js";
import type { Operation } from "../src/types.js";
import { sourceChange as contentChange } from "../src/content-bindings.js";
import { dataFixture } from "./data-sources-fixture.js";
import { applyOperations } from "../src/operations.js";
function setup() {
  const session = dataFixture(),
    doc = session.record.body,
    manifest = session.manifest;
  doc.nodes.n_heading!.type = "Button";
  manifest.actions = {
    "test.use": { args: { value: { type: "string", required: true } } },
    "test.optional": { args: { flag: { type: "boolean" }, count: { type: "number", default: 0 } } },
  };
  return {
    session,
    doc,
    manifest,
    fields: behaviorFields(doc, manifest, "n_heading", "/:slug").fields,
  };
}
it("STU-240/242/243/244: shared Go/TS fixture agrees on builtin schemas, strict types and canonical operations", () => {
  const f = JSON.parse(
    readFileSync(
      new URL("../../../packages/ir/fixtures/studio-behavior.json", import.meta.url),
      "utf8",
    ),
  ) as {
    builtins: Record<string, Fields>;
    compatibility: { name: string; from: Type; to: Type; valid: boolean }[];
    operations: (Operation & { field: "on" | "when"; expected: unknown })[];
  };
  expect(builtinBehavior).toEqual(f.builtins);
  for (const tc of f.compatibility) expect(compatible(tc.from, tc.to), tc.name).toBe(tc.valid);
  let { doc } = setup();
  for (const step of f.operations) {
    doc = applyOperations(doc, [{ ...step, target: "page" }]);
    expect(doc.nodes.n_root![step.field] ?? null).toEqual(step.expected);
  }
});
it("STU-237/238/240: contracts and strict operands distinguish missing, null, false, zero and empty", () => {
  const { doc, manifest, fields } = setup();
  expect(actionFields(manifest, "navigate")!.to!.type).toBe("link");
  expect(actionFields(manifest, "scrollTo")!.target!.type).toBe("nodeRef");
  expect(actionFields(manifest, "missing")).toBeUndefined();
  manifest.actions!.empty = {};
  expect(actionFields(manifest, "empty")).toEqual({});
  const cases: [unknown, Type, boolean][] = [
    [undefined, { type: "string", required: true }, false],
    [undefined, { type: "number", required: true, default: 0 }, true],
    [{ lit: null }, { type: "boolean" }, true],
    [{ lit: null }, { type: "boolean", required: true }, false],
    [{ lit: false }, { type: "boolean" }, true],
    [{ lit: 0 }, { type: "number" }, true],
    [{ lit: "" }, { type: "string" }, true],
    [{ lit: 42 }, { type: "string" }, false],
    [{ lit: 42 }, { type: "text" }, false],
    [{ lit: {} }, { type: "string" }, false],
    [{ lit: "x" }, { type: "enum", values: ["ok"] }, false],
    [{ lit: 2 }, { type: "number", max: 1 }, false],
    ["$context.locale", { type: "string" }, true],
    ["$context.viewer.authenticated", { type: "string" }, false],
    ["$missing", { type: "string" }, false],
    [{ expr: "$context.locale" }, { type: "string" }, false],
    [{ lit: "ok", extra: true }, { type: "string" }, false],
    [{ lit: {} }, { type: "object", fields: {} }, false],
    [{ lit: "n_root" }, { type: "nodeRef" }, true],
    [{ lit: "missing" }, { type: "nodeRef" }, false],
    [{ lit: "n_root" }, { type: "nodeRef", nodeType: "Modal" }, false],
    [{ lit: "2026-10-16" }, { type: "date" }, true],
    [{ lit: "2026-02-30" }, { type: "date" }, false],
    [{ lit: "garbage" }, { type: "date" }, false],
    [{ lit: "2026-10-16T12:30:00Z" }, { type: "datetime" }, true],
    [{ lit: "2026-10-16" }, { type: "datetime" }, false],
    [{ lit: { kind: "url", url: "https://example.test" } }, { type: "link" }, true],
    [{ lit: { kind: "url", url: "javascript:alert(1)" } }, { type: "link" }, false],
    [{ lit: { kind: "anchor", node: "n_root" } }, { type: "link" }, true],
    [{ lit: { kind: "anchor", node: "missing" } }, { type: "link" }, false],
    [{ lit: { kind: "page", page: "n_root" } }, { type: "link" }, false],
    [{ lit: "https://example.test" }, { type: "link" }, false],
  ];
  for (const [value, schema, valid] of cases)
    expect(validOperand(value, schema, fields, doc), JSON.stringify([value, schema])).toBe(valid);
  const good = { action: "test.use", args: { value: "$context.locale" } };
  for (const count of [1, 8, 9, 0])
    expect(
      editableChain(
        Array.from({ length: count }, () => good),
        manifest,
        fields,
        doc,
      ),
    ).toBe(count >= 1 && count <= 8);
  expect(editableChain(good, manifest, fields, doc)).toBe(true);
  for (const call of [
    null,
    { action: "pending:x" },
    { action: "missing" },
    { ...good, extra: 1 },
    { action: "test.use", args: { unknown: { lit: 1 } } },
    { action: "test.use" },
    { action: "openModal", args: { target: { lit: "n_root" } } },
  ])
    expect(editableChain(call, manifest, fields, doc)).toBe(false);
  expect(
    editableChain(
      { action: "test.optional", args: { flag: { lit: false }, count: { lit: 0 } } },
      manifest,
      fields,
      doc,
    ),
  ).toBe(true);
});
it("STU-241: fields follow schemas, ancestor provides, route declarations and Repeat empty scope", () => {
  const { session, doc, manifest } = setup();
  doc.localContent = { title: { type: "text", value: { ru: "RU", en: "EN" } } };
  manifest.components!.Provider = {
    origin: "native",
    container: true,
    provides: { current: { type: "number", required: true } },
  };
  doc.nodes.n_root!.type = "Provider";
  const list = resultFields(doc, manifest).fields.find((f) => f.path === "$data.featured.items")!;
  const tree = repeatTree(list, 4, "$item.id", new Set(Object.keys(doc.nodes)));
  session.record.body = applyOperations(doc, [
    insertRepeat(session.record.id, { parentId: doc.root, index: 0, slot: "" }, tree),
  ]);
  const updated = session.record.body,
    item = updated.nodes[tree.id!]!.children![0]!,
    empty = updated.nodes[tree.id!]!.slots!.empty![0]!;
  const paths = behaviorFields(updated, manifest, item, "/:slug").fields.map((f) => f.path);
  expect(paths).toEqual(
    expect.arrayContaining([
      "$content.handle",
      "$local.title",
      "$context.current",
      "$context.route.params.slug",
      "$item.id",
    ]),
  );
  expect(
    behaviorFields(updated, manifest, empty).fields.some((f) => f.path.startsWith("$item")),
  ).toBe(false);
  manifest.schemas!.Collection!.fields = Object.fromEntries(
    Array.from({ length: 210 }, (_, i) => ["v" + i, { type: "text" }]),
  );
  expect(behaviorFields(updated, manifest, item).limited).toBe(true);
  expect(behaviorFields(updated, manifest, item).fields).toHaveLength(200);
  delete updated.content;
  expect(
    behaviorFields(updated, manifest, "absent").fields.some((f) => f.path.startsWith("$content")),
  ).toBe(false);
});
it("STU-244/245/252: typed predicates, group size, depth and total budget preserve unsupported trees", () => {
  const { doc, fields } = setup();
  fields.push(
    { path: "$local.n", schema: { type: "number" } },
    { path: "$local.day", schema: { type: "date" } },
    { path: "$local.obj", schema: { type: "object", fields: {} } },
  );
  const exists = { op: "exists", arg: "$context.locale" };
  const valid = (p: unknown) => editablePredicate(p, fields, doc);
  expect(valid(exists)).toBe(true);
  expect(valid({ op: "empty", arg: "$data.featured" })).toBe(true);
  for (const op of ["eq", "neq"]) {
    for (const right of [{ lit: "" }, { lit: null }, "$content.handle"])
      expect(valid({ op, args: ["$context.locale", right] })).toBe(true);
  }
  for (const op of ["gt", "gte", "lt", "lte"]) {
    expect(valid({ op, args: ["$local.n", { lit: 0 }] })).toBe(true);
    expect(valid({ op, args: ["$local.n", { lit: null }] })).toBe(false);
    expect(valid({ op, args: ["$local.day", { lit: "2026-10-16" }] })).toBe(false);
  }
  for (const count of [0, 1, 16, 17]) {
    expect(valid({ op: "and", args: Array.from({ length: count }, () => exists) })).toBe(
      count >= 1 && count <= 16,
    );
    expect(valid({ op: "or", args: Array.from({ length: count }, () => exists) })).toBe(
      count >= 1 && count <= 16,
    );
    expect(
      valid({
        op: "in",
        args: ["$context.locale", { lit: Array.from({ length: count }, () => "ru") }],
      }),
    ).toBe(count >= 1 && count <= 16);
  }
  for (const p of [
    null,
    {},
    { op: "eval" },
    { op: "exists", arg: "$event.x" },
    { ...exists, extra: true },
    { op: "eq", args: ["$local.obj", { lit: {} }] },
    { op: "eq", args: ["$context.locale"] },
    { op: "in", args: ["$context.locale", { lit: ["ru", 1] }] },
    { op: "in", args: ["$context.locale", { lit: [null] }] },
  ])
    expect(valid(p)).toBe(false);
  let p: unknown = exists;
  for (let i = 0; i < 8; i++) p = { op: "not", arg: p };
  expect(valid(p)).toBe(true);
  expect(predicateStats(p)).toEqual({ count: 9, depth: 8 });
  expect(valid({ op: "not", arg: p })).toBe(false);
  const big = {
    op: "and",
    args: Array.from({ length: 4 }, () => ({
      op: "or",
      args: Array.from({ length: 15 }, () => exists),
    })),
  };
  expect(predicateStats(big).count).toBe(65);
  expect(valid(big)).toBe(false);
  big.args[0]!.args.pop();
  expect(valid(big)).toBe(true);
  expect(predicateStats({ op: "and", args: null }).count).toBe(1);
});
it("STU-241/243/251: compatible changes preserve behavior; incompatible changes remove whole expressions explicitly", () => {
  const { doc, manifest, session } = setup();
  doc.nodes.n_heading!.on = { click: { action: "test.use", args: { value: "$content.handle" } } };
  doc.nodes.n_heading!.when = { op: "eq", args: ["$content.handle", { lit: "summer" }] };
  const after = structuredClone(doc);
  delete after.content;
  const change = behaviorDependencies(doc, after, manifest, session.record.id, "n_heading", [
    "$content",
  ]);
  expect(change.blocked).toEqual([]);
  expect(change.operations).toHaveLength(2);
  const content = contentChange(doc, manifest, session.record.id, null);
  expect(content.blocked).toEqual(["/dataSources/featured/params/collection"]);
  expect(content.operations.map((o) => o.type)).toEqual([
    "node.setBehavior",
    "node.setCondition",
    "document.setContent",
  ]);
  expect(contentChange(doc, manifest, session.record.id, doc.content!).operations).toHaveLength(1);
  doc.nodes.n_heading!.on!.click = { action: "test.use", args: { value: "$data.featured.items" } };
  expect(dataChange(doc, manifest, session.record.id, "featured", null).blocked).toContain(
    "/nodes/n_heading/on/click",
  );
  doc.nodes.n_heading!.on!.click = { action: "test.use", args: { value: "$context.locale" } };
  doc.nodes.n_heading!.when = { op: "exists", arg: "$data.featured.items" };
  expect(
    dataChange(doc, manifest, session.record.id, "featured", null).operations.map((o) => o.type),
  ).toContain("node.setCondition");
  expect(
    dataChange(doc, manifest, session.record.id, "featured", doc.dataSources!.featured!).blocked,
  ).toEqual([]);
  doc.nodes.n_heading!.locked = true;
  expect(conditionLocked(doc, doc.root)).toBe(true);
  expect(conditionLocked(doc, "absent")).toBe(false);
  expect(dataChange(doc, manifest, session.record.id, "featured", null).blocked).toContain(
    "/nodes/n_heading/when",
  );
  delete doc.nodes.n_heading!.locked;
  const broken = structuredClone(doc);
  delete broken.dataSources;
  expect(scopeProblems(doc, broken, manifest)).toContain("/nodes/n_heading/when");
  const op = behaviorOperation(session.record.id, "n_heading", "click", []);
  expect(op.payload.handlers).toBeNull();
  expect(conditionOperation(session.record.id, "n_heading", null).payload.when).toBeNull();
});
