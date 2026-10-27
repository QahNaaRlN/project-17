// Нагрузочные тесты NFR-001…007 (docs/load-tests.md, STU-543…551).
//
// LOAD_MODE=measure (по умолчанию) — полный прогон с порогами:
//   прогрев → Delivery 200 RPS на origin + 50 пользователей Studio + публикации профиля
//   + 10 публикаций и откатов (NFR-006/007) → тот же поток Delivery через прокси (NFR-001).
// LOAD_MODE=background — фон для измерения NFR-005 в сценарии §2 (без порогов).
import http from "k6/http";
import { check, sleep } from "k6";
import exec from "k6/execution";
import { Counter, Rate, Trend } from "k6/metrics";

const seed = __ENV.LOAD_SEED
  ? JSON.parse(open(__ENV.LOAD_SEED))
  : { config: {}, projects: [], editors: [] };
const ORIGIN = __ENV.LOAD_ORIGIN || "http://127.0.0.1:18290";
const PROXY = __ENV.LOAD_PROXY || "http://127.0.0.1:18291";
const MODE = __ENV.LOAD_MODE || "measure";
const RPS = Number(__ENV.LOAD_RPS || 200);
const WARMUP_S = seconds(__ENV.LOAD_WARMUP || "2m");
const DURATION_S = seconds(__ENV.LOAD_DURATION || "10m");
const MIN_MISSES = Number(__ENV.LOAD_MIN_MISSES || 1000);
const PUBLISHES = Number(__ENV.LOAD_PUBLISHES || 10);
const ENTITIES = seed.config.Entities || 1;
const DOCUMENTS = seed.config.Documents || 1;

function seconds(v) {
  const m = /^(\d+)(s|m)$/.exec(v);
  if (!m) throw Error("duration: " + v);
  return Number(m[1]) * (m[2] === "m" ? 60 : 1);
}

// NFR-001…007: measured only after the warm-up.
const cdnHit = new Trend("nfr001_cdn_hit_ms", true);
const originHit = new Trend("nfr002_origin_hit_ms", true);
const originMiss = new Trend("nfr003_origin_miss_ms", true);
const misses = new Counter("nfr003_origin_miss_count");
const apply = new Trend("nfr004_apply_operations_ms", true);
const publish = new Trend("nfr006_publish_ms", true);
const rollback = new Trend("nfr007_rollback_ms", true);
const published = new Counter("nfr006_publish_count");
const rolledBack = new Counter("nfr007_rollback_count");
const limited = new Counter("rate_limited");
const failures = new Rate("request_failures");

const delivery = (name, startTime, duration, base) => ({
  executor: "constant-arrival-rate",
  rate: RPS,
  timeUnit: "1s",
  duration: duration + "s",
  startTime: startTime + "s",
  preAllocatedVUs: Math.max(20, RPS / 2),
  maxVUs: RPS * 2,
  exec: "deliveryRequest",
  env: { BASE: base },
  tags: { scenario_group: name },
});
const studioWindow = WARMUP_S + DURATION_S;
const scenarios = {
  warmup: delivery("warmup", 0, WARMUP_S, ORIGIN),
  delivery_origin: delivery("origin", WARMUP_S, DURATION_S, ORIGIN),
  studio: {
    executor: "constant-vus",
    vus: Number(__ENV.LOAD_STUDIO_VUS || Math.max(1, seed.editors.length)),
    duration: studioWindow + "s",
    exec: "studioUser",
  },
  publications: {
    executor: "constant-arrival-rate",
    // 01 §5: 500 publications a day for every project.
    rate: Math.max(1, Math.round((500 * Math.max(1, seed.projects.length)) / 1440)),
    timeUnit: "1m",
    duration: studioWindow + "s",
    preAllocatedVUs: 3,
    maxVUs: 10,
    exec: "backgroundPublication",
  },
};
if (MODE === "measure") {
  scenarios.publish_rollback = {
    executor: "per-vu-iterations",
    vus: 1,
    iterations: PUBLISHES,
    startTime: WARMUP_S + "s",
    maxDuration: DURATION_S + "s",
    exec: "publishAndRollback",
  };
  scenarios.delivery_proxy = delivery("proxy", studioWindow, DURATION_S, PROXY);
} else {
  delete scenarios.delivery_origin;
  scenarios.warmup.duration = Number(__ENV.LOAD_BACKGROUND_S || 600) + "s";
  scenarios.studio.duration = scenarios.warmup.duration;
  scenarios.publications.duration = scenarios.warmup.duration;
}

// LOAD_ONLY=studio,warmup keeps only the named scenarios (debugging; not a measurement).
if (__ENV.LOAD_ONLY) {
  const keep = __ENV.LOAD_ONLY.split(",");
  for (const name of Object.keys(scenarios)) if (!keep.includes(name)) delete scenarios[name];
}

