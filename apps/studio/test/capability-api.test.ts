import { expect, it, vi } from "vitest";
import { createApi } from "../src/api.js";
import { capability, capabilityURL, capabilityMessage } from "../src/capabilities.js";
import { documentId, changesetId } from "./fixture.js";
import type { CapabilityRequest } from "../src/capabilities.js";
const card: CapabilityRequest = {
  id: documentId,
  kind: "action",
  environment: "staging",
  key: "store.new",
  title: "New",
  description: "Task",
  proposedContract: { args: {} },
  acceptance: ["works"],
  status: "draft",
  revision: 1,
  changesetId,
  documentId,
  nodeId: "n_root",
  event: "click",
  ownerId: documentId,
  externalRefs: [],
};
it("capability Query/Command API preserves frozen request bodies and validates identifiers and responses", async () => {
  const config = { project: "store", environment: "staging", appOrigin: "http://127.0.0.1:18083" };
  const fetcher = vi.fn<typeof fetch>(async (input) => {
    const url = String(input);
    if (url.endsWith("/config")) return Response.json(config);
    if (url.includes("/context-preview"))
      return Response.json({
        hash: "sha256:" + "a".repeat(64),
        pack: { request: { id: documentId } },
      });
    if (url.includes("/context-pack")) return Response.json({ request: { id: documentId } });
    if (url.includes("/commands/")) return Response.json({ result: { request: card } });
    if (url.includes("/capability-requests?"))
      return Response.json({ items: [card], nextCursor: null });
    return Response.json(card);
  });
  const api = createApi(fetcher);
  expect((await api.capabilities!({ status: "draft", key: "store.new" })).items[0]).toEqual(card);
  expect(await api.capability!(documentId)).toEqual(card);
  expect(await api.capabilityContext!(documentId)).toMatchObject({
    pack: { request: { id: documentId } },
  });
  expect(await api.capabilityContext!(documentId, 2)).toMatchObject({
    pack: { request: { id: documentId } },
  });
  const attempt = {
    name: "submit-capability-request" as const,
    payload: { id: documentId, expectedRevision: 1, expectedSeq: 2 },
    key: "exact-request",
    reason: "Explicit submit",
  };
  await api.capabilityCommand!(attempt);
  await api.capabilityCommand!(attempt);
  const requests = fetcher.mock.calls.filter(([url]) => String(url).includes("/commands/"));
  expect(requests[0]![1]).toEqual(requests[1]![1]);
  expect(requests[0]![1]?.body).toBe(
    JSON.stringify({ payload: attempt.payload, reason: attempt.reason }),
  );
  for (const id of ["bad", "https://example.test"]) {
    await expect(api.capability!(id)).rejects.toMatchObject({ code: "IDENTIFIER_INVALID" });
    await expect(api.capabilityContext!(id)).rejects.toMatchObject({ code: "IDENTIFIER_INVALID" });
  }
  await expect(api.capabilityContext!(documentId, 0)).rejects.toMatchObject({
    code: "IDENTIFIER_INVALID",
  });
  fetcher.mockImplementation(async (input) =>
    String(input).endsWith("/config") ? Response.json(config) : Response.json({ items: "bad" }),
  );
  await expect(api.capabilities!({})).rejects.toMatchObject({ code: "RESPONSE_INVALID" });
  await expect(api.capability!(documentId)).rejects.toMatchObject({ code: "RESPONSE_INVALID" });
});
it("safe projections reject active URLs and retain controlled localized failures", () => {
  expect(capabilityURL("https://example.test/pr/1")).toBe(true);
  for (const url of [
    "javascript:alert(1)",
    "http://example.test",
    "https://private:password@example.test/pr/1",
    "not-url",
  ]) {
    expect(capabilityURL(url)).toBe(false);
    expect(() => capability({ ...card, externalRefs: [url] })).toThrow();
  }
  for (const code of [
    "CAPABILITY_KEY_TAKEN",
    "CAPABILITY_ALREADY_AVAILABLE",
    "CAPABILITY_REVISION_CONFLICT",
    "CAPABILITY_CONTEXT_STALE",
    "CAPABILITY_INDEX_UNAVAILABLE",
    "CAPABILITY_CONTRACT_MISMATCH",
    "CAPABILITY_ANCHOR_CHANGED",
    "CAPABILITY_STATE_INVALID",
    "CONTEXT_PACK_LIMIT_EXCEEDED",
    "FORBIDDEN",
    "CAPABILITY_CODE_REQUIRED",
    "UNKNOWN",
  ]) {
    expect(capabilityMessage(code, true)).not.toBe(capabilityMessage(code, false));
  }
});
