import { createServer, type Server } from "node:http";
import { afterAll, beforeAll, expect, it, vi } from "vitest";
import { chromium, type Browser } from "playwright";
import {
  axeStatus,
  bounded,
  createClient,
  renderPassed,
  runCheck,
  step,
  type LeasedCheck,
} from "../src/runner.js";

const doc = (body: string, diagnostics: unknown = []) =>
  `<!doctype html><html lang="en"><head><title>Store</title></head><body><main><h1>Store</h1>${body}` +
  (diagnostics === null
    ? ""
    : `<script type="application/json" id="cms-render-diagnostics">${JSON.stringify(diagnostics)}</script>`) +
  `</main></body></html>`;
const routes: Record<string, (res: import("node:http").ServerResponse) => void> = {
  "/ok": (res) => res.end(doc(`<button>Buy</button>`)),
  "/warning": (res) =>
    res.end(
      doc(`<p>ok</p>`, [{ code: "CMS_IMAGE_UNAVAILABLE", nodeId: "n", severity: "warning" }]),
    ),
  "/broken": (res) =>
    res.end(doc(`<p>x</p>`, [{ code: "CMS_COMPONENT_MISSING", nodeId: "n", severity: "error" }])),
  "/nodiag": (res) => res.end(doc(`<p>x</p>`, null)),
  "/object": (res) => res.end(doc(`<p>x</p>`, { code: "X" })),
  "/throws": (res) => res.end(doc(`<script>throw new Error("boom")</script>`)),
  "/a11y": (res) => res.end(doc(`<button></button>`)),
  "/away": (res) => {
    res.writeHead(302, { Location: "http://127.0.0.2:9/elsewhere" });
    res.end();
  },
};
let server: Server, origin: string, browser: Browser;
beforeAll(async () => {
  server = createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://x");
    if (url.pathname === "/api/cms/preview") {
      res.writeHead(302, { Location: url.searchParams.get("path") ?? "/" });
      return res.end();
    }
    res.setHeader("Content-Type", "text/html; charset=utf-8");
    const route = routes[url.pathname];
    if (!route) {
      res.statusCode = 500;
      return res.end(doc("<p>error</p>"));
    }
    route(res);
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  origin = `http://127.0.0.1:${(server.address() as { port: number }).port}`;
  browser = await chromium.launch();
});
afterAll(async () => {
  await browser?.close();
  await new Promise((r) => server.close(r));
});
function check(stage: "preview" | "axe", ...paths: string[]): LeasedCheck {
  return {
    checkId: "c",
    leaseId: "l",
    stage,
    changesetId: "cs",
    environment: "staging",
    appUrl: origin,
    previewToken: "t",
    pages: paths.map((path, i) => ({ objectId: "o" + i, path })),
  };
}
it("STU-490: stage 8 passes rendered pages and fails errors, 500, exceptions and missing diagnostics", async () => {
  expect((await runCheck(browser, check("preview", "/ok", "/warning"))).status).toBe("passed");
  for (const path of ["/broken", "/500", "/throws", "/nodiag", "/away"]) {
    const result = await runCheck(browser, check("preview", path), 5000);
    expect(result.status, path).toBe("failed");
    expect(result.details.pages).toHaveLength(1);
  }
  const page = (await runCheck(browser, check("preview", "/broken"))).details.pages[0]!;
  expect(page).toMatchObject({
    status: 200,
    path: "/broken",
    diagnostics: [{ code: "CMS_COMPONENT_MISSING" }],
  });
});
it("STU-491: stage 9 fails serious violations and skips pages that do not render", async () => {
  expect((await runCheck(browser, check("axe", "/ok"))).status).toBe("passed");
  const failed = await runCheck(browser, check("axe", "/a11y", "/500"));
  expect(failed.status).toBe("failed");
  expect(failed.details.pages[0]).toMatchObject({
    path: "/a11y",
    violations: [expect.objectContaining({ id: "button-name", nodes: 1 })],
  });
  expect(failed.details.pages[1]).toMatchObject({ path: "/500", reason: "PREVIEW_FAILED" });
});
it("STU-498: details are bounded to 256 KiB and marked truncated", () => {
  const pages = Array.from({ length: 400 }, (_, i) => ({
    path: "/p" + i,
    errors: ["x".repeat(1000)],
  }));
  const out = bounded(pages);
  expect(out.truncated).toBe(true);
  expect(JSON.stringify(out).length).toBeLessThanOrEqual(256 * 1024);
  expect(bounded([{ path: "/a" }])).toEqual({ pages: [{ path: "/a" }], truncated: false });
  expect(renderPassed({ objectId: "o", path: "/", status: 200, diagnostics: [], errors: [] })).toBe(
    true,
  );
});
it("STU-485/486/492: the client claims and completes through the Command API without leaking tokens", async () => {
  const calls: { url: string; init: RequestInit }[] = [];
  const queue: (LeasedCheck | null)[] = [check("preview", "/ok"), null];
  const fetcher = vi.fn<typeof fetch>(async (url, init) => {
    calls.push({ url: String(url), init: init! });
    if (String(url).endsWith("/claim-check"))
      return Response.json({ result: { check: queue.shift() ?? null } });
    return Response.json({ result: { changesetState: "in_review" } });
  });
  const log = vi.fn();
  const client = createClient({
    apiUrl: "http://cms.test",
    token: "cms_svc_secret",
    project: "store",
    fetch: fetcher,
  });
  expect(await step(browser, client, { log })).toBe(true);
  expect(await step(browser, client, { log })).toBe(false);
  const complete = calls.find((c) => c.url.endsWith("/complete-check"))!;
  expect(JSON.parse(String(complete.init.body))).toMatchObject({
    payload: { checkId: "c", leaseId: "l", status: "passed", details: { truncated: false } },
  });
  expect(complete.init.headers).toMatchObject({
    "X-CMS-Project": "store",
    Authorization: "Bearer cms_svc_secret",
  });
  expect(log.mock.calls.flat().join(" ")).not.toContain("cms_svc_secret");
  const failing = createClient({
    apiUrl: "http://cms.test",
    token: "t",
    project: "store",
    fetch: async () => Response.json({ code: "FORBIDDEN" }, { status: 403 }),
  });
  await expect(failing.claim()).rejects.toThrow("claim-check: 403 FORBIDDEN");
  const broken = createClient({
    apiUrl: "http://cms.test",
    token: "t",
    project: "store",
    fetch: async () => new Response("not json", { status: 502 }),
  });
  await expect(broken.claim()).rejects.toThrow("claim-check: 502 UNKNOWN");
});
it("STU-491: axe status ranks failures above warnings", () => {
  const v = (impact: string | null) => ({ id: "r", impact, nodes: 1, targets: [] });
  expect(axeStatus("passed", [])).toBe("passed");
  expect(axeStatus("passed", [v("minor")])).toBe("warning");
  expect(axeStatus("warning", [v(null)])).toBe("warning");
  expect(axeStatus("warning", [v("serious")])).toBe("failed");
  expect(axeStatus("failed", [])).toBe("failed");
  expect(axeStatus("passed", [v("critical")])).toBe("failed");
});
it("STU-489/490: non-list diagnostics are ignored, not trusted", async () => {
  const result = await runCheck(browser, check("preview", "/object"));
  expect(result.status).toBe("passed");
  expect(result.details.pages[0]).toMatchObject({ diagnostics: [] });
});
