import { createServer, type Server } from "node:http";
import { PNG } from "pngjs";
import { afterAll, beforeAll, expect, it, vi } from "vitest";
import { chromium, type Browser } from "playwright";
import { createClient, runCheck, type CheckIO, type LeasedCheck } from "../src/runner.js";
import {
  artifactBase,
  compare,
  defaultThresholds,
  lighthouseStatus,
  widths,
} from "../src/stages.js";

const doc = (body: string) =>
  `<!doctype html><html lang="en"><head><title>Store</title></head><body><main>${body}` +
  `<script type="application/json" id="cms-render-diagnostics">[]</script></main></body></html>`;
let server: Server, origin: string, browser: Browser;
beforeAll(async () => {
  // The preview entry sets a cookie; pages render the draft for it (as the SDK does).
  server = createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://x");
    if (url.pathname === "/api/cms/preview") {
      res.writeHead(302, {
        Location: url.searchParams.get("path") ?? "/",
        "Set-Cookie": "cms_preview=1; Path=/",
      });
      return res.end();
    }
    const draft = (req.headers.cookie ?? "").includes("cms_preview=1");
    res.setHeader("Content-Type", "text/html; charset=utf-8");
    switch (url.pathname) {
      case "/changed":
        return res.end(doc(draft ? `<h1 style="color:red">New title</h1>` : `<h1>Old</h1>`));
      case "/same":
        return res.end(doc(`<h1>Same</h1>`));
      case "/new":
        if (!draft) res.statusCode = 404;
        return res.end(doc(`<h1>New page</h1>`));
      case "/a11y":
        return res.end(doc(`<h1>Store</h1><button></button>`));
      default:
        res.statusCode = draft ? 500 : 200;
        return res.end(doc(`<p>error</p>`));
    }
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  origin = `http://127.0.0.1:${(server.address() as { port: number }).port}`;
  browser = await chromium.launch();
});
afterAll(async () => {
  await browser?.close();
  await new Promise((r) => server.close(r));
});
function check(stage: "visual" | "lighthouse", ...paths: string[]): LeasedCheck {
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
function memoryIO() {
  const files = new Map<string, { type: string; body: Uint8Array }>();
  const io: CheckIO = {
    upload: vi.fn(async (_c, name, type, body) => void files.set(name, { type, body })),
    extend: vi.fn(async () => {}),
  };
  return { io, files };
}

it("STU-576…578: stage 10 compares the draft with the published page at three widths", async () => {
  const { io, files } = memoryIO();
  const result = await runCheck(
    browser,
    check("visual", "/changed", "/same", "/new", "/broken"),
    10_000,
    io,
  );
  expect(result.status).toBe("warning");
  const [changed, same, fresh, broken] = result.details.pages as {
    widths: Record<string, unknown>[];
  }[];
  expect(changed!.widths.map((w) => w.width)).toEqual([...widths]);
  for (const w of changed!.widths) {
    expect(w).toMatchObject({ baseline: "published" });
    expect(w.changedRatio as number).toBeGreaterThan(0);
    expect(Object.keys(w.artifacts as object).sort()).toEqual(["after", "before", "diff"]);
  }
  for (const w of same!.widths)
    expect(w).toMatchObject({
      baseline: "published",
      changedRatio: 0,
      artifacts: { after: expect.any(String) },
    });
  expect(same!.widths[0]!.artifacts).not.toHaveProperty("diff");
  for (const w of fresh!.widths)
    expect(w).toMatchObject({ baseline: "none", artifacts: { after: expect.any(String) } });
  for (const w of broken!.widths) expect(w.error).toContain("draft HTTP 500");
  expect(files.get("00-changed-375-diff.png")?.type).toBe("image/png");
  expect(PNG.sync.read(Buffer.from(files.get("00-changed-1440-after.png")!.body)).width).toBe(1440);
  expect(io.extend).toHaveBeenCalledTimes(4);
});

it("STU-577: a height difference is a changed strip; identical images do not differ", () => {
  const png = (width: number, height: number, shade: number) => {
    const img = new PNG({ width, height });
    img.data.fill(shade);
    return PNG.sync.write(img);
  };
  const same = compare(png(10, 10, 255), png(10, 10, 255));
  expect(same.changedRatio).toBe(0);
  const taller = compare(png(10, 10, 255), png(10, 20, 255));
  expect(taller.changedRatio).toBe(0.5);
  expect(taller.heights).toEqual({ before: 10, after: 20 });
  const diff = PNG.sync.read(taller.diff);
  expect([diff.width, diff.height]).toEqual([10, 20]);
  expect([...diff.data.subarray((10 * 15 + 5) * 4, (10 * 15 + 5) * 4 + 4)]).toEqual([
    255, 0, 0, 255,
  ]);
  expect(artifactBase(3, "/Каталог/Shoes 2")).toBe("03-shoes-2");
  expect(artifactBase(0, "/")).toBe("00-root");
});

it("STU-581: scores below a blocking threshold fail, below another warn", () => {
  const t = defaultThresholds;
  expect(lighthouseStatus("passed", { performance: 0.9, accessibility: 0.95 }, t)).toBe("passed");
  expect(lighthouseStatus("passed", { performance: 0.3, accessibility: 0.95 }, t)).toBe("warning");
  expect(lighthouseStatus("warning", { performance: 0.9, accessibility: 0.5 }, t)).toBe("failed");
  expect(lighthouseStatus("failed", { performance: 0.3, accessibility: null }, t)).toBe("failed");
});

it("STU-579, 580: stage 11 scores rendered pages, keeps the report and skips broken ones", async () => {
  const { io, files } = memoryIO();
  const leased = {
    ...check("lighthouse", "/a11y", "/broken"),
    thresholds: {
      performance: { min: 0, blocking: false },
      accessibility: { min: 1, blocking: true },
    },
  };
  const result = await runCheck(browser, leased, 30_000, io);
  expect(result.status).toBe("failed");
  const [scored, broken] = result.details.pages as Record<string, unknown>[];
  expect(scored).toMatchObject({ path: "/a11y", report: "00-a11y-lighthouse.html" });
  const scores = scored!.scores as Record<string, number>;
  expect(scores.accessibility).toBeLessThan(1);
  expect(scores.performance).toBeGreaterThan(0);
  expect(broken).toMatchObject({ path: "/broken", reason: "PREVIEW_FAILED" });
  expect(result.details.thresholds).toEqual(leased.thresholds);
  expect(new TextDecoder().decode(files.get("00-a11y-lighthouse.html")!.body)).toContain("<html");
  await expect(runCheck(browser, leased, 30_000)).rejects.toThrow("artifact uploads");
}, 180_000);

it("STU-573, 585: the client uploads under the lease and extends it", async () => {
  const calls: { url: string; init: RequestInit }[] = [];
  const client = createClient({
    apiUrl: "http://cms.test",
    token: "cms_svc_secret",
    project: "store",
    fetch: async (url, init) => {
      calls.push({ url: String(url), init: init! });
      return String(url).includes("/artifacts/")
        ? new Response(null, { status: 204 })
        : Response.json({ result: { leaseExpiresAt: "x" } });
    },
  });
  const leased = check("visual", "/x");
  await client.upload(leased, "00-x-375-after.png", "image/png", new Uint8Array([1, 2]));
  await client.extend(leased);
  expect(calls[0]).toMatchObject({
    url: "http://cms.test/api/v1/checks/c/artifacts/00-x-375-after.png",
    init: {
      method: "PUT",
      headers: {
        "X-CMS-Check-Lease": "l",
        "Content-Type": "image/png",
        Authorization: "Bearer cms_svc_secret",
      },
    },
  });
  expect(JSON.parse(String(calls[1]!.init.body))).toEqual({
    payload: { checkId: "c", leaseId: "l" },
  });
});
