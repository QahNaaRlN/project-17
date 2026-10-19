// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { CapabilityPreviewPanel } from "../src/capability-preview-panel.js";
import { createEditor, type Editor } from "../src/editor.js";
import { mockApi, documentId, changesetId, fixture } from "./fixture.js";
import { StudioError, type Api } from "../src/types.js";
import type { CapabilityRequest } from "../src/capabilities.js";
import type { PreviewList } from "../src/capability-preview.js";
import { previewItem } from "./preview-fixture.js";

let root: Root, editor: Editor;
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
const card: CapabilityRequest = {
  id: documentId,
  kind: "action",
  environment: "staging",
  key: "store.favorite",
  title: "Favorite",
  description: "Task",
  proposedContract: { args: { value: { type: "string", required: true } } },
  acceptance: ["Works"],
  status: "pr_open",
  revision: 4,
  changesetId,
  documentId,
  nodeId: "n_custom",
  event: "click",
  ownerId: documentId,
  externalRefs: [],
};
const button = (s: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find(
    (b) => b.textContent?.trim() === s,
  );
async function setup(list: PreviewList, prepare?: (api: Api) => void, ru = true) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("crypto", webcrypto);
  const session = fixture();
  session.config.previewAppOrigins = ["https://pr.example.test"];
  const { api } = mockApi(session);
  api.account = vi.fn(async () => ({
    authenticated: true,
    user: { id: documentId, name: "Designer" },
    rights: ["design.read", "behavior.use"],
    csrfToken: "csrf",
    hasAccess: true,
  }));
  api.capabilityPreview = vi.fn(async () => structuredClone(list));
  api.previewArgs = vi.fn(async () => {});
  api.preview = vi.fn(async () => ({
    token: "pr",
    expiresAt: new Date(Date.now() + 900_000).toISOString(),
    registrationSeq: 1,
  }));
  prepare?.(api);
  editor = createEditor(api);
  if (!ru) editor.locale("en");
  await editor.open(documentId, changesetId);
  document.body.innerHTML = "<div id='root'></div>";
  root = createRoot(document.getElementById("root")!);
  await act(async () =>
    root.render(<CapabilityPreviewPanel {...{ api, editor }} request={card} />),
  );
  return api;
}
async function type(label: string, value: string) {
  await act(async () => {
    const e = [...document.querySelectorAll("label")]
      .find((e) => e.textContent?.startsWith(label))!
      .querySelector<HTMLInputElement>("input")!;
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(e, value);
    e.dispatchEvent(new Event("input", { bubbles: true }));
  });
}
async function submit() {
  await act(async () =>
    document
      .querySelector("form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })),
  );
}

it("STU-321/330: preview args are explicit, then the PR build opens read-only", async () => {
  const api = await setup(previewItem());
  expect(document.body.textContent).toContain("Готов к просмотру");
  expect(document.body.textContent).toContain("не означает слияния");
  expect(button("Открыть в предпросмотре")!.disabled).toBe(true);
  expect(document.querySelector("form fieldset")!.textContent).toContain("value");
  await type("Причина", "Check the PR build");
  expect(editor.store.getState().dirtyFields["property-capability-preview"]).toBe(true);
  await submit();
  expect(api.previewArgs).toHaveBeenCalledWith(
    expect.objectContaining({
      reason: "Check the PR build",
      actor: documentId,
      payload: expect.objectContaining({
        requestId: documentId,
        expectedRequestRevision: 4,
        environment: "preview/pr-1",
        manifestHash: "sha256:" + "b".repeat(64),
        expectedArgsRevision: 0,
        handlers: [{ action: "store.favorite", args: {} }],
      }),
    }),
  );
  expect(api.capabilityPreview).toHaveBeenCalledTimes(2);
});

it("STU-330: opens a ready build and returns to the working environment", async () => {
  const list = previewItem();
  list.items[0]!.args = { status: "set", revision: 1 };
  list.items[0]!.state = "ready_last_known";
  await setup(list);
  expect(document.body.textContent).toContain("проверено GitHub");
  await act(async () => button("Открыть в предпросмотре")!.click());
  expect(editor.store.getState().viewTarget).toMatchObject({
    environment: "preview/pr-1",
    origin: "https://pr.example.test",
    registrationSeq: 1,
  });
  await act(async () => button("Вернуться в рабочее окружение")!.click());
  expect(editor.store.getState().viewTarget).toBeNull();
});

it("STU-327/328: engineers see state only; foreign origins are not embedded", async () => {
  const list = previewItem();
  list.canOpen = false;
  list.canSetArgs = false;
  list.items[0]!.args = { status: "not_required", revision: 0 };
  list.items.push({
    ...structuredClone(list.items[0]!),
    environment: "preview/pr-2",
    pullNumber: 2,
    state: "environment_expired",
    archivedAt: "2026-10-19T12:00:00Z",
    archiveReason: "pr_merged",
    registration: { ...list.items[0]!.registration!, appUrl: "https://evil.example.test/" },
  });
  await setup(list, undefined, false);
  expect(document.querySelector("form")).toBeNull();
  expect(button("Open in preview")!.disabled).toBe(true);
  expect(document.body.textContent).toContain("PR merged");
  expect(document.body.textContent).toContain("outside Studio's allowed origins");
});

it("STU-321: lost response is retried with the same key; errors are explained", async () => {
  let calls = 0;
  const api = await setup(previewItem(), (a) => {
    a.previewArgs = vi.fn(async () => {
      calls++;
      if (calls === 1) throw new StudioError("NETWORK", true);
      if (calls === 3) throw new StudioError("PREVIEW_ARGS_STALE");
    });
  });
  await type("Причина", "Check");
  await submit();
  expect(document.body.textContent).toContain("Сведения предпросмотра недоступны");
  await act(async () => button("Повторить тот же запрос")!.click());
  const [first, second] = vi.mocked(api.previewArgs!).mock.calls;
  expect(second![0].key).toBe(first![0].key);
  await type("Причина", "Again");
  await submit();
  expect(document.body.textContent).toContain("Сборка preview изменилась");
  await act(async () => button("Сбросить ввод предпросмотра")!.click());
  expect(editor.store.getState().dirtyFields["property-capability-preview"]).toBe(false);
  api.capabilityPreview = vi.fn(async () => {
    throw new StudioError("FORBIDDEN");
  });
  await act(async () => button("Обновить предпросмотр")!.click());
});

it("STU-319: shows an empty list and read failures", async () => {
  await setup({ items: [], canOpen: false, canSetArgs: false }, (a) => {
    a.capabilityPreview = vi
      .fn()
      .mockResolvedValueOnce({ items: [], canOpen: false, canSetArgs: false })
      .mockRejectedValueOnce(new StudioError("FORBIDDEN"))
      .mockRejectedValueOnce(new Error("offline"));
  });
  expect(document.body.textContent).toContain("Preview-окружений этого PR нет");
  await act(async () => button("Обновить предпросмотр")!.click());
  expect(document.body.textContent).toContain("Недостаточно прав");
  await act(async () => button("Обновить предпросмотр")!.click());
  expect(document.body.textContent).toContain("Сведения предпросмотра недоступны");
});
