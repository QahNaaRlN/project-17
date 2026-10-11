import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createCmsClient, type ClientOptions, type ReadOptions } from "@cms/core";
import { loadStore } from "./load.js";
import { storeHandler, type Assets } from "./http.js";

const production = process.env.NODE_ENV === "production";
const root = production
  ? resolve(dirname(fileURLToPath(import.meta.url)), "../..")
  : resolve(dirname(fileURLToPath(import.meta.url)), "..");
const configFile = process.env.CMS_STORE_CONFIG_FILE;
if (!configFile)
  throw new Error(
    "CMS_STORE_CONFIG_FILE is required; run the seed or provide a published configuration",
  );
const config = JSON.parse(await readFile(configFile, "utf8")) as ClientOptions &
  ReadOptions & { path: string };
// Manual refresh must reach the CMS, including for published content.
const cms = createCmsClient({ ...config, cache: false });
if (typeof config.path !== "string" || !config.path.startsWith("/"))
  throw new Error("Invalid CMS page path");
const options: ReadOptions = config.preview ? { preview: config.preview } : {};
const vite = production
  ? undefined
  : await (
      await import("vite")
    ).createServer({
      root,
      server: { middlewareMode: true, host: "127.0.0.1" },
      appType: "custom",
    });
async function assets(): Promise<Assets> {
  if (!production) return { scripts: ["/src/client.tsx"], styles: [] };
  const manifest = JSON.parse(
    await readFile(resolve(root, "dist/client/.vite/manifest.json"), "utf8"),
  ) as Record<string, { file: string; css?: string[] }>;
  const entry = manifest["src/client.tsx"];
  if (!entry) throw new Error("STORE_BUILD_MISSING");
  return { scripts: [`/${entry.file}`], styles: (entry.css ?? []).map((file) => `/${file}`) };
}
const handler = storeHandler(
  () => loadStore(cms, config.path, options),
  assets,
  async (html) => (vite ? vite.transformIndexHtml("/", html) : html),
);
const server = createServer((req, res) => {
  res.setHeader(
    "Content-Security-Policy",
    `default-src 'self'; script-src 'self'${production ? "" : " 'unsafe-inline'"}; style-src 'self' 'unsafe-inline'; img-src 'self' http: https:; connect-src 'self'${production ? "" : " ws://127.0.0.1:*"}; object-src 'none'; base-uri 'none'; frame-ancestors 'none'`,
  );
  if (production && req.url === "/favicon.svg") {
    void readFile(resolve(root, "dist/client/favicon.svg"))
      .then((data) => {
        res.writeHead(200, {
          "Content-Type": "image/svg+xml",
          "X-Content-Type-Options": "nosniff",
        });
        res.end(req.method === "HEAD" ? undefined : data);
      })
      .catch(() => {
        res.writeHead(404);
        res.end();
      });
    return;
  }
  if (production && req.url?.startsWith("/assets/")) {
    const file = req.url.split("?")[0]!.slice("/assets/".length);
    if (!/^[a-zA-Z0-9._-]+$/.test(file) || !/\.(js|css)$/.test(file)) {
      res.writeHead(404);
      res.end();
      return;
    }
    void readFile(resolve(root, "dist/client/assets", file))
      .then((data) => {
        res.writeHead(200, {
          "Content-Type": file.endsWith(".css") ? "text/css" : "text/javascript",
          "Cache-Control": "public, max-age=31536000, immutable",
          "X-Content-Type-Options": "nosniff",
        });
        res.end(data);
      })
      .catch(() => {
        res.writeHead(404);
        res.end();
      });
    return;
  }
  if (vite && req.url !== "/" && !req.url?.startsWith("/api/store"))
    vite.middlewares(req, res, () => {
      void handler(req, res);
    });
  else void handler(req, res);
});
const port = Number(process.env.PORT ?? 18082);
if (!Number.isInteger(port) || port < 1024 || port > 65535) throw new Error("Invalid PORT");
server.listen(port, "127.0.0.1", () => {
  console.log(`Project 17 storefront: http://127.0.0.1:${port}`);
});
for (const signal of ["SIGINT", "SIGTERM"] as const)
  process.on(signal, () => {
    server.close();
    void vite?.close();
  });
