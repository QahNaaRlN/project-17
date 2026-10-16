import { afterEach, expect, it, vi } from "vitest";
import { createServer, request as httpRequest } from "node:http";
import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { checkConfig, createStudioHandler, type ServerConfig } from "../src/handler.js";
import { documentId, changesetId } from "./fixture.js";
const config: ServerConfig = {
  baseUrl: "http://127.0.0.1:18080",
  studioOrigin: "http://127.0.0.1:18085",
  appOrigin: "http://127.0.0.1:18083",
  project: "store",
  environment: "staging",
  credential: "cms_svc_protected_test",
};
const cleanups: (() => Promise<void>)[] = [];
it("STU-167/170: publication proxy enforces environment and guarded rollback", async () => {
  const { origin, post, fetcher } = await setup();
  const root = "/api/studio/api/v1/publication-history";
  const hash = "sha256:" + "a".repeat(64);
  for (const route of [
    root + "?environment=staging&limit=50",
    root + "/" + documentId + "?environment=staging",
    root +
      "/" +
      documentId +
      "/rollback-preview/diff?environment=staging&objectId=" +
      changesetId +
      "&snapshot=" +
      hash,
  ])
    expect((await fetch(origin + route)).status).toBe(200);
  for (const query of [
    "environment=staging&limit=0",
    "environment=staging&limit=101",
    "environment=staging&cursor=" + "x".repeat(2049),
    "environment=staging&other=1",
  ])
    expect((await fetch(origin + root + "?" + query)).status).toBe(400);
  expect((await fetch(origin + root + "?environment=production")).status).toBe(403);
  const route = "/api/studio/api/v1/commands/rollback",
    payload = { publicationId: documentId, resetHead: false, expectedRollbackSnapshot: hash };
  fetcher.mockImplementation(async (input) =>
    String(input).includes("publication-history")
      ? Response.json({ publication: { environment: "staging" } })
      : Response.json({ result: { id: changesetId } }),
  );
  expect((await post(route, { payload, reason: "Test" })).status).toBe(200);
  for (const bad of [
    { payload, reason: "" },
    { payload: { ...payload, resetHead: true }, reason: "Test" },
    { payload: { ...payload, expectedRollbackSnapshot: undefined }, reason: "Test" },
    { payload: { ...payload, environment: "production" }, reason: "Test" },
  ])
    expect((await post(route, bad)).status).toBe(400);
  fetcher.mockImplementation(async () =>
    Response.json({ publication: { environment: "production" } }),
  );
  expect((await post(route, { payload, reason: "Test" })).status).toBe(403);
  fetcher.mockImplementation(async () => Response.json({ code: "FORBIDDEN" }, { status: 403 }));
  expect((await post(route, { payload, reason: "Test" })).status).toBe(403);
});
it("STU-124/125: Agent proxy scopes reads and writes and excludes provider/session secrets", async () => {
  const { origin, post, fetcher } = await setup();
  const path = `/api/studio/api/v1/agent-runs?environment=staging&documentId=${documentId}&changesetId=${changesetId}`;
  expect((await fetch(origin + path)).status).toBe(200);
  expect((await fetch(origin + path + "&after=1")).status).toBe(400);
  expect(
    (await fetch(origin + path.replace("environment=staging", "environment=production"))).status,
  ).toBe(403);
  expect((await fetch(origin + "/api/studio/api/v1/agent-config")).status).toBe(200);
  const payload = {
    documentId,
    changesetId,
    environment: "staging",
    expectedSeq: 1,
    prompt: "Compose page",
  };
  const command = "/api/studio/api/v1/commands/start-agent-run";
  expect((await post(command, { payload })).status).toBe(200);
  expect((await post(command, { payload: { ...payload, model: "external" } })).status).toBe(400);
  expect((await post(command, { payload: { ...payload, environment: "production" } })).status).toBe(
    403,
  );
  expect((await post(command, { payload: { ...payload, expectedSeq: -1 } })).status).toBe(400);
  expect(
    (await post("/api/studio/api/v1/commands/cancel-agent-run", { payload: { runId: documentId } }))
      .status,
  ).toBe(200);
  expect(
    (await post("/api/studio/api/v1/commands/cancel-agent-run", { payload: { runId: "bad" } }))
      .status,
  ).toBe(400);
  expect(
    (await post("/api/studio/api/v1/commands/start-agent-session", { payload: {} })).status,
  ).toBe(404);
  expect(fetcher.mock.calls.some(([input]) => String(input).includes("openrouter"))).toBe(false);
});
it("STU-110/113: SSE streams immediately; adapter validates scope and cancels on disconnect", async () => {
  const { origin, fetcher, post } = await setup();
  const path = `/api/studio/api/v1/events?environment=staging&documentId=${documentId}&changesetId=${changesetId}`;
  for (const suffix of ["&after=bad", "&documentId=" + documentId, "&unknown=1"])
    expect((await fetch(origin + path + suffix)).status).toBe(400);
  expect(
    (await fetch(origin + path.replace("environment=staging", "environment=production"))).status,
  ).toBe(403);
  const cancelled = vi.fn();
  fetcher.mockResolvedValueOnce(
    new Response(
      new ReadableStream({
        start(controller) {
          controller.enqueue(new TextEncoder().encode('event: sync\ndata: {"revision":"1"}\n\n'));
        },
        cancel: cancelled,
      }),
      { headers: { "Content-Type": "text/event-stream", "Set-Cookie": "secret" } },
    ),
  );
  const lifecycle = new AbortController();
  const response = await fetch(origin + path + "&after=1", { signal: lifecycle.signal });
  expect(response.headers.get("content-type")).toBe("text/event-stream");
  expect(response.headers.get("set-cookie")).toBeNull();
  const reader = response.body!.getReader();
  expect(new TextDecoder().decode((await reader.read()).value)).toContain("event: sync");
  const upstreamSignal = fetcher.mock.calls.at(-1)![1]!.signal!;
  lifecycle.abort();
  await reader.cancel().catch(() => {});
  await vi.waitFor(() => expect(upstreamSignal.aborted).toBe(true));
  for (const leave of [false, true]) {
    const route = "/api/studio/api/v1/commands/presence-" + (leave ? "leave" : "heartbeat");
    const payload = {
      leaseId: documentId,
      ...(leave ? {} : { documentId, changesetId, environment: "staging" }),
    };
    expect((await post(route, { payload })).status).toBe(200);
    expect((await post(route, { payload: { ...payload, actorId: documentId } })).status).toBe(400);
    expect((await post(route, { payload: { ...payload, leaseId: "bad" } })).status).toBe(400);
    if (!leave)
      expect(
        (await post(route, { payload: { ...payload, environment: "production" } })).status,
      ).toBe(403);
  }
  fetcher.mockResolvedValueOnce(Response.json({}));
  expect((await fetch(origin + path)).status).toBe(502);
});
it("STU-100/101/103: контент и upload проходят только через разрешённые маршруты/окружение", async () => {
  const { origin, post } = await setup();
  const base =
    "/api/studio/api/v1/content-catalog?environment=staging&changesetId=" +
    changesetId +
    "&kind=entity";
  expect((await fetch(origin + base)).status).toBe(200);
  // STU-203: catalogue search crosses the authenticated proxy in the same CS.
  expect((await fetch(origin + base + "&q=Story")).status).toBe(200);
  for (const tail of ["&changesetId=" + changesetId, "&unknown=x", "&after=bad"])
    expect((await fetch(origin + base + tail)).status).toBe(400);
  expect(
    (await fetch(origin + base.replace("environment=staging", "environment=production"))).status,
  ).toBe(403);
  expect(
    (
      await fetch(
        origin +
          `/api/studio/api/v1/content/${documentId}?environment=staging&changesetId=${changesetId}`,
      )
    ).status,
  ).toBe(200);
  expect((await fetch(origin + `/api/studio/api/v1/asset-uploads/${documentId}`)).status).toBe(200);
  for (const command of ["create-asset-upload", "complete-asset-upload"]) {
    expect(
      (
        await post("/api/studio/api/v1/commands/" + command, {
          payload: command.startsWith("create")
            ? { filename: "test.png", mimeType: "image/png", size: 1, sha256: "a".repeat(64) }
            : { assetId: documentId },
        })
      ).status,
    ).toBe(200);
    expect(
      (await post("/api/studio/api/v1/commands/" + command, { payload: { other: true } })).status,
    ).toBe(400);
  }
  expect(
    (
      await post("/api/studio/api/v1/commands/apply-operations", {
        payload: {
          operations: [{ type: "entity.create", payload: { environment: "production" } }],
        },
      })
    ).status,
  ).toBe(403);
  for (const type of [
    "entity.create",
    "entity.setFields",
    "entity.delete",
    "entity.restore",
    "asset.create",
    "asset.updateMeta",
    "asset.replaceFile",
  ])
    expect(
      (
        await post("/api/studio/api/v1/commands/apply-operations", {
          payload: { operations: [{ type, payload: { environment: "staging" } }] },
        })
      ).status,
    ).toBe(200);
  expect(
    checkConfig({ ...config, assetUploadOrigin: "https://bucket.test" }).assetUploadOrigin,
  ).toBe("https://bucket.test");
  for (const assetUploadOrigin of [
    "http://bucket.test",
    "https://bucket.test/path",
    "https://user:pass@bucket.test",
    "file:///tmp",
  ]) {
    expect(() => checkConfig({ ...config, assetUploadOrigin })).toThrow();
  }
});
it("STU-070/071/072/073/074: review and workflow stay within the configured adapter contract", async () => {
  const { origin, post, fetcher } = await setup();
  const review = `/api/studio/api/v1/changesets/${changesetId}/review`;
  expect((await fetch(origin + review)).status).toBe(200);
  expect((await fetch(origin + review + "?environment=production")).status).toBe(400);
  for (const command of [
    "submit-changeset",
    "reopen-changeset",
    "approve-changeset",
    "request-changes",
    "publish",
  ]) {
    const route = "/api/studio/api/v1/commands/" + command;
    const payload = {
      changesetId,
      expectedSeq: 3,
      ...(command === "publish" ? { environment: "staging" } : {}),
      ...(command === "request-changes" ? { comment: "Revise" } : {}),
    };
    expect((await post(route, { payload })).status).toBe(200);
    expect(JSON.parse(String(fetcher.mock.calls.at(-1)![1]!.body)).payload).toEqual(payload);
    expect(fetcher.mock.calls.at(-1)![1]!.headers).toMatchObject({ "Idempotency-Key": "test-key" });
    for (const bad of [
      { ...payload, expectedSeq: -1 },
      { ...payload, expectedSeq: 2147483648 },
      { ...payload, changesetId: "bad" },
      { ...payload, targets: ["production"] },
    ])
      expect((await post(route, { payload: bad })).status).toBe(400);
  }
  for (const environment of [undefined, "production"])
    expect(
      (
        await post("/api/studio/api/v1/commands/publish", {
          payload: { changesetId, expectedSeq: 3, environment },
        })
      ).status,
    ).toBe(403);
  for (const comment of [undefined, " ", 1, "x".repeat(2001)])
    expect(
      (
        await post("/api/studio/api/v1/commands/request-changes", {
          payload: { changesetId, expectedSeq: 3, comment },
        })
      ).status,
    ).toBe(400);
  expect(
    (
      await post("/api/studio/api/v1/commands/set-approval-policy", {
        payload: { low: 0, medium: 0, high: 0 },
      })
    ).status,
  ).toBe(404);
});
it("STU-071: adapter forwards only narrow validation diagnostics from rejected commands", async () => {
  const { post, fetcher } = await setup();
  fetcher.mockResolvedValueOnce(
    Response.json(
      {
        code: "VALIDATION_FAILED",
        detail: config.credential,
        params: {
          documents: {
            [documentId]: [
              {
                code: "PROP_REQUIRED",
                severity: "error",
                pointer: "/nodes/n_heading",
                message: config.credential,
              },
            ],
          },
        },
      },
      { status: 422 },
    ),
  );
  const result = await post("/api/studio/api/v1/commands/publish", {
    payload: { changesetId, expectedSeq: 3, environment: "staging" },
  });
  expect(result.status).toBe(422);
  const text = await result.text();
  expect(text).toContain("PROP_REQUIRED");
  expect(text).not.toContain(config.credential);
});
it("STU-061/064: адаптер пропускает структурные группы без изменения payload/seq/key", async () => {
  const { post, fetcher } = await setup();
  for (const type of ["node.insert", "node.remove", "node.move"]) {
    const payload = {
      changesetId,
      expectedSeq: 5,
      group: true,
      operations: [
        {
          type,
          target: documentId,
          payload: {
            nodeId: "n_test",
            parentId: "n_root",
            slot: "footer",
            index: 0,
            subtree: { id: "n_new", type: "Text" },
          },
        },
      ],
    };
    expect((await post("/api/studio/api/v1/commands/apply-operations", { payload })).status).toBe(
      200,
    );
    expect(JSON.parse(String(fetcher.mock.calls.at(-1)![1]!.body)).payload).toEqual(payload);
    expect(fetcher.mock.calls.at(-1)![1]!.headers).toMatchObject({ "Idempotency-Key": "test-key" });
  }
});
it("STU-204/217: proxy сохраняет document.setContent, null и группу без переписывания", async () => {
  const { post, fetcher } = await setup();
  const payload = {
    changesetId,
    expectedSeq: 3,
    group: true,
    operations: [{ type: "document.setContent", target: documentId, payload: { content: null } }],
  };
  expect((await post("/api/studio/api/v1/commands/apply-operations", { payload })).status).toBe(
    200,
  );
  expect(JSON.parse(String(fetcher.mock.calls.at(-1)![1]!.body)).payload).toEqual(payload);
});
it("STU-220/235: source declaration passes the same public BFF without rewriting", async () => {
  const { post, fetcher } = await setup();
  for (const use of [{ source: "test.items", params: { limit: { lit: 4 } } }, null]) {
    const payload = {
      changesetId,
      expectedSeq: 3,
      group: true,
      operations: [
        { type: "document.setDataSource", target: documentId, payload: { key: "featured", use } },
      ],
    };
    expect((await post("/api/studio/api/v1/commands/apply-operations", { payload })).status).toBe(
      200,
    );
    expect(JSON.parse(String(fetcher.mock.calls.at(-1)![1]!.body)).payload).toEqual(payload);
  }
});
it("STU-050/052: адаптер разрешает только историю CS и замороженные undo/redo", async () => {
  const { origin, post, fetcher } = await setup();
  for (const query of ["", "?beforeSeq=20"])
    expect(
      (await fetch(origin + `/api/studio/api/v1/changesets/${changesetId}/history` + query)).status,
    ).toBe(200);
  for (const query of [
    "?beforeSeq=0",
    "?beforeSeq=bad",
    "?beforeSeq=1&beforeSeq=2",
    "?afterSeq=0",
    "?beforeSeq=2147483648",
  ])
    expect(
      (await fetch(origin + `/api/studio/api/v1/changesets/${changesetId}/history` + query)).status,
    ).toBe(400);
  for (const command of ["undo", "redo"]) {
    const route = "/api/studio/api/v1/commands/" + command;
    expect((await post(route, { payload: { changesetId, expectedSeq: 3 } })).status).toBe(200);
    const [, init] = fetcher.mock.calls.at(-1)!;
    expect(init!.headers).toMatchObject({ "Idempotency-Key": "test-key" });
    for (const payload of [
      { changesetId, expectedSeq: -1 },
      { changesetId, expectedSeq: 2147483648 },
      { changesetId: "bad", expectedSeq: 1 },
      { changesetId, expectedSeq: 1, operationId: documentId },
      { changesetId },
    ])
      expect((await post(route, { payload })).status).toBe(400);
  }
});
it("STU-040/041/042: адаптер ограничивает каталог/курсор и target создания", async () => {
  const { origin, fetcher, post } = await setup();
  for (const path of [
    `/pages?environment=staging&after=${changesetId}`,
    `/working-drafts?environment=staging&documentId=${documentId}`,
  ]) {
    expect((await fetch(origin + "/api/studio/api/v1" + path)).status).toBe(200);
    expect(String(fetcher.mock.calls.at(-1)![0])).toBe(config.baseUrl + "/api/v1" + path);
  }
  for (const path of [
    "/pages?environment=production",
    "/pages",
    "/working-drafts?environment=production&documentId=" + documentId,
  ])
    expect((await fetch(origin + "/api/studio/api/v1" + path)).status).toBe(403);
  for (const path of [
    "/pages?environment=staging&ownerId=x",
    "/pages?environment=staging&after=bad",
    "/pages?environment=staging&environment=staging",
    "/working-drafts?environment=staging",
    "/working-drafts?environment=staging&documentId=bad",
  ])
    expect((await fetch(origin + "/api/studio/api/v1" + path)).status).toBe(400);
  const route = "/api/studio/api/v1/commands/create-changeset";
  expect(
    (await post(route, { payload: { title: "New draft", targets: ["staging"] } })).status,
  ).toBe(200);
  for (const targets of [undefined, [], ["production"], ["staging", "production"]])
    expect((await post(route, { payload: { title: "New draft", targets } })).status).toBe(403);
  for (const payload of [
    { title: " ", targets: ["staging"] },
    { title: "x".repeat(201), targets: ["staging"] },
    { title: "new", targets: ["staging"], ownerId: documentId },
  ])
    expect((await post(route, { payload })).status).toBe(400);
});
afterEach(async () => {
  for (const cleanup of cleanups.splice(0)) await cleanup();
});
async function setup() {
  const directory = await mkdtemp(join(tmpdir(), "studio-test-"));
  await writeFile(join(directory, "index.html"), "<h1>Studio</h1>");
  await writeFile(join(directory, "client.js"), "export const studio=true");
  await writeFile(join(directory, "style.css"), "body{color:red}");
  await writeFile(join(directory, "inter.woff2"), Buffer.from([119, 79, 70, 50]));
  const fetcher = vi.fn<typeof fetch>(async () => Response.json({ result: { ok: true } }));
  const server = createServer((req, res) => {
    void handler(req, res);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address() as { port: number };
  const origin = `http://127.0.0.1:${address.port}`;
  const handler = createStudioHandler({ ...config, studioOrigin: origin }, directory, fetcher);
  cleanups.push(async () => {
    await new Promise<void>((resolve) => server.close(() => resolve()));
    await rm(directory, { recursive: true, force: true });
  });
  const post = (path: string, body: unknown, headers: Record<string, string> = {}) =>
    fetch(origin + path, {
      method: "POST",
      headers: {
        Origin: origin,
        "Content-Type": "application/json",
        "Idempotency-Key": "test-key",
        ...headers,
      },
      body: typeof body === "string" ? body : JSON.stringify(body),
    });
  return { origin, fetcher, post };
}
it("STU-132: поиск остаётся в выбранном окружении и не расширяет allowlist", async () => {
  const { origin, fetcher } = await setup();
  const route = origin + "/api/studio/api/v1/pages";
  expect(
    (await fetch(route + "?environment=staging&q=" + encodeURIComponent("Главная_%"))).status,
  ).toBe(200);
  const forwarded = new URL(String(fetcher.mock.calls[0]![0]));
  expect(forwarded.searchParams.get("q")).toBe("Главная_%");
  expect(forwarded.searchParams.get("environment")).toBe("staging");
  fetcher.mockClear();
  for (const query of ["q=" + "x".repeat(201), "q=%00", "q=x&q=y"])
    expect((await fetch(route + "?environment=staging&" + query)).status).toBe(400);
  expect((await fetch(route + "?environment=production&q=home")).status).toBe(403);
  expect(
    (await fetch(origin + "/api/studio/api/v1/manifest?environment=staging&q=home")).status,
  ).toBe(400);
  expect(fetcher).not.toHaveBeenCalled();
});
it("STU-130: локальный шрифт отдаётся как WOFF2", async () => {
  const { origin } = await setup();
  const response = await fetch(origin + "/inter.woff2");
  expect(response.status).toBe(200);
  expect(response.headers.get("content-type")).toBe("font/woff2");
  expect(new Uint8Array(await response.arrayBuffer())).toEqual(new Uint8Array([119, 79, 70, 50]));
});
it("STU-001: config и bundle не раскрывают credential; CSP ограничивает iframe", async () => {
  const { origin, fetcher } = await setup();
  let response = await fetch(origin + "/api/studio/config");
  expect(await response.json()).toEqual({
    project: "store",
    environment: "staging",
    appOrigin: config.appOrigin,
  });
  expect(response.headers.get("content-security-policy")).toContain(
    "frame-src " + config.appOrigin,
  );
  for (const path of ["/", "/editor", "/client.js", "/style.css"]) {
    response = await fetch(origin + path);
    expect(response.status).toBe(200);
    expect(await response.text()).not.toContain("cms_svc_");
  }
  response = await fetch(
    origin + `/api/studio/api/v1/documents/${documentId}?changesetId=${changesetId}`,
  );
  expect(response.status).toBe(200);
  expect(fetcher.mock.calls[0]![1]!.headers).toEqual({
    Authorization: "Bearer " + config.credential,
    "X-CMS-Project": "store",
  });
});
it("Host/Origin/Fetch Metadata и пути не дают управлять чужим проектом", async () => {
  const { origin, fetcher, post } = await setup();
  for (const headers of [
    { Origin: "https://evil.example" },
    { Host: "evil.example" },
    { "Sec-Fetch-Site": "cross-site" },
  ])
    expect(
      await new Promise<number>((resolve) => {
        httpRequest(origin + "/api/studio/config", { headers }, (response) => {
          response.resume();
          response.on("end", () => resolve(response.statusCode!));
        }).end();
      }),
    ).toBe(403);
  for (const path of [
    "/api/studio/api/v1/delivery-keys",
    "/api/studio/api/v1/commands/publish",
    "/api/studio/api/v1/documents/bad",
    "/%2e%2e%2fsecret.txt",
    "/missing.js",
    "/editor?x=y",
  ]) {
    expect((await fetch(origin + path)).status).toBeGreaterThanOrEqual(400);
  }
  expect(
    (
      await post(
        "/api/studio/api/v1/commands/apply-operations",
        { payload: { operations: [] } },
        { Origin: "" },
      )
    ).status,
  ).toBe(403);
  expect(fetcher).not.toHaveBeenCalled();
});
it("STU-001: allowlist Query/Command и фиксированное окружение", async () => {
  const { origin, post, fetcher } = await setup();
  const manifest = "/api/studio/api/v1/manifest";
  expect((await fetch(origin + manifest + "?environment=staging")).status).toBe(200);
  expect((await fetch(origin + `/api/studio/api/v1/changesets/${changesetId}`)).status).toBe(200);
  for (const suffix of [
    "",
    "?environment=staging&environment=staging",
    "?environment=staging&project=other",
  ])
    expect((await fetch(origin + manifest + suffix)).status).toBe(400);
  expect((await fetch(origin + manifest + "?environment=production")).status).toBe(403);
  const preview = "/api/studio/api/v1/commands/create-preview-token";
  expect(
    (await post(preview, { payload: { environment: "production", changesetId } })).status,
  ).toBe(403);
  expect(
    (await post(preview, { payload: { environment: "staging", changesetId }, reason: "preview" }))
      .status,
  ).toBe(200);
  const apply = "/api/studio/api/v1/commands/apply-operations";
  expect(
    (
      await post(apply, {
        payload: {
          changesetId,
          expectedSeq: 1,
          operations: [
            {
              type: "node.setDesign",
              target: documentId,
              payload: { nodeId: "n_root", set: { padding: { raw: 12 } } },
            },
          ],
        },
      })
    ).status,
  ).toBe(200);
  for (const operations of [
    [],
    Array.from({ length: 201 }, () => ({ type: "node.setProps" })),
    [{ type: "document.create" }],
    null,
  ])
    expect((await post(apply, { payload: { operations } })).status).toBe(400);
  for (const body of ["{", {}, { payload: null }])
    expect((await post(apply, body)).status).toBe(400);
  expect((await post(apply, { payload: {} }, { "Content-Type": "text/plain" })).status).toBe(400);
  expect((await post(apply, { payload: {} }, { "Idempotency-Key": "" })).status).toBe(400);
  expect((await post(apply, "x".repeat(1024 * 1024 + 1))).status).toBe(413);
  expect(JSON.stringify(fetcher.mock.calls)).not.toContain("publish");
});
it("безопасные upstream ошибки и запрет редиректов с Authorization", async () => {
  const { origin, fetcher } = await setup(),
    path = `/api/studio/api/v1/changesets/${changesetId}`;
  fetcher.mockResolvedValueOnce(
    Response.json(
      { code: "FORBIDDEN", detail: config.credential },
      { status: 403, headers: { "Set-Cookie": "secret", Location: "https://evil.example" } },
    ),
  );
  let response = await fetch(origin + path);
  expect(await response.json()).toEqual({ code: "FORBIDDEN" });
  expect(response.headers.get("set-cookie")).toBeNull();
  expect(response.headers.get("location")).toBeNull();
  expect(fetcher.mock.calls[0]![1]!.redirect).toBe("error");
  fetcher.mockResolvedValueOnce(new Response("bad", { status: 503 }));
  response = await fetch(origin + path);
  expect(await response.json()).toEqual({ code: "CMS_REQUEST_FAILED" });
  fetcher.mockResolvedValueOnce(Response.json({ code: "secret detail" }, { status: 400 }));
  response = await fetch(origin + path);
  expect(await response.json()).toEqual({ code: "CMS_REQUEST_FAILED" });
  fetcher.mockRejectedValueOnce(Error("network " + config.credential));
  response = await fetch(origin + path);
  expect(await response.json()).toEqual({ code: "STUDIO_REQUEST_FAILED" });
});
it("отклоняет публичный сервер, userinfo, чужие пути и некорректную конфигурацию", () => {
  expect(checkConfig(config)).toEqual(config);
  for (const patch of [
    { studioOrigin: "http://0.0.0.0:18085" },
    { baseUrl: "https://public.example" },
    { appOrigin: "http://public.example" },
    { baseUrl: "http://user:pass@localhost:18080" },
    { appOrigin: "https://app.example/path" },
    { studioOrigin: config.studioOrigin + "/" },
    { appOrigin: config.appOrigin + "/" },
    { project: "bad/path" },
    { environment: "" },
    { credential: "secret" },
  ])
    expect(() => checkConfig({ ...config, ...patch })).toThrow();
  expect(checkConfig({ ...config, appOrigin: "https://app.example" }).appOrigin).toBe(
    "https://app.example",
  );
});
it("STU-156: diff proxy bounds query context and guarded workflow payloads", async () => {
  const { origin, post, fetcher } = await setup();
  const hash = "sha256:" + "a".repeat(64);
  const route = `/api/studio/api/v1/changesets/${changesetId}/diff`;
  expect((await fetch(origin + route + "?environment=staging")).status).toBe(200);
  const detail = route + `/${documentId}?environment=staging&baseline=base&snapshot=${hash}`;
  expect((await fetch(origin + detail + "&limit=100&after=opaque")).status).toBe(200);
  for (const q of [
    "&limit=101",
    "&limit=0",
    "&baseline=published",
    "&other=1",
    "&after=",
    "&snapshot=bad",
  ])
    expect((await fetch(origin + detail + q)).status).toBe(400);
  expect((await fetch(origin + route + "?environment=production")).status).toBe(403);
  const command = "/api/studio/api/v1/commands/approve-changeset";
  expect(
    (await post(command, { payload: { changesetId, expectedSeq: 1, expectedDiffSnapshot: hash } }))
      .status,
  ).toBe(200);
  expect(
    (await post(command, { payload: { changesetId, expectedSeq: 1, expectedDiffSnapshot: "bad" } }))
      .status,
  ).toBe(400);
  expect(fetcher.mock.calls.some(([url]) => String(url).includes("/diff/"))).toBe(true);
});
