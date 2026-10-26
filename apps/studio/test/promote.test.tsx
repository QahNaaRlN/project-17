// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { PublicationHistory } from "../src/publication-history-panel.js";
import {
  promoteCard,
  type PromoteCard,
  type PublicationCard,
  type PublicationEntry,
} from "../src/publication-history.js";
import { createEditor, type Editor } from "../src/editor.js";
import { createApi } from "../src/api.js";
import { StudioError } from "../src/types.js";
import { mockApi, documentId, changesetId } from "./fixture.js";
const hash = "sha256:" + "a".repeat(64);
const promotionId = "0193b0a0-0000-7000-8000-000000000001";
const production = { environment: "production", appOrigin: "http://127.0.0.1:18087" };
function entry(id = documentId, environment = "staging", kind = "publish"): PublicationEntry {
  return {
    id,
    environment,
    kind,
    actorId: changesetId,
    createdAt: "2026-10-26T01:00:00Z",
    changesetId,
    sourcePublicationId: kind === "promote" ? documentId : null,
    reason: kind === "promote" ? "Релиз" : null,
    manifestChanged: false,
    previousManifestId: null,
    currentManifestId: null,
  };
}
const object = {
  id: documentId,
  kind: "document",
  docKind: "page",
  title: "Store",
  beforeVersionId: null,
  afterVersionId: documentId,
  beforePath: null,
  afterPath: "/store",
  summary: { effect: "added" as const, changeCount: 1 },
};
function card(id = documentId, environment = "staging", kind = "publish"): PublicationCard {
  return {
    publication: entry(id, environment, kind),
    snapshot: hash,
    complete: true,
    canRollback: true,
    blockers: [],
    objects: [object],
  };
}
function preview(overrides: Partial<PromoteCard> = {}): PromoteCard {
  return {
    publication: entry(),
    toEnvironment: "production",
    objects: [{ ...object, skipped: false }],
    snapshot: hash,
    canPromote: true,
    blockers: [],
    diagnostics: {},
    dependencies: [],
    ...overrides,
  };
}
const page = {
  publicationId: documentId,
  objectId: documentId,
  snapshot: hash,
  nextCursor: null,
  totalChanges: 1,
  items: [
    {
      key: "0",
      kind: "add" as const,
      category: "property",
      pointer: "/title",
      before: { present: false as const },
      after: { present: true as const, value: "Store" },
    },
  ],
};
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
async function setup(targets = [production]) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const { api } = mockApi();
  vi.mocked(api.config).mockResolvedValue({
    project: "store",
    environment: "staging",
    appOrigin: "http://127.0.0.1:18083",
    promotionTargets: targets,
  });
  api.publications = vi.fn(async (_cursor, _signal, environment = "staging") => ({
    items:
      environment === "production"
        ? [{ ...entry(promotionId, "production", "promote"), objectCount: 1 }]
        : [{ ...entry(), objectCount: 1 }],
    nextCursor: null,
  }));
  api.publication = vi.fn(async (id, _preview, _signal, environment = "staging") =>
    id === promotionId ? card(promotionId, "production", "promote") : card(id, environment),
  );
  api.publicationDiff = vi.fn(async () => page);
  api.promotePreview = vi.fn(async () => preview());
  api.promoteDiff = vi.fn(async () => page);
  api.promote = vi.fn(async () => entry(promotionId, "production", "promote"));
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
  );
}
async function click(label: string) {
  await act(async () => button(label)!.click());
  await settle();
}
async function reason(value = "Релиз витрины") {
  await act(async () => {
    const input = document.querySelector("textarea")!;
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(
      input,
      value,
    );
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}
async function open() {
  await click("Перенести в production…");
  await settle();
  await click("Перенести в production");
  await reason();
}
it("STU-469/473: preview, reason and a fresh snapshot precede the promotion", async () => {
  const api = await setup();
  await open();
  expect(document.body.textContent).toContain("Будет перенесено: 1");
  expect(document.body.textContent).toContain("staging → production");
  expect(api.promote).not.toHaveBeenCalled();
  expect(editor.store.getState().dirtyFields["promotion:production"]).toBe(true);
  await click("Подтвердить перенос");
  expect(api.promote).toHaveBeenCalledWith(
    expect.objectContaining({
      publicationId: documentId,
      toEnvironment: "production",
      expectedPromoteSnapshot: hash,
      reason: "Релиз витрины",
    }),
  );
  expect(document.body.textContent).toContain("Перенос выполнен: production");
  expect(document.querySelector<HTMLAnchorElement>("a")?.href).toBe("http://127.0.0.1:18087/store");
  expect(window.location.hash).toContain("environment=production");
  expect(button("production")?.getAttribute("aria-pressed")).toBe("true");
  // STU-474: a promotion in the target offers no promotion back to the base.
  expect(button("Перенести в staging…")).toBeUndefined();
  expect(editor.store.getState().dirtyFields["promotion:production"]).toBe(false);
});
it("STU-471: unknown outcome blocks other actions and retries the identical attempt", async () => {
  const api = await setup();
  vi.mocked(api.promote!).mockRejectedValueOnce(new StudioError("NETWORK_UNCERTAIN", true));
  await open();
  await click("Подтвердить перенос");
  expect(document.body.textContent).toContain("Результат пока неизвестен");
  expect(button("production")?.disabled).toBe(true);
  await click("Повторить запрос переноса");
  expect(api.promote).toHaveBeenCalledTimes(2);
  expect(vi.mocked(api.promote!).mock.calls[1]![0]).toEqual(
    vi.mocked(api.promote!).mock.calls[0]![0],
  );
});
it("STU-472: a changed snapshot closes the dialog and never sends promote", async () => {
  const api = await setup();
  await open();
  vi.mocked(api.promotePreview!).mockResolvedValue(
    preview({ snapshot: "sha256:" + "b".repeat(64) }),
  );
  await click("Подтвердить перенос");
  expect(api.promote).not.toHaveBeenCalled();
  expect(document.body.textContent).toContain("PROMOTE_SNAPSHOT_STALE");
  expect(document.querySelector('[role="dialog"]')).toBeNull();
});
it("STU-468: blockers explain the next step and link to the needed publication", async () => {
  const api = await setup();
  const dependency = "0193b0a0-0000-7000-8000-000000000002";
  vi.mocked(api.promotePreview!).mockResolvedValue(
    preview({
      canPromote: false,
      blockers: ["VALIDATION_FAILED", "PROMOTE_OUTDATED:" + documentId],
      diagnostics: { [documentId]: [{ code: "CONTENT_REFERENCE_UNPUBLISHED", pointer: "/a" }] },
      dependencies: [{ objectId: changesetId, publicationId: dependency }],
    }),
  );
  await click("Перенести в production…");
  await settle();
  expect(document.body.textContent).toContain("Будущее состояние целевого окружения");
  expect(document.body.textContent).toContain("CONTENT_REFERENCE_UNPUBLISHED");
  expect(button("Перенести в production")?.disabled).toBe(true);
  await click("Открыть публикацию-зависимость");
  expect(api.publication).toHaveBeenLastCalledWith(dependency, false, expect.anything(), "staging");
});
it("STU-470: the environment switch reads target history and keeps rollback there", async () => {
  const api = await setup();
  await click("production");
  expect(api.publications).toHaveBeenLastCalledWith(undefined, expect.anything(), "production");
  expect(window.location.hash).toContain("environment=production");
  const item = [...document.querySelectorAll<HTMLButtonElement>(".publication-list button")].find(
    (b) => b.textContent?.startsWith("Перенос · production"),
  )!;
  await act(async () => item.click());
  await settle();
  expect(api.publication).toHaveBeenLastCalledWith(
    promotionId,
    false,
    expect.anything(),
    "production",
  );
  expect(button("Посмотреть последствия отката")).toBeDefined();
  expect(button("Перенести в production…")).toBeUndefined();
});
it("STU-460: without promotion targets there is no switch and no promotion", async () => {
  await setup([]);
  expect(button("production")).toBeUndefined();
  expect(button("Перенести в production…")).toBeUndefined();
});
it("STU-462/467: browser adapter checks targets and the promotion response", async () => {
  const fetcher = vi.fn<typeof fetch>(async (input) => {
    const path = String(input);
    if (path.endsWith("/config"))
      return Response.json({
        environment: "staging",
        project: "store",
        appOrigin: "http://127.0.0.1:18083",
        promotionTargets: [production],
      });
    if (path.includes("/commands/promote"))
      return Response.json({ result: entry(promotionId, "production", "promote") });
    if (path.includes("/diff?")) return Response.json(page);
    return Response.json(preview());
  });
  const api = createApi(fetcher);
  const c = await api.promotePreview!(documentId, "staging", "production");
  expect(String(fetcher.mock.calls.at(-1)![0])).toContain(
    "/promote-preview?environment=staging&toEnvironment=production",
  );
  await api.promoteDiff!(c, documentId);
  expect(String(fetcher.mock.calls.at(-1)![0])).toContain("toEnvironment=production");
  const attempt = {
    publicationId: documentId,
    toEnvironment: "production",
    expectedPromoteSnapshot: hash,
    reason: "Test",
    key: "same",
    startedAt: Date.now(),
  };
  await api.promote!(attempt);
  const call = fetcher.mock.calls.at(-1)!;
  expect(call[1]?.body).toBe(
    JSON.stringify({
      reason: "Test",
      payload: {
        publicationId: documentId,
        toEnvironment: "production",
        expectedPromoteSnapshot: hash,
      },
    }),
  );
  expect(call[1]?.headers).toMatchObject({ "Idempotency-Key": "same" });
  await expect(api.promotePreview!(documentId, "staging", "qa")).rejects.toThrow();
  await expect(api.publications!(undefined, undefined, "qa")).rejects.toThrow();
  fetcher.mockImplementation(async (input) =>
    String(input).endsWith("/config")
      ? Response.json({ environment: "staging", project: "store", appOrigin: "http://a.test" })
      : Response.json({ result: entry(promotionId, "staging", "promote") }),
  );
  await expect(api.promote!(attempt)).rejects.toThrow();
});
it("STU-462: the promote validator rejects foreign targets and inconsistent cards", () => {
  expect(promoteCard(preview(), documentId, "staging", "production").canPromote).toBe(true);
  for (const bad of [
    preview({ toEnvironment: "qa" }),
    preview({ blockers: ["PROMOTE_NOTHING"] }),
    preview({ dependencies: [{ objectId: "bad", publicationId: null }] }),
    preview({ objects: [{ ...object } as PromoteCard["objects"][number]] }),
    preview({ diagnostics: { a: [{} as { code: string }] } }),
    preview({ snapshot: "bad" }),
  ])
    expect(() => promoteCard(bad, documentId, "staging", "production")).toThrow();
  expect(() => promoteCard(preview(), documentId, "production", "production")).toThrow();
});
