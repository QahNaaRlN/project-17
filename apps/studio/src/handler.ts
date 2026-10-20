import { githubCommands } from "./capability-github.js";
import type { IncomingMessage, ServerResponse } from "node:http";
import { readFile } from "node:fs/promises";
import { resolve, sep } from "node:path";
import type { Config } from "./types.js";
import { problemDiagnostics } from "./review.js";
import { identityCookies, proxyIdentity } from "./identity-proxy.js";
import { capabilityCommands } from "./capabilities.js";
import { deliveryCommands } from "./capability-delivery.js";
export interface ServerConfig extends Config {
  baseUrl: string;
  studioOrigin: string;
  credential?: string;
  authMode?: "service" | "oidc";
}
export function checkConfig(config: ServerConfig): ServerConfig {
  const local = (url: URL) =>
    url.protocol === "http:" && ["127.0.0.1", "localhost", "[::1]"].includes(url.hostname);
  const studio = new URL(config.studioOrigin),
    base = new URL(config.baseUrl),
    app = new URL(config.appOrigin);
  const upload = config.assetUploadOrigin ? new URL(config.assetUploadOrigin) : undefined;
  // STU-328: exact origins only; no wildcard patterns, paths or credentials.
  const previews = config.previewAppOrigins ?? [];
  if (
    !Array.isArray(previews) ||
    previews.length > 32 ||
    previews.some((origin) => {
      if (typeof origin !== "string" || origin.includes("*")) return true;
      try {
        const url = new URL(origin);
        return (
          !(local(url) || url.protocol === "https:") ||
          url.origin !== origin ||
          url.username !== "" ||
          url.password !== ""
        );
      } catch {
        return true;
      }
    })
  )
    throw new Error("STUDIO_CONFIG_INVALID");
  if (
    !(local(studio) || (config.authMode === "oidc" && studio.protocol === "https:")) ||
    !(local(base) || (config.authMode === "oidc" && base.protocol === "https:")) ||
    !(local(app) || app.protocol === "https:") ||
    [studio, base, app].some(
      (url) => url.username || url.password || url.pathname !== "/" || url.search || url.hash,
    ) ||
    studio.origin !== config.studioOrigin ||
    app.origin !== config.appOrigin ||
    (upload &&
      (!(local(upload) || upload.protocol === "https:") ||
        upload.origin !== config.assetUploadOrigin ||
        upload.username ||
        upload.password)) ||
    !/^[a-zA-Z0-9_-]{1,100}$/.test(config.project) ||
    !/^[a-zA-Z0-9_-]{1,100}$/.test(config.environment) ||
    ![undefined, "service", "oidc"].includes(config.authMode) ||
    (config.authMode === "oidc"
      ? config.credential !== undefined
      : !/^cms_svc_[a-zA-Z0-9_-]+$/.test(config.credential ?? ""))
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
      `default-src 'self'; connect-src 'self'${config.assetUploadOrigin ? " " + config.assetUploadOrigin : ""}; script-src 'self'; style-src 'self' 'unsafe-inline'; frame-src ${[config.appOrigin, ...(config.previewAppOrigins ?? [])].join(" ")}; frame-ancestors 'none'; object-src 'none'; base-uri 'none'; form-action 'self'`,
    );
    function error(status: number, code: string, diagnostics = problemDiagnostics(undefined)) {
      res.writeHead(status, { "Content-Type": "application/problem+json" });
      res.end(JSON.stringify({ code, ...(diagnostics.length ? { diagnostics } : {}) }));
    }
    try {
      const url = new URL(req.url ?? "/", config.studioOrigin);
      const callback =
        config.authMode === "oidc" &&
        req.method === "GET" &&
        url.pathname === "/api/studio/auth/callback";
      // The IdP callback redirects to the static shell. Browsers retain the
      // redirect chain's fetch-site value; this exception grants no API access.
      const navigation =
        config.authMode === "oidc" &&
        req.method === "GET" &&
        ["/", "/editor"].includes(url.pathname) &&
        req.headers["sec-fetch-mode"] === "navigate" &&
        req.headers["sec-fetch-dest"] === "document";
      if (
        req.headers.host !== host ||
        (!callback &&
          req.headers.origin !== undefined &&
          req.headers.origin !== config.studioOrigin) ||
        (req.headers["sec-fetch-site"] !== undefined &&
          !callback &&
          !navigation &&
          !["same-origin", "none"].includes(String(req.headers["sec-fetch-site"])))
      )
        return error(403, "STUDIO_ORIGIN_FORBIDDEN");
      if (await proxyIdentity(req, res, url, config, fetcher)) return;
      if (url.pathname === "/api/studio/config" && req.method === "GET") {
        res.setHeader("Content-Type", "application/json");
        res.end(
          JSON.stringify({
            project: config.project,
            environment: config.environment,
            appOrigin: config.appOrigin,
            ...(config.previewAppOrigins?.length
              ? { previewAppOrigins: config.previewAppOrigins }
              : {}),
            ...(config.assetUploadOrigin ? { assetUploadOrigin: config.assetUploadOrigin } : {}),
            ...(config.authMode === "oidc" ? { authMode: "oidc" } : {}),
          }),
        );
        return;
      }
      if (url.pathname.startsWith("/api/studio/")) {
        const path = url.pathname.slice("/api/studio".length);
        const uuid = "[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}";
        const diffRoute = new RegExp(`^/api/v1/changesets/${uuid}/diff(?:/${uuid})?$`).test(path);
        const publicationRoute = new RegExp(
          `^/api/v1/publication-history(?:/${uuid}(?:/diff|/rollback-preview(?:/diff)?)?)?$`,
        ).test(path);
        const queueRoute = new RegExp(`^/api/v1/work-queue(?:/${uuid})?$`).test(path);
        const capabilityRoute = new RegExp(
          `^/api/v1/capability-requests(?:/${uuid}(?:/context-preview|/context-pack)?)?$`,
        ).test(path);
        const deliveryRoute = new RegExp(
          `^/api/v1/(?:capability-delivery-(?:endpoints|target)|capability-deliveries/${uuid}|capability-requests/${uuid}/(?:delivery-preview|deliveries))$`,
        ).test(path);
        const githubRoute = new RegExp(
          `^/api/v1/(?:capability-github-config|capability-requests/${uuid}/github(?:-observations)?|capability-requests/${uuid}/preview)$`,
        ).test(path);
        // PR preview environments of this Studio's base environment (STU-327).
        const previewEnvironment = /^preview\/pr-[1-9][0-9]{0,18}$/;
        const allowedGet =
          githubRoute ||
          queueRoute ||
          capabilityRoute ||
          deliveryRoute ||
          publicationRoute ||
          diffRoute ||
          path === "/api/v1/agent-config" ||
          path === "/api/v1/agent-runs" ||
          path === "/api/v1/events" ||
          path === "/api/v1/content-catalog" ||
          new RegExp(`^/api/v1/(content|asset-uploads)/${uuid}$`).test(path) ||
          path === "/api/v1/pages" ||
          path === "/api/v1/working-drafts" ||
          path === "/api/v1/work-items" ||
          path === "/api/v1/manifest" ||
          new RegExp(`^/api/v1/changesets/${uuid}/history$`).test(path) ||
          new RegExp(`^/api/v1/changesets/${uuid}/review$`).test(path) ||
          new RegExp(`^/api/v1/documents/${uuid}/component-extract-plan$`).test(path) ||
          new RegExp(`^/api/v1/(documents|changesets)/${uuid}$`).test(path);
        const allowedPost = [
          ...githubCommands.map((name) => "/api/v1/commands/" + name),
          "/api/v1/commands/set-capability-preview-args",
          ...deliveryCommands.map((name) => "/api/v1/commands/" + name),
          ...capabilityCommands.map((name) => "/api/v1/commands/" + name),
          "/api/v1/commands/rollback",
          "/api/v1/commands/start-agent-run",
          "/api/v1/commands/cancel-agent-run",
          "/api/v1/commands/presence-heartbeat",
          "/api/v1/commands/presence-leave",
          "/api/v1/commands/create-asset-upload",
          "/api/v1/commands/complete-asset-upload",
          "/api/v1/commands/apply-operations",
          "/api/v1/commands/create-preview-token",
          "/api/v1/commands/create-changeset",
          "/api/v1/commands/undo",
          "/api/v1/commands/redo",
          "/api/v1/commands/submit-changeset",
          "/api/v1/commands/reopen-changeset",
          "/api/v1/commands/approve-changeset",
          "/api/v1/commands/request-changes",
          "/api/v1/commands/publish",
        ].includes(path);
        if (!(req.method === "GET" && allowedGet) && !(req.method === "POST" && allowedPost))
          return error(404, "STUDIO_ROUTE_UNAVAILABLE");
        const upstream = new URL(path, config.baseUrl);
        let body: string | undefined;
        const headers: Record<string, string> = {
          ...(config.authMode === "oidc"
            ? {
                Cookie: identityCookies(req.headers.cookie),
                Origin: config.studioOrigin,
                "X-CSRF-Token": String(req.headers["x-csrf-token"] ?? ""),
                "X-CMS-Actor": String(req.headers["x-cms-actor"] ?? ""),
              }
            : { Authorization: "Bearer " + config.credential }),
          "X-CMS-Project": config.project,
        };
        if (req.method === "GET") {
          if (capabilityRoute || deliveryRoute || githubRoute) {
            const allowed = path.endsWith("capability-requests")
              ? ["environment", "status", "kind", "key", "cursor", "limit"]
              : path.endsWith("/deliveries") || path.endsWith("/github-observations")
                ? ["environment", "limit", "cursor"]
                : path.endsWith("/context-pack")
                  ? ["environment", "version"]
                  : ["environment"];
            if (
              [...url.searchParams.keys()].some(
                (k) => !allowed.includes(k) || url.searchParams.getAll(k).length !== 1,
              ) ||
              [...url.searchParams.values()].some((v) => v.length > 4096)
            )
              return error(400, "PARAM_INVALID");
            if (url.searchParams.get("environment") !== config.environment)
              return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
            upstream.search = url.search;
          } else if (publicationRoute) {
            const p = url.searchParams;
            const detail = path.endsWith("/diff"),
              list = path === "/api/v1/publication-history";
            const allowed = [
              "environment",
              ...(list ? ["limit", "cursor"] : []),
              ...(detail ? ["objectId", "limit", "cursor", "snapshot"] : []),
            ];
            if (
              [...p.keys()].some((k) => !allowed.includes(k) || p.getAll(k).length !== 1) ||
              (p.has("limit") && !/^(?:[1-9][0-9]?|100)$/.test(p.get("limit")!)) ||
              (p.get("cursor")?.length ?? 0) > 2048 ||
              (detail &&
                (!new RegExp(`^${uuid}$`).test(p.get("objectId") ?? "") ||
                  !/^sha256:[a-f0-9]{64}$/.test(p.get("snapshot") ?? "")))
            )
              return error(400, "PARAM_INVALID");
            if (p.get("environment") !== config.environment)
              return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
            upstream.search = url.search;
          } else if (path === "/api/v1/events" || path === "/api/v1/agent-runs") {
            if (
              [...url.searchParams.keys()].some(
                (key) =>
                  ![
                    "environment",
                    "documentId",
                    "changesetId",
                    ...(path.endsWith("/events") ? ["after"] : []),
                  ].includes(key) || url.searchParams.getAll(key).length !== 1,
              ) ||
              !new RegExp(`^${uuid}$`).test(url.searchParams.get("documentId") ?? "") ||
              !new RegExp(`^${uuid}$`).test(url.searchParams.get("changesetId") ?? "") ||
              (url.searchParams.has("after") &&
                !/^(0|[1-9][0-9]{0,18})$/.test(url.searchParams.get("after")!))
            )
              return error(400, "PARAM_INVALID");
            if (url.searchParams.get("environment") !== config.environment)
              return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
            upstream.search = url.search;
          } else if (diffRoute) {
            const detail = !path.endsWith("/diff"),
              params = url.searchParams;
            const allowed = [
              "environment",
              ...(detail ? ["baseline", "snapshot", "after", "limit"] : []),
            ];
            if (
              [...params.keys()].some(
                (k) => !allowed.includes(k) || params.getAll(k).length !== 1,
              ) ||
              (detail &&
                (!["base", "published"].includes(params.get("baseline") ?? "") ||
                  !/^sha256:[a-f0-9]{64}$/.test(params.get("snapshot") ?? "") ||
                  (params.has("after") &&
                    (params.get("after")!.length > 2048 || !params.get("after"))) ||
                  (params.has("limit") &&
                    (!/^[1-9][0-9]*$/.test(params.get("limit")!) ||
                      Number(params.get("limit")) > 100))))
            )
              return error(400, "PARAM_INVALID");
            if (params.get("environment") !== config.environment)
              return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
            upstream.search = url.search;
          } else if (queueRoute) {
            const allowed = path.endsWith("work-queue")
              ? ["environment", "q", "state", "ownership", "kind", "limit", "cursor"]
              : ["environment"];
            if (
              [...url.searchParams.keys()].some(
                (k) => !allowed.includes(k) || url.searchParams.getAll(k).length !== 1,
              ) ||
              (url.searchParams.has("q") &&
                ([...url.searchParams.get("q")!.trim()].length > 200 ||
                  url.searchParams.get("q")!.includes("\0")))
            )
              return error(400, "PARAM_INVALID");
            if (url.searchParams.get("environment") !== config.environment)
              return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
            upstream.search = url.search;
          } else if (path === "/api/v1/content-catalog" || path.startsWith("/api/v1/content/")) {
            const allowed = [
              "environment",
              "changesetId",
              ...(path.endsWith("content-catalog") ? ["kind", "schema", "after", "q"] : []),
            ];
            if (
              [...url.searchParams.keys()].some(
                (key) => !allowed.includes(key) || url.searchParams.getAll(key).length !== 1,
              ) ||
              !new RegExp(`^${uuid}$`).test(url.searchParams.get("changesetId") ?? "") ||
              (url.searchParams.has("after") &&
                !new RegExp(`^${uuid}$`).test(url.searchParams.get("after")!))
            )
              return error(400, "PARAM_INVALID");
            if (url.searchParams.get("environment") !== config.environment)
              return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
            upstream.search = url.search;
          } else if (path.endsWith("/component-extract-plan")) {
            const allowed = ["changesetId", "nodeId", "environment"];
            if (
              [...url.searchParams.keys()].some(
                (key) => !allowed.includes(key) || url.searchParams.getAll(key).length !== 1,
              ) ||
              !new RegExp(`^${uuid}$`).test(url.searchParams.get("changesetId") ?? "") ||
              !/^[A-Za-z0-9_-]{4,32}$/.test(url.searchParams.get("nodeId") ?? "")
            )
              return error(400, "PARAM_INVALID");
            if (url.searchParams.get("environment") !== config.environment)
              return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
            upstream.search = url.search;
          } else if (path.endsWith("/history")) {
            if (
              [...url.searchParams.keys()].some(
                (key) => key !== "beforeSeq" || url.searchParams.getAll(key).length !== 1,
              ) ||
              (url.searchParams.has("beforeSeq") &&
                (!/^[1-9][0-9]*$/.test(url.searchParams.get("beforeSeq")!) ||
                  Number(url.searchParams.get("beforeSeq")) > 2147483647))
            )
              return error(400, "PARAM_INVALID");
            upstream.search = url.search;
          } else if (
            ["/api/v1/pages", "/api/v1/working-drafts", "/api/v1/work-items"].includes(path)
          ) {
            const allowed = [
              "environment",
              "after",
              ...(path.endsWith("working-drafts") ? ["documentId"] : []),
              ...(path.endsWith("/pages") ? ["q"] : []),
            ];
            if (
              [...url.searchParams.keys()].some(
                (key) => !allowed.includes(key) || url.searchParams.getAll(key).length !== 1,
              ) ||
              (url.searchParams.has("after") &&
                !new RegExp(`^${uuid}$`).test(url.searchParams.get("after")!)) ||
              (path.endsWith("working-drafts") &&
                !new RegExp(`^${uuid}$`).test(url.searchParams.get("documentId") ?? "")) ||
              (url.searchParams.has("q") &&
                ([...(url.searchParams.get("q") ?? "").trim()].length > 200 ||
                  url.searchParams.get("q")!.includes("\0")))
            )
              return error(400, "PARAM_INVALID");
            if (url.searchParams.get("environment") !== config.environment)
              return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
            upstream.search = url.search;
          } else {
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
          }
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
          if (githubCommands.some((name) => path.endsWith("/" + name))) {
            if (config.authMode !== "oidc") return error(403, "CAPABILITY_GITHUB_FORBIDDEN");
            const p = envelope.payload;
            if (path.endsWith("/set-capability-github-config")) {
              if (p.environment !== config.environment)
                return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
            } else {
              if (!new RegExp(`^${uuid}$`).test(String(p.requestId)))
                return error(400, "PAYLOAD_INVALID");
              const check = await fetcher(
                new URL(
                  "/api/v1/capability-requests/" +
                    p.requestId +
                    "?environment=" +
                    encodeURIComponent(config.environment),
                  config.baseUrl,
                ),
                { headers, redirect: "error", signal: AbortSignal.timeout(15000) },
              );
              if (!check.ok) return error(check.status, "CAPABILITY_GITHUB_NOT_FOUND");
            }
          }
          if (path.endsWith("/set-capability-preview-args")) {
            const p = envelope.payload;
            if (
              Object.keys(p).some(
                (k) =>
                  ![
                    "requestId",
                    "expectedRequestRevision",
                    "environment",
                    "manifestHash",
                    "handlers",
                    "expectedArgsRevision",
                  ].includes(k),
              ) ||
              !new RegExp(`^${uuid}$`).test(String(p.requestId)) ||
              !previewEnvironment.test(String(p.environment))
            )
              return error(400, "PAYLOAD_INVALID");
            const check = await fetcher(
              new URL(
                "/api/v1/capability-requests/" +
                  p.requestId +
                  "?environment=" +
                  encodeURIComponent(config.environment),
                config.baseUrl,
              ),
              { headers, redirect: "error", signal: AbortSignal.timeout(15000) },
            );
            if (!check.ok) return error(check.status, "CAPABILITY_REQUEST_UNAVAILABLE");
          }
          if (deliveryCommands.some((name) => path.endsWith("/" + name))) {
            if (config.authMode !== "oidc") return error(403, "CAPABILITY_DELIVERY_FORBIDDEN");
            const p = envelope.payload;
            if (path.endsWith("/set-capability-delivery-target")) {
              if (p.environment !== config.environment)
                return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
            } else {
              const sending = path.endsWith("/send-capability-context");
              const id = sending ? p.requestId : p.deliveryId;
              if (!new RegExp(`^${uuid}$`).test(String(id))) return error(400, "PAYLOAD_INVALID");
              const check = await fetcher(
                new URL(
                  "/api/v1/" +
                    (sending ? "capability-requests/" : "capability-deliveries/") +
                    id +
                    "?environment=" +
                    encodeURIComponent(config.environment),
                  config.baseUrl,
                ),
                { headers, redirect: "error", signal: AbortSignal.timeout(15000) },
              );
              if (!check.ok) return error(check.status, "CAPABILITY_DELIVERY_NOT_FOUND");
            }
          }
          if (capabilityCommands.some((name) => path.endsWith("/" + name))) {
            const p = envelope.payload;
            if (path.endsWith("/create-capability-request")) {
              if (p.environment !== config.environment)
                return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
            } else {
              if (!new RegExp(`^${uuid}$`).test(String(p.id))) return error(400, "PAYLOAD_INVALID");
              const check = await fetcher(
                new URL(
                  "/api/v1/capability-requests/" +
                    p.id +
                    "?environment=" +
                    encodeURIComponent(config.environment),
                  config.baseUrl,
                ),
                { headers, redirect: "error", signal: AbortSignal.timeout(15000) },
              );
              if (!check.ok) return error(check.status, "CAPABILITY_REQUEST_UNAVAILABLE");
              const card = await check.json();
              if (card.environment !== config.environment)
                return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
            }
          }
          if (path.endsWith("/commands/rollback")) {
            const p = envelope.payload;
            if (
              Object.keys(envelope).some((k) => !["payload", "reason"].includes(k)) ||
              Object.keys(p).some(
                (k) => !["publicationId", "resetHead", "expectedRollbackSnapshot"].includes(k),
              ) ||
              !new RegExp(`^${uuid}$`).test(String(p.publicationId)) ||
              p.resetHead !== false ||
              !/^sha256:[a-f0-9]{64}$/.test(p.expectedRollbackSnapshot) ||
              typeof envelope.reason !== "string" ||
              !envelope.reason.trim() ||
              [...envelope.reason.trim()].length > 2000
            )
              return error(400, "PAYLOAD_INVALID");
            const check = await fetcher(
              new URL(
                "/api/v1/publication-history/" +
                  p.publicationId +
                  "?environment=" +
                  encodeURIComponent(config.environment),
                config.baseUrl,
              ),
              { headers, redirect: "error", signal: AbortSignal.timeout(15000) },
            );
            if (!check.ok) return error(check.status, "ROLLBACK_PREVIEW_UNAVAILABLE");
            const card = await check.json();
            if (card?.publication?.environment !== config.environment)
              return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
          }
          if (path.includes("/commands/presence-")) {
            const p = envelope.payload,
              leave = path.endsWith("presence-leave");
            if (
              Object.keys(p).some(
                (key) =>
                  !(
                    leave ? ["leaseId"] : ["leaseId", "documentId", "changesetId", "environment"]
                  ).includes(key),
              ) ||
              !new RegExp(`^${uuid}$`).test(String(p.leaseId)) ||
              (!leave &&
                (!new RegExp(`^${uuid}$`).test(String(p.documentId)) ||
                  !new RegExp(`^${uuid}$`).test(String(p.changesetId))))
            )
              return error(400, "PAYLOAD_INVALID");
            if (!leave && p.environment !== config.environment)
              return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
          }
          if (path.endsWith("create-asset-upload") || path.endsWith("complete-asset-upload")) {
            const allowed = path.endsWith("create-asset-upload")
              ? ["filename", "mimeType", "size", "sha256"]
              : ["assetId"];
            if (Object.keys(envelope.payload).some((key) => !allowed.includes(key)))
              return error(400, "PAYLOAD_INVALID");
          }
          if (path.endsWith("/start-agent-run")) {
            const p = envelope.payload;
            if (
              Object.keys(p).some(
                (k) =>
                  !["documentId", "changesetId", "environment", "expectedSeq", "prompt"].includes(
                    k,
                  ),
              ) ||
              !new RegExp(`^${uuid}$`).test(String(p.documentId)) ||
              !new RegExp(`^${uuid}$`).test(String(p.changesetId)) ||
              !Number.isSafeInteger(p.expectedSeq) ||
              p.expectedSeq < 0 ||
              typeof p.prompt !== "string" ||
              !p.prompt.trim() ||
              p.prompt.length > 16000
            )
              return error(400, "PAYLOAD_INVALID");
            if (p.environment !== config.environment)
              return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
          }
          if (
            path.endsWith("/cancel-agent-run") &&
            (Object.keys(envelope.payload).some((k) => k !== "runId") ||
              !new RegExp(`^${uuid}$`).test(String(envelope.payload.runId)))
          )
            return error(400, "PAYLOAD_INVALID");
          if (
            [
              "submit-changeset",
              "reopen-changeset",
              "approve-changeset",
              "request-changes",
              "publish",
            ].some((command) => path.endsWith("/" + command))
          ) {
            const p = envelope.payload;
            const allowed = [
              "changesetId",
              "expectedSeq",
              ...(["/approve-changeset", "/request-changes", "/publish"].some((c) =>
                path.endsWith(c),
              )
                ? ["expectedDiffSnapshot"]
                : []),
              ...(path.endsWith("/publish") ? ["environment"] : []),
              ...(path.endsWith("/request-changes") ? ["comment"] : []),
            ];
            if (
              Object.keys(p).some((key) => !allowed.includes(key)) ||
              (p.expectedDiffSnapshot !== undefined &&
                !/^sha256:[a-f0-9]{64}$/.test(p.expectedDiffSnapshot)) ||
              !new RegExp(`^${uuid}$`).test(String(p.changesetId)) ||
              !Number.isSafeInteger(p.expectedSeq) ||
              p.expectedSeq < 0 ||
              p.expectedSeq > 2147483647 ||
              (path.endsWith("/request-changes") &&
                (typeof p.comment !== "string" ||
                  !p.comment.trim() ||
                  [...p.comment].length > 2000))
            )
              return error(400, "PAYLOAD_INVALID");
            if (path.endsWith("/publish") && p.environment !== config.environment)
              return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
          }
          if (
            ["/api/v1/commands/undo", "/api/v1/commands/redo"].includes(path) &&
            (Object.keys(envelope.payload).some(
              (key) => !["changesetId", "expectedSeq"].includes(key),
            ) ||
              !new RegExp(`^${uuid}$`).test(String(envelope.payload.changesetId)) ||
              !Number.isSafeInteger(envelope.payload.expectedSeq) ||
              envelope.payload.expectedSeq < 0 ||
              envelope.payload.expectedSeq > 2147483647)
          )
            return error(400, "PAYLOAD_INVALID");
          if (
            (path.endsWith("create-preview-token") &&
              envelope.payload.environment !== config.environment &&
              !(
                previewEnvironment.test(String(envelope.payload.environment)) &&
                new RegExp(`^${uuid}$`).test(String(envelope.payload.changesetId))
              )) ||
            (path.endsWith("create-changeset") &&
              (!Array.isArray(envelope.payload.targets) ||
                envelope.payload.targets.length !== 1 ||
                envelope.payload.targets[0] !== config.environment))
          )
            return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
          if (
            path.endsWith("create-changeset") &&
            (Object.keys(envelope.payload).some((key) => !["title", "targets"].includes(key)) ||
              typeof envelope.payload.title !== "string" ||
              envelope.payload.title.trim().length === 0 ||
              [...envelope.payload.title].length > 200)
          )
            return error(400, "PAYLOAD_INVALID");
          if (
            path.endsWith("apply-operations") &&
            (!Array.isArray(envelope.payload.operations) ||
              envelope.payload.operations.length < 1 ||
              envelope.payload.operations.length > 200 ||
              envelope.payload.operations.some(
                (op: { type?: unknown }) =>
                  ![
                    "document.setContent",
                    "document.setDataSource",
                    "node.setProps",
                    "node.setDesign",
                    "node.setBinding",
                    "node.rename",
                    "node.setCondition",
                    "node.setBehavior",
                    "localContent.set",
                    "localContent.remove",
                    "node.insert",
                    "node.remove",
                    "node.move",
                    "component.extract",
                    "entity.create",
                    "entity.setFields",
                    "entity.delete",
                    "entity.restore",
                    "asset.create",
                    "asset.updateMeta",
                    "asset.replaceFile",
                  ].includes(String(op?.type)),
              ))
          )
            return error(400, "STUDIO_OPERATION_UNAVAILABLE");
          if (
            path.endsWith("apply-operations") &&
            envelope.payload.operations.some(
              (op: { type: string; payload?: { environment?: string } }) =>
                op.type === "entity.create" && op.payload?.environment !== config.environment,
            )
          )
            return error(403, "STUDIO_ENVIRONMENT_FORBIDDEN");
          body = JSON.stringify(envelope);
          headers["Content-Type"] = "application/json";
          headers["Idempotency-Key"] = String(req.headers["idempotency-key"]);
        }
        const stream = path === "/api/v1/events";
        const connection = new AbortController();
        const disconnect = () => connection.abort();
        const streamSignal = stream
          ? AbortSignal.any([connection.signal, AbortSignal.timeout(35_000)])
          : undefined;
        if (stream) res.once("close", disconnect);
        const result = await fetcher(upstream, {
          method: req.method,
          headers,
          ...(body === undefined ? {} : { body }),
          redirect: "error",
          signal: streamSignal ?? AbortSignal.timeout(15_000),
        });
        // Do not forward Set-Cookie, Location, request metadata or upstream error details.
        if (!result.ok) {
          let code = "CMS_REQUEST_FAILED";
          let diagnostics = problemDiagnostics(undefined);
          try {
            const problem = await result.json();
            if (/^[A-Z0-9_]{1,80}$/.test(problem.code)) code = problem.code;
            diagnostics = problemDiagnostics(problem.params);
          } catch {
            /* safe generic code */
          }
          return error(result.status, code, diagnostics);
        }
        if (stream) {
          try {
            if (
              !result.headers.get("content-type")?.startsWith("text/event-stream") ||
              !result.body
            )
              return error(502, "RESPONSE_INVALID");
            res.writeHead(200, { "Content-Type": "text/event-stream", "X-Accel-Buffering": "no" });
            res.flushHeaders();
            const reader = result.body.getReader();
            try {
              while (!streamSignal!.aborted) {
                const chunk = await reader.read();
                if (chunk.done) break;
                if (!res.write(chunk.value))
                  await new Promise<void>((resolve) => {
                    const done = () => {
                      res.off("drain", done);
                      res.off("close", done);
                      streamSignal!.removeEventListener("abort", done);
                      resolve();
                    };
                    res.once("drain", done);
                    res.once("close", done);
                    streamSignal!.addEventListener("abort", done, { once: true });
                  });
              }
            } finally {
              await reader.cancel().catch(() => {});
              reader.releaseLock();
            }
            res.end();
          } finally {
            connection.abort();
            res.off("close", disconnect);
          }
          return;
        }
        res.writeHead(result.status, { "Content-Type": "application/json" });
        res.end(await result.text());
        return;
      }
      const loginError =
        config.authMode === "oidc" &&
        url.pathname === "/editor" &&
        url.search === "?authError=OIDC_LOGIN_FAILED";
      if (req.method !== "GET" || (url.search && !loginError)) return error(404, "NOT_FOUND");
      const relative =
        url.pathname === "/" || url.pathname === "/editor"
          ? "index.html"
          : decodeURIComponent(url.pathname).slice(1);
      const file = resolve(directory, relative);
      if (!file.startsWith(directory + sep) || !/\.(html|js|css|woff2)$/.test(file))
        return error(404, "NOT_FOUND");
      const bytes = await readFile(file);
      res.setHeader(
        "Content-Type",
        file.endsWith(".html")
          ? "text/html; charset=utf-8"
          : file.endsWith(".woff2")
            ? "font/woff2"
            : file.endsWith(".js")
              ? "text/javascript"
              : "text/css",
      );
      res.end(bytes);
    } catch (cause) {
      if (res.headersSent) {
        res.destroy();
        return;
      }
      error(
        cause instanceof SyntaxError ? 400 : 502,
        cause instanceof SyntaxError ? "PAYLOAD_INVALID" : "STUDIO_REQUEST_FAILED",
      );
    }
  };
}
