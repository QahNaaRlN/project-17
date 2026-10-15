import { expect, it, vi } from "vitest";
import {
  contentCompatible,
  contentFields,
  bindingOperations,
  sourceChange,
} from "../src/content-bindings.js";
import { applyOperations } from "../src/operations.js";
import { documentDiff } from "../src/json-diff.js";
import { createEditor } from "../src/editor.js";
import { StudioError } from "../src/types.js";
import { fixture, mockApi, persist, documentId, changesetId } from "./fixture.js";
import type { Type } from "@cms/manifest";
function setup() {
  const session = fixture();
  session.manifest.schemas = {
    Article: {
      version: 1,
      fields: {
        title: { type: "text", localized: true },
        count: { type: "number" },
        cover: { type: "asset", assetKind: "image" },
        nested: { type: "object", fields: { name: { type: "string" } } },
        ref: { type: "reference", schema: "Article" },
        list: { type: "list", of: { type: "string" } },
      },
    },
  };
  session.record.body.content = {
    schema: "Article",
    resolve: { by: "entity", entityId: documentId },
  };
  return session;
}
it("STU-206/207/211: дерево выбирает прямые поля и object, сохраняя missing, null, false и 0", () => {
  const session = setup();
  const result = contentFields(
    session.manifest,
    "Article",
    { title: { en: "Title" }, count: 0, cover: null, nested: { name: "" } },
    "ru",
  );
  expect(result.fields.map((f) => f.path)).toEqual([
    "$content.title",
    "$content.count",
    "$content.cover",
    "$content.nested.name",
  ]);
  expect(result.fields[0]).toMatchObject({ present: false, value: undefined });
  expect(result.fields[1]).toMatchObject({ present: true, value: 0 });
  expect(result.fields[2]).toMatchObject({ present: true, value: null });
  expect(result.fields[3]).toMatchObject({ value: "" });
  expect(contentFields(session.manifest, "Missing").fields).toEqual([]);
  expect(contentCompatible({ type: "number" }, { type: "text" })).toBe(false);
  expect(contentCompatible({ type: "text" }, { type: "text", required: true })).toBe(false);
  expect(
    contentCompatible({ type: "text", required: true }, { type: "text", required: true }),
  ).toBe(true);
  expect(
    contentCompatible({ type: "asset", assetKind: "image" }, { type: "asset", assetKind: "image" }),
  ).toBe(true);
  expect(
    contentCompatible({ type: "asset", assetKind: "file" }, { type: "asset", assetKind: "image" }),
  ).toBe(false);
  expect(contentCompatible({ type: "boolean" }, { type: "boolean" })).toBe(true);
  expect(
    contentCompatible(
      { type: "number", integer: true, min: 1, max: 5 },
      { type: "number", integer: true, min: 0, max: 10 },
    ),
  ).toBe(true);
  expect(
    contentCompatible({ type: "enum", values: ["a"] }, { type: "enum", values: ["a", "b"] }),
  ).toBe(true);
  expect(
    contentCompatible({ type: "enum", values: ["a", "b"] }, { type: "enum", values: ["a"] }),
  ).toBe(false);
  expect(
    contentCompatible(
      { type: "string", maxLength: 3, pattern: "a" },
      { type: "string", maxLength: 5, pattern: "a" },
    ),
  ).toBe(true);
  expect(contentCompatible({ type: "string" }, { type: "string", maxLength: 5 })).toBe(false);
});
it("STU-206: ограничения глубины и количества не разрешают prototype-пути", () => {
  const session = setup();
  const fields: Record<string, Type> = Object.fromEntries(
    Array.from({ length: 201 }, (_, i) => ["f" + i, { type: "text" }]),
  );
  let nested: Type = { type: "text" };
  for (let i = 0; i < 8; i++) nested = { type: "object", fields: { child: nested } };
  session.manifest.schemas!.Article!.fields = {
    deep: nested,
    ...fields,
    ["constructor"]: { type: "text" as const },
  };
  expect(contentFields(session.manifest, "Article")).toMatchObject({ limited: true });
  expect(contentFields(session.manifest, "Article").fields).toHaveLength(200);
  expect(() =>
    bindingOperations(
      documentId,
      session.record.body.nodes.n_heading!,
      "text",
      "$content.__proto__.name",
    ),
  ).toThrow();
});
it("STU-208/209/217: явная замена удаляет props, unbind сохраняет общий localContent, JSON diff применяет декларацию", () => {
  const session = setup(),
    before = session.record.body,
    node = before.nodes.n_heading!;
  node.props = { text: "Static" };
  before.localContent = { t_title: { type: "text", value: { ru: "Keep" } } };
  const after = applyOperations(
    before,
    bindingOperations(documentId, node, "text", "$content.title"),
  );
  expect(after.nodes.n_heading!.props).toBeUndefined();
  expect(after.nodes.n_heading!.bindings!.text).toBe("$content.title");
  const plain = applyOperations(
    after,
    bindingOperations(documentId, after.nodes.n_heading!, "text", null),
  );
  expect(plain.localContent).toEqual(before.localContent);
  const change = sourceChange(after, session.manifest, documentId, null);
  expect(change.affected).toHaveLength(2);
  expect(change.blocked).toEqual([]);
  const removed = applyOperations(after, change.operations);
  expect(removed).not.toHaveProperty("content");
  expect(applyOperations(after, documentDiff(after, removed, documentId))).toEqual(removed);
  after.nodes.n_root!.when = { op: "exists", arg: "$content.title" };
  expect(sourceChange(after, session.manifest, documentId, null).blocked).toContain(
    "/nodes/n_root/when/arg",
  );
});
it("STU-209/217: одинаковая схема сохраняет сложные выражения, несовместимый источник блокирует их", () => {
  const session = setup(),
    doc = session.record.body;
  doc.nodes.n_heading!.bindings = { text: { expr: "$content.title", default: "fallback" } };
  const same = sourceChange(doc, session.manifest, documentId, {
    schema: "Article",
    resolve: { by: "entity", entityId: changesetId },
  });
  expect(same.blocked).toEqual([]);
  expect(same.affected).toEqual([]);
  expect(sourceChange(doc, session.manifest, documentId, null).blocked).toContain(
    "/nodes/n_heading/bindings/text/expr",
  );
});
it("STU-213/214: потеря ответа повторяет точные key/seq/ops без optimistic patch", async () => {
  const { api, session } = mockApi(setup()),
    editor = createEditor(api);
  await editor.open(documentId, changesetId);
  const calls: unknown[][] = [];
  let committed = false;
  vi.mocked(api.apply).mockImplementation(async (...args) => {
    calls.push(structuredClone(args.slice(0, 3)));
    if (!committed) {
      committed = true;
      persist(session, args[1]);
      throw new StudioError("NETWORK_UNCERTAIN", true);
    }
  });
  const frame = { postMessage: vi.fn() };
  editor.bindFrame(frame);
  await editor.apply(
    bindingOperations(documentId, session.record.body.nodes.n_heading!, "text", "$content.title"),
  );
  expect(editor.store.getState()).toMatchObject({ status: "uncertain", bindingRetry: true });
  await editor.retryBinding();
  expect(calls[1]).toEqual(calls[0]);
  expect(editor.store.getState()).toMatchObject({ status: "ready", bindingRetry: false });
  expect(frame.postMessage.mock.calls.some(([message]) => message.type === "document.patch")).toBe(
    false,
  );
  editor.dispose();
});
it("STU-211/213: выбор локали до первого ready не подтверждает handshake заранее", async () => {
  const { api, session } = mockApi(setup()),
    editor = createEditor(api);
  await editor.open(documentId, changesetId);
  const frame = { postMessage: vi.fn() };
  editor.bindFrame(frame);
  editor.locale("en");
  expect(frame.postMessage.mock.calls.filter(([m]) => m.type === "init")).toHaveLength(0);
  const ready = {
    origin: session.config.appOrigin,
    source: frame,
    data: { cms: "preview", v: 1, type: "ready", payload: { documentId, protocolVersion: 1 } },
  };
  editor.receive(ready);
  expect(editor.store.getState().connected).toBe(false);
  editor.receive({
    ...ready,
    data: {
      cms: "preview",
      v: 1,
      type: "diagnostics",
      payload: { items: [{ code: "BINDING_VALUE_MISSING", message: "private value" }] },
    },
  });
  expect(frame.postMessage.mock.calls.find(([m]) => m.type === "init")?.[0].payload.locale).toBe(
    "en",
  );
  editor.receive(ready);
  expect(editor.store.getState().connected).toBe(true);
  expect(editor.store.getState().previewError).toBe("BINDING_VALUE_MISSING");
  expect(JSON.stringify(editor.store.getState())).not.toContain("private value");
  editor.receive({
    ...ready,
    data: { cms: "preview", v: 1, type: "diagnostics", payload: { items: [] } },
  });
  expect(editor.store.getState().previewError).toBeNull();
  editor.dispose();
});
