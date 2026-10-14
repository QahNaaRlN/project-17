import { setTimeout as delay } from "node:timers/promises";
const { fetch, AbortSignal } = globalThis;
// Isolated development stand. Requires a migrated loopback PostgreSQL and built CMS/Studio/store.
import { spawn, spawnSync } from "node:child_process";
import { readFileSync, writeFileSync, openSync, existsSync, mkdirSync } from "node:fs";
import { randomUUID } from "node:crypto";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath, URL } from "node:url";
import { createServer } from "node:net";
if (!process.env.CMS_REVIEW_DIR || !process.env.CMS_REVIEW_BINARY || !process.env.CMS_DATABASE_URL)
  throw Error("CMS_REVIEW_DIR, CMS_REVIEW_BINARY and CMS_DATABASE_URL are required");
const local = resolve(process.env.CMS_REVIEW_DIR),
  repo = dirname(dirname(fileURLToPath(import.meta.url)));
const database = new URL(process.env.CMS_DATABASE_URL);
if (!["postgres:", "postgresql:"].includes(database.protocol) || database.hostname !== "127.0.0.1")
  throw Error("Only a loopback development database is allowed");
const binary = resolve(process.env.CMS_REVIEW_BINARY);
const env = {
  ...process.env,
  CMS_HTTP_ADDR: "127.0.0.1:18093",
  CMS_OIDC_ISSUER: "http://127.0.0.1:18095",
  CMS_OIDC_CLIENT_ID: "studio",
  CMS_STUDIO_PUBLIC_URL: "http://127.0.0.1:18094",
  CMS_OIDC_ALLOW_LOOPBACK_HTTP: "true",
};
mkdirSync(local, { recursive: true });
if (existsSync(join(local, "studio-work-admin.private.json")))
  throw Error("Use a fresh review directory and project database");
const children = [];
const stop = () => {
  for (const child of children) if (child.exitCode === null) child.kill();
};
process.on("SIGINT", () => {
  stop();
  process.exit(0);
});
process.on("SIGTERM", () => {
  stop();
  process.exit(0);
});
process.on("uncaughtException", () => {
  stop();
  console.error("Local stand setup failed; inspect local logs.");
  process.exit(1);
});
const project = "studio-work-review";
for (const port of [18093, 18094, 18095, 18096]) {
  const probe = createServer();
  await new Promise((resolve, reject) => {
    probe.once("error", reject);
    probe.listen(port, "127.0.0.1", resolve);
  });
  await new Promise((resolve) => probe.close(resolve));
}
const start = (binary, args, extra, log) => {
  const fd = openSync(join(local, log), "a");
  const child = spawn(binary, args, {
    cwd: repo,
    env: { ...env, ...extra },
    windowsHide: true,
    stdio: ["ignore", fd, fd],
  });
  children.push(child);
  child.once("error", () => {
    stop();
    process.exit(1);
  });
  return child.pid;
};
const oidcPid = start(
  process.execPath,
  [join(repo, "scripts", "dev-oidc.mjs")],
  {},
  "studio-work-oidc.log",
);
const ready = async (url) => {
  for (let i = 0; i < 60; i++) {
    try {
      const r = await fetch(url, { signal: AbortSignal.timeout(1000) });
      if (r.ok) return;
    } catch {
      /* Retry while the local service starts. */
    }
    await delay(250);
  }
  throw Error("Local service startup failed");
};
await ready("http://127.0.0.1:18095/.well-known/openid-configuration");
const boot = spawnSync(
  binary,
  ["bootstrap", "-slug", project, "-name", "Studio Work Review", "-token-ttl", "8h"],
  { env, encoding: "utf8", windowsHide: true },
);
if (boot.status !== 0) throw Error("Local bootstrap failed");
const credential = boot.stdout.match(/cms_svc_[a-zA-Z0-9_-]+/)[0];
const apiPid = start(binary, ["serve"], {}, "studio-work-api.log");
await ready("http://127.0.0.1:18093/healthz");
const baseUrl = "http://127.0.0.1:18093";
const command = async (name, payload) => {
  const r = await fetch(baseUrl + "/api/v1/commands/" + name, {
    method: "POST",
    headers: {
      Authorization: "Bearer " + credential,
      "X-CMS-Project": project,
      "Content-Type": "application/json",
      "Idempotency-Key": randomUUID(),
    },
    body: JSON.stringify({ payload, reason: "Isolated two-person Studio review setup" }),
  });
  const d = await r.json();
  if (!r.ok) throw Error(name + " " + r.status + " " + d.code);
  return d.result;
};
await command("register-manifest", {
  environment: "staging",
  manifest: JSON.parse(
    readFileSync(join(repo, "examples", "react-store", ".cms", "manifest.json"), "utf8"),
  ),
});
const author = await command("create-role", {
  name: "studio-author",
  capabilities: [
    "design.read",
    "content.read",
    "design.compose",
    "content.write",
    "content.publish",
  ],
});
const reviewer = await command("create-role", {
  name: "studio-reviewer",
  capabilities: ["design.read", "content.read", "content.publish"],
});
const studioFile = join(local, "studio-work-ui.private.json");
writeFileSync(
  studioFile,
  JSON.stringify({
    baseUrl,
    project,
    environment: "staging",
    authMode: "oidc",
    studioOrigin: "http://127.0.0.1:18094",
    appOrigin: "http://127.0.0.1:18096",
  }),
);
const delivery = await command("create-delivery-key", {
  name: "local-test-store",
  environment: "staging",
});
const storeFile = join(local, "studio-work-store.private.json");
writeFileSync(
  storeFile,
  JSON.stringify({
    baseUrl,
    project,
    environment: "staging",
    path: "/store",
    deliveryKey: delivery.key,
  }),
);
const studioPid = start(
  process.execPath,
  [join(repo, "apps", "studio", "dist", "server", "server.js")],
  { CMS_STUDIO_CONFIG_FILE: studioFile },
  "studio-work-ui.log",
);
const storePid = start(
  process.execPath,
  [join(repo, "examples", "react-store", "dist", "server", "server.js")],
  {
    NODE_ENV: "production",
    PORT: "18096",
    CMS_STORE_CONFIG_FILE: storeFile,
    CMS_STUDIO_ORIGIN: "http://127.0.0.1:18094",
    CMS_APP_ORIGIN: "http://127.0.0.1:18096",
  },
  "studio-work-store.log",
);
writeFileSync(
  join(local, "studio-work-admin.private.json"),
  JSON.stringify({
    baseUrl,
    project,
    credential,
    authorRole: author.id,
    reviewerRole: reviewer.id,
  }),
);
writeFileSync(
  join(local, "studio-work-review.processes.json"),
  JSON.stringify({ oidcPid, apiPid, studioPid, storePid }),
);
await ready("http://127.0.0.1:18094/api/studio/config");
console.log("Live two-user stand ready at http://127.0.0.1:18094");
