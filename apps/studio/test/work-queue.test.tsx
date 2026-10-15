// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { WorkItems } from "../src/work-items.js";
import { Studio } from "../src/ui.js";
import { createEditor, type Editor } from "../src/editor.js";
import { createApi } from "../src/api.js";
import { validateQueue, validateQueueCard } from "../src/work-queue.js";
import {
  StudioError,
  type WorkContext,
  type WorkQueueCard,
  type WorkQueueItem,
} from "../src/types.js";
import { fixture, mockApi, changesetId, documentId } from "./fixture.js";
const item: WorkQueueItem = {
  id: changesetId,
  title: "Entity draft",
  ownerId: documentId,
  ownerName: "Alice",
  own: true,
  state: "open",
  createdAt: "2026-10-15T00:00:00Z",
  updatedAt: "2026-10-15T01:00:00Z",
  seq: 1,
  objectCount: 1,
  deletedCount: 0,
  countsByKind: { page: 0, component: 0, entity: 1, asset: 0 },
};
const card: WorkQueueCard = {
  item,
  objects: [
    {
      id: documentId,
      kind: "entity",
      docKind: null,
      title: "",
      baseVersionId: null,
      workingVersionId: documentId,
      deleted: false,
      basePath: null,
      workingPath: null,
    },
  ],
};
function context(): WorkContext {
  const s = fixture();
  const { record: _record, ...work } = s;
  void _record;
  return { ...work, work: structuredClone(card) };
}
let root: Root | undefined, editor: Editor | undefined, client: QueryClient | undefined;
afterEach(async () => {
  await act(async () => root?.unmount());
  root = undefined;
  editor?.dispose();
  client?.clear();
  document.body.innerHTML = "";
  window.history.replaceState({}, "", "/");
  vi.unstubAllGlobals();
});
async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 25));
  });
}
async function setup(locale: "ru" | "en" = "ru", compact = false, studio = false) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const { api } = mockApi();
  const work = context();
  api.workQueue = vi.fn(async () => ({ items: [structuredClone(item)], nextCursor: null }));
  api.openWork = vi.fn(async () => structuredClone(work));
  editor = createEditor(api);
  editor.locale(locale);
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  document.body.innerHTML = '<div id="root"></div>';
  root = createRoot(document.getElementById("root")!);
  const opened = vi.fn();
  await act(async () =>
    root!.render(
      <QueryClientProvider client={client!}>
        {studio ? (
          <Studio api={api} editor={editor!} rights={["content.read"]} />
        ) : (
          <WorkItems api={api} editor={editor!} compact={compact} onOpened={opened} />
        )}
      </QueryClientProvider>,
    ),
  );
  await flush();
  return { api, work, opened };
}
const button = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === text)!;
async function click(text: string) {
  await act(async () => button(text).click());
  await flush();
}
async function change(el: HTMLInputElement | HTMLSelectElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype,
      "value",
    )!.set!.call(el, value);
    el.dispatchEvent(
      new Event(el instanceof HTMLSelectElement ? "change" : "input", { bubbles: true }),
    );
  });
  await flush();
}
it("STU-177/180: server filters and one CS row open work without document or preview", async () => {
  const { api, opened } = await setup();
  expect(document.body.textContent).toContain("Сущности: 1");
  await change(document.querySelector<HTMLInputElement>('input[type="search"]')!, " needle ");
  await act(async () => {
    document
      .querySelector("input")!
      .dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
  });
  await flush();
  expect(api.workQueue).toHaveBeenLastCalledWith(
    expect.objectContaining({ q: "needle" }),
    undefined,
    expect.any(AbortSignal),
  );
  for (const [index, value] of [
    [0, "approved"],
    [1, "mine"],
    [2, "entity"],
  ] as const)
    await change(document.querySelectorAll("select")[index]!, value);
  expect(api.workQueue).toHaveBeenLastCalledWith(
    { q: "needle", state: "approved", ownership: "mine", kind: "entity" },
    undefined,
    expect.any(AbortSignal),
  );
  await click("Открыть работу");
  expect(api.openWork).toHaveBeenCalledWith(changesetId, expect.any(AbortSignal));
  expect(api.open).not.toHaveBeenCalled();
  expect(api.preview).not.toHaveBeenCalled();
  expect(opened).toHaveBeenCalledOnce();
  expect(window.location.hash).toBe("#work=" + changesetId);
  expect(editor!.store.getState().session?.record).toBeUndefined();
  await click("Обновить работы");
});
it("STU-178/186: next-page failure keeps rows and retries; empty server search is distinct", async () => {
  const { api } = await setup("en");
  vi.mocked(api.workQueue!).mockResolvedValueOnce({
    items: [
      { ...item, countsByKind: { page: 0, component: 0, entity: 0, asset: 0 }, objectCount: 0 },
    ],
    nextCursor: "cursor.token",
  });
  await click("Refresh work");
  expect(document.body.textContent).toContain("No changes");
  vi.mocked(api.workQueue!).mockRejectedValueOnce(new Error("offline"));
  await click("More work");
  expect(document.body.textContent).toContain("Loaded work remains available");
  vi.mocked(api.workQueue!).mockResolvedValueOnce({ items: [], nextCursor: null });
  await click("Retry");
  expect(document.body.textContent).toContain("Entity draft");
  vi.mocked(api.workQueue!).mockResolvedValue({ items: [], nextCursor: null });
  await change(document.querySelector("select")!, "approved");
  expect(document.body.textContent).toContain("No matching work");
  await change(document.querySelector("select")!, "all");
  expect(document.body.textContent).toContain("No available work");
  vi.mocked(api.workQueue!).mockRejectedValue(new Error("offline"));
  await click("Refresh work");
  expect(document.body.textContent).toContain("Could not load work");
  await click("Retry");
});
it("STU-183: compact queue, open failure and guarded dirty transition", async () => {
  const { api } = await setup("ru", true);
  expect(document.querySelector("summary")?.textContent).toContain("Мои работы");
  vi.mocked(api.openWork!).mockRejectedValue(new StudioError("NOT_FOUND"));
  await click("Открыть работу");
  expect(document.body.textContent).toContain("Не удалось открыть работу");
  vi.mocked(api.openWork!).mockResolvedValue(context());
  await click("Открыть работу");
  await act(async () => editor!.formDirty("content", true));
  expect(button("Открыть работу").disabled).toBe(true);
  await editor!.openWork("other");
  expect(api.openWork).toHaveBeenCalledTimes(2);
});
it("STU-180/185: content-only shell deep link opens card without the page catalogue", async () => {
  window.history.replaceState({}, "", "/editor#work=" + changesetId);
  const { api } = await setup("en", false, true);
  expect(api.openWork).toHaveBeenCalled();
  expect(api.pages).not.toHaveBeenCalled();
  expect(document.body.textContent).toContain("Selected work");
  expect(document.querySelector("#work-card-title")?.textContent).toContain("Test draft");
  expect(document.activeElement?.id).toBe("work-card-title");
  expect(document.body.textContent).toContain("All work objects");
});
it("STU-181/182: content and workflow reload WorkContext and keep exact uncertain attempts", async () => {
  const { api, work } = await setup();
  await act(async () => editor!.openWork(changesetId));
  vi.mocked(api.applyContent).mockImplementation(async () => {
    work.changeset.seq++;
    work.history.seq++;
    return [documentId];
  });
  vi.mocked(api.openWork!).mockImplementation(async () => structuredClone(work));
  await act(async () => {
    await editor!.applyContent(
      [{ type: "entity.setFields", target: documentId, payload: { fields: { title: "new" } } }],
      1,
    );
  });
  expect(editor!.store.getState().session?.changeset.seq).toBe(2);
  vi.mocked(api.workflow).mockResolvedValueOnce(null);
  await act(async () => editor!.workflow("submit"));
  expect(api.workflow).toHaveBeenCalled();
  await act(async () => editor!.changeHistory("undo")); // no undo is available
  const publication = {
    id: documentId,
    changesetId,
    environment: "staging",
    createdAt: item.createdAt,
  };
  vi.mocked(api.workflow)
    .mockRejectedValueOnce(new StudioError("NETWORK_UNCERTAIN", true))
    .mockResolvedValueOnce(publication);
  await act(async () => editor!.workflow("publish", "", "sha256:" + "a".repeat(64)));
  expect(editor!.store.getState().status).toBe("uncertain");
  await act(async () => editor!.retryWorkflow());
  const calls = vi.mocked(api.workflow).mock.calls;
  expect(calls.at(-1)?.slice(0, 4)).toEqual(calls.at(-2)?.slice(0, 4));
  expect(editor!.store.getState()).toMatchObject({ status: "ready", session: null, publication });
});
it("STU-180/183: another work never inherits the previously selected content form", async () => {
  window.history.replaceState({}, "", "/editor#work=" + changesetId);
  const { api } = await setup("ru", false, true);
  await click("Открыть контент");
  expect(document.querySelector(".content-panel")).not.toBeNull();
  const next = context();
  next.changeset.id = documentId;
  next.review.changeset = next.changeset;
  next.work!.item.id = documentId;
  vi.mocked(api.openWork!).mockResolvedValue(next);
  await act(async () => editor!.openWork(documentId));
  await flush();
  expect(editor!.store.getState().session?.changeset.id).toBe(documentId);
  expect(document.querySelector(".content-panel")).toBeNull();
});
it("STU-184: loss of access clears card; read outage prevents decisions", async () => {
  const { api } = await setup();
  await act(async () => editor!.openWork(changesetId));
  vi.mocked(api.openWork!).mockRejectedValueOnce(new Error("offline"));
  await act(async () => editor!.refresh());
  expect(editor!.store.getState().status).toBe("uncertain");
  vi.mocked(api.openWork!).mockRejectedValueOnce(new StudioError("WORK_QUEUE_ITEM_NOT_FOUND"));
  await act(async () => editor!.refresh());
  expect(editor!.store.getState()).toMatchObject({ status: "idle", session: null });
});
it("STU-184/186: revoked queue access clears private card and cached rows; configuration can be retried", async () => {
  const { api } = await setup();
  await act(async () => editor!.openWork(changesetId));
  vi.mocked(api.workQueue!).mockRejectedValue(new StudioError("FORBIDDEN"));
  await click("Обновить работы");
  expect(editor!.store.getState().session).toBeNull();
  expect(document.querySelectorAll("tbody tr")).toHaveLength(0);
  vi.mocked(api.config).mockRejectedValueOnce(new Error("config offline"));
  await act(async () => {
    await client!.refetchQueries({ queryKey: ["studio-config"] });
  });
  await flush();
  expect(document.body.textContent).toContain("Не удалось загрузить работы");
  vi.mocked(api.workQueue!).mockResolvedValue({ items: [], nextCursor: null });
  await click("Повторить");
});
it("STU-187: API loads coherent work context without document reads and rejects crossed snapshots", async () => {
  const s = fixture();
  let crossed = false;
  const fetcher = vi.fn<typeof fetch>(async (input) => {
    const path = String(input);
    return Response.json(
      path.endsWith("/config")
        ? s.config
        : path.includes("/work-queue/")
          ? { ...card, item: { ...item, seq: crossed ? 2 : 1 } }
          : path.includes("/work-queue?")
            ? { items: [item], nextCursor: null }
            : path.includes("/manifest?")
              ? { manifest: s.manifest, manifestHash: s.manifestHash }
              : path.endsWith("/review")
                ? s.review
                : path.endsWith("/history")
                  ? s.history
                  : s.changeset,
    );
  });
  const api = createApi(fetcher),
    signal = new AbortController().signal;
  await expect(api.openWork!(changesetId, signal)).resolves.toMatchObject({ work: card });
  expect(fetcher.mock.calls.every(([p]) => !String(p).includes("/documents/"))).toBe(true);
  await api.workQueue!(
    { q: "route", state: "all", ownership: "mine", kind: "entity" },
    "signed.cursor",
    signal,
  );
  expect(String(fetcher.mock.calls.at(-1)![0])).toContain("cursor=signed.cursor");
  crossed = true;
  await expect(api.openWork!(changesetId, signal)).rejects.toThrow("CHANGESET_SEQ_CONFLICT");
  await expect(api.openWork!("invalid", signal)).rejects.toThrow("IDENTIFIER_INVALID");
});
it("STU-187: queue response validation rejects partial counts, duplicate IDs, bad cursors and objects", () => {
  expect(validateQueue({ items: [item], nextCursor: null }).items).toHaveLength(1);
  expect(validateQueueCard(card, changesetId)).toEqual(card);
  for (const value of [
    null,
    {},
    { items: [item, item], nextCursor: null },
    { items: [{ ...item, objectCount: 2 }], nextCursor: null },
    { items: [], nextCursor: "bad cursor" },
  ])
    expect(() => validateQueue(value as never)).toThrow();
  for (const value of [
    { ...card, objects: [] },
    { ...card, objects: [{ ...card.objects[0], workingPath: "//evil" }] },
    { ...card, item: { ...item, id: documentId } },
  ])
    expect(() => validateQueueCard(value as WorkQueueCard, changesetId)).toThrow();
});
