import { expect, it, vi } from "vitest";
import { createApi } from "../src/api.js";
import { documentId, fixture } from "./fixture.js";
import { previewItem } from "./preview-fixture.js";

it("STU-319/321/329: preview reads, exact-retry args and build-bound tokens", async () => {
  let previewResult: unknown = previewItem();
  let tokenResult: Record<string, unknown> = {
    token: "t",
    expiresAt: new Date(Date.now() + 900_000).toISOString(),
    registrationSeq: 3,
  };
  let argsResult: unknown = { revision: 1 };
  const fetcher = vi.fn<typeof fetch>(async (input) => {
    const p = String(input);
    if (p.endsWith("/config"))
      return Response.json({
        project: "store",
        environment: "staging",
        appOrigin: "http://127.0.0.1:18083",
      });
    if (p.includes("/auth/session"))
      return Response.json({
        authenticated: true,
        user: { id: documentId, name: "Designer" },
        rights: ["design.read"],
        csrfToken: "c".repeat(43),
        expiresAt: "2030-01-01T00:00:00Z",
        hasAccess: true,
      });
    if (p.includes("set-capability-preview-args")) return Response.json({ result: argsResult });
    if (p.includes("create-preview-token")) return Response.json({ result: tokenResult });
    return Response.json(previewResult);
  });
  const api = createApi(fetcher);
  expect(await api.capabilityPreview!(documentId)).toEqual(previewItem());
  expect(String(fetcher.mock.calls.at(-1)![0])).toContain(
    "/capability-requests/" + documentId + "/preview?environment=staging",
  );
  await expect(api.capabilityPreview!("bad")).rejects.toMatchObject({ code: "PAYLOAD_INVALID" });
  previewResult = { items: "x" };
  await expect(api.capabilityPreview!(documentId)).rejects.toMatchObject({
    code: "RESPONSE_INVALID",
  });

  const attempt = {
    payload: {
      requestId: documentId,
      expectedRequestRevision: 2,
      environment: "preview/pr-1",
      manifestHash: "sha256:" + "a".repeat(64),
      handlers: [{ action: "store.favorite" }],
      expectedArgsRevision: 0,
    },
    key: "frozen",
    reason: "Explicit",
    actor: documentId,
  };
  await api.previewArgs!(attempt);
  await api.previewArgs!(attempt);
  const calls = fetcher.mock.calls.filter(([p]) => String(p).includes("preview-args"));
  expect(calls[0]).toEqual(calls[1]);
  await expect(api.previewArgs!({ ...attempt, actor: "other" })).rejects.toMatchObject({
    code: "SESSION_CHANGED",
    uncertain: true,
  });
  argsResult = {};
  await expect(api.previewArgs!(attempt)).rejects.toMatchObject({
    code: "RESPONSE_INVALID",
    uncertain: true,
  });

  const session = fixture();
  const signal = new AbortController().signal;
  expect(await api.preview(session, signal, "preview/pr-1")).toMatchObject({ registrationSeq: 3 });
  const body = JSON.parse(String(fetcher.mock.calls.at(-1)![1]!.body));
  expect(body.payload).toEqual({ environment: "preview/pr-1", changesetId: session.changeset.id });
  tokenResult = { token: "t", expiresAt: new Date().toISOString() };
  await expect(api.preview(session, signal, "preview/pr-1")).rejects.toMatchObject({
    code: "PREVIEW_INVALID",
  });
  expect(await api.preview(session, signal)).toMatchObject({ token: "t" });
});
