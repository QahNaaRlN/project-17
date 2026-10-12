import { afterEach, expect, it, vi } from "vitest";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { checkConfig, createStudioHandler, type ServerConfig } from "../src/handler.js";
import { identityCookies } from "../src/identity-proxy.js";
import { changesetId } from "./fixture.js";
const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const c of cleanups.splice(0)) await c();
});
async function setup() {
  const fetcher = vi.fn<typeof fetch>(async () =>
    Response.json({ authenticated: false, rights: [], hasAccess: false }),
  );
  const server = createServer((req, res) => {
    void handler(req, res);
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  const origin = "http://127.0.0.1:" + (server.address() as AddressInfo).port;
  const config: ServerConfig = {
    baseUrl: "http://127.0.0.1:18080",
    studioOrigin: origin,
    appOrigin: "http://127.0.0.1:18083",
    project: "store",
    environment: "staging",
    authMode: "oidc",
  };
  const handler = createStudioHandler(config, ".", fetcher);
  cleanups.push(() => new Promise((r) => server.close(() => r())));
  return { fetcher, origin, config };
}
it("STU-080/082: identity mode forbids credentials and non-loopback plain HTTP", () => {
  const c = {
    baseUrl: "https://api.example",
    studioOrigin: "https://studio.example",
    appOrigin: "https://app.example",
    project: "store",
    environment: "staging",
    authMode: "oidc" as const,
  };
  expect(checkConfig(c)).toEqual(c);
  expect(() => checkConfig({ ...c, credential: "cms_svc_secret" })).toThrow();
  expect(() => checkConfig({ ...c, studioOrigin: "http://studio.example" })).toThrow();
  expect(
    identityCookies(
      "other=secret; cms_session=" + "a".repeat(43) + "; __Host-cms_login=" + "b".repeat(43),
    ),
  ).toBe("cms_session=" + "a".repeat(43) + "; __Host-cms_login=" + "b".repeat(43));
  expect(identityCookies()).toBe("");
});
it("STU-081/082: adapter forwards only human cookies and CSRF, never browser Authorization or a service fallback", async () => {
  const { origin, fetcher } = await setup();
  await fetch(origin + "/api/studio/auth/session", {
    headers: {
      Cookie: "other=secret; cms_session=" + "a".repeat(43),
      Authorization: "Bearer attacker",
    },
  });
  expect(fetcher.mock.calls.at(-1)![1]!.headers).toMatchObject({
    Cookie: "cms_session=" + "a".repeat(43),
    "X-CMS-Project": "store",
  });
  expect(JSON.stringify(fetcher.mock.calls)).not.toContain("attacker");
  expect(JSON.stringify(fetcher.mock.calls)).not.toContain("other=secret");
  const result = await fetch(origin + "/api/studio/api/v1/commands/submit-changeset", {
    method: "POST",
    headers: {
      Origin: origin,
      "Content-Type": "application/json",
      "Idempotency-Key": "key",
      Cookie: "cms_session=" + "a".repeat(43),
      "X-CSRF-Token": "b".repeat(43),
    },
    body: JSON.stringify({ payload: { changesetId, expectedSeq: 1 } }),
  });
  expect(result.status).toBe(200);
  expect(fetcher.mock.calls.at(-1)![1]!.headers).toMatchObject({
    "X-CSRF-Token": "b".repeat(43),
    Origin: origin,
  });
  for (const path of ["/auth/other", "/auth/session?next=evil", "/auth/logout"]) {
    expect((await fetch(origin + "/api/studio" + path)).status).toBeGreaterThanOrEqual(400);
  }
});
it("STU-080: cross-site callback is allowed only on its exact path; redirects/cookies are constrained", async () => {
  const { origin, fetcher } = await setup();
  fetcher.mockResolvedValueOnce(
    new Response(null, {
      status: 302,
      headers: {
        Location: origin + "/editor",
        "Set-Cookie": "cms_session=" + "a".repeat(43) + "; Path=/; HttpOnly; SameSite=Lax",
      },
    }),
  );
  const response = await fetch(origin + "/api/studio/auth/callback?state=x&code=y", {
    redirect: "manual",
    headers: { "Sec-Fetch-Site": "cross-site" },
  });
  expect(response.status).toBe(302);
  expect(response.headers.get("set-cookie")).toContain("HttpOnly");
  expect(
    (
      await fetch(origin + "/api/studio/auth/session", {
        headers: { "Sec-Fetch-Site": "cross-site" },
      })
    ).status,
  ).toBe(403);
  fetcher.mockResolvedValueOnce(
    new Response(null, { status: 302, headers: { Location: "https://evil.example/editor" } }),
  );
  expect((await fetch(origin + "/api/studio/auth/callback", { redirect: "manual" })).status).toBe(
    502,
  );
  fetcher.mockResolvedValueOnce(
    new Response(null, { status: 302, headers: { Location: "https://issuer.example/auth" } }),
  );
  expect((await fetch(origin + "/api/studio/auth/login", { redirect: "manual" })).status).toBe(302);
  fetcher.mockResolvedValueOnce(
    new Response(null, { status: 302, headers: { Location: "javascript://localhost/x" } }),
  );
  expect((await fetch(origin + "/api/studio/auth/login", { redirect: "manual" })).status).toBe(502);
  fetcher.mockResolvedValueOnce(Response.json({ detail: "secret" }, { status: 503 }));
  const failed = await fetch(origin + "/api/studio/auth/session");
  expect(await failed.text()).not.toContain("secret");
  expect(
    (
      await fetch(origin + "/api/studio/auth/logout", {
        method: "POST",
        headers: { Origin: "https://evil.example" },
      })
    ).status,
  ).toBe(403);
});
