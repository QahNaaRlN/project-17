import { expect, it, vi } from "vitest";
import { createApi } from "../src/api.js";
import {
  delivery,
  deliveryTarget,
  deliveryPreview,
  deliveryMessage,
  deliveryStatus,
  deliveryActive,
  sendPayload,
  type Delivery,
  type DeliveryPreview,
} from "../src/capability-delivery.js";
import { documentId, changesetId } from "./fixture.js";
const hash = "sha256:" + "a".repeat(64);
const target = {
  configured: true,
  id: changesetId,
  environment: "staging",
  endpointId: "receiver",
  url: "https://receiver.example/context",
  fingerprint: hash,
  enabled: true,
  revision: 1,
  ready: true,
};
const item: Delivery = {
  id: changesetId,
  requestId: documentId,
  requestRevision: 3,
  packRevision: 2,
  packHash: hash,
  targetId: changesetId,
  targetRevision: 1,
  targetFingerprint: hash,
  destinationUrl: target.url,
  bodyHash: hash,
  initiatedBy: documentId,
  createdAt: "2026-10-17T00:00:00Z",
  revision: 1,
  state: "queued",
  cycle: 1,
  totalAttempts: 0,
  attemptsInCycle: 0,
  nextAttemptAt: "2026-10-17T00:00:00Z",
  lastError: "",
  retryableExhaustion: false,
};
const preview: DeliveryPreview = {
  requestId: documentId,
  requestRevision: 3,
  packRevision: 2,
  packHash: hash,
  seq: 2,
  manifestHash: hash,
  contextPack: { safe: true },
  target,
  canSend: true,
};
it("validates delivery projections and preserves environment-pinned exact command envelopes", async () => {
  const fetcher = vi.fn<typeof fetch>(async (input) => {
    const path = String(input);
    if (path.endsWith("/config"))
      return Response.json({
        project: "store",
        environment: "staging",
        appOrigin: "http://127.0.0.1:18083",
      });
    if (path.includes("/auth/session"))
      return Response.json({
        authenticated: true,
        user: { id: documentId, name: "Author" },
        rights: ["capability.deliver"],
        csrfToken: "c".repeat(43),
        expiresAt: "2030-01-01T00:00:00Z",
        hasAccess: true,
      });
    if (path.includes("/delivery-preview")) return Response.json(preview);
    if (path.includes("/capability-delivery-target")) return Response.json(target);
    if (path.includes("/capability-delivery-endpoints"))
      return Response.json({
        items: [{ endpointId: target.endpointId, url: target.url, fingerprint: hash, ready: true }],
      });
    if (path.includes("/deliveries?")) return Response.json({ items: [item], nextCursor: "next" });
    if (path.includes("/commands/set-")) return Response.json({ result: target });
    if (path.includes("/commands/"))
      return Response.json({ result: { delivery: item, deduplicated: false } });
    return Response.json(item);
  });
  const api = createApi(fetcher);
  expect(await api.deliveryTarget!()).toEqual(target);
  expect((await api.deliveryEndpoints!()).items).toHaveLength(1);
  expect(await api.deliveryPreview!(documentId)).toEqual(preview);
  expect((await api.deliveries!(documentId, "before")).items).toEqual([item]);
  expect(await api.delivery!(changesetId)).toEqual(item);
  const attempt = {
    name: "send-capability-context" as const,
    key: "exact-key",
    payload: sendPayload(preview),
    reason: "Explicit external sharing",
    actor: documentId,
  };
  await api.deliveryCommand!(attempt);
  await api.deliveryCommand!(attempt);
  const calls = fetcher.mock.calls.filter(([url]) => String(url).includes("/commands/"));
  expect(calls[0]![1]).toEqual(calls[1]![1]);
  expect(JSON.parse(String(calls[0]![1]?.body))).toEqual({
    payload: attempt.payload,
    reason: attempt.reason,
  });
  await expect(api.deliveryCommand!({ ...attempt, actor: changesetId })).rejects.toMatchObject({
    code: "SESSION_CHANGED",
    uncertain: true,
  });
  await expect(
    api.deliveryCommand!({ ...attempt, name: "set-capability-delivery-target" }),
  ).resolves.toEqual(target);
  for (const read of [api.delivery!, api.deliveryPreview!, api.deliveries!])
    await expect(read("invalid")).rejects.toMatchObject({ code: "IDENTIFIER_INVALID" });
  for (const [url] of fetcher.mock.calls.filter(
    ([url]) =>
      !String(url).includes("/commands/") &&
      !String(url).includes("/auth/") &&
      !String(url).endsWith("/config"),
  ))
    expect(String(url)).toContain("environment=staging");
  fetcher.mockImplementation(async (input) =>
    String(input).endsWith("/config")
      ? Response.json({
          project: "store",
          environment: "staging",
          appOrigin: "http://127.0.0.1:18083",
        })
      : Response.json({ items: "invalid" }),
  );
  await expect(api.deliveryEndpoints!()).rejects.toMatchObject({ code: "RESPONSE_INVALID" });
  await expect(api.deliveries!(documentId)).rejects.toMatchObject({ code: "RESPONSE_INVALID" });
  const { actor: _actor, ...withoutActor } = attempt;
  await expect(api.deliveryCommand!(withoutActor)).rejects.toMatchObject({
    code: "RESPONSE_INVALID",
    uncertain: true,
  });
});
it("rejects invalid delivery/preview/target DTOs and keeps all controlled statuses and outcomes localized", () => {
  expect(delivery(item)).toEqual(item);
  expect(deliveryTarget({ configured: false, revision: 0, ready: false })).toMatchObject({
    configured: false,
  });
  expect(deliveryPreview(preview)).toEqual(preview);
  for (const bad of [
    { ...item, id: "bad" },
    { ...item, state: "fulfilled" },
    { ...item, totalAttempts: 19 },
    { ...item, cycle: 4 },
    { ...item, createdAt: "bad" },
    { ...item, bodyHash: "bad" },
  ])
    expect(() => delivery(bad as Delivery, true)).toThrow();
  for (const bad of [
    { ...target, fingerprint: "bad" },
    { ...target, revision: 0 },
    { ...target, configured: undefined },
  ])
    expect(() => deliveryTarget(bad as typeof target)).toThrow();
  for (const bad of [
    { ...preview, contextPack: [] },
    { ...preview, target: { ...target, ready: false } },
    { ...preview, canSend: false },
    { ...preview, seq: -1 },
  ])
    expect(() => deliveryPreview(bad as DeliveryPreview)).toThrow();
  for (const state of [
    "queued",
    "sending",
    "retry_wait",
    "delivered",
    "failed",
    "cancelled",
  ] as const) {
    expect(deliveryStatus(state, true)).not.toBe(deliveryStatus(state, false));
    expect(deliveryActive({ ...item, state })).toBe(
      ["queued", "sending", "retry_wait"].includes(state),
    );
  }
  for (const code of [
    "REVISION_CONFLICT",
    "CONTEXT_STALE",
    "ANCHOR_CHANGED",
    "TARGET_CHANGED",
    "TARGET_DISABLED",
    "KEY_UNAVAILABLE",
    "UNAVAILABLE",
    "FORBIDDEN",
    "AUTH_REVOKED",
    "REQUEST_TERMINAL",
    "STATE_INVALID",
    "RETRY_EXHAUSTED",
    "ADDRESS_BLOCKED",
    "ACK_INVALID",
    "NETWORK_UNCERTAIN",
    "HTTP_RETRYABLE",
    "HTTP_PERMANENT",
    "LIMIT_EXCEEDED",
    "CANCELLED_BY_HUMAN",
    "UNKNOWN",
  ]) {
    expect(deliveryMessage("CAPABILITY_DELIVERY_" + code, true)).not.toBe(
      deliveryMessage(code, false),
    );
  }
});
