import { expect, it } from "vitest";
import fc from "fast-check";
import { normalize, validateDocument, type NestedNode, type IrDocument } from "@cms/ir";
import {
  canRemove,
  childList,
  contract,
  createBlock,
  destinations,
  duplicate,
  insertion,
  location,
  palette,
  structuralApply,
  subtree,
} from "../src/structure.js";
import { fixture, documentId } from "./fixture.js";
const gen = () => {
  let n = 0;
  return () => "n_copy_" + ++n;
};
it("IR-022/STU-063: копия переназначает ссылки и не изменяет исходные actions", () => {
  const { record, manifest } = fixture(),
    doc = record.body;
  doc.nodes.n_heading!.on = {
    click: { action: "scrollTo", args: { target: { lit: "n_custom" } } },
  };
  const before = structuredClone(doc);
  const copy = duplicate(doc, doc.root, manifest, gen());
  expect(copy.children![0]!.on!.click).toEqual({
    action: "scrollTo",
    args: { target: { lit: "n_copy_3" } },
  });
  expect(doc).toEqual(before);
});
it("STU-060: каталог, расширенные примитивы, required inputs и специальные типы", () => {
  const s = fixture();
  s.manifest.primitives = { Badge: { props: { title: { type: "string", required: true } } } };
  s.manifest.components!.Complex = { props: { asset: { type: "asset", required: true } } };
  s.manifest.components!.Slotted = { slots: { main: { min: 1 } } };
  const items = palette(s.manifest);
  expect(items.find((i) => i.type === "Badge")).toMatchObject({
    supported: true,
    required: [["title", { type: "string", required: true }]],
  });
  for (const type of ["Repeat", "Slot", "Composed", "Complex", "Slotted"])
    expect(items.find((i) => i.type === type)?.supported).toBe(false);
  expect(contract("Badge", s.manifest)?.props?.title?.type).toBe("string");
  expect(contract("missing", s.manifest)).toBeUndefined();
});
it("STU-060: создание локального текста, defaults и typed required значений", () => {
  const s = fixture();
  const block = createBlock(
    "Heading",
    s.manifest,
    documentId,
    "en",
    { text: "Hello" },
    new Set(),
    gen(),
  );
  expect(block.tree).toMatchObject({
    id: "n_copy_1",
    type: "Heading",
    bindings: { text: expect.stringMatching(/^\$local\.t_/) },
  });
  expect(block.operations[0]).toMatchObject({
    type: "localContent.set",
    payload: { value: { en: "Hello" } },
  });
  s.manifest.components!.Form = {
    props: {
      count: { type: "number", required: true, min: 2 },
      enabled: { type: "boolean", required: true },
      choice: { type: "enum", values: ["a", "b"], default: "b" },
      caption: { type: "text", content: true, default: "Caption" },
    },
  };
  const form = createBlock(
    "Form",
    s.manifest,
    documentId,
    "ru",
    { count: "2", enabled: "false" },
    new Set(),
    gen(),
  );
  expect(form.tree.props).toEqual({ count: 2, enabled: false, choice: "b" });
  expect(form.operations[0]!.payload.value).toEqual({ ru: "Caption" });
  expect(() => createBlock("Form", s.manifest, documentId, "ru", {}, new Set())).toThrow(
    "FIELD_INVALID",
  );
  expect(() => createBlock("Composed", s.manifest, documentId, "ru", {}, new Set())).toThrow(
    "STRUCTURE_SETUP_REQUIRED",
  );
  expect(
    createBlock("Text", s.manifest, documentId, "ru", {}, new Set()).operations[0]!.payload.value,
  ).toEqual({ ru: "Text" });
});
it("STU-061: только контейнеры/объявленные слоты, границы max/min и allowedTypes", () => {
  const s = fixture(),
    doc = s.record.body;
  s.manifest.components!.Custom!.slots = { footer: { allowedTypes: ["Text"], min: 1, max: 1 } };
  doc.nodes.n_custom!.slots = { footer: ["n_footer"] };
  delete doc.nodes.n_root!.slots;
  const text: NestedNode = { id: "n_new_text", type: "Text" };
  expect(destinations(doc, s.manifest, text).filter((p) => p.parentId === "n_custom")).toEqual([]);
  expect(canRemove(doc, "n_footer", s.manifest)).toBe(false);
  expect(destinations(doc, s.manifest, subtree(doc, "n_footer"), "n_footer")).toEqual([]);
  s.manifest.components!.Custom!.slots.footer!.min = 0;
  s.manifest.components!.Custom!.slots.footer!.max = 2;
  expect(destinations(doc, s.manifest, text).filter((p) => p.parentId === "n_custom")).toHaveLength(
    2,
  );
  expect(
    destinations(doc, s.manifest, { id: "n_new_head", type: "Heading" }).some(
      (p) => p.parentId === "n_custom",
    ),
  ).toBe(false);
  expect(destinations(doc, s.manifest, text).some((p) => p.parentId === "n_heading")).toBe(false);
});
// DS-022: the palette offers only what the effective mode allows.
it("STU-061: пересечение политик, STRICT, zones, unknown/Composed и циклы", () => {
  const s = fixture(),
    doc = s.record.body;
  doc.policy = { allowedTypes: ["Text", "Heading", "Custom"], deniedTypes: ["Heading"] };
  doc.nodes.n_root!.zone = { id: "test", mode: "STRICT", allowedTypes: ["Custom", "Text"] };
  expect(destinations(doc, s.manifest, { id: "n_new_text", type: "Text" })).toEqual([]);
  expect(destinations(doc, s.manifest, { id: "n_new_cust", type: "Custom" })).toHaveLength(3);
  expect(destinations(doc, s.manifest, { id: "n_new_head", type: "Heading" })).toEqual([]);
  doc.nodes.n_root!.zone.mode = "SYSTEM";
  expect(destinations(doc, s.manifest, { id: "n_new_text", type: "Text" })).toHaveLength(3);
  expect(
    destinations(doc, s.manifest, {
      id: "n_new_comp",
      type: "Composed",
      ref: { component: documentId },
    }),
  ).toEqual([]);
  delete doc.policy;
  delete doc.nodes.n_root!.zone;
  doc.nodes.n_root!.type = "Unknown";
  expect(destinations(doc, s.manifest, { id: "n_new_text", type: "Text" })).toEqual([]);
  doc.nodes.n_root!.type = "Box";
  expect(destinations(doc, s.manifest, subtree(doc, doc.root), doc.root)).toEqual([]);
  expect(canRemove(doc, doc.root, s.manifest)).toBe(false);
});
it("STU-061: locks запрещают вставку до защищённого соседа и удаление/движение предка", () => {
  const s = fixture(),
    doc = s.record.body;
  doc.nodes.n_custom!.locked = true;
  expect(
    destinations(doc, s.manifest, { id: "n_new_text", type: "Text" }).filter((p) => !p.slot),
  ).toEqual([{ parentId: "n_root", slot: "", index: 2 }]);
  expect(canRemove(doc, "n_heading", s.manifest)).toBe(false);
  expect(canRemove(doc, "n_custom", s.manifest)).toBe(false);
  expect(destinations(doc, s.manifest, subtree(doc, "n_custom"), "n_custom")).toEqual([]);
  doc.nodes.n_root!.zone = { id: "locked", mode: "SYSTEM", locked: true };
  expect(destinations(doc, s.manifest, { id: "n_new_text", type: "Text" })).toEqual([]);
  expect(canRemove(doc, "n_footer", s.manifest)).toBe(false);
});
it("STU-064: insert/remove/move поддерживают слоты и индекс после изъятия", () => {
  const s = fixture(),
    doc = s.record.body;
  const tree: NestedNode = {
    id: "n_new_box",
    type: "Box",
    slots: { empty: [{ id: "n_new_text", type: "Text" }] },
  };
  structuralApply(doc, insertion(documentId, { parentId: "n_root", slot: "", index: 1 }, tree));
  expect(doc.nodes.n_root!.children).toEqual(["n_heading", "n_new_box", "n_custom"]);
  structuralApply(doc, {
    type: "node.move",
    target: documentId,
    payload: { nodeId: "n_new_box", parentId: "n_root", slot: "footer", index: 0 },
  });
  expect(location(doc, "n_new_box")).toEqual({ parentId: "n_root", slot: "footer", index: 0 });
  structuralApply(doc, {
    type: "node.remove",
    target: documentId,
    payload: { nodeId: "n_new_box" },
  });
  expect(doc.nodes.n_new_box).toBeUndefined();
  expect(doc.nodes.n_new_text).toBeUndefined();
  expect(() =>
    structuralApply(
      doc,
      insertion(
        documentId,
        { parentId: "n_root", slot: "", index: 0 },
        { id: "n_heading", type: "Text" },
      ),
    ),
  ).toThrow("NODE_ID_DUPLICATE");
  expect(() =>
    structuralApply(doc, { type: "node.move", target: documentId, payload: { nodeId: "n_root" } }),
  ).toThrow("ROOT_IMMUTABLE");
  expect(() => childList(doc, { parentId: "missing", slot: "", index: 0 })).toThrow(
    "NODE_NOT_FOUND",
  );
  expect(() => subtree(doc, "missing")).toThrow("NODE_NOT_FOUND");
});
it("IR-022/STU-063: typed nodeRef/list/object/link и actions, внешние ссылки и строки", () => {
  const s = fixture(),
    doc = s.record.body;
  s.manifest.components!.Custom!.props = {
    target: { type: "nodeRef" },
    items: {
      type: "list",
      of: {
        type: "object",
        fields: { link: { type: "link" }, node: { type: "nodeRef" }, text: { type: "string" } },
      },
    },
  };
  s.manifest.actions = { custom: { args: { target: { type: "nodeRef" } } } };
  doc.nodes.n_custom!.props = {
    target: "n_heading",
    items: [{ link: { kind: "anchor", node: "n_heading" }, node: "n_outside", text: "n_heading" }],
  };
  doc.nodes.n_custom!.bindings = { target: "$local.t_shared" };
  doc.nodes.n_custom!.on = {
    click: [
      { action: "custom", args: { target: { lit: "n_heading" } } },
      { action: "navigate", args: { to: { kind: "anchor", node: "n_heading" } } },
      { action: "scrollTo", args: { target: "$context.node" } },
      { action: "track" },
    ],
  };
  const copy = duplicate(doc, doc.root, s.manifest, gen());
  const custom = copy.children![1]!;
  expect(custom.props).toEqual({
    target: "n_copy_2",
    items: [{ link: { kind: "anchor", node: "n_copy_2" }, node: "n_outside", text: "n_heading" }],
  });
  expect(custom.bindings).toEqual({ target: "$local.t_shared" });
  expect(custom.on!.click).toEqual([
    { action: "custom", args: { target: { lit: "n_copy_2" } } },
    { action: "navigate", args: { to: { kind: "anchor", node: "n_copy_2" } } },
    { action: "scrollTo", args: { target: "$context.node" } },
    { action: "track" },
  ]);
  expect(copy.slots!.footer![0]!.id).toBe("n_copy_4");
});
it("IR-022/STU-063 property: новые ID, связность, исходный документ и порядок сохраняются", () => {
  fc.assert(
    fc.property(fc.array(fc.string(), { minLength: 1, maxLength: 30 }), (labels) => {
      const s = fixture(),
        doc = normalize({
          irVersion: "1.0",
          kind: "page",
          root: {
            id: "n_root",
            type: "Stack",
            children: labels.map((name, i) => ({ id: "n_item_" + i, type: "Text", name })),
          },
        });
      const before = structuredClone(doc),
        tree = duplicate(doc, doc.root, s.manifest, gen());
      structuralApply(doc, insertion(documentId, { parentId: doc.root, slot: "", index: 0 }, tree));
      expect(validateDocument(doc).valid).toBe(true);
      expect(tree.children!.map((n) => n.name)).toEqual(labels);
      expect(
        Object.keys(normalize({ irVersion: "1.0", kind: "page", root: tree }).nodes).every(
          (id) => !before.nodes[id],
        ),
      ).toBe(true);
      for (const [id, node] of Object.entries(before.nodes))
        if (id !== before.root) expect(doc.nodes[id]).toEqual(node);
      structuralApply(doc, {
        type: "node.remove",
        target: documentId,
        payload: { nodeId: tree.id },
      });
      expect(doc).toEqual(before);
    }),
    { seed: 60, numRuns: 40 },
  );
});
it("IR-022/STU-063: responsive nodeRef и fallback привязки ссылаются на копию", () => {
  const s = fixture(),
    doc = s.record.body;
  s.manifest.components!.Custom!.props = {
    target: { type: "nodeRef", responsive: true },
    fallback: { type: "nodeRef" },
  };
  doc.nodes.n_custom!.props = { target: { base: "n_heading", sm: "n_external" } };
  doc.nodes.n_custom!.bindings = { fallback: { expr: "$context.target", default: "n_heading" } };
  const before = structuredClone(doc),
    copy = duplicate(doc, doc.root, s.manifest, gen());
  expect(copy.children![1]!.props).toEqual({ target: { base: "n_copy_2", sm: "n_external" } });
  expect(copy.children![1]!.bindings).toEqual({
    fallback: { expr: "$context.target", default: "n_copy_2" },
  });
  expect(doc).toEqual(before);
});
it("STU-061 property: допустимые места совпадают с защищёнными путями фактического дерева", () => {
  function paths(doc: IrDocument) {
    const out = new Map<string, string>();
    function walk(id: string, path: string, inherited: boolean) {
      const n = doc.nodes[id]!,
        locked = inherited || !!n.locked || !!n.zone?.locked;
      if (locked) out.set(id, path);
      n.children?.forEach((child, index) => walk(child, path + "/c/" + index, locked));
      Object.entries(n.slots ?? {}).forEach(([slot, list]) =>
        list.forEach((child, index) => walk(child, path + "/s/" + slot + "/" + index, locked)),
      );
    }
    walk(doc.root, doc.root, false);
    return out;
  }
  fc.assert(
    fc.property(fc.array(fc.boolean(), { minLength: 2, maxLength: 7 }), (locks) => {
      const s = fixture(),
        doc = normalize({
          irVersion: "1.0",
          kind: "page",
          root: {
            id: "n_root",
            type: "Stack",
            children: locks.map((locked, i) => ({
              id: "n_box_" + i,
              type: "Box",
              children: [{ id: "n_text_" + i, type: "Text", locked }],
            })),
          },
        });
      const free = structuredClone(doc);
      for (const n of Object.values(free.nodes)) delete n.locked;
      const protectedBefore = paths(doc);
      const unchanged = (next: IrDocument) => {
        const after = paths(next);
        return [...protectedBefore].every(
          ([id, path]) =>
            after.get(id) === path &&
            JSON.stringify(next.nodes[id]) === JSON.stringify(doc.nodes[id]),
        );
      };
      for (const moving of [undefined, "n_box_0", "n_text_0", "n_box_1", "n_text_1"]) {
        const tree = moving ? subtree(doc, moving) : { id: "n_new_item", type: "Text" };
        const candidates = destinations(
          free,
          s.manifest,
          moving ? subtree(free, moving) : tree,
          moving,
        );
        const expected = candidates.filter((place) => {
          const next = structuredClone(doc);
          structuralApply(
            next,
            moving
              ? { type: "node.move", target: documentId, payload: { ...place, nodeId: moving } }
              : insertion(documentId, place, tree),
          );
          return unchanged(next);
        });
        expect(destinations(doc, s.manifest, tree, moving)).toEqual(expected);
        if (moving) {
          const next = structuredClone(doc);
          structuralApply(next, {
            type: "node.remove",
            target: documentId,
            payload: { nodeId: moving },
          });
          expect(canRemove(doc, moving, s.manifest)).toBe(unchanged(next));
        }
      }
    }),
    { seed: 61, numRuns: 25 },
  );
});
