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
