import { expect, it, vi } from "vitest";
import { createApi } from "../src/api.js";
import { fixture, documentId, changesetId } from "./fixture.js";
const signal = new AbortController().signal;
it("STU-021: чтение через конкурентную запись не выдаёт смешанный snapshot", async () => {
  const { api, fetcher, s } = setup();
  let reads = 0;
  const original = fetcher.getMockImplementation()!;
  fetcher.mockImplementation(async (input, init) => {
    if (String(input).includes("/changesets/") && ++reads === 2)
      return Response.json({ ...s.changeset, seq: s.changeset.seq + 1 });
    return original(input, init);
  });
  await expect(api.open(documentId, changesetId, signal)).rejects.toThrow("CHANGESET_SEQ_CONFLICT");
});
function setup() {
  const s = fixture();
  const fetcher = vi.fn<typeof fetch>(async (input) => {
    const url = String(input);
    return Response.json(
      url.endsWith("/config")
        ? s.config
        : url.includes("/manifest")
          ? { manifest: s.manifest, manifestHash: s.manifestHash }
          : url.includes("/documents/")
            ? s.record
            : url.includes("/changesets/")
              ? s.changeset
              : {
                  result: { token: "jwt", expiresAt: new Date(Date.now() + 900_000).toISOString() },
                },
    );
  });
  return { s, fetcher, api: createApi(fetcher) };
}
it("STU-001: читает публичные API и передаёт seq/idempotency/reason без credential", async () => {
  const { api, fetcher } = setup(),
    session = await api.open(documentId, changesetId, signal);
  await api.apply(
    session,
    [
      {
        type: "node.setProps",
        target: documentId,
        payload: { nodeId: "n_heading", set: { level: 3 } },
      },
    ],
    "same-key",
    signal,
  );
  const [, init] = fetcher.mock.calls.find(([url]) => String(url).includes("apply-operations"))!;
  expect(JSON.parse(String(init!.body))).toMatchObject({
    payload: { changesetId, expectedSeq: 1 },
    reason: "Studio: edit page draft",
  });
  expect(init!.headers).toEqual({
    "Content-Type": "application/json",
    "Idempotency-Key": "same-key",
  });
  expect(await api.preview(session, signal)).toMatchObject({ token: "jwt" });
  expect(JSON.stringify(fetcher.mock.calls)).not.toContain("Authorization");
});
it("отклоняет неверные ID и несовместимые сессии", async () => {
  const { api, s } = setup();
  await expect(api.open("bad", changesetId, signal)).rejects.toThrow("IDENTIFIER_INVALID");
  s.changeset.kind = "schema";
  await expect(api.open(documentId, changesetId, signal)).rejects.toThrow("SESSION_INVALID");
  s.changeset.kind = "standard";
  s.record.body.root = "n_missing";
  await expect(api.open(documentId, changesetId, signal)).rejects.toThrow("SESSION_INVALID");
});
it.each([403, 409, 503])("STU-021: сохраняет безопасный код ошибки %i", async (status) => {
  const { s } = setup();
  const api = createApi(
    vi.fn(async () => Response.json({ code: "FORBIDDEN", detail: "cms_svc_secret" }, { status })),
  );
  await expect(api.apply(s, [], "key", signal)).rejects.toMatchObject({
    code: "FORBIDDEN",
    uncertain: status >= 500,
  });
});
it("неопределённый исход записи отличается от чтения, детали ошибки не выводятся", async () => {
  const { s } = setup();
  let fetcher: typeof fetch = async () => {
    throw Error("credential");
  };
  let api = createApi(fetcher);
  await expect(api.config()).rejects.toMatchObject({ code: "NETWORK_UNCERTAIN", uncertain: false });
  await expect(api.apply(s, [], "key", signal)).rejects.toMatchObject({ uncertain: true });
  fetcher = async () => new Response("not json", { status: 500 });
  api = createApi(fetcher);
  await expect(api.config()).rejects.toThrow("CMS_REQUEST_FAILED");
  api = createApi(async () => Response.json({ code: "unsafe secret" }, { status: 400 }));
  await expect(api.config()).rejects.toThrow("CMS_REQUEST_FAILED");
  api = createApi(async () => new Response("bad"));
  await expect(api.config()).rejects.toThrow("RESPONSE_INVALID");
  api = createApi(async () => Response.json({ result: { token: 1, expiresAt: "bad" } }));
  await expect(api.preview(s, signal)).rejects.toThrow("PREVIEW_INVALID");
});
