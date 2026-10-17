// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { CapabilityDeliveryPanel, DeliveryTargetPanel } from "../src/capability-delivery-panel.js";
import { createEditor, type Editor } from "../src/editor.js";
import { mockApi, documentId, changesetId } from "./fixture.js";
import { StudioError, type Api } from "../src/types.js";
import type { CapabilityRequest } from "../src/capabilities.js";
import type { Delivery, DeliveryPreview, DeliveryTarget } from "../src/capability-delivery.js";
import { manifestHash } from "@cms/manifest";

let root: Root, editor: Editor;
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  document.body.innerHTML = "";
  vi.useRealTimers();
  vi.unstubAllGlobals();
});
const button = (s: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find(
    (b) => b.textContent?.trim() === s,
  )!;
async function click(s: string) {
  expect(button(s), s).toBeDefined();
  await act(async () => button(s).click());
  for (let i = 0; i < 100 && document.querySelector('section[aria-busy="true"]'); i++) {
    await act(async () => {
      await new Promise<void>((resolve) => setTimeout(resolve, 10));
    });
  }
  expect(document.querySelector('section[aria-busy="true"]')).toBeNull();
}
function field(s: string) {
  return [...document.querySelectorAll("label")]
    .find((l) => l.textContent?.startsWith(s))!
    .querySelector<HTMLInputElement | HTMLSelectElement>("input,select")!;
}
async function check(s: string) {
  await act(async () => field(s).click());
}
async function choose(s: string, v: string) {
  await act(async () => {
    const e = field(s);
    Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value")!.set!.call(e, v);
    e.dispatchEvent(new Event("change", { bubbles: true }));
  });
}
async function submit() {
  await act(async () => {
    document
      .querySelector("form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  });
}
const hash = "sha256:" + "a".repeat(64);
const target: DeliveryTarget = {
  configured: true,
  id: changesetId,
  environment: "staging",
  endpointId: "engineering",
  url: "https://engineering.example/context",
  fingerprint: hash,
  enabled: true,
  revision: 2,
  ready: true,
};
const saved: Delivery = {
  id: changesetId,
  requestId: documentId,
  requestRevision: 4,
  packRevision: 2,
  packHash: hash,
  targetId: changesetId,
  targetRevision: 2,
  targetFingerprint: hash,
  destinationUrl: target.url!,
  bodyHash: hash,
  initiatedBy: documentId,
  createdAt: "2026-10-17T00:00:00Z",
  revision: 3,
  state: "retry_wait",
  cycle: 1,
  totalAttempts: 1,
  attemptsInCycle: 1,
  nextAttemptAt: "2026-10-17T00:01:00Z",
  lastError: "ACK_INVALID",
  retryableExhaustion: false,
  canCancel: true,
  attempts: [
    {
      id: documentId,
      ordinal: 1,
      cycle: 1,
      startedAt: "2026-10-17T00:00:00Z",
      outcome: "ACK_INVALID",
      retryable: true,
      httpStatus: 200,
    },
  ],
  audit: [
    {
      id: 1,
      event: "queued",
      source: "human",
      reason: "Explicit consent",
      revision: 1,
      createdAt: "2026-10-17T00:00:00Z",
    },
  ],
};
const card: CapabilityRequest = {
  id: documentId,
  kind: "action",
  environment: "staging",
  key: "store.favorite",
  title: "Favorite",
  description: "Task",
  proposedContract: { args: {} },
  acceptance: ["Works"],
  status: "submitted",
  revision: 4,
  changesetId,
  documentId,
  nodeId: "n_root",
  event: "click",
  ownerId: documentId,
  externalRefs: [],
};
async function setup(
  mode: "card" | "target" = "card",
  options: {
    items?: Delivery[];
    rights?: string[];
    ru?: boolean;
    prepare?: (api: Api) => void;
  } = {},
) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("crypto", webcrypto);
  const { api } = mockApi();
  api.account = vi.fn(async () => ({
    authenticated: true,
    user: { id: documentId, name: "Author" },
    rights: options.rights ?? [
      "project.admin",
      "capability.request",
      "design.read",
      "capability.deliver",
    ],
    csrfToken: "csrf",
    hasAccess: true,
  }));
  api.deliveryTarget = vi.fn(async () => structuredClone(target));
  api.deliveryEndpoints = vi.fn(async () => ({
    items: [{ endpointId: target.endpointId!, url: target.url!, fingerprint: hash, ready: true }],
  }));
  let items = options.items ?? [];
  let detail = structuredClone(items[0] ?? saved);
  api.deliveries = vi.fn(async () => ({ items: structuredClone(items), nextCursor: null }));
  api.delivery = vi.fn(async () => structuredClone(detail));
  const contextPack = { request: { id: documentId }, safeContext: true };
  const packHash = await manifestHash(contextPack);
  const preview: DeliveryPreview = {
    requestId: documentId,
    requestRevision: 4,
    packRevision: 2,
    packHash,
    seq: 2,
    manifestHash: hash,
    contextPack,
    target: structuredClone(target),
    canSend: true,
  };
  api.deliveryPreview = vi.fn(async () => structuredClone(preview));
  api.deliveryCommand = vi.fn(async (a) =>
    a.name === "set-capability-delivery-target" ? target : { delivery: saved, deduplicated: false },
  );
  options.prepare?.(api);
  editor = createEditor(api);
  if (options.ru === false) editor.locale("en");
  document.body.innerHTML = "<button id='before'>Before</button><div id='root'></div>";
  root = createRoot(document.getElementById("root")!);
  await act(async () =>
    root.render(
      mode === "target" ? (
        <DeliveryTargetPanel {...{ api, editor }} />
      ) : (
        <CapabilityDeliveryPanel {...{ api, editor }} request={card} />
      ),
    ),
  );
  return {
    api,
    preview,
    setItems: (next: Delivery[]) => {
      items = next;
      detail = structuredClone(next[0] ?? saved);
    },
  };
}

it("STU-286/287: external consent pins the reviewed saved pack, distinct from request status", async () => {
  const { api, preview } = await setup();
  expect(document.body.textContent).toContain("Инженерный статус запроса меняется отдельно");
  await click("Передать context pack");
  expect(document.querySelector("[role=dialog]")).not.toBeNull();
  expect(button("Подтвердить передачу").disabled).toBe(true);
  expect(document.body.textContent).toContain(preview.target.url);
  expect(editor.store.getState().dirtyFields["property-capability-delivery"]).toBe(true);
  await check("Разрешаю передать этот context pack");
  preview.seq = 999; // API returned a clone; consent stays attached to the reviewed snapshot.
  await click("Подтвердить передачу");
  expect(api.deliveryCommand).toHaveBeenCalledTimes(1);
  expect(vi.mocked(api.deliveryCommand!).mock.calls[0]![0]).toMatchObject({
    name: "send-capability-context",
    actor: documentId,
    payload: {
      requestId: documentId,
      expectedRequestRevision: 4,
      expectedPackRevision: 2,
      expectedSeq: 2,
      expectedTargetRevision: 2,
      expectedTargetFingerprint: hash,
      confirmExternalShare: true,
    },
  });
  expect(document.querySelector("[role=dialog]")).toBeNull();
  expect(editor.store.getState().dirtyFields["property-capability-delivery"]).toBeFalsy();
});
it("uncertain commands retain byte-equivalent payload and idempotency key while blocking dismiss", async () => {
  const { api } = await setup();
  vi.mocked(api.deliveryCommand!).mockRejectedValueOnce(new StudioError("NETWORK_UNCERTAIN", true));
  await click("Передать context pack");
  await check("Разрешаю передать этот context pack");
  await click("Подтвердить передачу");
  expect(button("Закрыть без передачи").disabled).toBe(true);
  expect(editor.store.getState().status).toBe("uncertain");
  const first = structuredClone(vi.mocked(api.deliveryCommand!).mock.calls[0]![0]);
  await click("Повторить тот же запрос");
  expect(vi.mocked(api.deliveryCommand!).mock.calls[1]![0]).toEqual(first);
  expect(editor.store.getState().status).toBe("idle");
});
it("conflicts retain the reviewed context and require refreshed details and fresh consent", async () => {
  const { api } = await setup();
  vi.mocked(api.deliveryCommand!).mockRejectedValueOnce(
    new StudioError("CAPABILITY_DELIVERY_TARGET_CHANGED"),
  );
  await click("Передать context pack");
  await check("Разрешаю передать этот context pack");
  await click("Подтвердить передачу");
  expect(document.body.textContent).toContain("Получатель изменился");
  expect(document.querySelector("pre")?.textContent).toContain("safeContext");
  expect(button("Подтвердить передачу").disabled).toBe(true);
  await check("Разрешаю передать этот context pack");
  expect(button("Подтвердить передачу").disabled).toBe(true);
  await click("Перечитать сведения");
  expect((field("Разрешаю передать этот context pack") as HTMLInputElement).checked).toBe(false);
  await check("Разрешаю передать этот context pack");
  await click("Подтвердить передачу");
  expect(api.deliveryCommand).toHaveBeenCalledTimes(2);
});
it("manual retry uses the same delivery and displays receipts, journal and cancellation limits", async () => {
  const failed = { ...saved, state: "failed" as const, retryableExhaustion: true, canRetry: true };
  const { api } = await setup("card", { items: [failed], ru: false });
  await click("Retry delivery");
  await check("I consent to sharing this context pack");
  await click("Confirm delivery");
  expect(vi.mocked(api.deliveryCommand!).mock.calls[0]![0]).toMatchObject({
    name: "retry-capability-delivery",
    payload: { deliveryId: saved.id, expectedDeliveryRevision: 3, confirmExternalShare: true },
  });
  await click("Stop attempts");
  expect(vi.mocked(api.deliveryCommand!).mock.calls[1]![0].name).toBe("cancel-capability-delivery");
  expect(document.body.textContent).toContain("Delivery audit");
  expect(document.body.textContent).toContain("Explicit consent");
});
it("journal polls active deliveries, stops at terminal state, and aborts after unmount", async () => {
  const { api, setItems } = await setup("card", { items: [saved] });
  vi.useFakeTimers();
  setItems([
    {
      ...saved,
      state: "delivered",
      lastError: "",
      canCancel: false,
      deliveredAt: "2026-10-17T00:00:01Z",
      attempts: [
        {
          ...saved.attempts![0]!,
          outcome: "STORED",
          receiptDeliveryId: saved.id,
          receiptBodyHash: hash,
        },
      ],
    },
  ]);
  await click("Обновить журнал передачи");
  expect(document.body.textContent).toContain("Квитанция сохранения");
  const count = vi.mocked(api.deliveries!).mock.calls.length;
  await act(async () => vi.advanceTimersByTimeAsync(30000));
  expect(api.deliveries).toHaveBeenCalledTimes(count);
  await act(async () => root.unmount());
  expect(editor.store.getState().dirtyFields["property-capability-delivery"]).toBeFalsy();
});
it("engineers can inspect delivery history without gaining the external-sharing button", async () => {
  await setup("card", {
    items: [{ ...saved, state: "delivered", lastError: "", canCancel: false }],
    rights: ["code.propose"],
  });
  expect(button("Передать context pack")).toBeUndefined();
  expect(document.body.textContent).toContain("Пакет сохранён получателем");
  expect(button("Остановить попытки")).toBeUndefined();
});
it("a preview error and hash mismatch never enable external consent", async () => {
  const { api } = await setup();
  vi.mocked(api.deliveryPreview!).mockRejectedValueOnce(
    new StudioError("CAPABILITY_DELIVERY_CONTEXT_STALE"),
  );
  await click("Передать context pack");
  expect(document.body.textContent).toContain("Контекст устарел");
  expect(document.querySelector("[role=dialog]")).toBeNull();
  vi.mocked(api.deliveryPreview!).mockResolvedValueOnce({
    requestId: documentId,
    requestRevision: 4,
    packRevision: 2,
    packHash: hash,
    seq: 2,
    manifestHash: hash,
    contextPack: { wrong: true },
    target,
    canSend: true,
  });
  await click("Передать context pack");
  expect(document.body.textContent).toContain("RESPONSE_INVALID");
  expect(api.deliveryCommand).not.toHaveBeenCalled();
});
it("dialog escape and explicit discard clear dirty state and restore focus", async () => {
  await setup();
  button("Передать context pack").focus();
  await click("Передать context pack");
  await act(async () =>
    document
      .querySelector("[role=dialog]")!
      .dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })),
  );
  expect(document.querySelector("[role=dialog]")).toBeNull();
  expect(document.activeElement).toBe(button("Передать context pack"));
  await click("Передать context pack");
  await click("Закрыть без передачи");
  expect(editor.store.getState().dirtyFields["property-capability-delivery"]).toBeFalsy();
});
it("admin target settings never send a pack and preserve revision expectations", async () => {
  const { api } = await setup("target");
  await check("Разрешить передачу после отдельного подтверждения автора");
  expect(editor.store.getState().dirtyFields["property-delivery-target"]).toBe(true);
  await click("Сбросить ввод");
  expect(editor.store.getState().dirtyFields["property-delivery-target"]).toBeFalsy();
  await choose("Получатель", "engineering");
  await check("Разрешить передачу после отдельного подтверждения автора");
  await submit();
  expect(vi.mocked(api.deliveryCommand!).mock.calls[0]![0]).toMatchObject({
    name: "set-capability-delivery-target",
    payload: {
      environment: "staging",
      endpointId: "engineering",
      enabled: false,
      expectedRevision: 2,
    },
  });
  expect(api.deliveries).not.toHaveBeenCalled();
  await click("Перечитать настройки");
});
it("target setup supports an empty catalogue and an explicit first revision", async () => {
  const { api } = await setup("target", {
    ru: false,
    prepare: (a) => {
      a.deliveryTarget = vi.fn(async () => ({ configured: false, revision: 0, ready: false }));
      a.deliveryEndpoints = vi.fn(async () => ({ items: [] }));
    },
  });
  expect(document.body.textContent).toContain("catalogue is empty");
  expect(button("Save recipient").disabled).toBe(true);
  vi.mocked(api.deliveryEndpoints!).mockResolvedValue({
    items: [{ endpointId: "engineering", url: target.url!, fingerprint: hash, ready: true }],
  });
  await click("Reload settings");
  await choose("Recipient", "engineering");
  await check("Enable delivery after");
  await submit();
  expect(vi.mocked(api.deliveryCommand!).mock.calls[0]![0].payload).toMatchObject({
    expectedRevision: 0,
    enabled: true,
  });
});
it("target errors retain input; uncertain target updates allow only the exact retry", async () => {
  const { api } = await setup("target");
  await check("Разрешить передачу после отдельного подтверждения автора");
  vi.mocked(api.deliveryCommand!).mockRejectedValueOnce(new StudioError("NETWORK_UNCERTAIN", true));
  await submit();
  expect(button("Перечитать настройки").disabled).toBe(true);
  await click("Повторить тот же запрос");
  expect(vi.mocked(api.deliveryCommand!).mock.calls[1]![0]).toEqual(
    vi.mocked(api.deliveryCommand!).mock.calls[0]![0],
  );
  vi.mocked(api.deliveryTarget!).mockRejectedValueOnce(
    new StudioError("CAPABILITY_DELIVERY_UNAVAILABLE"),
  );
  await click("Перечитать настройки");
  expect(document.body.textContent).toContain("Доставка не настроена");
});
