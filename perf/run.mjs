// STU-540…555: `pnpm load` — нагрузочный прогон NFR-001…007 одной командой.
//
// 1. PostgreSQL 16 и Redis 7 в контейнерах (или CMS_DATABASE_URL / CMS_REDIS_URL).
// 2. `cms migrate up`, генератор данных профиля 01 §5 (`loadseed`).
// 3. `cms serve` и кэширующий прокси nginx; k6 (`perf/k6/load.js`, LOAD_MODE=measure).
// 4. NFR-005: сценарий §2 (`pnpm e2e:scenario`) под фоновой нагрузкой k6 со строгим
//    порогом 400 мс (решение §11.5). Во время сценария — загрузка CPU по процессам
//    (cpu.jsonl, cpu-summary.json) и CPU-профили его CMS (profiles/*.pprof).
// Результаты — perf/results/: сводка k6, журналы, окружение; секреты — только *.private.*.
// LOAD_DURATION, LOAD_WARMUP, LOAD_RPS, LOAD_PROJECTS, LOAD_ENTITIES, LOAD_DOCUMENTS
// уменьшают прогон для отладки; такой прогон не подтверждает NFR (STU-555).
import { execFileSync, spawn, spawnSync } from "node:child_process";
import {
  closeSync,
  copyFileSync,
  mkdirSync,
  openSync,
  readdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { cpus, totalmem, platform, release } from "node:os";
import { join, resolve } from "node:path";
import { Buffer } from "node:buffer";
import { clearInterval, setInterval } from "node:timers";
import { setTimeout as delay } from "node:timers/promises";
import { repo, ready } from "../scripts/e2e-stand.mjs";

const env = process.env;
const { fetch } = globalThis;
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
const ports = {
  api: 18290,
  proxy: 18291,
  pprof: 18292,
  postgres: 55432,
  redis: 56379,
  scenario: 18190,
};
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

// CPU of the machine and of each process name while §2 runs (Linux /proc): shows whether
// NFR-005 waits for the processor. Writes cpu.jsonl, returns the summary.
function sampleCpu(file, seconds = 5) {
  if (platform() !== "linux") return async () => null;
  const tick = 100; // USER_HZ on Linux
  const machine = () =>
    readFileSync("/proc/stat", "utf8").split("\n")[0].trim().split(/\s+/).slice(1).map(Number);
  const processes = () => {
    const m = new Map();
    for (const pid of readdirSync("/proc")) {
      if (!/^\d+$/.test(pid)) continue;
      try {
        const s = readFileSync(`/proc/${pid}/stat`, "utf8");
        const close = s.lastIndexOf(")");
        const f = s.slice(close + 2).split(" ");
        m.set(pid, { comm: s.slice(s.indexOf("(") + 1, close), ticks: +f[11] + +f[12] });
      } catch {
        // the process exited between readdir and read
      }
    }
    return m;
  };
  const samples = [];
  let last = { at: Date.now(), cpu: machine(), procs: processes() };
  const timer = setInterval(() => {
    const now = { at: Date.now(), cpu: machine(), procs: processes() };
    const d = now.cpu.map((v, i) => v - last.cpu[i]);
    const sum = d.reduce((a, b) => a + b, 0) || 1;
    const elapsed = (now.at - last.at) / 1000;
    const cores = {};
    for (const [pid, p] of now.procs) {
      const used = (p.ticks - (last.procs.get(pid)?.ticks ?? p.ticks)) / tick / elapsed;
      if (used > 0) cores[p.comm] = (cores[p.comm] ?? 0) + used;
    }
    const sample = {
      t: new Date(now.at).toISOString(),
      busy: +(1 - (d[3] + d[4]) / sum).toFixed(3),
      iowait: +(d[4] / sum).toFixed(3),
      steal: +((d[7] ?? 0) / sum).toFixed(3),
      load1: +readFileSync("/proc/loadavg", "utf8").split(" ")[0],
      cores: Object.fromEntries(
        Object.entries(cores)
          .sort((a, b) => b[1] - a[1])
          .slice(0, 12)
          .map(([k, v]) => [k, +v.toFixed(2)]),
      ),
    };
    samples.push(sample);
    writeFileSync(file, JSON.stringify(sample) + "\n", { flag: "a" });
    last = now;
  }, seconds * 1000);
  return async () => {
    clearInterval(timer);
    if (!samples.length) return null;
    const busy = samples.map((s) => s.busy).sort((a, b) => a - b);
    const mean = (xs) => xs.reduce((a, b) => a + b, 0) / xs.length;
    const byName = {};
    for (const s of samples)
      for (const [k, v] of Object.entries(s.cores)) byName[k] = (byName[k] ?? 0) + v;
    return {
      cpus: cpus().length,
      samples: samples.length,
      busyMean: +mean(busy).toFixed(3),
      busyP95: busy[Math.floor(busy.length * 0.95)],
      saturatedShare: +(busy.filter((b) => b > 0.9).length / busy.length).toFixed(3),
      iowaitMean: +mean(samples.map((s) => s.iowait)).toFixed(3),
      stealMean: +mean(samples.map((s) => s.steal)).toFixed(3),
      load1Max: Math.max(...samples.map((s) => s.load1)),
      meanCores: Object.fromEntries(
        Object.entries(byName)
          .sort((a, b) => b[1] - a[1])
          .slice(0, 12)
          .map(([k, v]) => [k, +(v / samples.length).toFixed(2)]),
      ),
    };
  };
}

// Back-to-back CPU profiles of the §2 stand's CMS until `done` (CMS_E2E_PPROF_ADDR, e2e build).
async function profileCms(dir, done) {
  let n = 0;
  while (!done()) {
    const started = new Date().toISOString().replace(/[:.]/g, "-");
    try {
      const r = await fetch(`http://127.0.0.1:${ports.pprof}/debug/pprof/profile?seconds=20`);
      if (!r.ok) throw Error(`pprof ${r.status}`);
      writeFileSync(
        join(dir, `cms-cpu-${String(++n).padStart(3, "0")}-${started}.pprof`),
        Buffer.from(await r.arrayBuffer()),
      );
    } catch {
      await delay(1000); // the stand's CMS is not up yet or has stopped
    }
  }
  return n;
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
      // Docker's default 64 MB /dev/shm breaks parallel queries (SQLSTATE 53100).
      "--shm-size=1g",
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
  const profiles = join(results, "profiles");
  mkdirSync(profiles, { recursive: true });
  const stopCpu = sampleCpu(join(results, "cpu.jsonl"));
  // Async, unlike run(): the CPU sampler and the profiler work while §2 runs.
  const stand = spawn(process.execPath, [join(repo, "scripts/e2e-scenario.mjs")], {
    cwd: repo,
    stdio: "inherit",
    windowsHide: true,
    env: {
      ...env,
      CMS_DATABASE_URL: database,
      CMS_REDIS_URL: redis,
      CMS_E2E_PPROF_ADDR: `127.0.0.1:${ports.pprof}`,
      E2E_SKIP_BUILD: "true",
      E2E_DIR: scenarioDir,
      E2E_NFR005_MAX_MS: "400",
    },
  });
  children.push(stand);
  const profiled = profileCms(profiles, () => stand.exitCode !== null);
  let scenario = (await exited(stand)) === 0 ? 0 : 1;
  const cpu = await stopCpu();
  log(`CMS CPU profiles during §2: ${await profiled}; CPU: ${JSON.stringify(cpu)}`);
  writeFileSync(join(results, "cpu-summary.json"), JSON.stringify(cpu, null, 2));
  // NFR-005 counts only under load: a background run that stopped early invalidates it.
  if (background.exitCode !== null) {
    log(
      `background load stopped early (exit ${background.exitCode}): NFR-005 was not measured under load`,
    );
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
