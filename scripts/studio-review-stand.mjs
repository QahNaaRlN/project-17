import { setTimeout as delay } from "node:timers/promises";
const { fetch, AbortSignal } = globalThis;
// Isolated development stand. Requires a migrated loopback PostgreSQL and built CMS/Studio/store.
import { spawn, spawnSync } from "node:child_process";
import { readFileSync, writeFileSync, openSync, existsSync, mkdirSync } from "node:fs";
import { randomUUID, randomBytes } from "node:crypto";
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
const basePort = Number(process.env.CMS_REVIEW_BASE_PORT ?? 18093);
if (!Number.isInteger(basePort) || basePort < 1024 || basePort > 65532)
  throw Error("Invalid loopback port range");
const apiOrigin = `http://127.0.0.1:${basePort}`,
  studioOrigin = `http://127.0.0.1:${basePort + 1}`,
  oidcOrigin = `http://127.0.0.1:${basePort + 2}`,
  appOrigin = `http://127.0.0.1:${basePort + 3}`;

const env = {
  ...process.env,
  CMS_HTTP_ADDR: `127.0.0.1:${basePort}`,
  CMS_OIDC_ISSUER: oidcOrigin,
  CMS_OIDC_CLIENT_ID: "studio",
  CMS_STUDIO_PUBLIC_URL: studioOrigin,
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
for (const port of [basePort, basePort + 1, basePort + 2, basePort + 3]) {
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
  { DEV_OIDC_PORT: String(basePort + 2), DEV_STUDIO_PORT: String(basePort + 1) },
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
await ready(oidcOrigin + "/.well-known/openid-configuration");
const boot = spawnSync(
  binary,
  ["bootstrap", "-slug", project, "-name", "Studio Work Review", "-token-ttl", "8h"],
  { env, encoding: "utf8", windowsHide: true },
);
if (boot.status !== 0) throw Error("Local bootstrap failed");
const credential = boot.stdout.match(/cms_svc_[a-zA-Z0-9_-]+/)[0];
const projectId = boot.stdout.match(/id ([a-f0-9-]{36})/)[1];
const capabilityDeliveryStand = process.env.CMS_REVIEW_CAPABILITY_DELIVERY === "true";
let receiverPid;
if (capabilityDeliveryStand) {
  const receiverPort = Number(process.env.CAPABILITY_RECEIVER_PORT ?? basePort + 4),
    key = randomBytes(32).toString("base64");
  const receiverConfig = join(local, "receiver.private.json"),
    receiverDatabase = join(local, "receiver-inbox.sqlite"),
    receiverMode = join(local, "receiver-mode.txt"),
    webhookConfig = join(local, "webhooks.private.json");
  writeFileSync(receiverConfig, JSON.stringify({ bindings: [], projectId, key }));
  writeFileSync(receiverMode, "drop-first");
  writeFileSync(
    webhookConfig,
    JSON.stringify({
      endpoints: [
        {
          endpointId: "engineering",
          projectId,
          environment: "staging",
          url: `http://127.0.0.1:${receiverPort}/context`,
          activeKeyId: "local-v1",
          keys: { "local-v1": key },
        },
      ],
    }),
  );
  env.CMS_CAPABILITY_WEBHOOK_CONFIG_FILE = webhookConfig;
  env.CMS_CAPABILITY_WEBHOOK_ALLOW_LOOPBACK_HTTP = "true";
  receiverPid = start(
    process.execPath,
    [join(repo, "scripts/capability-receiver.mjs")],
    {
      CAPABILITY_RECEIVER_CONFIG_FILE: receiverConfig,
      CAPABILITY_RECEIVER_DATABASE: receiverDatabase,
      CAPABILITY_RECEIVER_MODE_FILE: receiverMode,
      CAPABILITY_RECEIVER_PORT: String(receiverPort),
    },
    "receiver.log",
  );
  await ready(`http://127.0.0.1:${receiverPort}/healthz`);
}
const apiPid = start(binary, ["serve"], {}, "studio-work-api.log");
await ready(apiOrigin + "/healthz");
const baseUrl = apiOrigin;
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
const reviewManifest = JSON.parse(
  readFileSync(join(repo, "examples", "react-store", ".cms", "manifest.json"), "utf8"),
);
const capabilityStand = process.env.CMS_REVIEW_CAPABILITIES === "true";
if (!capabilityStand)
  reviewManifest.schemas = {
    ...reviewManifest.schemas,
    Article: {
      version: 1,
      title: "Article",
      fields: {
        title: { type: "text", localized: true },
        ...(process.env.CMS_REVIEW_BINDINGS === "true"
          ? { cover: { type: "asset", assetKind: "image" }, alt: { type: "text", localized: true } }
          : {}),
      },
    },
  };
if (process.env.CMS_REVIEW_DATA_SOURCES === "true")
  reviewManifest.schemas.Collection = {
    version: 1,
    title: "Collection",
    fields: {
      title: { type: "text", localized: true },
      handle: { type: "string", required: true, localized: true, maxLength: 64 },
    },
  };
let tokenFile;
if (capabilityStand) {
  tokenFile = join(local, "studio-code-index-token.private.txt");
  writeFileSync(tokenFile, credential);
  const push = spawnSync(
    process.execPath,
    [
      join(repo, "packages/component-registry/bin/cms.mjs"),
      "manifest",
      "push",
      "--dir",
      join(repo, "examples/react-store/.cms"),
      "--base-url",
      baseUrl,
      "--project",
      project,
      "--env",
      "staging",
      "--token-file",
      tokenFile,
    ],
    { env, encoding: "utf8", windowsHide: true },
  );
  if (push.status !== 0)
    throw Error("Initial CLI index/manifest registration failed: " + push.stderr.slice(0, 300));
} else await command("register-manifest", { environment: "staging", manifest: reviewManifest });
const author = await command("create-role", {
  name: "studio-author",
  capabilities: [
    "design.read",
    "content.read",
    "design.compose",
    ...(process.env.CMS_REVIEW_BEHAVIOR === "true" ? ["behavior.use"] : []),
    ...(capabilityStand ? ["behavior.use", "capability.request", "design.zones.manage"] : []),
    ...(capabilityDeliveryStand ? ["capability.deliver"] : []),
    "content.write",
    "asset.write",
    "content.publish",
  ],
});
const reviewer = await command("create-role", {
  name: "studio-reviewer",
  capabilities: [
    "design.read",
    "content.read",
    "content.publish",
    ...(capabilityStand ? ["code.propose"] : []),
  ],
});
const deliveryAdmin = capabilityDeliveryStand
  ? await command("create-role", { name: "delivery-operator", capabilities: ["project.admin"] })
  : null;
const studioFile = join(local, "studio-work-ui.private.json");
writeFileSync(
  studioFile,
  JSON.stringify({
    baseUrl,
    project,
    environment: "staging",
    authMode: "oidc",
    ...(env.CMS_S3_ENDPOINT
      ? {
          assetUploadOrigin: `${env.CMS_S3_SECURE === "false" ? "http" : "https"}://${env.CMS_S3_ENDPOINT}`,
        }
      : {}),
    studioOrigin,
    appOrigin,
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
    PORT: String(basePort + 3),
    CMS_STORE_CONFIG_FILE: storeFile,
    CMS_STUDIO_ORIGIN: studioOrigin,
    CMS_APP_ORIGIN: appOrigin,
  },
  "studio-work-store.log",
);
writeFileSync(
  join(local, "studio-work-admin.private.json"),
  JSON.stringify({
    baseUrl,
    project,
    credential,
    projectId,
    ...(deliveryAdmin ? { deliveryAdminRole: deliveryAdmin.id } : {}),
    authorRole: author.id,
    reviewerRole: reviewer.id,
    studioOrigin,
    appOrigin,
    ...(tokenFile ? { tokenFile } : {}),
  }),
);
writeFileSync(
  join(local, "studio-work-review.processes.json"),
  JSON.stringify({ oidcPid, apiPid, studioPid, storePid, ...(receiverPid ? { receiverPid } : {}) }),
);
await ready(studioOrigin + "/api/studio/config");
console.log("Live two-user stand ready at " + studioOrigin);
