// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { CapabilityFulfillmentPanel } from "../src/capability-fulfillment-panel.js";
import {
  anchorLabel,
  anchorStates,
  fulfillmentMessage,
  fulfillmentReason,
  fulfillmentView,
  openRequest,
  type FulfillmentView,
} from "../src/capability-fulfillment.js";
import { createEditor, type Editor } from "../src/editor.js";
import { createApi } from "../src/api.js";
import { mockApi, documentId, changesetId, fixture } from "./fixture.js";
import { StudioError, type Api } from "../src/types.js";
import type { CapabilityRequest } from "../src/capabilities.js";

let root: Root, editor: Editor;
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
const anchorId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c33";
const card: CapabilityRequest = {
  id: documentId,
  kind: "action",
  environment: "staging",
  key: "store.favorite",
  title: "Favorite",
  description: "Task",
  proposedContract: { args: { value: { type: "string", required: true } } },
  acceptance: ["Works"],
  status: "fulfilled",
  revision: 6,
  changesetId,
  documentId,
  nodeId: "n_custom",
  event: "click",
  ownerId: documentId,
  externalRefs: [],
};
const view = (): FulfillmentView => ({
  requestId: documentId,
  status: "fulfilled",
  state: "fulfilled",
  reason: "",
  mode: "automatic",
  commit: "e".repeat(40),
  fulfilledAt: "2026-10-20T10:00:00Z",
  basis: {
    pullNumber: 1,
    integrationSha: "d".repeat(40),
    commit: "e".repeat(40),
    comparison: { status: "ahead", aheadBy: 2 },
  },
  anchorCount: 2,
  anchors: [
    {
      id: anchorId,
      state: "awaiting_args",
      revision: 3,
      original: false,
      own: true,
      changesetId,
      documentId,
      nodeId: "n_custom",
      event: "click",
      argsSet: false,
    },
    { id: documentId, state: "replaced", revision: 2, original: true, own: false },
  ],
  canCheck: false,
});
const button = (s: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find(
    (b) => b.textContent?.trim() === s,
  );
async function setup(v: FulfillmentView, prepare?: (api: Api) => void) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("crypto", webcrypto);
  const { api } = mockApi(fixture());
  api.account = vi.fn(async () => ({
    authenticated: true,
    user: { id: documentId, name: "Designer" },
    rights: ["behavior.use"],
    csrfToken: "c",
    hasAccess: true,
  }));
  api.fulfillment = vi.fn(async () => structuredClone(v));
  api.fulfillmentCommand = vi.fn(async () => ({}));
  prepare?.(api);
  editor = createEditor(api);
  await editor.open(documentId, changesetId);
  document.body.innerHTML = "<div id='root'></div>";
  root = createRoot(document.getElementById("root")!);
  await act(async () =>
    root.render(<CapabilityFulfillmentPanel {...{ api, editor }} request={card} />),
  );
  return api;
}

it("STU-351: validates the fulfillment projection", () => {
  expect(fulfillmentView(view())).toEqual(view());
  for (const change of [
    (v: FulfillmentView) => ((v.state as string) = "done"),
    (v: FulfillmentView) => (v.commit = "abc"),
    (v: FulfillmentView) => ((v.mode as string) = "magic"),
    (v: FulfillmentView) => ((v.anchors[0]!.state as string) = "gone"),
    (v: FulfillmentView) => (v.anchors[0]!.changesetId = "x"),
    (v: FulfillmentView) => ((v.anchors as unknown) = null),
    (v: FulfillmentView) => (v.requestId = "x"),
  ]) {
    const v = view();
    change(v);
    expect(() => fulfillmentView(v)).toThrowError(StudioError);
  }
  expect(() => openRequest({ id: "x", key: "k", title: "t", status: "s", revision: 1 })).toThrow();
  for (const s of anchorStates) expect(anchorLabel(s, true)).not.toBe(anchorLabel(s, false));
  expect(anchorLabel("other", true)).toBe("Недоступно");
  for (const r of [
    "",
    "key_missing",
    "contract_incompatible",
    "no_github_link",
    "pr_not_merged",
    "observation_stale",
    "ci_not_successful",
    "no_code_index",
    "commit_unrelated",
    "comparison_unavailable",
    "other",
  ])
    expect(fulfillmentReason(r, true)).not.toBe(fulfillmentReason(r, false));
  for (const c of [
    "CAPABILITY_ANCHOR_LIMIT",
    "CAPABILITY_FULFILLMENT_ARGS_INVALID",
    "CAPABILITY_REVISION_CONFLICT",
    "CAPABILITY_STATE_INVALID",
    "FORBIDDEN",
    "OTHER",
  ])
    expect(fulfillmentMessage(c, true)).not.toBe(fulfillmentMessage(c, false));
});

