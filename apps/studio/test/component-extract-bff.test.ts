import { afterEach, expect, it, vi } from "vitest";
import { createServer } from "node:http";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createStudioHandler } from "../src/handler.js";

const changesetId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c22";
const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0)) await cleanup();
});

it("STU-401: адаптер пропускает только план извлечения с проверенными параметрами", async () => {
  const directory = await mkdtemp(join(tmpdir(), "studio-extract-"));
  await writeFile(join(directory, "index.html"), "<h1>Studio</h1>");
  const fetcher = vi.fn<typeof fetch>(async () => Response.json({ nodeId: "n_block" }));
  const server = createServer((req, res) => void handler(req, res));
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${(server.address() as { port: number }).port}`;
  const handler = createStudioHandler(
    {
      project: "store",
      environment: "staging",
      appOrigin: "http://127.0.0.1:18083",
      baseUrl: "http://127.0.0.1:18080",
      credential: "cms_svc_test",
      studioOrigin: origin,
    } as Parameters<typeof createStudioHandler>[0],
    directory,
    fetcher,
  );
  cleanups.push(async () => {
    await new Promise<void>((resolve) => server.close(() => resolve()));
    await rm(directory, { recursive: true, force: true });
  });
  const route = `${origin}/api/studio/api/v1/documents/${changesetId}/component-extract-plan`;
  const ok = `changesetId=${changesetId}&nodeId=n_block&environment=staging`;
  expect((await fetch(route + "?" + ok)).status).toBe(200);
  const forwarded = new URL(String(fetcher.mock.calls.at(-1)![0]));
  expect(forwarded.pathname).toContain("/component-extract-plan");
  expect(forwarded.searchParams.get("nodeId")).toBe("n_block");
  fetcher.mockClear();
  for (const query of [
    `changesetId=bad&nodeId=n_block&environment=staging`,
    `changesetId=${changesetId}&nodeId=a.b&environment=staging`,
    `${ok}&nodeId=n_other`,
    `${ok}&extra=1`,
  ])
    expect((await fetch(route + "?" + query)).status).toBe(400);
  expect(
    (await fetch(route + `?changesetId=${changesetId}&nodeId=n_block&environment=production`))
      .status,
  ).toBe(403);
  expect(fetcher).not.toHaveBeenCalled();
  // STU-402: the command adapter forwards component.extract, never the internal inverse.
  const post = (type: string) =>
    fetch(origin + "/api/studio/api/v1/commands/apply-operations", {
      method: "POST",
      headers: {
        Origin: origin,
        "Content-Type": "application/json",
        "Idempotency-Key": "extract-key",
      },
      body: JSON.stringify({
        payload: {
          changesetId,
          expectedSeq: 1,
          operations: [{ type, target: changesetId, payload: { nodeId: "n_block", name: "X" } }],
        },
        reason: "Studio: save as component «X»",
      }),
    });
  expect((await post("component.extract")).status).toBe(200);
  expect(fetcher).toHaveBeenCalledTimes(1);
  expect((await post("component.revertExtract")).status).toBe(400);
});
