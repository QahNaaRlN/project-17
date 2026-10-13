import type { IncomingMessage, ServerResponse } from "node:http";
import { randomBytes } from "node:crypto";
// Browser tests model the public HTTP identity boundary. Signed OIDC/PKCE is tested in Go.
export function identityBoundary(aliceId: string, bobId: string) {
  let enabled = false,
    expired = false;
  const flows = new Set<string>();
  const user = (req: IncomingMessage) => {
    const c = req.headers.cookie ?? "";
    if (expired) return null;
    return c.includes("cms_session=" + "a".repeat(43))
      ? { id: aliceId, name: "Alice", csrf: "c".repeat(43) }
      : c.includes("cms_session=" + "b".repeat(43))
        ? { id: bobId, name: "Bob", csrf: "d".repeat(43) }
        : null;
  };
  return {
    get enabled() {
      return enabled;
    },
    user,
    reset() {
      enabled = false;
      expired = false;
      flows.clear();
    },
    configure(c: { identity?: boolean; expired?: boolean; user?: string }, res: ServerResponse) {
      enabled = c.identity ?? enabled;
      expired = c.expired ?? expired;
      if (c.user)
        res.setHeader(
          "Set-Cookie",
          "cms_session=" +
            (c.user === "alice" ? "a" : "b").repeat(43) +
            "; Path=/; HttpOnly; SameSite=Lax",
        );
    },
    handle(req: IncomingMessage, res: ServerResponse) {
      if (!enabled) return false;
      const url = new URL(req.url ?? "/", "http://127.0.0.1:18084"),
        path = url.pathname,
        who = user(req);
      if (path === "/fixture-idp") {
        const state = url.searchParams.get("state");
        res.setHeader("Content-Type", "text/html; charset=utf-8");
        if (!state || !flows.has(state)) {
          res.end("invalid state");
          return true;
        }
        res.end(
          '<!doctype html><h1>Тестовый провайдер</h1><a href="/api/studio/auth/callback?state=' +
            state +
            '&code=alice">Войти как Alice</a><a href="/api/studio/auth/callback?state=' +
            state +
            '&code=bob">Войти как Bob</a>',
        );
        return true;
      }
      if (!path.startsWith("/auth/")) return false;
      res.setHeader("Content-Type", "application/json");
      res.setHeader("Cache-Control", "no-store");
      if (path === "/auth/session") {
        res.end(
          JSON.stringify(
            who
              ? {
                  authenticated: true,
                  user: { id: who.id, name: who.name },
                  csrfToken: who.csrf,
                  rights:
                    who.id === aliceId
                      ? [
                          "design.read",
                          "content.read",
                          "content.write",
                          "design.compose",
                          "content.publish",
                          "agent.delegate",
                        ]
                      : ["design.read", "content.read", "content.publish"],
                  hasAccess: true,
                  expiresAt: new Date(Date.now() + 8 * 3600000).toISOString(),
                }
              : { authenticated: false, rights: [], hasAccess: false },
          ),
        );
        return true;
      }
      if (path === "/auth/login") {
        const state = randomBytes(32).toString("base64url");
        flows.add(state);
        res.setHeader("Set-Cookie", "cms_login=" + state + "; Path=/; HttpOnly; SameSite=Lax");
        res.writeHead(302, { Location: "http://127.0.0.1:18084/fixture-idp?state=" + state });
        res.end();
        return true;
      }
      if (path === "/auth/callback") {
        const state = url.searchParams.get("state") ?? "",
          code = url.searchParams.get("code") ?? "";
        if (
          !flows.has(state) ||
          !(req.headers.cookie ?? "").includes("cms_login=" + state) ||
          !["alice", "bob"].includes(code)
        ) {
          res.writeHead(302, {
            Location: "http://127.0.0.1:18084/editor?authError=OIDC_LOGIN_FAILED",
          });
          res.end();
          return true;
        }
        flows.delete(state);
        expired = false;
        res.setHeader("Set-Cookie", [
          "cms_session=" +
            (code === "alice" ? "a" : "b").repeat(43) +
            "; Path=/; HttpOnly; SameSite=Lax",
          "cms_login=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax",
        ]);
        res.writeHead(302, { Location: "http://127.0.0.1:18084/editor" });
        res.end();
        return true;
      }
      if (path === "/auth/logout" && req.method === "POST") {
        if (who && req.headers["x-csrf-token"] !== who.csrf) {
          res.writeHead(403);
          res.end(JSON.stringify({ code: "CSRF_INVALID" }));
          return true;
        }
        res.setHeader("Set-Cookie", "cms_session=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax");
        res.end(JSON.stringify({ ok: true }));
        return true;
      }
      res.writeHead(404);
      res.end(JSON.stringify({ code: "NOT_FOUND" }));
      return true;
    },
    denial(req: IncomingMessage) {
      const who = user(req);
      if (!who) return { status: 401, code: "UNAUTHENTICATED" };
      if (req.headers["x-cms-actor"] && req.headers["x-cms-actor"] !== who.id)
        return { status: 403, code: "SESSION_CHANGED" };
      if (req.method === "POST" && req.headers["x-csrf-token"] !== who.csrf)
        return { status: 403, code: "CSRF_INVALID" };
      return null;
    },
  };
}
