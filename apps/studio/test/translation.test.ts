import { expect, it } from "vitest";
import { fields, fieldOperations } from "../src/fields.js";
import { fixture, documentId } from "./fixture.js";
it("IR-041/STU-003: новая локальная копия сохраняет переводы и не меняет общий ключ", () => {
  const s = fixture(),
    doc = s.record.body,
    node = doc.nodes.n_heading!;
  doc.localContent = { t_shared: { type: "text", value: { ru: "Русский", en: "English" } } };
  node.bindings = { text: "$local.t_shared" };
  const field = fields(doc, node, s.manifest, "ru").find((f) => f.name === "text")!;
  const operation = fieldOperations(documentId, node, field, "Новый русский", "t_private")[0]!;
  expect(operation.payload).toEqual({
    key: "t_private",
    type: "text",
    value: { ru: "Новый русский", en: "English" },
  });
  expect(doc.localContent.t_shared!.value).toEqual({ ru: "Русский", en: "English" });
});
