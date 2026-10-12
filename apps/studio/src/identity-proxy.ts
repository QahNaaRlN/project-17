import type { IncomingMessage, ServerResponse } from "node:http";
import type { ServerConfig } from "./handler.js";
export function identityCookies(raw = ""): string {
  return raw
    .split(/;\s*/)
    .filter((c) => /^(?:__Host-)?cms_(?:login|session)=[a-zA-Z0-9_-]{43}$/.test(c))
    .join("; ");
}
export async function proxyIdentity(
  req: IncomingMessage,
  res: ServerResponse,
  url: URL,
  config: ServerConfig,
  fetcher: typeof fetch,
): Promise<boolean> {
  if (!url.pathname.startsWith("/api/studio/auth/")) return false;
  const action = url.pathname.slice("/api/studio/auth/".length);
  const error = (status: number, code: string) => {
    res.writeHead(status, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ code }));
  };
  if (
    config.authMode !== "oidc" ||
    !["login", "callback", "session", "logout"].includes(action) ||
    req.method !== (action === "logout" ? "POST" : "GET")
  ) {
    error(404, "STUDIO_ROUTE_UNAVAILABLE");
    return true;
  }
  if (
    (action !== "callback" && url.search) ||
    (action === "logout" && req.headers.origin !== config.studioOrigin)
  ) {
    error(400, "STUDIO_COMMAND_INVALID");
    return true;
  }
  const result = await fetcher(config.baseUrl + "/auth/" + action + url.search, {
    method: req.method,
    redirect: "manual",
    headers: {
      "X-CMS-Project": config.project,
      Cookie: identityCookies(req.headers.cookie),
      ...(req.method === "POST"
        ? { Origin: config.studioOrigin, "X-CSRF-Token": String(req.headers["x-csrf-token"] ?? "") }
        : {}),
    },
    signal: AbortSignal.timeout(15000),
  });
  const cookies = result.headers
    .getSetCookie()
    .filter((c) => /^(?:__Host-)?cms_(?:login|session)=/.test(c) && !/(?:^|;)\s*Domain=/i.test(c));
  if (cookies.length) res.setHeader("Set-Cookie", cookies);
  if (result.status === 302) {
    const target = new URL(result.headers.get("Location") ?? "", config.studioOrigin);
    if (
      (action === "login" &&
        target.protocol !== "https:" &&
        !(
          target.protocol === "http:" &&
          ["localhost", "127.0.0.1", "[::1]"].includes(target.hostname)
        )) ||
      (action !== "login" &&
        (target.origin !== config.studioOrigin || target.pathname !== "/editor")) ||
      target.username ||
      target.password
    ) {
      error(502, "STUDIO_REQUEST_FAILED");
      return true;
    }
    res.writeHead(302, { Location: target.href });
    res.end();
    return true;
  }
  if (!result.ok) {
    error(result.status, "SESSION_UNAVAILABLE");
    return true;
  }
  res.writeHead(200, { "Content-Type": "application/json" });
  res.end(JSON.stringify(await result.json()));
  return true;
}
