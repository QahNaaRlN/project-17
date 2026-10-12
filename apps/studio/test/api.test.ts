import { expect, it, vi } from "vitest";
import { createApi } from "../src/api.js";
import { fixture, documentId, changesetId } from "./fixture.js";
import fc from "fast-check";
import { StudioError } from "../src/types.js";
it("STU-050: произвольные JSON ответы не вызывают исключение без безопасного кода", async () => {
  await fc.assert(
    fc.asyncProperty(fc.jsonValue(), async (body) => {
      const api = createApi(async () => Response.json(body));
      try {
        await api.history(fixture(), undefined, new AbortController().signal);
      } catch (error) {
        expect(error).toBeInstanceOf(StudioError);
      }
    }),
    { numRuns: 60, seed: 501 },
  );
});
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
it("STU-071/072/073/074: workflow uses frozen seq/key, configured environment and a publication receipt", async () => {
  const s = fixture(),
    publication = {
      id: documentId,
      changesetId,
      environment: "staging",
      createdAt: "2026-10-12T00:00:00Z",
    };
  const fetcher = vi.fn<typeof fetch>(async (input) => {
      const path = String(input);
      const state = path.endsWith("submit-changeset")
        ? "in_review"
        : path.endsWith("reopen-changeset")
          ? "open"
          : path.endsWith("approve-changeset")
            ? "approved"
            : "changes_requested";
      return Response.json({
        result: path.endsWith("/publish") ? publication : { changeset: { ...s.changeset, state } },
      });
    }),
    api = createApi(fetcher);
  for (const action of ["submit", "reopen", "approve", "request-changes", "publish"] as const) {
    expect(await api.workflow(s, action, "Revise", "frozen-key", signal)).toEqual(
      action === "publish" ? publication : null,
    );
    const init = fetcher.mock.calls.at(-1)![1]!;
    expect(init.headers).toMatchObject({ "Idempotency-Key": "frozen-key" });
    expect(JSON.parse(String(init.body)).payload).toEqual({
      changesetId,
      expectedSeq: 1,
      ...(action === "publish" ? { environment: "staging" } : {}),
      ...(action === "request-changes" ? { comment: "Revise" } : {}),
    });
  }
  fetcher.mockResolvedValue(
    Response.json({ result: { ...publication, environment: "production" } }),
  );
  await expect(api.workflow(s, "publish", "", "k", signal)).rejects.toMatchObject({
    code: "RESPONSE_INVALID",
    uncertain: true,
  });
});
it("STU-074: structured validation error keeps codes and paths without server detail or raw values", async () => {
  const api = createApi(async () =>
    Response.json(
      {
        code: "VALIDATION_FAILED",
        detail: "cms_svc_secret",
        diagnostics: [
          {
            objectId: documentId,
            environment: "staging",
            code: "PROP_REQUIRED",
            severity: "error",
            pointer: "/nodes/n_heading",
            message: "secret",
          },
        ],
      },
      { status: 422 },
    ),
  );
  try {
    await api.workflow(fixture(), "submit", "", "k", signal);
    throw Error("must reject");
  } catch (error) {
    expect(error).toBeInstanceOf(StudioError);
    expect((error as StudioError).diagnostics).toHaveLength(1);
    expect(JSON.stringify(error)).not.toContain("secret");
  }
});
it.each([
  null,
  1,
  {},
  { changeset: { id: documentId, seq: 1, state: "approved" } },
  { changeset: { id: changesetId, seq: 2, state: "approved" } },
  { changeset: { id: changesetId, seq: 1, state: "merged" } },
])("STU-074: an invalid successful submit response remains uncertain", async (result) => {
  const api = createApi(async () => Response.json({ result }));
  await expect(api.workflow(fixture(), "submit", "", "k", signal)).rejects.toMatchObject({
    code: "RESPONSE_INVALID",
    uncertain: true,
  });
});
it("STU-070/074: a review crossing state or seq changes cannot form an editor session", async () => {
  const { api, s } = setup();
  s.review = { ...s.review, changeset: { ...s.changeset, state: "in_review" } };
  await expect(api.open(documentId, changesetId, signal)).rejects.toThrow("CHANGESET_SEQ_CONFLICT");
});
it("STU-050/051/052: история и команды используют публичный API, group, seq и ключ", async () => {
  const { api, fetcher, s } = setup();
  fetcher.mockImplementation(async (input) =>
    Response.json(
      String(input).endsWith("/config")
        ? s.config
        : String(input).includes("/history")
          ? s.history
          : { result: {} },
    ),
  );
  expect(await api.history(s, 1, signal)).toEqual(s.history);
  for (const action of ["undo", "redo"] as const)
    await api.changeHistory(s, action, "frozen-key", signal);
  const call = fetcher.mock.calls.find(([p]) => String(p).endsWith("/commands/undo"))!;
  expect(JSON.parse(String(call[1]!.body))).toEqual({
    payload: { changesetId, expectedSeq: 1 },
    reason: "Studio: undo action",
  });
  expect(call[1]!.headers).toMatchObject({ "Idempotency-Key": "frozen-key" });
  await expect(api.history(s, 0, signal)).rejects.toThrow("PARAM_INVALID");
  s.history.seq = 2;
  await expect(
    api.history({ ...s, changeset: { ...s.changeset, seq: 1 } }, undefined, signal),
  ).rejects.toThrow("CHANGESET_SEQ_CONFLICT");
});
it.each([
  null,
  { seq: -1 },
  { ...fixture().history, undoReason: 10 },
  { ...fixture().history, canUndo: true },
  { ...fixture().history, nextBeforeSeq: 1 },
  { ...fixture().history, items: [null] },
  { ...fixture().history, nextBeforeSeq: 0 },
])("STU-050: повреждённая история отклоняется", async (body) => {
  const api = createApi(async () => Response.json(body));
  await expect(api.history(fixture(), undefined, signal)).rejects.toThrow("HISTORY_INVALID");
});
it("STU-053: история и документ разных seq не образуют смешанную сессию", async () => {
  const { api, s } = setup();
  s.history.seq = 2;
  await expect(api.open(documentId, changesetId, signal)).rejects.toThrow("CHANGESET_SEQ_CONFLICT");
});
it("STU-021: чтение через конкурентную запись не выдаёт смешанный snapshot", async () => {
  const { api, fetcher, s } = setup();
  let reads = 0;
  const original = fetcher.getMockImplementation()!;
  fetcher.mockImplementation(async (input, init) => {
    if (
      String(input).includes("/changesets/") &&
      !String(input).endsWith("/history") &&
      !String(input).endsWith("/review") &&
      ++reads === 2
    )
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
          : url.endsWith("/history")
            ? s.history
            : url.endsWith("/review")
              ? s.review
              : url.includes("/documents/")
                ? s.record
                : url.includes("/changesets/")
                  ? s.changeset
                  : {
                      result: {
                        token: "jwt",
                        expiresAt: new Date(Date.now() + 900_000).toISOString(),
                      },
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
    payload: { changesetId, expectedSeq: 1, group: true },
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
