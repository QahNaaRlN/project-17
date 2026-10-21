// Development-only OIDC fixture. Identities are explicitly selected; no passwords or production login.
import { createServer } from "node:http";
import { generateKeyPairSync, randomBytes, createHash, sign } from "node:crypto";
import { Buffer } from "node:buffer";
import { pathToFileURL, URL, URLSearchParams } from "node:url";
export function createDevOIDC({ port = 18095, studioPort = 18094 } = {}) {
  if (![port, studioPort].every((p) => Number.isInteger(p) && p > 1024 && p < 65536))
    throw Error("Invalid loopback port");
  const issuer = `http://127.0.0.1:${port}`,
    redirect = `http://127.0.0.1:${studioPort}/api/studio/auth/callback`;
  const { privateKey, publicKey } = generateKeyPairSync("rsa", { modulusLength: 2048 });
  const jwk = {
    ...publicKey.export({ format: "jwk" }),
    kid: "local-dev",
    use: "sig",
    alg: "RS256",
  };
  const codes = new Map();
  // Erin is the content editor of the §2 scenario (STU-443).
  const names = { alice: "Alice", bob: "Bob", erin: "Erin" };
  const json = (res, value, status = 200) => {
    res.writeHead(status, { "Content-Type": "application/json", "Cache-Control": "no-store" });
    res.end(JSON.stringify(value));
  };
  const server = createServer(async (req, res) => {
    try {
      const url = new URL(req.url, issuer),
        q = url.searchParams;
      if (req.method === "GET" && url.pathname === "/.well-known/openid-configuration")
        return json(res, {
          issuer,
          authorization_endpoint: issuer + "/authorize",
          token_endpoint: issuer + "/token",
          jwks_uri: issuer + "/keys",
          response_types_supported: ["code"],
          subject_types_supported: ["public"],
          id_token_signing_alg_values_supported: ["RS256"],
          token_endpoint_auth_methods_supported: ["none"],
          code_challenge_methods_supported: ["S256"],
        });
      if (req.method === "GET" && url.pathname === "/keys") return json(res, { keys: [jwk] });
      if (req.method === "GET" && url.pathname === "/authorize") {
        if (
          q.get("client_id") !== "studio" ||
          q.get("redirect_uri") !== redirect ||
          q.get("response_type") !== "code" ||
          q.get("code_challenge_method") !== "S256" ||
          !/^[A-Za-z0-9_-]{43}$/.test(q.get("code_challenge") ?? "") ||
          !q.get("state") ||
          !q.get("nonce")
        )
          return json(res, { error: "invalid_request" }, 400);
        const subject = q.get("user");
        if (!subject) {
          // URLSearchParams escapes markup-sensitive query characters in generated links.
          const links = Object.entries(names)
            .map(([id, name]) => {
              const params = new URLSearchParams(q);
              params.set("user", id);
              return `<p><a href="/authorize?${params.toString().replaceAll("&", "&amp;")}">Войти как ${name}</a></p>`;
            })
            .join("");
          res.writeHead(200, {
            "Content-Type": "text/html; charset=utf-8",
            "Cache-Control": "no-store",
            "Content-Security-Policy":
              "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'",
          });
          res.end(
            `<!doctype html><html lang="ru"><meta charset="utf-8"><title>Локальный тестовый вход</title><body style="background:#111;color:#ededed;font:18px system-ui;padding:40px"><h1>Локальный тестовый вход</h1><p>Только для разработки. Выберите тестового пользователя.</p>${links}</body></html>`,
          );
          return;
        }
        if (!Object.hasOwn(names, subject)) return json(res, { error: "invalid_request" }, 400);
        for (const [key, grant] of codes) if (grant.expires < Date.now()) codes.delete(key);
        if (codes.size >= 1024) return json(res, { error: "temporarily_unavailable" }, 503);
        const code = randomBytes(32).toString("base64url");
        codes.set(code, {
          subject,
          nonce: q.get("nonce"),
          challenge: q.get("code_challenge"),
          expires: Date.now() + 300_000,
        });
        const callback = new URL(redirect);
        callback.searchParams.set("state", q.get("state"));
        callback.searchParams.set("code", code);
        res.writeHead(302, { Location: callback.href, "Cache-Control": "no-store" });
        res.end();
        return;
      }
      if (req.method === "POST" && url.pathname === "/token") {
        let body = "";
        for await (const part of req) {
          body += part;
          if (body.length > 8192) return json(res, { error: "invalid_request" }, 413);
        }
        const form = new URLSearchParams(body),
          code = form.get("code"),
          grant = codes.get(code);
        codes.delete(code);
        const client =
          form.get("client_id") ??
          (req.headers.authorization?.startsWith("Basic ")
            ? Buffer.from(req.headers.authorization.slice(6), "base64").toString().split(":")[0]
            : "");
        if (
          !grant ||
          grant.expires < Date.now() ||
          client !== "studio" ||
          form.get("grant_type") !== "authorization_code" ||
          form.get("redirect_uri") !== redirect ||
          createHash("sha256")
            .update(form.get("code_verifier") ?? "")
            .digest("base64url") !== grant.challenge
        )
          return json(res, { error: "invalid_grant" }, 400);
        const now = Math.floor(Date.now() / 1000);
        const encode = (value) => Buffer.from(JSON.stringify(value)).toString("base64url");
        const payload =
          encode({ alg: "RS256", kid: "local-dev", typ: "JWT" }) +
          "." +
          encode({
            iss: issuer,
            sub: grant.subject,
            aud: "studio",
            iat: now,
            exp: now + 300,
            nonce: grant.nonce,
            name: names[grant.subject],
          });
        return json(res, {
          access_token: randomBytes(32).toString("base64url"),
          token_type: "Bearer",
          expires_in: 300,
          id_token:
            payload +
            "." +
            sign("RSA-SHA256", Buffer.from(payload), privateKey).toString("base64url"),
        });
      }
      json(res, { error: "not_found" }, 404);
    } catch {
      json(res, { error: "invalid_request" }, 400);
    }
  });
  return server;
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const port = Number(process.env.DEV_OIDC_PORT ?? 18095),
    studioPort = Number(process.env.DEV_STUDIO_PORT ?? 18094);
  createDevOIDC({ port, studioPort }).listen(port, "127.0.0.1", () =>
    console.log(`Local test OIDC: http://127.0.0.1:${port}`),
  );
}
