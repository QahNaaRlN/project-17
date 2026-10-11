import { expect, it } from "vitest";
import fc from "fast-check";
import type { IrDocument } from "@cms/ir";
import { applyDocumentPatch, documentHash } from "../src/patch.js";
const doc = (): IrDocument => ({
  irVersion: "1.0",
  kind: "page",
  root: "root",
  nodes: {
    root: { id: "root", type: "Stack", children: ["text", "next"] },
    text: { id: "text", type: "Text", props: { text: "one" } },
    next: { id: "next", type: "Text", props: { text: "two" } },
  },
});
it("SDK-040: применяет все операции RFC 6902 атомарно, test сравнивает структуру", () => {
  const input = doc(),
    before = JSON.stringify(input);
  const result = applyDocumentPatch(input, [
    { op: "test", path: "/nodes/text/props", value: { text: "one" }, ignored: true },
    { op: "replace", path: "/nodes/text/props/text", value: "edited" },
    { op: "add", path: "/nodes/root/design", value: { gap: { raw: 12 } } },
    { op: "copy", from: "/nodes/root/design", path: "/nodes/text/design" },
    { op: "move", from: "/nodes/root/children/0", path: "/nodes/root/children/-" },
    { op: "remove", path: "/nodes/text/design" },
  ]);
  expect(result.nodes.root!.children).toEqual(["next", "text"]);
  expect(result.nodes.text!.props).toEqual({ text: "edited" });
  expect(result.nodes.text!.design).toBeUndefined();
  expect(JSON.stringify(input)).toBe(before);
  expect(() =>
    applyDocumentPatch(input, [
      { op: "replace", path: "/nodes/text/props/text", value: "changed" },
      { op: "test", path: "/kind", value: "component" },
    ]),
  ).toThrow("CMS_PREVIEW_PATCH_INVALID");
  expect(JSON.stringify(input)).toBe(before);
  expect(applyDocumentPatch(input, [{ op: "add", path: "", value: input }])).toEqual(input);
  expect(
    applyDocumentPatch(input, [
      { op: "replace", path: "/nodes/root/children/0", value: "next" },
      { op: "replace", path: "/nodes/root/children/1", value: "text" },
    ]).nodes.root!.children,
  ).toEqual(["next", "text"]);
  expect(
    applyDocumentPatch(input, [
      { op: "remove", path: "" },
      { op: "add", path: "", value: input },
    ]),
  ).toEqual(input);
  expect(
    applyDocumentPatch(input, [
      { op: "move", from: "/nodes/text/props/text", path: "/nodes/text/props/text" },
    ]),
  ).toEqual(input);
});
it("SDK-040: JSON Pointer корректно декодирует ~0/~1 и заменяет существующий ключ", () => {
  const input = doc();
  input.nodes.text!.props = { text: "one", "a/b~c": 1 };
  const result = applyDocumentPatch(input, [
    { op: "replace", path: "/nodes/text/props/a~1b~0c", value: 2 },
    { op: "add", path: "/nodes/text/props/text", value: "new" },
  ]);
  expect(result.nodes.text!.props).toEqual({ text: "new", "a/b~c": 2 });
});
it.each([
  {},
  [null],
  [{ op: "unknown", path: "/kind" }],
  [{ op: "add", path: "/kind" }],
  [{ op: "replace", path: "/nodes/missing", value: {} }],
  [{ op: "remove", path: "/nodes/missing" }],
  [{ op: "add", path: "/missing/child", value: 1 }],
  [{ op: "add", path: "/kind/child", value: 1 }],
  [{ op: "add", path: "bad", value: 1 }],
  [{ op: "add", path: "/nodes/~2", value: 1 }],
  [{ op: "add", path: "/__proto__/polluted", value: true }],
  [{ op: "copy", from: "/constructor", path: "/kind" }],
  [{ op: "move", from: "/nodes", path: "/nodes/nested" }],
  [{ op: "remove", path: "" }],
  [{ op: "replace", path: "/root", value: "missing" }],
  [{ op: "remove", path: "/nodes/text" }],
  [{ op: "remove", path: "/nodes/root/children/2" }],
  [{ op: "remove", path: "/nodes/root/children/00" }],
  [{ op: "add", path: "/nodes/root/children/3", value: "text" }],
  [{ op: "remove", path: "/nodes/root/children/-" }],
  [{ op: "add", path: "/" + "a".repeat(2049), value: true }],
  Array(129).fill({ op: "test", path: "/kind", value: "page" }),
])("SDK-040: некорректный patch %j не возвращает частичный документ", (patch) => {
  const input = doc(),
    before = JSON.stringify(input);
  expect(() => applyDocumentPatch(input, patch)).toThrow("CMS_PREVIEW_PATCH_INVALID");
  expect(JSON.stringify(input)).toBe(before);
});
it("SDK-040: hash каноничен, изменения меняют hash, произвольный patch даёт результат или безопасную ошибку", async () => {
  const input = doc();
  expect(await documentHash(input)).toBe(await documentHash(JSON.parse(JSON.stringify(input))));
  expect(
    await documentHash(
      applyDocumentPatch(input, [
        { op: "replace", path: "/nodes/text/props/text", value: "changed" },
      ]),
    ),
  ).not.toBe(await documentHash(input));
  await expect(documentHash({ ...input, root: "missing" })).rejects.toThrow(
    "CMS_PREVIEW_DOCUMENT_INVALID",
  );
  fc.assert(
    fc.property(fc.jsonValue(), (patch) => {
      try {
        applyDocumentPatch(input, patch);
      } catch (cause) {
        expect((cause as Error).message).toBe("CMS_PREVIEW_PATCH_INVALID");
      }
    }),
    { seed: 20261012, numRuns: 50 },
  );
});
