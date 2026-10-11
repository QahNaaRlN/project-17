import { createServer } from "node:http";
import { mkdtemp, readFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn } from "node:child_process";
import { catalog } from "../scripts/catalog.js";
import { storeDocument } from "../src/composition.js";

// Deterministic CMS boundary for CI; the real SDK, HTTP server, SSR and browser run unchanged.
const id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11";
const key = "server-only-e2e-delivery-key";
let mode = "ready";
let title = catalog.title;
let release: (() => void)[] = [];
const fixture = createServer(async (req, res) => {
  const path = new URL(req.url ?? "/", "http://127.0.0.1:18084").pathname;
  if (path === "/control" && req.method === "POST") {
    let body = "";
    for await (const chunk of req) body += String(chunk);
    const next = JSON.parse(body) as { mode: string; title?: string };
    mode = next.mode;
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
  if (req.headers.authorization !== `Bearer ${key}`) {
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
  const document = storeDocument(value);
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
  env: { ...process.env, PORT: "18083", NODE_ENV: "production", CMS_STORE_CONFIG_FILE: config },
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