export const options = {
  scenarios,
  setupTimeout: "5m",
  summaryTrendStats: ["avg", "min", "med", "max", "p(95)", "p(99)", "count"],
  discardResponseBodies: false,
  thresholds:
    MODE === "measure"
      ? {
          nfr001_cdn_hit_ms: ["p(95)<50"],
          nfr002_origin_hit_ms: ["p(95)<80"],
          nfr003_origin_miss_ms: ["p(95)<250"],
          nfr003_origin_miss_count: ["count>=" + MIN_MISSES],
          nfr004_apply_operations_ms: ["p(95)<300"],
          nfr006_publish_ms: ["max<30000"],
          nfr006_publish_count: ["count>=" + PUBLISHES],
          nfr007_rollback_ms: ["max<5000"],
          nfr007_rollback_count: ["count>=" + PUBLISHES],
          rate_limited: ["count==0"],
          request_failures: ["rate<0.01"],
        }
      : {},
};

const measuring = () => exec.instance.currentTestRunDuration / 1000 >= WARMUP_S;

function record(res, expected) {
  if (res.status === 429) limited.add(1);
  const ok = expected.includes(res.status);
  failures.add(!ok);
  return ok;
}

let ids = 0;
function idempotencyKey() {
  ids++;
  return `load-${exec.vu.idInTest}-${exec.vu.iterationInInstance}-${ids}-${Date.now()}`;
}

function command(token, project, name, payload) {
  const res = http.post(
    ORIGIN + "/api/v1/commands/" + name,
    JSON.stringify({ payload, reason: "load test" }),
    {
      headers: {
        Authorization: "Bearer " + token,
        "X-CMS-Project": project,
        "Content-Type": "application/json",
        "Idempotency-Key": idempotencyKey(),
      },
      tags: { name: "command " + name },
    },
  );
  const ok = record(res, [200]);
  check(res, { [name + " 200"]: () => ok });
  return { res, ok, result: ok ? res.json("result") : null };
}

function query(token, project, path) {
  const res = http.get(ORIGIN + "/api/v1" + path, {
    headers: { Authorization: "Bearer " + token, "X-CMS-Project": project },
    tags: { name: "query " + path.split("?")[0].replace(/[0-9a-f-]{36}/g, "{id}") },
  });
  record(res, [200]);
  return res;
}

// Zipf (s = 1.1) by inverting the continuous bounded power law: rank 0…n-1.
function zipf(n) {
  const s = 1.1;
  const a = Math.pow(n + 1, 1 - s);
  const x = Math.pow((a - 1) * Math.random() + 1, 1 / (1 - s));
  return Math.min(n - 1, Math.max(0, Math.floor(x) - 1));
}

const hex = (v, width) => v.toString(16).padStart(width, "0");
const entityID = (project, i) => `00000000-0000-4000-8000-${hex(project, 4)}${hex(i, 8)}`;

export function setup() {
  if (!seed.projects.length) throw Error("LOAD_SEED is required");
  for (let i = 0; i < 180; i++) {
    const r = http.get(ORIGIN + "/healthz", { timeout: "2s" });
    if (r.status === 200) break;
    sleep(1);
  }
  for (const p of seed.projects) {
    const r = command(p.adminToken, p.slug, "set-approval-policy", { low: 0, medium: 0, high: 0 });
    if (!r.ok) throw Error("approval policy " + p.slug + " " + r.res.status + " " + r.res.body);
  }
  return {};
}

export function deliveryRequest() {
  const p = seed.projects[Math.floor(Math.random() * seed.projects.length)];
  const base = __ENV.BASE;
  const r = Math.random();
  let path;
  let name;
  if (r < 0.7) {
    path = "/page?path=/p/" + zipf(DOCUMENTS);
    name = "page";
  } else if (r < 0.9) {
    path = "/entity/" + entityID(p.index, zipf(ENTITIES));
    name = "entity";
  } else {
    path = "/routes";
    name = "routes";
  }
  const res = http.get(`${base}/delivery/v1/${p.slug}/staging${path}`, {
    headers: { Authorization: "Bearer " + p.deliveryKey },
    responseType: "none",
    tags: { name: "delivery " + name },
  });
  record(res, [200]);
  if (!measuring() || res.status !== 200) return;
  const ms = res.timings.duration;
  if (base === PROXY) {
    if (res.headers["X-Cache-Status"] === "HIT") cdnHit.add(ms, { resource: name });
    return;
  }
  const cache = res.headers["X-Cms-Cache"];
  if (cache === "hit") originHit.add(ms, { resource: name });
  else if (cache === "miss") {
    originMiss.add(ms, { resource: name });
    misses.add(1);
  }
}

