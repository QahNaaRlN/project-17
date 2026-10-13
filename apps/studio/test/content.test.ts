import { expect, it, vi } from "vitest";
import fc from "fast-check";
import {
  assetFields,
  contentDiff,
  contentProblems,
  uploadLimit,
  uploadUrl,
  equal,
} from "../src/content.js";
import { createApi } from "../src/api.js";
import { createEditor } from "../src/editor.js";
import { StudioError, type ContentRecord, type ContentOperation } from "../src/types.js";
import { mockApi, documentId, changesetId, fixture } from "./fixture.js";
const id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c44";
const record: ContentRecord = {
  id,
  versionId: id,
  kind: "entity",
  schema: "Article",
  schemaVersion: 1,
  deleted: false,
  data: { title: { ru: "before", en: "keep" }, flag: false, list: [1] },
};
const fields = {
  title: { type: "text" as const, localized: true },
  flag: { type: "boolean" as const },
  list: { type: "list" as const, of: { type: "number" as const } },
};
it("STU-101: некорректный Unicode/числа дают диагностику без падения буфера", () => {
  expect(() => equal({ title: "valid" }, { title: "\ud800" })).not.toThrow();
  expect(equal({ title: null }, { title: Infinity })).toBe(false);
  expect(contentProblems({ title: "\ud800" }, { title: { type: "text" } })).toEqual([
    { path: "/", warning: false },
  ]);
});
it("STU-101: diff сохраняет другие локали и различает отсутствие/null/пустое", () => {
  const after = { ...record.data, title: { ru: "after", en: "keep" }, flag: null };
  expect(contentDiff(record, after, fields)).toEqual([
    {
      type: "entity.setFields",
      target: id,
      expectedVersionId: record.versionId,
      payload: { set: { flag: null, "title.ru": "after" } },
    },
  ]);
  expect(contentDiff(record, { title: { en: "keep" }, list: [1] }, fields)[0]!.payload).toEqual({
    unset: ["flag", "title.ru"],
  });
  expect(contentDiff(record, record.data, fields)).toEqual([]);
  expect(
    contentDiff(
      { ...record, kind: "asset" },
      { ...record.data, title: { ru: "changed", en: "keep" } },
      fields,
    )[0]!.type,
  ).toBe("asset.updateMeta");
  expect(() => contentDiff(record, { ...record.data, unknown: "change" }, fields)).toThrow(
    "CONTENT_FIELD_READ_ONLY",
  );
  expect(() => contentDiff(record, { ...record.data, flag: "bad" }, fields)).toThrow(
    "FIELD_INVALID",
  );
});
it("STU-101/102: required — предупреждение, типы/locale/rich text — ошибка", () => {
  expect(contentProblems({}, { title: { type: "text", required: true } })).toEqual([
    { path: "title", warning: true },
  ]);
  for (const [value, path] of [
    [1, "title"],
    [[], "title"],
    [{ bad_locale: "x" }, "title.bad_locale"],
    [{ ru: 1 }, "title.ru"],
  ] as const)
    expect(contentProblems({ title: value }, fields)).toContainEqual({
      path,
      warning: false,
    });
  expect(contentProblems({ title: null }, fields)).toEqual([]);
  expect(
    contentProblems(
      { object: {} },
      {
        object: {
          type: "object",
          fields: { title: { type: "text", localized: true, required: true } },
        },
      },
      ["kk"],
    ),
  ).toEqual([{ path: "object.title", warning: true }]);
  expect(
    contentProblems(
      { title: { ru: "old" } },
      { title: { type: "text", localized: true, required: true } },
      ["kk"],
    ),
  ).toEqual([
    { path: "title.ru", warning: false },
    { path: "title.kk", warning: true },
  ]);
  expect(
    contentProblems({ object: { unknown: true } }, { object: { type: "object", fields: {} } }),
  ).toEqual([{ path: "object", warning: false }]);
  expect(
    contentProblems(
      { list: [{}] },
      {
        list: {
          type: "list",
          min: 1,
          of: { type: "object", fields: { title: { type: "text", required: true } } },
        },
      },
    ),
  ).toEqual([{ path: "list.0.title", warning: true }]);
  expect(contentProblems({ title: { ru: "" } }, fields)).toEqual([]);
  expect(contentProblems({ body: "<p>HTML</p>" }, { body: { type: "richText" } })[0]!.warning).toBe(
    false,
  );
  expect(
    contentProblems(
      { body: { type: "doc", content: [{ type: "heading", attrs: { level: 2 }, content: [] }] } },
      { body: { type: "richText", blocks: ["paragraph"] } },
    ),
  ).toHaveLength(1);
  expect(contentProblems({ focalPoint: { x: 2, y: 0.5 } }, assetFields)).toHaveLength(1);
  expect(contentProblems({ unsafe: JSON.parse('{"__proto__":1}') }, fields)[0]!.path).toBe("/");
  fc.assert(
    fc.property(fc.jsonValue(), (value) => {
      expect(() => contentProblems({ flag: value }, fields)).not.toThrow();
    }),
    { numRuns: 100 },
  );
});
it("STU-103: MIME пределы и origin загрузки явно ограничены", () => {
  expect(uploadLimit("image/png")).toBe(25 * 1024 * 1024);
  expect(uploadLimit("video/webm")).toBe(500 * 1024 * 1024);
  expect(uploadLimit("application/pdf")).toBe(100 * 1024 * 1024);
  expect(uploadLimit("text/html")).toBe(0);
  expect(uploadUrl("https://bucket.test/upload?signature=test", "https://bucket.test")).toContain(
    "signature=test",
  );
  for (const [url, origin] of [
    ["https://other.test/u", "https://bucket.test"],
    ["https://a:b@bucket.test/u", "https://bucket.test"],
    ["https://bucket.test/u#x", "https://bucket.test"],
    ["https://bucket.test/u", undefined],
    ["file:///u", "null"],
  ])
    expect(() => uploadUrl(url!, origin)).toThrow();
});
it("STU-100/101/103: client читает выбранный CS и пишет grouped/idempotent контент", async () => {
  const calls: { url: string; init?: RequestInit }[] = [];
  let response: unknown = {
    locales: ["ru", "en"],
    defaultLocale: "ru",
    items: [{ id, kind: "entity", title: "Article", schema: "Article", deleted: false }],
    nextCursor: null,
  };
  const api = createApi(async (url, init) => {
    calls.push({ url: String(url), ...(init ? { init } : {}) });
    return Response.json(response);
  });
  const s = fixture();
  await api.contentCatalog(s, "entity", "Article", id);
  expect(calls.at(-1)!.url).toContain("changesetId=" + changesetId);
  expect(calls.at(-1)!.url).toContain("after=" + id);
  response = record;
  expect(await api.content(s, id)).toEqual(record);
  expect(calls.at(-1)!.url).toContain("environment=staging");
  const ops: ContentOperation[] = [
    { type: "entity.create", payload: { schema: "Article", environment: "staging", data: {} } },
  ];
  response = {
    result: {
      changeset: { ...s.changeset, seq: 2 },
      operations: [{ type: "entity.create", target: id }],
    },
  };
  expect(await api.applyContent(s, ops, "exact", new AbortController().signal)).toEqual([id]);
  expect(JSON.parse(String(calls.at(-1)!.init!.body)).payload).toMatchObject({
    expectedSeq: 1,
    group: true,
    operations: ops,
  });
  const upload = {
    assetId: id,
    status: "ready",
    expiresAt: new Date(Date.now() + 60000).toISOString(),
    fileHash: "a".repeat(64),
  };
  response = { result: upload };
  expect(
    await api.uploadCommand("create-asset-upload", {}, "key", new AbortController().signal),
  ).toEqual(upload);
  response = upload;
  expect(await api.uploadStatus(id, new AbortController().signal)).toEqual(upload);
  for (const invalid of [
    {},
    { ...upload, status: "other" },
    { ...upload, fileHash: "bad" },
    { ...upload, errorCode: "secret details" },
    { ...upload, uploadUrl: 1 },
  ]) {
    response = invalid;
    await expect(api.uploadStatus(id, new AbortController().signal)).rejects.toThrow(
      "RESPONSE_INVALID",
    );
  }
  response = { items: [{ id: "bad" }], nextCursor: null };
  await expect(api.contentCatalog(s, "asset")).rejects.toThrow("RESPONSE_INVALID");
  response = { ...record, versionId: "bad" };
  await expect(api.content(s, id)).rejects.toThrow("RESPONSE_INVALID");
  response = { result: { changeset: s.changeset, operations: [] } };
  await expect(api.applyContent(s, ops, "key", new AbortController().signal)).rejects.toMatchObject(
    { uncertain: true },
  );
  await expect(api.content(s, "bad")).rejects.toThrow("IDENTIFIER_INVALID");
  await expect(api.uploadStatus("bad", new AbortController().signal)).rejects.toThrow(
    "IDENTIFIER_INVALID",
  );
});
it("STU-104: неизвестное создание повторяет прежний CS/seq/body/key, затем перечитывает", async () => {
  const { api, session } = mockApi();
  const editor = createEditor(api);
  await editor.open(documentId, changesetId);
  const ops: ContentOperation[] = [
    {
      type: "entity.create",
      payload: { schema: "Article", environment: "staging", data: { title: "x" } },
    },
  ];
  vi.mocked(api.applyContent)
    .mockImplementationOnce(async () => {
      session.changeset.seq++;
      throw new StudioError("NETWORK_UNCERTAIN", true);
    })
    .mockResolvedValue([id]);
  expect(await editor.applyContent(ops, 1)).toBeNull();
  expect(editor.store.getState()).toMatchObject({ status: "uncertain", contentRetry: true });
  ops[0]!.payload.data = "mutated by caller";
  await editor.refresh();
  expect(editor.store.getState().status).toBe("uncertain");
  expect(await editor.retryContent()).toEqual([id]);
  const calls = vi.mocked(api.applyContent).mock.calls;
  expect(calls[0]).toEqual(calls[1]);
  expect(calls[1]![0].changeset.seq).toBe(1);
  expect(calls[1]![1][0]!.payload.data).toEqual({ title: "x" });
  expect(editor.store.getState()).toMatchObject({ status: "ready", contentRetry: false });
  editor.dispose();
});
it("STU-104: буфер/closed/seq и поздний ответ защищают редактор", async () => {
  const { api, session } = mockApi();
  const editor = createEditor(api);
  await editor.open(documentId, changesetId);
  editor.formDirty("content", true);
  await editor.open(documentId, changesetId);
  expect(editor.store.getState().error).toBe("CONTENT_DRAFT_PENDING");
  await editor.apply([]);
  editor.viewMode("structure");
  await editor.changeHistory("undo");
  expect(api.apply).not.toHaveBeenCalled();
  editor.formDirty("content", false);
  editor.formDirty("json", true);
  expect(
    await editor.applyContent([{ type: "entity.delete", target: id, payload: {} }], 1),
  ).toBeNull();
  editor.formDirty("json", false);
  expect(
    await editor.applyContent([{ type: "entity.delete", target: id, payload: {} }], 0),
  ).toBeNull();
  expect(editor.store.getState().error).toBe("CHANGESET_SEQ_CONFLICT");
  session.changeset.state = "merged";
  await editor.refresh();
  expect(
    await editor.applyContent([{ type: "entity.delete", target: id, payload: {} }], 1),
  ).toBeNull();
  session.changeset.state = "open";
  session.changeset.targets = ["production", "staging"];
  await editor.refresh();
  expect(
    await editor.applyContent([{ type: "entity.delete", target: id, payload: {} }], 1),
  ).toBeNull();
  expect(editor.store.getState().error).toBe("CONTENT_TARGET_MISMATCH");
  session.changeset.targets = ["staging"];
  await editor.refresh();
  let finish!: (v: string[]) => void;
  vi.mocked(api.applyContent).mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const pending = editor.applyContent([{ type: "entity.delete", target: id, payload: {} }], 1);
  const count = vi.mocked(api.open).mock.calls.length;
  editor.dispose();
  finish([id]);
  expect(await pending).toBeNull();
  expect(api.open).toHaveBeenCalledTimes(count);
});
it("STU-104: отказ перечитывает; подтверждённая запись при ошибке чтения не отправляется ещё раз", async () => {
  const { api, session } = mockApi();
  const editor = createEditor(api);
  await editor.open(documentId, changesetId);
  vi.mocked(api.applyContent).mockRejectedValueOnce(new StudioError("FORBIDDEN"));
  expect(
    await editor.applyContent([{ type: "entity.delete", target: id, payload: {} }], 1),
  ).toBeNull();
  expect(editor.store.getState()).toMatchObject({
    status: "ready",
    error: "FORBIDDEN",
    contentRetry: false,
  });
  vi.mocked(api.applyContent).mockImplementation(async () => {
    session.changeset.seq++;
    return [id];
  });
  vi.mocked(api.open).mockRejectedValueOnce(Error("read failed"));
  expect(
    await editor.applyContent([{ type: "entity.delete", target: id, payload: {} }], 1),
  ).toBeNull();
  expect(editor.store.getState().status).toBe("uncertain");
  expect(await editor.retryContent()).toEqual([id]);
  expect(api.applyContent).toHaveBeenCalledTimes(2);
  editor.dispose();
});
