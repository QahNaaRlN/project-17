// STU-540…555: `pnpm load` — нагрузочный прогон NFR-001…007 одной командой.
//
// 1. PostgreSQL 16 и Redis 7 в контейнерах (или CMS_DATABASE_URL / CMS_REDIS_URL).
// 2. `cms migrate up`, генератор данных профиля 01 §5 (`loadseed`).
// 3. `cms serve` и кэширующий прокси nginx; k6 (`perf/k6/load.js`, LOAD_MODE=measure).
// 4. NFR-005: сценарий §2 (`pnpm e2e:scenario`) под фоновой нагрузкой k6 со строгим
//    порогом 400 мс (решение §11.5).
// Результаты — perf/results/: сводка k6, журналы, окружение; секреты — только *.private.*.
// LOAD_DURATION, LOAD_WARMUP, LOAD_RPS, LOAD_PROJECTS, LOAD_ENTITIES, LOAD_DOCUMENTS
// уменьшают прогон для отладки; такой прогон не подтверждает NFR (STU-555).
import { execFileSync, spawn, spawnSync } from "node:child_process";
import {
  closeSync,
  copyFileSync,
  mkdirSync,
  openSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { cpus, totalmem, platform, release } from "node:os";
import { join, resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { repo, ready } from "../scripts/e2e-stand.mjs";

const env = process.env;
const results = resolve(env.LOAD_RESULTS ?? join(repo, "perf", "results"));
rmSync(results, { recursive: true, force: true });
mkdirSync(results, { recursive: true });
const bin = join(repo, "perf", ".bin");
mkdirSync(bin, { recursive: true });
const exe = platform() === "win32" ? ".exe" : "";
const cms = join(bin, "cms" + exe);
const seedBin = join(bin, "loadseed" + exe);
// Linux runners reach loopback services from containers through the host network.
const hostNetwork = platform() === "linux";
const dockerHost = hostNetwork ? "127.0.0.1" : "host.docker.internal";
const K6_IMAGE = "grafana/k6:1.0.0";
const NGINX_IMAGE = "mirror.gcr.io/library/nginx:1.27-alpine";
const ports = { api: 18290, proxy: 18291, postgres: 55432, redis: 56379, scenario: 18190 };
const containers = [];
const children = [];

const run = (command, args, options = {}) =>
  execFileSync(command, args, { cwd: repo, stdio: "inherit", windowsHide: true, ...options });
const docker = (args) => spawnSync("docker", args, { encoding: "utf8", windowsHide: true });
const log = (...a) => console.log("[load]", ...a);

function container(name, args) {
  docker(["rm", "-f", name]);
  const r = docker(["run", "-d", "--rm", "--name", name, ...args]);
  if (r.status !== 0) throw Error(`docker run ${name}: ${r.stderr}`);
  containers.push(name);
}

function start(command, args, extra, file) {
  const fd = openSync(join(results, file), "a");
  const child = spawn(command, args, {
    cwd: repo,
    env: { ...env, ...extra },
    stdio: ["ignore", fd, fd],
    windowsHide: true,
  });
  closeSync(fd);
  children.push(child);
  return child;
}

function k6(mode, base, extra = {}) {
  const network = hostNetwork ? ["--network", "host"] : [];
  const vars = {
    LOAD_MODE: mode,
    LOAD_SEED: "/perf/results/seed.private.json",
    LOAD_SUMMARY: `/perf/results/k6-${mode}.json`,
    LOAD_ORIGIN: `http://${dockerHost}:${base}`,
    LOAD_PROXY: `http://${dockerHost}:${ports.proxy}`,
    ...Object.fromEntries(
      Object.entries(env).filter(([k]) =>
        /^LOAD_(RPS|WARMUP|DURATION|MIN_MISSES|PUBLISHES|BACKGROUND_S)$/.test(k),
      ),
    ),
    ...extra,
  };
  const args = [
    "run",
    "--rm",
    "--name",
    "cms-perf-k6-" + mode,
    ...network,
    // The seed file is 0600: k6 reads it as the runner's user (Linux bind mounts keep uids).
    ...(process.getuid ? ["--user", `${process.getuid()}:${process.getgid()}`] : []),
    "-v",
    `${join(repo, "perf")}:/perf`,
  ];
  for (const [k, v] of Object.entries(vars)) args.push("-e", `${k}=${v}`);
  args.push(
    K6_IMAGE,
    "run",
    "--quiet",
    ...(mode === "background" ? ["--no-thresholds"] : []),
    "/perf/k6/load.js",
  );
  return start("docker", args, {}, `k6-${mode}.log`);
}

const exited = (child) => new Promise((done) => child.on("exit", (code) => done(code ?? 1)));

function stopAll() {
  for (const child of children) if (child.exitCode === null) child.kill();
  for (const name of containers) docker(["rm", "-f", name]);
}

async function main() {
  // 1. Builds: the CMS with the e2e tag (the §2 stand needs it), the generator and the apps.
  if (env.LOAD_SKIP_BUILD !== "true") {
    const server = join(repo, "apps/server");
    run("go", ["build", "-tags", "e2e", "-o", cms, "./cmd/cms"], { cwd: server });
    run("go", ["build", "-o", seedBin, "./cmd/loadseed"], { cwd: server });
    // turbo builds the workspace packages the apps depend on first (as the E2E job does).
    run(
      "pnpm",
      [
        "exec",
        "turbo",
        "run",
        "build",
        "--filter",
        "@cms/studio",
        "--filter",
        "@cms/store-example",
        "--filter",
        "@cms/check-runner",
      ],
      { shell: platform() === "win32" },
    );
  }

  // 2. Databases: tuned PostgreSQL for the profile size, Redis without persistence.
  let database = env.CMS_DATABASE_URL;
  if (!database) {
    container("cms-perf-postgres", [
      "-p",
      `${ports.postgres}:5432`,
      "-e",
      "POSTGRES_PASSWORD=load",
      "-e",
      "POSTGRES_DB=load",
      "mirror.gcr.io/library/postgres:16-alpine",
      "-c",
      "shared_buffers=1GB",
      "-c",
      "max_connections=300",
      "-c",
      "synchronous_commit=on",
    ]);
    database = `postgres://postgres:load@127.0.0.1:${ports.postgres}/load?sslmode=disable`;
  }
  let redis = env.CMS_REDIS_URL;
  if (!redis) {
    container("cms-perf-redis", [
      "-p",
      `${ports.redis}:6379`,
      "mirror.gcr.io/library/redis:7-alpine",
      "redis-server",
      "--save",
      "",
      "--appendonly",
      "no",
    ]);
    redis = `redis://127.0.0.1:${ports.redis}/0`;
  }
  const cmsEnv = { CMS_DATABASE_URL: database, CMS_REDIS_URL: redis, CMS_LOG_LEVEL: "warn" };
  for (let i = 0; ; i++) {
    const m = spawnSync(cms, ["migrate", "up"], {
      env: { ...env, ...cmsEnv },
      encoding: "utf8",
      windowsHide: true,
    });
    if (m.status === 0) break;
    if (i === 60) throw Error("migrate: " + m.stderr.slice(0, 500));
    await delay(1000);
  }

  // 3. Profile data (decision §11.4).
  const seedArgs = ["-out", join(results, "seed.private.json")];
  for (const [flag, name] of [
    ["projects", "LOAD_PROJECTS"],
    ["entities", "LOAD_ENTITIES"],
    ["documents", "LOAD_DOCUMENTS"],
  ])
    if (env[name]) seedArgs.push("-" + flag, env[name]);
  const seeded = Date.now();
  run(seedBin, seedArgs, { env: { ...env, ...cmsEnv } });
  const seedSeconds = Math.round((Date.now() - seeded) / 1000);

  // 4. The measured run: CMS, caching proxy, k6.
  const api = start(
    cms,
    ["serve"],
    { ...cmsEnv, CMS_HTTP_ADDR: `127.0.0.1:${ports.api}` },
    "cms.log",
  );
  await ready(`http://127.0.0.1:${ports.api}/healthz`);
  const listen = hostNetwork ? String(ports.proxy) : "8080";
  const conf = readFileSync(join(repo, "perf/stand/nginx.conf.template"), "utf8")
    .replace("${LISTEN}", listen)
    .replace("${UPSTREAM}", `${dockerHost}:${ports.api}`);
  writeFileSync(join(results, "nginx.conf"), conf);
  container("cms-perf-nginx", [
    ...(hostNetwork ? ["--network", "host"] : ["-p", `${ports.proxy}:8080`]),
    "-v",
    `${join(results, "nginx.conf")}:/etc/nginx/conf.d/default.conf:ro`,
    NGINX_IMAGE,
  ]);
  await ready(`http://127.0.0.1:${ports.proxy}/delivery/`, { any: true });
  log("measured run started");
  const measured = await exited(k6("measure", ports.api));
  api.kill(); // the §2 stand starts its own CMS on the same database

  // 5. NFR-005 under the background load (decision §11.5).
  // The §2 runner takes the CMS binary from its stand directory.
  const scenarioDir = join(results, "scenario");
  mkdirSync(scenarioDir, { recursive: true });
  copyFileSync(cms, join(scenarioDir, "cms" + exe));
  const background = k6("background", ports.scenario, { LOAD_WARMUP: "0s" });
  let scenario = 0;
  try {
    run(process.execPath, [join(repo, "scripts/e2e-scenario.mjs")], {
      env: {
        ...env,
        CMS_DATABASE_URL: database,
        CMS_REDIS_URL: redis,
        E2E_SKIP_BUILD: "true",
        E2E_DIR: scenarioDir,
        E2E_NFR005_MAX_MS: "400",
      },
    });
  } catch {
    scenario = 1;
  }
  // NFR-005 counts only under load: a background run that stopped early invalidates it.
  if (background.exitCode !== null) {
    log(`background load stopped early (exit ${background.exitCode}): NFR-005 was not measured under load`);
    scenario = 1;
  }
  background.kill();
  docker(["rm", "-f", "cms-perf-k6-background"]);

  writeFileSync(
    join(results, "environment.json"),
    JSON.stringify(
      {
        commit: spawnSync("git", ["rev-parse", "HEAD"], {
          cwd: repo,
          encoding: "utf8",
        }).stdout.trim(),
        platform: `${platform()} ${release()}`,
        cpus: cpus().length,
        cpu: cpus()[0]?.model,
        memoryGiB: Math.round(totalmem() / 2 ** 30),
        k6: K6_IMAGE,
        nginx: NGINX_IMAGE,
        go: spawnSync("go", ["version"], { encoding: "utf8" }).stdout.trim(),
        seedSeconds,
        generator: "k6 on the same machine as the stand",
        measuredExit: measured,
        scenarioExit: scenario,
      },
      null,
      2,
    ),
  );
  log(`measured run exit ${measured}, §2 scenario under load exit ${scenario}`);
  return measured === 0 && scenario === 0 ? 0 : 1;
}

let code = 1;
try {
  code = await main();
} catch (error) {
  console.error(error);
} finally {
  stopAll();
}
process.exit(code);