// One Change Set and one page per virtual Studio user; edits of 1…20 text nodes.
let studio = null;
const TEXT_NODES = 20;
export function studioUser() {
  const token = seed.editors[(exec.vu.idInTest - 1) % seed.editors.length];
  const project = seed.projects[0].slug;
  if (!studio) {
    const cs = command(token, project, "create-changeset", {
      title: "load editor " + exec.vu.idInTest,
    });
    if (!cs.ok) return sleep(1);
    const children = [];
    for (let i = 0; i < TEXT_NODES; i++)
      children.push({ id: "n_t" + i, type: "Text", props: { text: "текст " + i } });
    const created = command(token, project, "apply-operations", {
      changesetId: cs.result.id,
      expectedSeq: 0,
      operations: [
        {
          type: "document.create",
          payload: {
            kind: "page",
            path: `/load/editor-${exec.vu.idInTest}-${Date.now()}`,
            root: { id: "n_root", type: "Stack", children },
          },
        },
      ],
    });
    if (!created.ok) return sleep(1);
    studio = {
      cs: cs.result.id,
      doc: created.result.operations[0].target,
      seq: created.result.changeset.seq,
    };
  }
  query(token, project, "/changesets?state=open");
  query(token, project, `/changesets/${studio.cs}/history`);
  const n = 1 + Math.floor(Math.random() * 20);
  const operations = [];
  for (let i = 0; i < n; i++) {
    operations.push({
      type: "node.setProps",
      target: studio.doc,
      payload: {
        nodeId: "n_t" + Math.floor(Math.random() * TEXT_NODES),
        set: { text: "правка " + Math.random().toString(36).slice(2, 8) },
      },
    });
  }
  const r = command(token, project, "apply-operations", {
    changesetId: studio.cs,
    expectedSeq: studio.seq,
    operations,
  });
  if (r.ok) {
    studio.seq = r.result.changeset.seq;
    if (measuring()) apply.add(r.res.timings.duration, { operations: String(n) });
  } else {
    studio = null; // a lost seq starts a new Change Set
  }
  sleep(1 + Math.random() * 2);
}

// 01 §5: publications of the other projects change one entity and invalidate caches.
export function backgroundPublication() {
  const others = seed.projects.length > 1 ? seed.projects.slice(1) : seed.projects;
  const p = others[Math.floor(Math.random() * others.length)];
  const cs = command(p.adminToken, p.slug, "create-changeset", { title: "load publication" });
  if (!cs.ok) return;
  const target = entityID(p.index, zipf(ENTITIES));
  const changed = command(p.adminToken, p.slug, "apply-operations", {
    changesetId: cs.result.id,
    expectedSeq: 0,
    operations: [
      { type: "entity.setFields", target, payload: { set: { title: "обновлено " + Date.now() } } },
    ],
  });
  if (!changed.ok) return;
  if (
    !command(p.adminToken, p.slug, "submit-changeset", {
      changesetId: cs.result.id,
      expectedSeq: changed.result.changeset.seq,
    }).ok
  )
    return;
  command(p.adminToken, p.slug, "publish", { changesetId: cs.result.id, environment: "staging" });
}

// NFR-006/007: a Change Set of 50 pages is published and rolled back on the background load.
export function publishAndRollback() {
  const p = seed.projects[0];
  const started = Date.now();
  const cs = command(p.adminToken, p.slug, "create-changeset", {
    title: "NFR-006 " + exec.scenario.iterationInTest,
  });
  if (!cs.ok) return;
  const operations = [];
  for (let i = 0; i < 50; i++) {
    operations.push({
      type: "document.create",
      payload: {
        kind: "page",
        path: `/nfr006/${Date.now()}-${i}`,
        root: { id: "n_root", type: "Stack" },
      },
    });
  }
  const created = command(p.adminToken, p.slug, "apply-operations", {
    changesetId: cs.result.id,
    expectedSeq: 0,
    operations,
  });
  if (!created.ok) return;
  if (
    !command(p.adminToken, p.slug, "submit-changeset", {
      changesetId: cs.result.id,
      expectedSeq: created.result.changeset.seq,
    }).ok
  )
    return;
  const pub = command(p.adminToken, p.slug, "publish", {
    changesetId: cs.result.id,
    environment: "staging",
  });
  if (!pub.ok) return;
  publish.add(pub.res.timings.duration);
  published.add(1);
  sleep(5);
  const back = command(p.adminToken, p.slug, "rollback", {
    publicationId: pub.result.id,
    resetHead: false,
  });
  if (back.ok) {
    rollback.add(back.res.timings.duration);
    rolledBack.add(1);
  }
  // Spread the publications over the measured window.
  const left = DURATION_S / PUBLISHES - (Date.now() - started) / 1000;
  if (left > 0) sleep(left);
}

export function handleSummary(data) {
  const out = __ENV.LOAD_SUMMARY || "/dev/stdout";
  return { [out]: JSON.stringify(data, null, 2), stdout: textSummary(data) };
}

function textSummary(data) {
  const lines = ["", "NFR measurements:"];
  for (const [name, m] of Object.entries(data.metrics)) {
    if (!/^nfr|^rate_limited|^request_failures/.test(name)) continue;
    const v = m.values;
    const shown =
      "p(95)" in v
        ? `p95=${v["p(95)"].toFixed(1)} max=${v.max.toFixed(1)} n=${v.count ?? ""}`
        : JSON.stringify(v);
    const failed = m.thresholds && Object.values(m.thresholds).some((t) => !t.ok);
    lines.push(`  ${failed ? "FAIL" : "ok  "} ${name}: ${shown}`);
  }
  return lines.join("\n") + "\n";
}
