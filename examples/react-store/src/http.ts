import { createElement } from "react";
import { renderToString } from "react-dom/server";
import type { IncomingMessage, ServerResponse } from "node:http";
import { StoreApp } from "./app.js";
import { serializeModel, type StoreModel } from "./model.js";

export interface Assets {
  scripts: string[];
  styles: string[];
}
export function documentHtml(model: StoreModel, assets: Assets): string {
  // Asset paths come from Vite build output, never CMS content.
  const tags = [
    ...assets.styles.map((url) => `<link rel="stylesheet" href="${url}">`),
    ...assets.scripts.map((url) => `<script type="module" src="${url}"></script>`),
  ].join("");
  return `<!doctype html><html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="/favicon.svg" type="image/svg+xml"><title>Тихая форма · Project 17</title>${tags}</head><body><div id="root">${renderToString(createElement(StoreApp, { initialModel: model }))}</div><script id="store-model" type="application/json">${serializeModel(model)}</script></body></html>`;
}
export function storeHandler(
  load: () => Promise<StoreModel>,
  assets: () => Promise<Assets>,
  transform: (html: string) => Promise<string> = async (html) => html,
  deployedHash?: string,
) {
  return async (req: IncomingMessage, res: ServerResponse) => {
    if (req.method !== "GET" && req.method !== "HEAD") {
      res.writeHead(405, { Allow: "GET, HEAD" });
      res.end();
      return;
    }
    const path = req.url?.split("?")[0];
    if (path === "/.well-known/cms-manifest-hash" && deployedHash) {
      res.writeHead(200, {
        "Content-Type": "text/plain; charset=utf-8",
        "Cache-Control": "no-store",
        "X-Content-Type-Options": "nosniff",
      });
      res.end(req.method === "HEAD" ? undefined : deployedHash + "\n");
      return;
    }
    if (path !== "/" && path !== "/api/store") {
      res.writeHead(404);
      res.end();
      return;
    }
    res.setHeader("Cache-Control", "no-store");
    res.setHeader("X-Content-Type-Options", "nosniff");
    res.setHeader("Referrer-Policy", "no-referrer");
    try {
      const model = await load();
      res.statusCode = model.status === "error" ? 503 : model.status === "missing" ? 404 : 200;
      res.setHeader(
        "Content-Type",
        path === "/api/store" ? "application/json; charset=utf-8" : "text/html; charset=utf-8",
      );
      const body =
        path === "/api/store"
          ? serializeModel(model)
          : await transform(documentHtml(model, await assets()));
      res.end(req.method === "HEAD" ? undefined : body);
    } catch {
      res.statusCode = 503;
      res.setHeader("Content-Type", "application/json; charset=utf-8");
      res.end(req.method === "HEAD" ? undefined : '{"error":"STORE_UNAVAILABLE"}');
    }
  };
}
