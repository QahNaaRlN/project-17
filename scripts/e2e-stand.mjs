// STU-441…446: the stand of the §2 scenario, shared by CI and local runs.
// Real CMS (built with the e2e tag), PostgreSQL, River, Studio-BFF, two store
// processes (staging, production), local OIDC with three people, the CDN purge
// receiver and the GitHub facts fixture. Everything listens on loopback.
// The database must be empty: CI uses a fresh service, locally create one.
import { spawn, spawnSync } from "node:child_process";
import { mkdirSync, openSync, writeFileSync, closeSync } from "node:fs";
import { randomBytes, randomUUID } from "node:crypto";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, URL } from "node:url";
import { setTimeout as delay } from "node:timers/promises";
const { fetch, AbortSignal } = globalThis;

export const repo = dirname(dirname(fileURLToPath(import.meta.url)));
export const project = "e2e-scenario";

/** Waits for a local service. Retries only connection failures (STU-457). */
export async function ready(url, { attempts = 120, any = false } = {}) {
  for (let i = 0; i < attempts; i++) {
    try {
      const r = await fetch(url, { signal: AbortSignal.timeout(1000) });
      if (r.ok || any) return;
    } catch {
      /* the service is still starting */
    }
    await delay(250);
  }
  throw Error("Local service startup failed: " + url);
}

