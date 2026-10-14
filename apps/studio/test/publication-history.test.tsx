// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { PublicationHistory } from "../src/publication-history-panel.js";
import {
  publicationCard,
  publicationList,
  publicationPage,
  type PublicationCard,
  type PublicationPage,
} from "../src/publication-history.js";
import { createEditor, type Editor } from "../src/editor.js";
import { createApi } from "../src/api.js";
import { StudioError } from "../src/types.js";
import { mockApi, documentId, changesetId } from "./fixture.js";
const hash = "sha256:" + "a".repeat(64);
function card(id = documentId): PublicationCard {
  return {
    publication: {
      id,
      environment: "staging",
      kind: "publish",
      actorId: changesetId,
      createdAt: "2026-10-15T01:00:00Z",
      changesetId,
      sourcePublicationId: null,
      reason: null,
      manifestChanged: false,
      previousManifestId: null,
      currentManifestId: null,
    },
    snapshot: hash,
    complete: true,
    canRollback: true,
    blockers: [],
    objects: [
      {
        id: documentId,
        kind: "document",
        docKind: "page",
        title: "Collection",
        beforeVersionId: changesetId,
        afterVersionId: documentId,
        beforePath: "/new",
        afterPath: "/old",
        summary: { effect: "modified", changeCount: 1 },
      },
    ],
  };
}
function detail(id = documentId): PublicationPage {
  return {
    publicationId: id,
    objectId: documentId,
    snapshot: hash,
    nextCursor: null,
    totalChanges: 1,
    items: [
      {
        key: "0",
        kind: "replace",
        category: "property",
        pointer: "/title",
        before: { present: true, value: "New" },
        after: { present: true, rawJson: "9007199254740993" },
      },
    ],
  };
}
let root: Root, editor: Editor, client: QueryClient;
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  client?.clear();
  document.body.innerHTML = "";
  window.history.replaceState({}, "", "/");
  vi.unstubAllGlobals();
});
async function settle() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 35));
  });
}
async function setup() {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const { api } = mockApi();
  api.publications = vi.fn(async () => ({
    items: [{ ...card().publication, objectCount: 1 }],
    nextCursor: null,
  }));
  api.publication = vi.fn(async (id) => card(id));
  api.publicationDiff = vi.fn(async (c) => detail(c.publication.id));
  api.rollback = vi.fn(async () => ({
    ...card(changesetId).publication,
    kind: "rollback",
    sourcePublicationId: documentId,
  }));
  editor = createEditor(api);
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  window.history.replaceState({}, "", "/#publication=" + documentId);
  document.body.innerHTML = '<div id="root"></div>';
  root = createRoot(document.getElementById("root")!);
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <PublicationHistory api={api} editor={editor} />
      </QueryClientProvider>,
    ),
  );
  await settle();
  await settle();
  return api;
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>("button")].find(
    (b) => b.textContent === label,
  )!;
}
async function click(label: string) {
  await act(async () => button(label).click());
  await settle();
}
async function reason() {
  await act(async () => {
    const input = document.querySelector("textarea")!;
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(
      input,
      "Проверенный откат",
    );
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}
async function confirm() {
  await click("Посмотреть последствия отката");
  await settle();
  await click("Отменить публикацию…");
  await reason();
}
it("STU-164/166/170: historical comparison and explicit reason precede a fresh guarded rollback", async () => {
  const api = await setup();
  expect(document.body.textContent).toContain("9007199254740993");
  expect(document.activeElement).toBe(document.querySelector(".publication-card"));
  await confirm();
  expect(api.rollback).not.toHaveBeenCalled();
  expect(editor.store.getState().dirtyFields.publication).toBe(true);
  await click("Подтвердить откат");
  expect(api.rollback).toHaveBeenCalledTimes(1);
  expect(api.rollback).toHaveBeenCalledWith(
    expect.objectContaining({
      resetHead: false,
      publicationId: documentId,
      reason: "Проверенный откат",
      expectedRollbackSnapshot: hash,
    }),
  );
  expect(document.body.textContent).toContain("Черновики сохранены");
  expect(document.querySelector("a")?.href).toContain("/old");
  expect(editor.store.getState().dirtyFields.publication).toBe(false);
  await click("К списку публикаций");
  expect(document.activeElement).toBe(document.querySelector(".publication-list"));
});
it("STU-171: unknown outcome locks navigation and retries the identical attempt", async () => {
  const api = await setup();
  vi.mocked(api.rollback!).mockRejectedValueOnce(new StudioError("NETWORK_UNCERTAIN", true));
  await confirm();
  await click("Подтвердить откат");
  expect(document.body.textContent).toContain("Результат пока неизвестен");
  expect(editor.store.getState().dirtyFields.publication).toBe(true);
  await click("Повторить запрос отката");
  expect(api.rollback).toHaveBeenCalledTimes(2);
  expect(vi.mocked(api.rollback!).mock.calls[1]![0]).toEqual(
    vi.mocked(api.rollback!).mock.calls[0]![0],
  );
});
it("STU-168/170: changed snapshot requires another review and never sends rollback", async () => {
  const api = await setup();
  await confirm();
  vi.mocked(api.publication!).mockResolvedValue({
    ...card(),
    snapshot: "sha256:" + "b".repeat(64),
  });
  await click("Подтвердить откат");
  expect(api.rollback).not.toHaveBeenCalled();
  expect(document.body.textContent).toContain("ROLLBACK_SNAPSHOT_STALE");
  expect(document.querySelector('[role="dialog"]')).toBeNull();
});
it("STU-173: manifest publications explain the boundary without rollback controls", async () => {
  const api = await setup();
  vi.mocked(api.publication!).mockResolvedValue({
    ...card(),
    canRollback: false,
    objects: [],
    publication: { ...card().publication, manifestChanged: true },
  });
  await act(async () => client.invalidateQueries({ queryKey: ["publication-card"] }));
  await settle();
  expect(document.body.textContent).toContain("Откат схем из Studio пока недоступен");
  expect(button("Посмотреть последствия отката")).toBeUndefined();
});
it("STU-172: revoked read clears the historical comparison", async () => {
  const api = await setup();
  expect(document.body.textContent).toContain("Collection");
  vi.mocked(api.publication!).mockRejectedValue(new StudioError("FORBIDDEN"));
  await act(async () => client.invalidateQueries({ queryKey: ["publication-card"] }));
  await settle();
  expect(document.body.textContent).not.toContain("Collection");
  expect(document.body.textContent).not.toContain("9007199254740993");
});
it("STU-166: partial detail requires all pages before confirmation", async () => {
  const api = await setup();
  vi.mocked(api.publicationDiff!).mockImplementation(async (c, _o, _p, cursor) =>
    cursor
      ? { ...detail(c.publication.id), items: [], totalChanges: 1 }
      : { ...detail(c.publication.id), items: [], nextCursor: "next" },
  );
  await act(async () => client.removeQueries({ queryKey: ["publication-diff"] }));
  await click("Посмотреть последствия отката");
  await settle();
  expect(button("Отменить публикацию…").disabled).toBe(true);
  await click("Ещё изменения");
  expect(button("Отменить публикацию…").disabled).toBe(true);
});
it("STU-167: response validators reject foreign contexts and malformed values", () => {
  expect(publicationCard(card(), documentId, "staging")).toEqual(card());
  expect(() => publicationCard(card(), changesetId, "staging")).toThrow("RESPONSE_INVALID");
  expect(() =>
    publicationCard(
      { ...card(), objects: [...card().objects, ...card().objects] },
      documentId,
      "staging",
    ),
  ).toThrow();
  expect(() =>
    publicationList(
      { items: [{ ...card().publication, objectCount: -1 }], nextCursor: null },
      "staging",
    ),
  ).toThrow();
  expect(() =>
    publicationPage({ ...detail(), snapshot: "bad" }, documentId, documentId, hash),
  ).toThrow();
  expect(() =>
    publicationPage(
      { ...detail(), items: [{ ...detail().items[0]!, before: { present: true } as never }] },
      documentId,
      documentId,
      hash,
    ),
  ).toThrow();
});
it("STU-167/170: browser adapter binds reads and writes to the selected publication", async () => {
  const fetcher = vi.fn<typeof fetch>(async (input) => {
    const path = String(input);
    if (path.endsWith("/config"))
      return Response.json({
        environment: "staging",
        project: "store",
        appOrigin: "http://127.0.0.1:18083",
      });
    if (path.includes("/commands/rollback"))
      return Response.json({
        result: {
          ...card(changesetId).publication,
          kind: "rollback",
          sourcePublicationId: documentId,
        },
      });
    if (path.includes("/diff?")) return Response.json(detail());
    if (path.includes("publication-history?"))
      return Response.json({
        items: [{ ...card().publication, objectCount: 1 }],
        nextCursor: null,
      });
    return Response.json(card());
  });
  const api = createApi(fetcher);
  await api.publications!();
  const c = await api.publication!(documentId, true);
  await api.publicationDiff!(c, documentId, true);
  const attempt = {
    publicationId: documentId,
    resetHead: false as const,
    expectedRollbackSnapshot: hash,
    reason: "Test",
    key: "same",
    startedAt: Date.now(),
  };
  await api.rollback!(attempt);
  const call = fetcher.mock.calls.at(-1)!;
  expect(call[1]?.body).toBe(
    JSON.stringify({
      reason: "Test",
      payload: { publicationId: documentId, resetHead: false, expectedRollbackSnapshot: hash },
    }),
  );
  expect(call[1]?.headers).toMatchObject({ "Idempotency-Key": "same" });
});
