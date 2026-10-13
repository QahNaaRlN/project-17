import { expect, it } from "vitest";
import fc from "fast-check";
import type { IrDocument } from "@cms/ir";
import { applyOperations } from "../src/operations.js";
import { documentDiff, semanticDocument, retainedOrder, editedDocument } from "../src/json-diff.js";
import { analyzeJson, jsonRanges, jsonSuggestions, pointerRange } from "../src/json-source.js";
import { fixture, documentId } from "./fixture.js";
import diffFixture from "./fixtures/json-diff.json";
import type { Operation } from "../src/types.js";
const base = () => fixture().record.body;
it("STU-091/094: общий контракт TS diff и Go Command API", () => {
  const f = diffFixture as unknown as {
    before: IrDocument;
    after: IrDocument;
    target: string;
    operations: Operation[];
  };
  expect(documentDiff(f.before, f.after, f.target)).toEqual(f.operations);
  expect(semanticDocument(applyOperations(f.before, f.operations))).toEqual(f.after);
});
function roundTrip(before: IrDocument, after: IrDocument) {
  const ops = documentDiff(before, after, documentId);
  expect(semanticDocument(applyOperations(before, ops))).toEqual(semanticDocument(after));
  return ops;
}
it("STU-091: форматирование/порядок ключей/пустые контейнеры не создают правки", () => {
  const doc = base(),
    candidate = JSON.parse(JSON.stringify(doc));
  candidate.nodes.n_heading.props = { level: 2 };
  candidate.nodes.n_heading.children = [];
  candidate.nodes.n_heading.slots = { empty: [] };
  expect(documentDiff(doc, candidate, documentId)).toEqual([]);
  expect(analyzeJson(doc, JSON.stringify(doc), documentId).operations).toEqual([]);
});
it("STU-091: props/design объединяют set/unset; binding/event/name/when и text сохраняются точно", () => {
  const before = base(),
    after = base();
  before.nodes.n_heading!.design = { padding: "sm", gap: "sm" };
  before.nodes.n_heading!.on = {
    click: { action: "scrollTo", args: { target: { lit: "n_custom" } } },
  };
  after.nodes.n_heading!.props = { foo: { nested: 1 } };
  after.nodes.n_heading!.design = { padding: "lg" };
  after.nodes.n_heading!.bindings = { text: "$local.t_title" };
  after.nodes.n_heading!.name = "Title";
  after.nodes.n_heading!.when = { op: "exists", arg: "$local.t_title" };
  before.localContent = { t_remove: { type: "text", value: { ru: "gone" } } };
  after.localContent = { t_title: { type: "text", value: { ru: "новый", en: "new" } } };
  const ops = roundTrip(before, after);
  expect(ops.find((o) => o.type === "node.setProps")).toMatchObject({
    payload: { set: { foo: { nested: 1 } }, unset: ["level"] },
  });
  expect(ops.find((o) => o.type === "node.setDesign")).toMatchObject({
    payload: { set: { padding: "lg" }, unset: ["gap"] },
  });
  expect(ops.find((o) => o.type === "node.setBehavior")).toMatchObject({
    payload: { event: "click", handlers: null },
  });
  expect(ops.map((o) => o.type)).toContain("localContent.remove");
  const back = roundTrip(after, before);
  expect(back.find((o) => o.type === "node.setBinding")).toMatchObject({
    payload: { binding: null },
  });
});
it("STU-091: новое поддерево добавляется одним insert, удаляемое — одним remove", () => {
  const before = base(),
    after = base();
  after.nodes.n_new = { id: "n_new", type: "Stack", children: ["n_new_child"] };
  after.nodes.n_new_child = { id: "n_new_child", type: "Text", props: { text: "New" } };
  after.nodes.n_root!.children!.push("n_new");
  expect(roundTrip(before, after)).toHaveLength(1);
  expect(roundTrip(after, before)).toEqual([
    { type: "node.remove", target: documentId, payload: { nodeId: "n_new" } },
  ]);
});
it("STU-091: перенос спасает существующий узел из удаляемого родителя и разворачивает предков", () => {
  const before: IrDocument = {
    irVersion: "1.0",
    kind: "page",
    root: "n_root",
    nodes: {
      n_root: { id: "n_root", type: "Box", children: ["n_parent"] },
      n_parent: { id: "n_parent", type: "Box", children: ["n_child"] },
      n_child: { id: "n_child", type: "Box" },
    },
  };
  const after = structuredClone(before);
  after.nodes.n_root!.children = ["n_child"];
  after.nodes.n_child!.children = ["n_parent"];
  delete after.nodes.n_parent!.children;
  expect(roundTrip(before, after).filter((o) => o.type === "node.move")).toHaveLength(2);
  const removed = structuredClone(after);
  delete removed.nodes.n_parent;
  delete removed.nodes.n_child!.children;
  expect(roundTrip(before, removed).map((o) => o.type)).toEqual(["node.move", "node.remove"]);
});
it("STU-091: новый родитель сохраняет existing IDs и точный порядок children/slots", () => {
  const before = base(),
    after = base();
  after.nodes.n_root!.children = ["n_new"];
  delete after.nodes.n_root!.slots;
  after.nodes.n_new = {
    id: "n_new",
    type: "Box",
    children: ["n_custom", "n_heading"],
    slots: { footer: ["n_footer"] },
  };
  const ops = roundTrip(before, after);
  expect(ops.filter((o) => o.type === "node.insert")).toHaveLength(1);
  expect(ops.filter((o) => o.type === "node.move")).toHaveLength(3);
});
it("STU-091: перестановки сохраняют LCS и используют минимальное число move", () => {
  fc.assert(
    fc.property(
      fc.integer({ min: 1, max: 12 }).chain((n) =>
        fc.shuffledSubarray(
          Array.from({ length: n }, (_, i) => "n_item_" + i),
          { minLength: n, maxLength: n },
        ),
      ),
      (ids) => {
        const original = [...ids].sort(),
          before: IrDocument = {
            irVersion: "1.0",
            kind: "page",
            root: "n_root",
            nodes: { n_root: { id: "n_root", type: "Box", children: original } },
          };
        for (const id of original) before.nodes[id] = { id, type: "Box" };
        const after = structuredClone(before);
        after.nodes.n_root!.children = ids;
        const ops = roundTrip(before, after);
        const table = Array.from({ length: original.length + 1 }, () =>
          Array<number>(ids.length + 1).fill(0),
        );
        for (let i = 1; i <= original.length; i++)
          for (let j = 1; j <= ids.length; j++)
            table[i]![j] =
              original[i - 1] === ids[j - 1]
                ? table[i - 1]![j - 1]! + 1
                : Math.max(table[i - 1]![j]!, table[i]![j - 1]!);
        expect(ops).toHaveLength(ids.length - table.at(-1)!.at(-1)!);
        expect(retainedOrder(original, ids).size).toBe(table.at(-1)!.at(-1));
      },
    ),
    { numRuns: 100 },
  );
});
it("STU-092: local text защищённого узла нельзя изменить через общую запись", () => {
  const before = base();
  before.nodes.n_heading!.locked = true;
  before.nodes.n_heading!.bindings = { text: "$local.t_title" };
  before.localContent = { t_title: { type: "text", value: { ru: "keep", en: "keep" } } };
  const after = structuredClone(before);
  after.localContent!.t_title!.value = { ru: "changed", en: "keep" };
  expect(() => documentDiff(before, after, documentId)).toThrow("JSON_NODE_READ_ONLY");
  delete after.localContent!.t_title;
  expect(() => documentDiff(before, after, documentId)).toThrow("JSON_NODE_READ_ONLY");
});
it.each(["type", "ref", "zone", "locked"])(
  "STU-092: %s существующего узла не подменяется",
  (field) => {
    const before = base(),
      after = base();
    Object.assign(after.nodes.n_heading!, {
      [field]:
        field === "type"
          ? "Text"
          : field === "locked"
            ? true
            : field === "ref"
              ? { component: documentId }
              : { mode: "FREE" },
    });
    expect(() => documentDiff(before, after, documentId)).toThrow();
  },
);
it("STU-092: ID, root и декларации не заменяются; locked поддерево защищено", () => {
  const before = base(),
    after = base();
  expect(() =>
    editedDocument(before, { ...before.nodes.n_heading, id: "n_other" }, "n_heading"),
  ).toThrow("JSON_NODE_ID_IMMUTABLE");
  after.nodes.n_other = { ...after.nodes.n_heading!, id: "n_other" };
  delete after.nodes.n_heading;
  after.nodes.n_root!.children![0] = "n_other";
  expect(() => documentDiff(before, after, documentId)).toThrow("JSON_NODE_ID_IMMUTABLE");
  expect(() => documentDiff(before, { ...before, meta: { title: "new" } }, documentId)).toThrow(
    "JSON_FIELD_IMMUTABLE",
  );
  before.nodes.n_heading!.locked = true;
  const locked = structuredClone(before);
  locked.nodes.n_heading!.name = "bad";
  expect(() => documentDiff(before, locked, documentId)).toThrow("JSON_NODE_READ_ONLY");
});
it.each([null, {}, "bad", { id: "n_heading", type: "Heading", children: ["missing"] }])(
  "STU-092: неверная форма JSON узла диагностируется",
  (value) => {
    expect(
      analyzeJson(base(), JSON.stringify(value), documentId, "n_heading").diagnostics.length,
    ).toBeGreaterThan(0);
  },
);
it.each([
  "{",
  '{"id":"n_heading","id":"n_heading"}',
  '{"__proto__":{}}',
  "1e400",
  '"\\ud800"',
  "[".repeat(65) + "0" + "]".repeat(65),
])("STU-092: синтаксис, дубли, опасные ключи, числа и глубина отклоняются", (text) => {
  const result = analyzeJson(base(), text, documentId);
  expect(result.operations).toEqual([]);
  expect(result.diagnostics.length).toBe(1);
});
it("STU-092: text — единственный изменяемый localContent; лимиты batch/body обязательны", () => {
  const before = base(),
    after = base();
  after.localContent = { t_bad: { type: "text", value: { ru: null } } };
  expect(() => documentDiff(before, after, documentId)).toThrow();
  const large = base();
  large.nodes.n_custom!.props = { text: "x".repeat(1024 * 1024) };
  expect(() => documentDiff(before, large, documentId)).toThrow("JSON_BATCH_LIMIT");
  expect(
    analyzeJson(before, " ".repeat(2 * 1024 * 1024 + 1), documentId).diagnostics[0]?.code,
  ).toBe("JSON_BODY_LIMIT");
  const many = base(),
    modified = base();
  for (let i = 0; i < 201; i++) {
    const id = "n_many_" + i;
    many.nodes[id] = { id, type: "Box" };
    many.nodes.n_root!.children!.push(id);
  }
  Object.assign(modified, structuredClone(many));
  for (const n of Object.values(modified.nodes)) n.name = "Changed";
  expect(() => documentDiff(many, modified, documentId)).toThrow("JSON_BATCH_LIMIT");
});
it("STU-090: pointers/ranges и подсказки используют IR schema и manifest", () => {
  const s = fixture(),
    text = JSON.stringify(s.record.body),
    ranges = jsonRanges(text);
  const r = pointerRange(ranges, "/nodes/n_heading/props/level");
  expect(text.slice(r.from, r.to)).toBe("2");
  expect(pointerRange(ranges, "/nodes/n_heading/props/missing").from).toBeGreaterThan(0);
  expect(pointerRange([], "/missing")).toEqual({ from: 0, to: 0 });
  expect(jsonSuggestions(s.record.body, s.manifest, "", undefined)).toContain("nodes");
  expect(jsonSuggestions(s.record.body, s.manifest, "/props", "n_custom")).toContain("title");
  expect(jsonSuggestions(s.record.body, s.manifest, "/design", "n_custom")).toContain("padding");
  expect(jsonSuggestions(s.record.body, s.manifest, "/type", "n_custom")).toContain("Custom");
  expect(jsonSuggestions(s.record.body, s.manifest, "", "n_custom")).toContain("bindings");
});
it("STU-092: разбор произвольного текста никогда не падает и не меняет базу", () => {
  const doc = base(),
    snapshot = structuredClone(doc);
  fc.assert(
    fc.property(fc.string(), (text) => {
      const result = analyzeJson(doc, text, documentId);
      expect(Array.isArray(result.diagnostics)).toBe(true);
    }),
    { numRuns: 100 },
  );
  expect(doc).toEqual(snapshot);
});