it("STU-344/352: the owner confirms fulfillment arguments with an exact retry", async () => {
  let calls = 0;
  const api = await setup(view(), (a) => {
    a.fulfillmentCommand = vi.fn(async () => {
      calls++;
      if (calls === 1) throw new StudioError("NETWORK", true);
      return {};
    });
  });
  expect(document.body.textContent).toContain("Выполнен автоматически");
  expect(document.body.textContent).toContain("Собрано из слияния PR #1");
  expect(document.body.textContent).toContain("Привязка другого черновика");
  await act(async () => button("Подтвердить аргументы выполнения")!.click());
  await act(async () => button("Повторить тот же запрос")!.click());
  const [first, second] = vi.mocked(api.fulfillmentCommand!).mock.calls;
  expect(second![0]).toEqual(first![0]);
  expect(first![0]).toMatchObject({
    name: "set-capability-fulfillment-args",
    payload: { requestId: documentId, anchorId, expectedAnchorRevision: 3 },
  });
});

it("STU-346: reverted anchors can be retried and attached ones detached", async () => {
  const v = view();
  v.anchors[0]!.state = "reverted";
  const api = await setup(v);
  await act(async () => button("Повторить замену")!.click());
  expect(api.fulfillmentCommand).toHaveBeenLastCalledWith(
    expect.objectContaining({ name: "retry-capability-fulfillment" }),
  );
  await act(async () => button("Отсоединить")!.click());
  expect(api.fulfillmentCommand).toHaveBeenLastCalledWith(
    expect.objectContaining({
      name: "detach-capability-request",
      payload: expect.objectContaining({ expectedSeq: 1 }),
    }),
  );
});

it("decision §11.8: a waiting request can be checked explicitly; errors are explained", async () => {
  const v = view();
  Object.assign(v, {
    state: "waiting",
    reason: "pr_not_merged",
    mode: null,
    basis: null,
    canCheck: true,
  });
  const api = await setup(v, (a) => {
    a.fulfillmentCommand = vi.fn(async () => {
      throw new StudioError("CAPABILITY_STATE_INVALID");
    });
  });
  expect(document.body.textContent).toContain("PR ещё не слит");
  await act(async () => button("Проверить выполнение")!.click());
  expect(api.fulfillmentCommand).toHaveBeenCalledWith(
    expect.objectContaining({
      name: "check-capability-fulfillment",
      payload: { requestId: documentId },
    }),
  );
  expect(document.body.textContent).toContain("Действие сейчас недоступно");
});

it("STU-351: read failures are visible", async () => {
  await setup(view(), (a) => {
    a.fulfillment = vi.fn(async () => {
      throw new Error("offline");
    });
  });
  expect(document.body.textContent).toContain("Сведения о выполнении недоступны");
});

it("STU-336/351: API client reads, looks up and sends exact commands", async () => {
  let body: unknown = view();
  const fetcher = vi.fn<typeof fetch>(async (input) => {
    const p = String(input);
    if (p.endsWith("/config"))
      return Response.json({
        project: "store",
        environment: "staging",
        appOrigin: "http://127.0.0.1:1",
      });
    if (p.includes("/auth/session"))
      return Response.json({
        authenticated: true,
        user: { id: documentId, name: "D" },
        rights: [],
        csrfToken: "c".repeat(43),
        expiresAt: "2030-01-01T00:00:00Z",
        hasAccess: true,
      });
    if (p.includes("/open?"))
      return Response.json({
        id: documentId,
        key: "store.favorite",
        title: "t",
        status: "pr_open",
        revision: 4,
      });
    if (p.includes("/commands/")) return Response.json({ result: body });
    return Response.json(body);
  });
  const api = createApi(fetcher);
  expect(await api.fulfillment!(documentId)).toEqual(view());
  await expect(api.fulfillment!("bad")).rejects.toMatchObject({ code: "PAYLOAD_INVALID" });
  expect((await api.openRequest!("store.favorite")).revision).toBe(4);
  expect(String(fetcher.mock.calls.at(-1)![0])).toContain("key=store.favorite");
  const attempt = {
    name: "retry-capability-fulfillment" as const,
    payload: { requestId: documentId },
    key: "k",
    reason: "r",
    actor: documentId,
  };
  await api.fulfillmentCommand!(attempt);
  await expect(api.fulfillmentCommand!({ ...attempt, actor: "other" })).rejects.toMatchObject({
    code: "SESSION_CHANGED",
  });
  body = null;
  await expect(api.fulfillmentCommand!(attempt)).rejects.toMatchObject({
    code: "RESPONSE_INVALID",
  });
});
