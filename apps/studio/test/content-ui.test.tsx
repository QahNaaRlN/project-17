// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { webcrypto } from "node:crypto";
import ContentPanel from "../src/content-panel.js";
import { Studio } from "../src/ui.js";
import { createEditor, type Editor } from "../src/editor.js";
import { mockApi, documentId, changesetId } from "./fixture.js";
import { StudioError, type ContentRecord } from "../src/types.js";
const entityId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c55",
  assetId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c66";
let root: Root, editor: Editor, client: QueryClient;
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  client?.clear();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
const button = (name: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === name)!;
const control = (name: string) =>
  document.querySelector<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>(
    `[aria-label="${name}"]`,
  )!;
async function wait(check: () => void) {
  await vi.waitFor(async () => {
    await act(async () => {
      await Promise.resolve();
    });
    check();
  });
}
async function click(name: string) {
  const enabled = () =>
    [...document.querySelectorAll<HTMLButtonElement>("button")].find(
      (b) => b.textContent === name && !b.disabled,
    );
  await wait(() => expect(enabled()).toBeDefined());
  await act(async () => enabled()!.click());
  await wait(() => expect(editor.store.getState().status).not.toBe("saving"));
}
async function change(name: string, value: string) {
  await act(async () => {
    const e = control(name);
    const proto =
      e.tagName === "SELECT"
        ? HTMLSelectElement.prototype
        : e.tagName === "TEXTAREA"
          ? HTMLTextAreaElement.prototype
          : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, "value")!.set!.call(e, value);
    e.dispatchEvent(new Event(e.tagName === "SELECT" ? "change" : "input", { bubbles: true }));
  });
}
async function setup(full = false) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("crypto", webcrypto);
  const { api, session } = mockApi();
  session.manifest.schemas = {
    Article: {
      version: 1,
      title: "Статья",
      fields: {
        title: { type: "text", localized: true, required: true },
        flag: { type: "boolean" },
        choice: { type: "enum", values: ["one", "two"] },
        count: { type: "number", min: 0 },
        author: { type: "reference", schema: "Article" },
        cover: { type: "asset", assetKind: "image" },
        tags: { type: "list", of: { type: "string" } },
        body: { type: "richText" },
      },
    },
  };
  const records: Record<string, ContentRecord> = {
    [entityId]: {
      id: entityId,
      versionId: entityId,
      kind: "entity",
      schema: "Article",
      schemaVersion: 1,
      deleted: false,
      data: {
        title: { ru: "Статья", en: "Keep" },
        flag: false,
        choice: "one",
        count: 1,
        tags: ["keep"],
        body: { type: "doc", content: [] },
      },
    },
    [assetId]: {
      id: assetId,
      versionId: assetId,
      kind: "asset",
      deleted: false,
      data: {
        title: "Picture",
        assetKind: "image",
        mimeType: "image/png",
        fileHash: "a".repeat(64),
        alt: { ru: "old", en: "keep" },
      },
    },
  };
  vi.mocked(api.contentCatalog).mockImplementation(async (_s, kind, schema) => ({
    items: Object.values(records)
      .filter((r) => r.kind === kind && (!schema || r.schema === schema))
      .map((r) => ({
        id: r.id,
        kind: r.kind,
        schema: r.schema!,
        title: r.kind === "entity" ? "Статья" : "Picture",
        deleted: r.deleted,
        assetKind: "image",
        mimeType: "image/png",
      })),
    nextCursor: null,
    locales: ["ru", "en"],
    defaultLocale: "ru",
  }));
  vi.mocked(api.content).mockImplementation(async (_s, id) => structuredClone(records[id]!));
  vi.mocked(api.applyContent).mockImplementation(async (_s, ops) => {
    for (const op of ops) {
      if (op.type === "entity.create") {
        records[entityId] = {
          ...records[entityId]!,
          deleted: false,
          data: structuredClone(op.payload.data as Record<string, unknown>),
        };
      } else if (op.type === "entity.delete" || op.type === "entity.restore")
        records[op.target!]!.deleted = op.type === "entity.delete";
      else if (op.type === "asset.create")
        records[assetId]!.data = { ...records[assetId]!.data, fileHash: "b".repeat(64) };
      else if (op.type === "asset.replaceFile")
        Object.assign(records[op.target!]!.data, op.payload.set);
      else {
        const data = records[op.target!]!.data;
        for (const [path, v] of Object.entries((op.payload.set as object) ?? {})) {
          const [field, locale] = path.split(".");
          if (locale) (data[field!] as Record<string, unknown>)[locale] = v;
          else data[field!] = v;
        }
        for (const path of (op.payload.unset as string[]) ?? []) {
          const [field, locale] = path.split(".");
          if (locale) delete (data[field!] as Record<string, unknown>)[locale];
          else delete data[field!];
        }
      }
    }
    session.changeset.seq += ops.length;
    return ops.map((op) => op.target ?? (op.type === "asset.create" ? assetId : entityId));
  });
  editor = createEditor(api);
  await editor.open(documentId, changesetId);
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  document.body.innerHTML = '<div id="root"></div>';
  root = createRoot(document.getElementById("root")!);
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        {full ? <Studio api={api} editor={editor} /> : <ContentPanel api={api} editor={editor} />}
      </QueryClientProvider>,
    ),
  );
  if (full) await click("Контент");
  await wait(() => expect(document.body.textContent).toContain("Статья · " + entityId.slice(-8)));
  return { api, session, records };
}
async function selectEntity() {
  await click("Статья · " + entityId.slice(-8));
  await wait(() => expect(control("Content title ru")).not.toBeNull());
}
it("STU-101: новая сущность использует объявленную локаль проекта", async () => {
  const { api } = await setup();
  vi.mocked(api.contentCatalog).mockResolvedValue({
    items: [{ id: entityId, kind: "entity", schema: "Article", title: "Статья", deleted: false }],
    nextCursor: null,
    locales: ["kk"],
    defaultLocale: "kk",
  });
  await click("Обновить контент");
  await change("Схема контента", "Article");
  await wait(() => expect(button("Новая сущность").disabled).toBe(false));
  await click("Новая сущность");
  await wait(() => expect(control("Content title kk")).not.toBeNull());
  await change("Content title kk", "Мақала");
  await click("Сохранить контент");
  expect(api.applyContent).toHaveBeenLastCalledWith(
    expect.anything(),
    [
      expect.objectContaining({
        payload: expect.objectContaining({ data: { title: { kk: "Мақала" } } }),
      }),
    ],
    expect.any(String),
    expect.any(AbortSignal),
  );
});
it("STU-100/101/104: формы сохраняют поля/локали и блокируют уход, delete/restore явные", async () => {
  const { api, records } = await setup(true);
  await selectEntity();
  await change("Content title ru", "Изменение");
  await change("Content flag", "true");
  await change("Content choice", "two");
  await change("Content count", "2");
  await change("Content tags", '["new"]');
  expect(button("Контент").disabled).toBe(true);
  await editor.open(documentId, changesetId);
  expect(editor.store.getState().error).toBe("CONTENT_DRAFT_PENDING");
  await click("Сохранить контент");
  await wait(() => expect(records[entityId]!.data.title).toEqual({ ru: "Изменение", en: "Keep" }));
  expect(api.applyContent).toHaveBeenCalledTimes(1);
  await wait(() => expect(control("Content title ru")).not.toBeNull());
  vi.spyOn(window, "confirm").mockReturnValue(false);
  await click("Удалить сущность");
  expect(records[entityId]!.deleted).toBe(false);
  vi.mocked(window.confirm).mockReturnValue(true);
  await click("Удалить сущность");
  await wait(() => expect(button("Восстановить сущность")).toBeDefined());
  await click("Восстановить сущность");
  await wait(() => expect(records[entityId]!.deleted).toBe(false));
  await act(async () => editor.locale("en"));
  expect(document.body.textContent).toContain("Draft content");
  await click("Content");
  expect(document.querySelector('[aria-label="Content editor"]')).toBeNull();
});
it("STU-101/102: новая сущность, обязательные поля, RichText блоки/marks/JSON и refs", async () => {
  const { api, records } = await setup();
  await change("Схема контента", "Article");
  await click("Новая сущность");
  expect(document.body.textContent).toContain("Предупреждение");
  await change("Content title ru", "Created");
  await change("Content tags", "{");
  await act(async () =>
    control("Content tags")
      .closest(".content-field")!
      .querySelector<HTMLButtonElement>("button")!
      .click(),
  );
  expect(control("Content tags").value).toBe("null");
  await change("Content body add", "heading");
  await change("Content body/0 level", "3");
  await change("Content body/0 add", "text");
  await change("Content body/0/0 text", "Rich title");
  await act(async () => {
    const input = document.querySelector<HTMLInputElement>(".rich-node input[type=checkbox]")!;
    input.click();
  });
  await change("Content body add", "paragraph");
  await change("Content body/1 add", "text");
  await change("Content body/1/0 text", "Paragraph");
  await click("Выше");
  await click("RichText JSON");
  await change("Content body JSON", "{");
  expect(button("Редактор блоков").disabled).toBe(true);
  expect(button("Сохранить контент").disabled).toBe(true);
  await change(
    "Content body JSON",
    JSON.stringify({
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [{ type: "text", text: "Rich", marks: [{ type: "italic" }] }],
        },
      ],
    }),
  );
  await click("Редактор блоков");
  await wait(() => expect(control("Content author").querySelectorAll("option").length).toBe(2));
  await change("Content author", entityId);
  await wait(() => expect(control("Content cover").querySelectorAll("option").length).toBe(2));
  await change("Content cover", assetId);
  await click("Сохранить контент");
  expect(api.applyContent).toHaveBeenLastCalledWith(
    expect.anything(),
    [expect.objectContaining({ type: "entity.create" })],
    expect.any(String),
    expect.any(AbortSignal),
  );
  expect(records[entityId]!.data).toMatchObject({
    title: { ru: "Created" },
    author: { entityId },
    cover: { assetId },
    body: { type: "doc" },
  });
});
it("STU-101/104: JSON ошибка, unset, stale/сброс, schema failure, readonly", async () => {
  const { api, session, records } = await setup();
  await selectEntity();
  await change("Content tags", "{");
  expect(button("Сохранить контент").disabled).toBe(true);
  await click("Сбросить контент");
  await change("Content title ru", "unsaved");
  session.changeset.seq++;
  await act(async () => editor.refresh());
  await wait(() => expect(document.body.textContent).toContain("Версия черновика изменилась"));
  expect(control("Content title ru").value).toBe("unsaved");
  await click("Сбросить контент");
  await wait(() => expect(control("Content title ru").value).toBe("Статья"));
  await change("Content title ru", "Manifest buffer");
  session.manifestHash = "sha256:" + "b".repeat(64);
  await act(async () => editor.refresh());
  await wait(() => expect(document.body.textContent).toContain("Версия черновика изменилась"));
  expect(control("Content title ru").value).toBe("Manifest buffer");
  await click("Сбросить контент");
  await change("Content title ru", "Old head buffer");
  records[entityId]!.versionId = assetId;
  vi.mocked(api.applyContent).mockRejectedValueOnce(new StudioError("CONTENT_VERSION_CONFLICT"));
  await click("Сохранить контент");
  await wait(() => expect(document.body.textContent).toContain("Версия черновика изменилась"));
  expect(control("Content title ru").value).toBe("Old head buffer");
  await click("Сбросить контент");
  await click("Убрать значение");
  await click("Сохранить контент");
  await wait(() => expect(api.applyContent).toHaveBeenCalled());
  vi.mocked(api.content).mockRejectedValueOnce(new StudioError("SCHEMA_UPCAST_REQUIRED"));
  await click("Обновить контент");
  await wait(() => expect(document.body.textContent).toContain("SCHEMA_UPCAST_REQUIRED"));
  session.review.canEdit = false;
  session.changeset.state = "approved";
  await act(async () => editor.refresh());
  expect(button("Новая сущность").disabled).toBe(true);
});
it("STU-104: отказ сохраняет буфер; потерянное создание проверяется прежним ключом", async () => {
  const { api } = await setup();
  await selectEntity();
  await change("Content title ru", "unsaved");
  vi.mocked(api.applyContent).mockRejectedValueOnce(new StudioError("FORBIDDEN"));
  await click("Сохранить контент");
  expect(control("Content title ru").value).toBe("unsaved");
  await click("Сбросить контент");
  await change("Схема контента", "Article");
  await click("Новая сущность");
  await change("Content title ru", "new");
  vi.mocked(api.applyContent).mockRejectedValueOnce(new StudioError("NETWORK_UNCERTAIN", true));
  await click("Сохранить контент");
  expect(button("Проверить сохранение контента")).toBeDefined();
  await click("Проверить сохранение контента");
  const calls = vi.mocked(api.applyContent).mock.calls;
  expect(calls.at(-1)).toEqual(calls.at(-2));
});
it("STU-103: metadata сохраняется отдельно от файла, upload → ready → явный replace", async () => {
  const { api, session, records } = await setup();
  session.config.assetUploadOrigin = "https://bucket.test";
  await change("Тип контента", "asset");
  await wait(() => expect(button("Picture · " + assetId.slice(-8))).toBeDefined());
  await click("Picture · " + assetId.slice(-8));
  await wait(() => expect(control("Content alt ru")).not.toBeNull());
  await change("Content alt ru", "Новое");
  await change("Content focalPoint", '{"x":0.2,"y":0.7}');
  await change("Content tags", '["cover"]');
  await click("Сохранить контент");
  await wait(() => expect(records[assetId]!.data.alt).toEqual({ ru: "Новое", en: "keep" }));
  expect(records[assetId]!.data.fileHash).toBe("a".repeat(64));
  await wait(() => expect(control("Файл ассета")).not.toBeNull());
  const file = new File(["PNG test"], "test.png", { type: "image/png" });
  Object.defineProperty(file, "arrayBuffer", {
    value: async () => new TextEncoder().encode("PNG test").buffer,
  });
  await act(async () => {
    Object.defineProperty(control("Файл ассета"), "files", { value: [file], configurable: true });
    control("Файл ассета").dispatchEvent(new Event("change", { bubbles: true }));
  });
  const upload = {
    assetId,
    status: "pending" as const,
    expiresAt: new Date(Date.now() + 60000).toISOString(),
    uploadUrl: "https://bucket.test/signed?sig=hidden",
  };
  vi.mocked(api.uploadCommand).mockResolvedValue(upload);
  vi.mocked(api.uploadStatus).mockResolvedValue({
    ...upload,
    status: "ready",
    fileHash: "b".repeat(64),
  });
  const fetcher = vi.fn(async () => new Response(null, { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await click("Загрузить / повторить");
  await wait(() => expect(button("Заменить файл в черновике")).toBeDefined());
  expect(fetcher).toHaveBeenCalledWith(
    expect.any(String),
    expect.objectContaining({ method: "PUT", credentials: "omit", redirect: "error" }),
  );
  expect(document.body.textContent).not.toContain("sig=hidden");
  await click("Проверить обработку");
  await click("Заменить файл в черновике");
  await wait(() => expect(records[assetId]!.data.fileHash).toBe("b".repeat(64)));
  expect(records[assetId]!.data.alt).toEqual({ ru: "Новое", en: "keep" });
});
it("STU-103/114: выбор файла переживает завершение сохранения metadata", async () => {
  const { session } = await setup();
  session.config.assetUploadOrigin = "https://bucket.test";
  await change("Тип контента", "asset");
  await click("Picture · " + assetId.slice(-8));
  await wait(() => expect(control("Content alt ru")).not.toBeNull());
  await change("Content alt ru", "Новое");
  const file = new File(["test"], "keep.png", { type: "image/png" });
  let selected = false;
  const unsubscribe = editor.store.subscribe((state, previous) => {
    if (!selected && state.status === "ready" && previous.status === "saving") {
      selected = true;
      const input = control("Файл ассета");
      Object.defineProperty(input, "files", { value: [file], configurable: true });
      input.dispatchEvent(new Event("change", { bubbles: true }));
    }
  });
  try {
    await click("Сохранить контент");
    expect(selected).toBe(true);
    await wait(() => expect(button("Загрузить / повторить")).toBeDefined());
    expect(document.body.textContent).toContain("keep.png");
    expect(editor.store.getState().dirtyFields.contentUpload).toBe(true);
  } finally {
    unsubscribe();
  }
});
