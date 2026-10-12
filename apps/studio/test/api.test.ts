import { expect, it, vi } from "vitest";
import { createApi } from "../src/api.js";
import { fixture, documentId, changesetId } from "./fixture.js";
it("STU-040/041: списки используют настроенное окружение, курсор и выбранную страницу", async () => {
  const session = fixture();
  const fetcher = vi.fn<typeof fetch>(async (input) => {
    const path = String(input);
    return Response.json(
      path.endsWith("/config")
        ? session.config
        : {
            items: path.includes("/pages?")
              ? [{ id: documentId, path: "/store", title: "Store", hasHead: true }]
              : [session.changeset],
            nextCursor: changesetId,
          },
    );
  });
  const api = createApi(fetcher);
  expect((await api.pages(changesetId, signal)).items[0]).toMatchObject({ path: "/store" });
  expect((await api.drafts(documentId, changesetId, signal)).items[0]).toEqual(session.changeset);
  expect(fetcher.mock.calls.map(([p]) => String(p))).toContain(
    `/api/studio/api/v1/working-drafts?environment=staging&documentId=${documentId}&after=${changesetId}`,
  );
  await expect(api.pages("bad", signal)).rejects.toThrow("IDENTIFIER_INVALID");
  await expect(api.drafts("bad", undefined, signal)).rejects.toThrow("IDENTIFIER_INVALID");
});
it.each([
  { items: null, nextCursor: null },
  { items: [], nextCursor: "bad" },
  { items: [null], nextCursor: null },
])("STU-044: повреждённые списки отклоняются", async (body) => {
  const api = createApi(async (input) =>
    Response.json(String(input).endsWith("/config") ? fixture().config : body),
  );
  await expect(api.pages()).rejects.toThrow("RESPONSE_INVALID");
  await expect(api.drafts(documentId)).rejects.toThrow("RESPONSE_INVALID");
});
it("STU-042/043: создание использует один target, имя и предоставленный ключ повтора", async () => {
  const s = fixture();
  const fetcher = vi.fn<typeof fetch>(async (input) =>
    Response.json(String(input).endsWith("/config") ? s.config : { result: s.changeset }),
  );
  const api = createApi(fetcher);
  await api.createDraft(" Новый черновик ", "same-key");
  await api.createDraft(" Новый черновик ", "same-key");
  const calls = fetcher.mock.calls.filter(([p]) => String(p).includes("create-changeset"));
  expect(calls).toHaveLength(2);
  expect(calls[0]![1]).toEqual(calls[1]![1]);
  expect(JSON.parse(String(calls[0]![1]!.body))).toEqual({
    payload: { title: "Новый черновик", targets: ["staging"] },
    reason: "Studio: create page draft",
  });
  await expect(api.createDraft(" ", "k")).rejects.toThrow("TITLE_INVALID");
  await expect(api.createDraft("x".repeat(201), "k")).rejects.toThrow("TITLE_INVALID");
  s.changeset.state = "merged";
  await expect(api.createDraft("name", "k")).rejects.toMatchObject({
    code: "RESPONSE_INVALID",
    uncertain: true,
  });
});
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
