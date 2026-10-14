import { describe, expect, it } from "vitest";
import fc from "fast-check";
import { fields, fieldOperations, designOperation, designFields, layers } from "../src/fields.js";
import { fixture, documentId } from "./fixture.js";
import { applyOperations } from "../src/operations.js";
describe("STU-003/IR-041: manifest forms and catalog operations", () => {
  it("STU-138/141: отступы предлагаются только применимым компонентам", () => {
    const s = fixture();
    expect(designFields({ id: "n_test", type: "Heading" }, s.manifest)).toEqual([]);
    expect(designFields({ id: "n_test", type: "Box" }, s.manifest)).toEqual(["padding"]);
    expect(designFields({ id: "n_test", type: "Button" }, s.manifest)).toEqual(["padding"]);
    for (const type of ["Stack", "Flex", "Grid"])
      expect(designFields({ id: "n_test", type }, s.manifest)).toEqual(["padding", "gap"]);
    s.manifest.components!.Custom!.container = true;
    expect(designFields({ id: "n_test", type: "Custom" }, s.manifest)).toEqual(["padding"]);
    expect(designFields({ id: "n_test", type: "Missing" }, s.manifest)).toEqual([]);
  });
  it("STU-141: правка literal text удаляет props перед созданием локальной привязки", () => {
    const s = fixture(),
      doc = s.record.body,
      node = doc.nodes.n_heading!;
    delete node.bindings;
    node.props = { level: 2, text: "Literal text" };
    const field = fields(doc, node, s.manifest).find((item) => item.name === "text")!;
    const result = applyOperations(
      doc,
      fieldOperations(documentId, node, field, "Edited text", "t_edit_title"),
    );
    expect(result.nodes.n_heading!.props).toEqual({ level: 2 });
    expect(result.nodes.n_heading!.bindings).toEqual({ text: "$local.t_edit_title" });
    expect(result.localContent!.t_edit_title!.value).toEqual({ ru: "Edited text" });
  });
  it("строит builtin/native формы и защищает bindings/структуры", () => {
    const s = fixture(),
      doc = s.record.body;
    expect(fields(doc, doc.nodes.n_custom!, s.manifest).map((f) => [f.name, f.editable])).toEqual([
      ["enabled", true],
      ["title", true],
      ["count", true],
      ["choice", true],
      ["object", false],
      ["image", false],
    ]);
    expect(
      fields(doc, doc.nodes.n_footer!, s.manifest).find((f) => f.name === "text")?.editable,
    ).toBe(false);
    doc.nodes.n_custom!.props = { title: { base: "responsive" } };
    expect(fields(doc, doc.nodes.n_custom!, s.manifest)[1]!.editable).toBe(false);
    expect(fields(doc, { id: "n_other", type: "Missing" }, s.manifest)).toEqual([]);
  });
  it("сохраняет текст локально с новой приватной привязкой и локалью", () => {
    const s = fixture(),
      node = s.record.body.nodes.n_heading!,
      field = fields(s.record.body, node, s.manifest).find((f) => f.name === "text")!;
    const operations = fieldOperations(documentId, node, field, "Новый текст", "t_new_title");
    expect(operations).toEqual([
      {
        type: "localContent.set",
        target: documentId,
        payload: { key: "t_new_title", type: "text", value: { ru: "Новый текст" } },
      },
      {
        type: "node.setBinding",
        target: documentId,
        payload: { nodeId: "n_heading", prop: "text", binding: "$local.t_new_title" },
      },
    ]);
    s.record.body.localContent = { t_title: { type: "text", value: { ru: "Старый", en: "Old" } } };
    node.bindings = { text: "$local.t_title" };
    expect(
      fields(s.record.body, node, s.manifest, "en").find((f) => f.name === "text")?.value,
    ).toBe("Old");
    expect(() => fieldOperations(documentId, node, field, "x", "bad.key")).toThrow("FIELD_INVALID");
  });
  it("проверяет числовые границы, enum, boolean, длину и readonly", () => {
    const s = fixture(),
      node = s.record.body.nodes.n_custom!,
      all = fields(s.record.body, node, s.manifest);
    const run = (name: string, value: string) =>
      fieldOperations(
        documentId,
        node,
        all.find((f) => f.name === name)!,
        value,
        "t_key",
      );
    expect(run("enabled", "false")[0]!.payload.set).toEqual({ enabled: false });
    expect(run("choice", "two")[0]!.payload.set).toEqual({ choice: "two" });
    expect(run("count", "10")[0]!.payload.set).toEqual({ count: 10 });
    for (const [name, value] of [
      ["count", ""],
      ["count", "11"],
      ["count", "-1"],
      ["count", "Infinity"],
      ["enabled", "yes"],
      ["choice", "other"],
      ["title", ""],
      ["title", "x".repeat(11)],
      ["object", "x"],
    ])
      expect(() => run(name!, value!)).toThrow();
    const heading = s.record.body.nodes.n_heading!,
      level = fields(s.record.body, heading, s.manifest)[0]!;
    expect(() => fieldOperations(documentId, heading, level, "2.5", "t_key")).toThrow(
      "FIELD_INVALID",
    );
  });
  it("проверяет токены и сохраняет другие breakpoint значения", () => {
    const s = fixture(),
      node = s.record.body.nodes.n_root!;
    expect(designOperation(documentId, node, "gap", "sm", s.manifest).payload.set).toEqual({
      gap: "sm",
    });
    node.design = { padding: { base: { raw: 8 }, md: { raw: 24 } } };
    expect(designOperation(documentId, node, "padding", "12", s.manifest).payload.set).toEqual({
      padding: { base: { raw: 12 }, md: { raw: 24 } },
    });
    expect(
      designOperation(documentId, node, "padding", "16", s.manifest, "sm").payload.set,
    ).toEqual({ padding: { base: { raw: 8 }, md: { raw: 24 }, sm: { raw: 16 } } });
    node.design = {};
    expect(
      designOperation(documentId, node, "padding", "16", s.manifest, "sm").payload.set,
    ).toEqual({ padding: { base: { raw: 0 }, sm: { raw: 16 } } });
    for (const [name, value, point] of [
      ["color", "1", "base"],
      ["padding", "-1", "base"],
      ["gap", "", "base"],
      ["gap", "4001", "base"],
      ["gap", "unknown", "base"],
      ["gap", "12", "xl"],
    ])
      expect(() => designOperation(documentId, node, name!, value!, s.manifest, point)).toThrow(
        "FIELD_INVALID",
      );
  });
  it("дерево учитывает slots и унаследованные блокировки", () => {
    const s = fixture();
    s.record.body.nodes.n_root!.locked = true;
    s.record.body.nodes.n_heading!.name = "Название";
    expect(layers(s.record.body)).toEqual([
      { id: "n_root", depth: 1, parent: null, label: "Box", locked: true },
      { id: "n_heading", depth: 2, parent: "n_root", label: "Название", locked: true },
      { id: "n_custom", depth: 2, parent: "n_root", label: "Custom", locked: true },
      { id: "n_footer", depth: 2, parent: "n_root", label: "Text", locked: true },
    ]);
    s.record.body.nodes.n_root!.children!.push("n_missing", "n_root");
    expect(layers(s.record.body)).toHaveLength(4);
  });
  it("любое введённое значение не меняет исходный документ", () => {
    const s = fixture(),
      node = s.record.body.nodes.n_root!,
      original = structuredClone(node);
    fc.assert(
      fc.property(fc.string(), (raw) => {
        try {
          designOperation(documentId, node, "gap", raw, s.manifest);
        } catch {
          /* invalid input */
        }
        expect(node).toEqual(original);
      }),
    );
  });
});