export async function startStand({
  directory = process.env.E2E_DIR,
  binary = process.env.E2E_CMS_BINARY,
  database = process.env.CMS_DATABASE_URL,
  basePort = Number(process.env.E2E_BASE_PORT ?? 18190),
} = {}) {
  if (!directory || !binary || !database)
    throw Error("E2E_DIR, E2E_CMS_BINARY and CMS_DATABASE_URL are required");
  const db = new URL(database);
  if (!["postgres:", "postgresql:"].includes(db.protocol) || db.hostname !== "127.0.0.1")
    throw Error("Only a loopback scenario database is allowed");
  if (!Number.isInteger(basePort) || basePort < 1024 || basePort > 65520)
    throw Error("Invalid loopback port range");
  const local = resolve(directory);
  mkdirSync(local, { recursive: true });
  const port = (n) => basePort + n;
  const origin = (n) => `http://127.0.0.1:${port(n)}`;
  const apiOrigin = origin(0),
    studioOrigin = origin(1),
    oidcOrigin = origin(2),
    stagingOrigin = origin(3),
    purgeOrigin = origin(6),
    githubOrigin = origin(7);
  const purgeToken = randomBytes(24).toString("base64url"),
    githubControl = randomBytes(24).toString("base64url");
  const env = {
    ...process.env,
    CMS_DATABASE_URL: database,
    CMS_HTTP_ADDR: `127.0.0.1:${port(0)}`,
    CMS_OIDC_ISSUER: oidcOrigin,
    CMS_OIDC_CLIENT_ID: "studio",
    CMS_STUDIO_PUBLIC_URL: studioOrigin,
    CMS_OIDC_ALLOW_LOOPBACK_HTTP: "true",
    CMS_CDN_PURGE_URL: purgeOrigin + "/purge",
    CMS_CDN_PURGE_TOKEN: purgeToken,
    CMS_E2E_GITHUB_URL: githubOrigin,
  };
  const children = [];
  const stop = () => {
    for (const child of children) if (child.exitCode === null) child.kill();
  };
  const start = (command, args, extra, log) => {
    const fd = openSync(join(local, log), "a");
    const child = spawn(command, args, {
      cwd: repo,
      env: { ...env, ...extra },
      windowsHide: true,
      stdio: ["ignore", fd, fd],
    });
    closeSync(fd);
    children.push(child);
    return child;
  };
  try {
    const migrate = spawnSync(binary, ["migrate", "up"], {
      env,
      encoding: "utf8",
      windowsHide: true,
    });
    if (migrate.status !== 0) throw Error("Migrations failed: " + migrate.stderr.slice(0, 500));
    start(
      process.execPath,
      [join(repo, "scripts", "dev-oidc.mjs")],
      { DEV_OIDC_PORT: String(port(2)), DEV_STUDIO_PORT: String(port(1)) },
      "oidc.log",
    );
    start(
      process.execPath,
      [join(repo, "scripts", "e2e-purge-receiver.mjs")],
      { E2E_PURGE_PORT: String(port(6)), E2E_PURGE_TOKEN: purgeToken },
      "purge.log",
    );
    start(
      process.execPath,
      [join(repo, "scripts", "e2e-github-fixture.mjs")],
      { E2E_GITHUB_PORT: String(port(7)), E2E_GITHUB_CONTROL_TOKEN: githubControl },
      "github.log",
    );
    await Promise.all([
      ready(oidcOrigin + "/.well-known/openid-configuration"),
      ready(purgeOrigin + "/healthz"),
      ready(githubOrigin + "/healthz"),
    ]);
    const boot = spawnSync(
      binary,
      [
        "bootstrap",
        "-slug",
        project,
        "-name",
        "E2E scenario",
        "-token-ttl",
        "8h",
        "-max-mode",
        "CODE",
      ],
      { env, encoding: "utf8", windowsHide: true },
    );
    if (boot.status !== 0) throw Error("Bootstrap failed: " + boot.stderr.slice(0, 500));
    const credential = boot.stdout.match(/cms_svc_[a-zA-Z0-9_-]+/)[0];
    const projectId = boot.stdout.match(/id ([a-f0-9-]{36})/)[1];
    const githubConfig = join(local, "github.private.json");
    writeFileSync(
      githubConfig,
      JSON.stringify({
        repositories: [
          {
            configId: "local",
            projectId,
            environment: "staging",
            repositoryId: "42",
            owner: "local",
            name: "store",
            baseBranch: "dev",
            credentialId: "local",
            token: "e2e-fixture",
            requiredChecks: [
              { source: "commit_status", name: "ci", target: "head", creatorId: "1" },
            ],
          },
        ],
      }),
    );
    start(binary, ["serve"], { CMS_CAPABILITY_GITHUB_CONFIG_FILE: githubConfig }, "cms.log");
    await ready(apiOrigin + "/healthz");
    const command = async (name, payload) => {
      const r = await fetch(apiOrigin + "/api/v1/commands/" + name, {
        method: "POST",
        headers: {
          Authorization: "Bearer " + credential,
          "X-CMS-Project": project,
          "Content-Type": "application/json",
          "Idempotency-Key": randomUUID(),
        },
        body: JSON.stringify({ payload, reason: "§2 scenario stand setup" }),
      });
      const d = await r.json();
      if (!r.ok) throw Error(name + " " + r.status + " " + d.code);
      return d.result;
    };
    // STU-443: people, their rights and the approval policy.
    const role = (name, capabilities) => command("create-role", { name, capabilities });
    const roles = {
      designer: await role("designer", [
        "design.read",
        "design.compose",
        "design.zones.manage",
        "content.read",
        "content.write",
        "content.publish",
        "component.write",
        "behavior.use",
        "capability.request",
        "agent.delegate",
        "schema.read",
        "schema.apply",
      ]),
      editor: await role("editor", ["content.read", "content.write", "content.publish"]),
      lead: await role("lead-designer", [
        "design.read",
        "content.read",
        "content.publish",
        "code.propose",
      ]),
      admin: await role("project-admin", ["project.admin"]),
    };
    await command("set-approval-policy", { low: 1, medium: 1, high: 1 });
    const stores = {};
    for (const [environment, n] of [
      ["staging", 3],
      ["production", 4],
    ]) {
      const key = await command("create-delivery-key", { name: "e2e-" + environment, environment });
      const file = join(local, `store-${environment}.private.json`);
      writeFileSync(
        file,
        JSON.stringify({
          baseUrl: apiOrigin,
          project,
          environment,
          path: "/store",
          deliveryKey: key.key,
        }),
      );
      stores[environment] = { origin: origin(n), port: port(n), configFile: file };
    }
    const studioFile = join(local, "studio.private.json");
    writeFileSync(
      studioFile,
      JSON.stringify({
        baseUrl: apiOrigin,
        project,
        environment: "staging",
        authMode: "oidc",
        studioOrigin,
        appOrigin: stagingOrigin,
      }),
    );
    start(
      process.execPath,
      [join(repo, "apps", "studio", "dist", "server", "server.js")],
      { CMS_STUDIO_CONFIG_FILE: studioFile },
      "studio.log",
    );
    const tokenFile = join(local, "manifest-token.private.txt");
    writeFileSync(tokenFile, credential);
    await ready(studioOrigin + "/api/studio/config");
    const stand = {
      apiOrigin,
      studioOrigin,
      project,
      projectId,
      credential,
      tokenFile,
      roles: Object.fromEntries(Object.entries(roles).map(([k, v]) => [k, v.id])),
      stores,
      purge: { origin: purgeOrigin, token: purgeToken },
      github: { origin: githubOrigin, control: githubControl },
      directory: local,
    };
    /** Starts (or restarts) the store of an environment from the current build. */
    const storeProcess = {};
    stand.startStore = async (environment) => {
      storeProcess[environment]?.kill();
      const s = stores[environment];
      storeProcess[environment] = start(
        process.execPath,
        [join(repo, "examples", "react-store", "dist", "server", "server.js")],
        {
          NODE_ENV: "production",
          PORT: String(s.port),
          CMS_STORE_CONFIG_FILE: s.configFile,
          ...(environment === "staging"
            ? { CMS_STUDIO_ORIGIN: studioOrigin, CMS_APP_ORIGIN: s.origin }
            : {}),
        },
        `store-${environment}.log`,
      );
      // A store without a published page answers 404: any HTTP answer means it listens.
      await ready(s.origin + "/", { any: true });
    };
    stand.stop = stop;
    return stand;
  } catch (error) {
    stop();
    throw error;
  }
}
