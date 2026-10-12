import type { IncomingMessage, ServerResponse } from "node:http";
import { readFile } from "node:fs/promises";
import { resolve, sep } from "node:path";
import type { Config } from "./types.js";
export interface ServerConfig extends Config {
  baseUrl: string;
  studioOrigin: string;
  credential: string;
}
export function checkConfig(config: ServerConfig): ServerConfig {
  const local = (url: URL) =>
    url.protocol === "http:" && ["127.0.0.1", "localhost", "[::1]"].includes(url.hostname);
  const studio = new URL(config.studioOrigin),
    base = new URL(config.baseUrl),
    app = new URL(config.appOrigin);
  if (
    !local(studio) ||
    !local(base) ||
    !(local(app) || app.protocol === "https:") ||
    [studio, base, app].some(
      (url) => url.username || url.password || url.pathname !== "/" || url.search || url.hash,
    ) ||
    studio.origin !== config.studioOrigin ||
    app.origin !== config.appOrigin ||
    !/^[a-zA-Z0-9_-]{1,100}$/.test(config.project) ||
    !/^[a-zA-Z0-9_-]{1,100}$/.test(config.environment) ||
    !/^cms_svc_[a-zA-Z0-9_-]+$/.test(config.credential)
  )
    throw new Error("STUDIO_CONFIG_INVALID");
  return { ...config, baseUrl: base.origin };
}
export function createStudioHandler(
  input: ServerConfig,
  clientDirectory: string,
  fetcher: typeof fetch = fetch,
) {
  const config = checkConfig(input),
    directory = resolve(clientDirectory),
    host = new URL(config.studioOrigin).host;
  return async function handle(req: IncomingMessage, res: ServerResponse) {
    res.setHeader("Cache-Control", "no-store");
    res.setHeader("Referrer-Policy", "no-referrer");
    res.setHeader("X-Content-Type-Options", "nosniff");
    res.setHeader(
      "Content-Security-Policy",
      `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; frame-src ${config.appOrigin}; frame-ancestors 'none'; object-src 'none'; base-uri 'none'; form-action 'self'`,
    );
    function error(status: number, code: string) {
      res.writeHead(status, { "Content-Type": "application/problem+json" });
      res.end(JSON.stringify({ code }));
    }
    try {
      if (
        req.headers.host !== host ||
        (req.headers.origin !== undefined && req.headers.origin !== config.studioOrigin) ||
        (req.headers["sec-fetch-site"] !== undefined &&
          !["same-origin", "none"].includes(String(req.headers["sec-fetch-site"])))
      )
        return error(403, "STUDIO_ORIGIN_FORBIDDEN");
      const url = new URL(req.url ?? "/", config.studioOrigin);
      if (url.pathname === "/api/studio/config" && req.method === "GET") {
        res.setHeader("Content-Type", "application/json");
        res.end(
          JSON.stringify({
            project: config.project,
            environment: config.environment,
            appOrigin: config.appOrigin,
          }),
        );
        return;
      }
      if (url.pathname.startsWith("/api/studio/")) {
        const path = url.pathname.slice("/api/studio".length);
        const uuid = "[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}";
        const allowedGet =
          path === "/api/v1/manifest" ||
          new RegExp(`^/api/v1/(documents|changesets)/${uuid}$`).test(path);
        const allowedPost = [
          "/api/v1/commands/apply-operations",
          "/api/v1/commands/create-preview-token",
        ].includes(path);
        if (!(req.method === "GET" && allowedGet) && !(req.method === "POST" && allowedPost))
          return error(404, "STUDIO_ROUTE_UNAVAILABLE");
        const upstream = new URL(path, config.baseUrl);
        let body: string | undefined;
        const headers: Record<string, string> = {
          Authorization: "Bearer " + config.credential,
          "X-CMS-Project": config.project,
        };
        if (req.method === "GET") {
          const key =
            path === "/api/v1/manifest"
              ? "environment"
              : path.includes("/documents/")
                ? "changesetId"
                : null;
          if (
            [...url.searchParams.keys()].some((param) => param !== key) ||
            (key && url.searchParams.getAll(key).length !== 1)
          )
            return error(400, "PARAM_INVALID");
          if (key === "environment" && url.searchParams.get(key) !== config.environment)
            return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
          if (key) upstream.searchParams.set(key, url.searchParams.get(key)!);
        } else {
          if (
            req.headers.origin !== config.studioOrigin ||
            !req.headers["content-type"]?.startsWith("application/json") ||
            !/^[a-zA-Z0-9_-]{1,128}$/.test(String(req.headers["idempotency-key"] ?? ""))
          )
            return error(400, "STUDIO_COMMAND_INVALID");
          const chunks: Buffer[] = [];
          let size = 0;
          for await (const chunk of req) {
            const buffer = Buffer.from(chunk);
            size += buffer.length;
            if (size > 1024 * 1024) return error(413, "LIMIT_EXCEEDED");
            chunks.push(buffer);
          }
          const envelope = JSON.parse(Buffer.concat(chunks).toString("utf8"));
          if (!envelope?.payload || typeof envelope.payload !== "object")
            return error(400, "PAYLOAD_INVALID");
          if (
            path.endsWith("create-preview-token") &&
            envelope.payload.environment !== config.environment
          )
            return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
          if (
            path.endsWith("apply-operations") &&
            (!Array.isArray(envelope.payload.operations) ||
              envelope.payload.operations.length < 1 ||
              envelope.payload.operations.length > 200 ||
              envelope.payload.operations.some(
                (op: { type?: unknown }) =>
                  ![
                    "node.setProps",
                    "node.setDesign",
                    "node.setBinding",
                    "localContent.set",
                  ].includes(String(op?.type)),
              ))
          )
            return error(400, "STUDIO_OPERATION_UNAVAILABLE");
          body = JSON.stringify(envelope);
          headers["Content-Type"] = "application/json";
          headers["Idempotency-Key"] = String(req.headers["idempotency-key"]);
        }
        const result = await fetcher(upstream, {
          method: req.method,
          headers,
          ...(body === undefined ? {} : { body }),
          redirect: "error",
          signal: AbortSignal.timeout(15_000),
        });
        // Do not forward Set-Cookie, Location, request metadata or upstream error details.
        if (!result.ok) {
          let code = "CMS_REQUEST_FAILED";
          try {
            const problem = await result.json();
            if (/^[A-Z0-9_]{1,80}$/.test(problem.code)) code = problem.code;
          } catch {
            /* safe generic code */
          }
          return error(result.status, code);
        }
        res.writeHead(result.status, { "Content-Type": "application/json" });
        res.end(await result.text());
        return;
      }
      if (req.method !== "GET" || url.search) return error(404, "NOT_FOUND");
      const relative =
        url.pathname === "/" || url.pathname === "/editor"
          ? "index.html"
          : decodeURIComponent(url.pathname).slice(1);
      const file = resolve(directory, relative);
      if (!file.startsWith(directory + sep) || !/\.(html|js|css)$/.test(file))
        return error(404, "NOT_FOUND");
      const bytes = await readFile(file);
      res.setHeader(
        "Content-Type",
        file.endsWith(".html")
          ? "text/html; charset=utf-8"
          : file.endsWith(".js")
            ? "text/javascript"
            : "text/css",
      );
      res.end(bytes);
    } catch (cause) {
      error(
        cause instanceof SyntaxError ? 400 : 502,
        cause instanceof SyntaxError ? "PAYLOAD_INVALID" : "STUDIO_REQUEST_FAILED",
      );
    }
  };
}
