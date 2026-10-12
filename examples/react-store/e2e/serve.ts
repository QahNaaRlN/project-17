import { createServer } from "node:http";
import { mkdtemp, readFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn } from "node:child_process";
import { catalog } from "../scripts/catalog.js";
import { storeDocument } from "../src/composition.js";
import { studioFixture } from "./studio-fixture.js";

// Deterministic CMS boundary for CI; the real SDK, HTTP server, SSR and browser run unchanged.
const id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11";
const key = "server-only-e2e-delivery-key";
const previewToken =
  "header." +
  Buffer.from(JSON.stringify({ exp: Math.floor(Date.now() / 1000) + 900, cs: id })).toString(
    "base64url",
  ) +
  ".fixture";
let mode = "ready";
let title = catalog.title;
let release: (() => void)[] = [];
const editor = await studioFixture(id, previewToken);
const fixture = createServer(async (req, res) => {
  if (await editor.handle(req, res)) return;
  const path = new URL(req.url ?? "/", "http://127.0.0.1:18084").pathname;
  if (path === "/studio" || path === "/rogue") {
    res.setHeader("Content-Type", "text/html; charset=utf-8");
    res.end(
      path === "/rogue"
        ? "<!doctype html><p>other window</p>"
        : `<!doctype html><html><body><script>window.previewMessages=[]; addEventListener('message',event=>{if(event.origin==='http://127.0.0.1:18083' && event.source===document.getElementById('preview')?.contentWindow) previewMessages.push(event.data);});</script><iframe id="preview" title="CMS preview" style="width:1024px;height:900px" src="http://127.0.0.1:18083/api/cms/preview?token=${previewToken}&path=/"></iframe><iframe id="rogue" title="Other window" src="/rogue"></iframe></body></html>`,
    );
    return;
  }
  if (path === "/control" && req.method === "POST") {
    let body = "";
    for await (const chunk of req) body += String(chunk);
    const next = JSON.parse(body) as { mode: string; title?: string };
    mode = next.mode;
    editor.reset(false);
    title = next.title ?? catalog.title;
    if (mode !== "hold") {
      for (const resolve of release) resolve();
      release = [];
    }
    res.end("ok");
    return;
  }
  if (path.startsWith("/assets/v1/")) {
    res.writeHead(mode === "badimage" ? 404 : 200, { "Content-Type": "image/png" });
    res.end(
      mode === "badimage"
        ? undefined
        : await readFile(new URL("../assets/vase.png", import.meta.url)),
    );
    return;
  }
  res.setHeader("Content-Type", "application/json");
  res.setHeader("Cache-Control", "no-store");
  if (
    req.headers.authorization !== `Bearer ${key}` &&
    req.headers.authorization !== `Preview ${previewToken}`
  ) {
    res.writeHead(401);
    res.end(JSON.stringify({ code: "UNAUTHORIZED" }));
    return;
  }
  if (mode === "hold") await new Promise<void>((resolve) => release.push(resolve));
  if (mode === "error" || mode === "missing") {
    res.writeHead(mode === "error" ? 503 : 404);
    res.end(JSON.stringify({ code: mode === "error" ? "INTERNAL_ERROR" : "NOT_FOUND" }));
    return;
  }
  if (path.endsWith("/routes")) {
    res.end(JSON.stringify({ items: [] }));
    return;
  }
  if (path.endsWith("/image")) {
    res.end(
      JSON.stringify({
        assetId: id,
        versionId: id,
        width: 800,
        height: 600,
        fit: "contain",
        format: "png",
        focalPoint: { x: 0.5, y: 0.5 },
        expiresAt: new Date(Date.now() + 300_000).toISOString(),
        variants: [320, 640].map((width) => ({
          width,
          url: `http://127.0.0.1:18084/assets/v1/staging/${id}/${"a".repeat(64)}?sig=fixture&w=${width}`,
        })),
      }),
    );
    return;
  }
  const value = structuredClone(catalog);
  value.title = title;
  value.heroAsset = { assetId: id };
  value.products.forEach((p) => {
    p.asset = { assetId: id };
  });
  delete value.products[0]!.priceRub;
  if (mode === "empty") value.products = [];
  const document = editor.document ?? storeDocument(value);
  if (mode === "sourceerror") document.dataSources!.featured!.source = "store.unavailable";
  if (mode === "actiontitle")
    document.nodes.n_product!.on = {
      favorite: {
        action: "store.favorite",
        args: { id: "$item.id", title: { lit: "Имя из документа CMS" } },
      },
    };
  if (mode === "reorder") document.nodes.n_store!.children = ["n_about", "n_collection", "n_hero"];
  if (mode === "hideabout")
    document.nodes.n_about!.when = { op: "eq", args: [{ lit: 1 }, { lit: 2 }] };
  if (mode === "adaptive")
    document.nodes.n_about!.design = { padding: { base: { raw: 12 }, sm: { raw: 24 } } };
  res.end(
    JSON.stringify({
      page: { objectId: id, versionId: id, path: "/store", params: {} },
      document,
      components: {},
      data: {},
      dataSources: {},
    }),
  );
});
await new Promise<void>((resolve) => fixture.listen(18084, "127.0.0.1", resolve));
const directory = await mkdtemp(join(tmpdir(), "cms-store-e2e-"));
const config = join(directory, "config.json");
await writeFile(
  config,
  JSON.stringify({
    baseUrl: "http://127.0.0.1:18084",
    project: "local-demo",
    environment: "staging",
    deliveryKey: key,
    path: "/store",
  }),
  { mode: 0o600 },
);
const child = spawn(process.execPath, ["dist/server/server.js"], {
  env: {
    ...process.env,
    PORT: "18083",
    NODE_ENV: "production",
    CMS_STORE_CONFIG_FILE: config,
    CMS_STUDIO_ORIGIN: "http://127.0.0.1:18084",
    CMS_APP_ORIGIN: "http://127.0.0.1:18083",
  },
  stdio: "inherit",
});
child.on("exit", (code) => {
  fixture.close();
  void rm(directory, { recursive: true, force: true });
  process.exitCode = code ?? 1;
});
for (const signal of ["SIGINT", "SIGTERM"] as const)
  process.on(signal, () => {
    child.kill();
    fixture.close();
    void rm(directory, { recursive: true, force: true });
  });
