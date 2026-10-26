import { Buffer } from "node:buffer";
import { URL, URLSearchParams } from "node:url";
const { fetch } = globalThis;
import test from "node:test";
import assert from "node:assert/strict";
import { createHash, createPublicKey, verify } from "node:crypto";
import { createServer } from "node:net";
import { once } from "node:events";
import { createDevOIDC } from "./dev-oidc.mjs";

test("local OIDC signs two distinct users and rejects redirect, PKCE and code replay", async (t) => {
  const reserve = createServer().listen(0, "127.0.0.1");
  await once(reserve, "listening");
  const port = reserve.address().port;
  await new Promise((r) => reserve.close(r));
  const provider = createDevOIDC({ port });
  provider.listen(port, "127.0.0.1");
  await once(provider, "listening");
  t.after(() => {
    provider.closeAllConnections();
    provider.close();
  });
  const origin = `http://127.0.0.1:${port}`,
    redirect = "http://127.0.0.1:18094/api/studio/auth/callback";
  const discovery = await (await fetch(origin + "/.well-known/openid-configuration")).json();
  assert.equal(discovery.issuer, origin);
  const { keys } = await (await fetch(discovery.jwks_uri)).json();
  const verifier = "a".repeat(43),
    challenge = createHash("sha256").update(verifier).digest("base64url");
  const params = new URLSearchParams({
    client_id: "studio",
    redirect_uri: redirect,
    response_type: "code",
    code_challenge_method: "S256",
    code_challenge: challenge,
    state: "browser-state",
    nonce: "nonce",
  });
  const authorize = async (user) =>
    fetch(origin + "/authorize?" + new URLSearchParams({ ...Object.fromEntries(params), user }), {
      redirect: "manual",
    });
  const exchange = async (code, v = verifier) =>
    fetch(origin + "/token", {
      method: "POST",
      body: new URLSearchParams({
        grant_type: "authorization_code",
        client_id: "studio",
        redirect_uri: redirect,
        code,
        code_verifier: v,
      }),
    });
  const selection = await fetch(origin + "/authorize?" + params);
  assert.match(await selection.text(), /Войти как Alice/);
  for (const user of ["alice", "bob", "erin"]) {
    const result = await authorize(user);
    assert.equal(result.status, 302);
    const location = new URL(result.headers.get("location"));
    assert.equal(location.searchParams.get("state"), "browser-state");
    const code = location.searchParams.get("code");
    const token = await (await exchange(code)).json();
    const [header, payload, signature] = token.id_token.split(".");
    assert.equal(
      verify(
        "RSA-SHA256",
        Buffer.from(header + "." + payload),
        createPublicKey({ key: keys[0], format: "jwk" }),
        Buffer.from(signature, "base64url"),
      ),
      true,
    );
    const claims = JSON.parse(Buffer.from(payload, "base64url"));
    assert.equal(claims.sub, user);
    assert.equal(claims.nonce, "nonce");
    assert.equal(claims.iss, origin);
    assert.equal(claims.aud, "studio");
    assert.ok(claims.exp > Math.floor(Date.now() / 1000));
    assert.equal((await exchange(code)).status, 400);
  }
  const result = await authorize("alice");
  assert.equal(
    (await exchange(new URL(result.headers.get("location")).searchParams.get("code"), "wrong"))
      .status,
    400,
  );
  params.set("redirect_uri", "https://other.invalid/callback");
  assert.equal((await authorize("alice")).status, 400);
  assert.throws(() => createDevOIDC({ port: 80 }), /loopback/);
});
