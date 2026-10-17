// STU-274: isolated CMS/PostgreSQL/OIDC/CLI/React acceptance. Requires the
// capability baseline build and CMS_REVIEW_CAPABILITIES=true review stand.
import { readFile, writeFile, mkdir, open } from "node:fs/promises";
import { execFileSync, spawn } from "node:child_process";
import { createRequire } from "node:module";
import { resolve, dirname } from "node:path";
import { randomUUID } from "node:crypto";
import { fileURLToPath, URL, URLSearchParams } from "node:url";
import vm from "node:vm";
const { fetch, Buffer, setTimeout } = globalThis;
import { DatabaseSync } from "node:sqlite";
const repo = fileURLToPath(new URL("..", import.meta.url));
const require = createRequire(new URL("../examples/react-store/package.json", import.meta.url));
const { chromium, expect } = require("@playwright/test");
if (
  !process.env.STUDIO_REVIEW_ADMIN_FILE ||
  !process.env.STUDIO_REVIEW_OUTPUT ||
  !process.env.CMS_PNPM_CLI
)
  throw Error("Isolated stand/output/pnpm required");
const cfg = JSON.parse(await readFile(process.env.STUDIO_REVIEW_ADMIN_FILE, "utf8"));
if (
  cfg.project !== "studio-work-review" ||
  [cfg.baseUrl, cfg.studioOrigin, cfg.appOrigin].some((u) => new URL(u).hostname !== "127.0.0.1")
)
  throw Error("Isolated loopback stand only");
const output = resolve(process.env.STUDIO_REVIEW_OUTPUT);
await mkdir(output, { recursive: true });
const browser = await chromium.launch(
  process.env.PLAYWRIGHT_EXECUTABLE_PATH
    ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH }
    : {},
);
const report = {
  requirements: "STU-275..294",
  sourceSha: execFileSync("git", ["rev-parse", "HEAD"], { cwd: repo, encoding: "utf8" }).trim(),
};
const database = new URL(process.env.CMS_DATABASE_URL ?? "");
if (
  database.hostname !== "127.0.0.1" ||
  !/^\/cms_delivery_acceptance_[a-z0-9_]+$/.test(database.pathname)
)
  throw Error("Dedicated delivery acceptance database required");
const databaseName = database.pathname.slice(1),
  databaseUser = decodeURIComponent(database.username);
function sql(query) {
  return execFileSync(
    "docker",
    [
      "exec",
      "project17-studio-behavior-tests",
      "psql",
      "-U",
      databaseUser,
      "-d",
      databaseName,
      "-tAc",
      query,
    ],
    { encoding: "utf8", windowsHide: true },
  ).trim();
}
const stand = dirname(process.env.STUDIO_REVIEW_ADMIN_FILE),
  processFile = resolve(stand, "studio-work-review.processes.json"),
  receiverFile = resolve(stand, "receiver.private.json"),
  modeFile = resolve(stand, "receiver-mode.txt");
let restarted;
async function ready() {
  for (let i = 0; i < 60; i++) {
    try {
      if ((await fetch(cfg.baseUrl + "/readyz")).ok) return;
    } catch {
      /* Wait for the local CMS listener. */
    }
    await new Promise((r) => setTimeout(r, 250));
  }
  throw Error("CMS restart failed");
}
async function restartCMS() {
  const processes = JSON.parse(await readFile(processFile, "utf8"));
  if (processes.apiPid > 0) {
    try {
      process.kill(processes.apiPid);
    } catch {
      /* The owned prior CMS process may already have exited. */
    }
  }
  await new Promise((r) => setTimeout(r, 300));
  const fd = await open(resolve(stand, "cms-restart.log"), "a");
  restarted = spawn(process.env.CMS_REVIEW_BINARY, ["serve"], {
    cwd: repo,
    env: {
      ...process.env,
      CMS_HTTP_ADDR: new URL(cfg.baseUrl).host,
      CMS_OIDC_ISSUER: cfg.baseUrl.replace(/:\d+$/, ":" + (Number(new URL(cfg.baseUrl).port) + 2)),
      CMS_OIDC_CLIENT_ID: "studio",
      CMS_STUDIO_PUBLIC_URL: cfg.studioOrigin,
      CMS_OIDC_ALLOW_LOOPBACK_HTTP: "true",
      CMS_CAPABILITY_WEBHOOK_CONFIG_FILE: resolve(stand, "webhooks.private.json"),
      CMS_CAPABILITY_WEBHOOK_ALLOW_LOOPBACK_HTTP: "true",
    },
    windowsHide: true,
    stdio: ["ignore", fd.fd, fd.fd],
  });
  await fd.close();
  processes.apiPid = restarted.pid;
  await writeFile(processFile, JSON.stringify(processes));
  await ready();
}
async function admin(name, payload) {
  const response = await fetch(cfg.baseUrl + "/api/v1/commands/" + name, {
    method: "POST",
    headers: {
      Authorization: "Bearer " + cfg.credential,
      "X-CMS-Project": cfg.project,
      "Content-Type": "application/json",
      "Idempotency-Key": randomUUID(),
    },
    body: JSON.stringify({ payload, reason: "Isolated capability acceptance fixture" }),
  });
  const data = await response.json();
  if (!response.ok) throw Error(name + " " + response.status + " " + data.code);
  return data.result;
}
try {
  sql(
    `UPDATE projects SET settings=settings || '{"defaultMode":"CODE","maxMode":"CODE"}' WHERE id='${cfg.projectId}'`,
  );
  const author = await browser.newContext({ viewport: { width: 1440, height: 1100 } }),
    reviewer = await browser.newContext();
  const alice = await author.newPage(),
    bob = await reviewer.newPage();
  async function login(page, name, roleId) {
    await page.goto(cfg.studioOrigin + "/editor");
    await page.getByRole("link", { name: "Войти", exact: true }).click();
    await page.getByRole("link", { name: "Войти как " + name, exact: true }).click();
    await page.waitForURL(cfg.studioOrigin + "/editor**");
    const account = await (
      await page.request.get(cfg.studioOrigin + "/api/studio/auth/session")
    ).json();
    await admin("bind-human-role", { actorId: account.user.id, roleId });
    await page.reload();
    return account;
  }
  const account = await login(alice, "Alice", cfg.authorRole);
  await login(bob, "Bob", cfg.reviewerRole);
  async function read(context, path) {
    const response = await context.request.get(cfg.baseUrl + "/api/v1" + path, {
      headers: { "X-CMS-Project": cfg.project },
    });
    if (!response.ok()) throw Error(path + " " + response.status());
    return response.json();
  }
  async function human(name, payload) {
    const response = await author.request.post(cfg.baseUrl + "/api/v1/commands/" + name, {
      headers: {
        "X-CMS-Project": cfg.project,
        Origin: cfg.studioOrigin,
        "X-CSRF-Token": account.csrfToken,
        "Idempotency-Key": randomUUID(),
      },
      data: { payload, reason: "Isolated capability acceptance fixture" },
    });
    const data = await response.json();
    if (!response.ok()) throw Error(name + " " + response.status() + " " + data.code);
    return data.result;
  }
  const initial = await read(author, "/manifest?environment=staging");
  expect(initial.codeIndexAvailable).toBe(true);
  expect(initial.manifest.actions?.["store.favorite"]).toBeUndefined();
  const previous = await read(
    author,
    "/capability-requests?environment=staging&key=store.favorite",
  );
  for (const request of previous.items) {
    if (!["cancelled", "rejected", "fulfilled"].includes(request.status))
      await human("update-capability-request", {
        id: request.id,
        expectedRevision: request.revision,
        transition: "cancelled",
      });
  }
  await admin("set-approval-policy", { low: 1, medium: 1, high: 1 });
  const cs = await human("create-changeset", {
    title: "Missing action acceptance",
    targets: ["staging"],
  });
  const catalogSource = await readFile(
    resolve(repo, "examples/react-store/scripts/catalog.ts"),
    "utf8",
  );
  const catalog = vm.runInNewContext(
    "(" + catalogSource.match(/export const catalog: Catalog = ([\s\S]*);/)[1] + ")",
  );
  delete catalog.heroAsset;
  catalog.products = catalog.products.slice(0, 2).map((product) => {
    const value = { ...product };
    delete value.asset;
    return value;
  });
  const page = await human("apply-operations", {
    changesetId: cs.id,
    expectedSeq: 0,
    operations: [
      {
        type: "document.create",
        payload: {
          kind: "page",
          path: "/store",
          root: {
            id: "n_store",
            type: "Storefront",
            props: { config: { base: catalog } },
            children: [
              { id: "n_favorite", type: "Button", bindings: { label: "$local.t_favorite" } },
            ],
          },
        },
      },
    ],
  });
  const pageId = page.operations[0].target;
  await human("apply-operations", {
    changesetId: cs.id,
    expectedSeq: 1,
    operations: [
      {
        type: "localContent.set",
        target: pageId,
        payload: {
          key: "t_favorite",
          type: "text",
          value: { ru: "Save favorite", en: "Save favorite" },
        },
      },
    ],
  });
  Object.assign(report, { changesetId: cs.id, pageId, initialManifestHash: initial.manifestHash });
  await alice.goto(
    cfg.studioOrigin + "/editor#" + new URLSearchParams({ document: pageId, changeset: cs.id }),
  );
  await alice.reload();
  await alice.locator('[role="treeitem"][title="n_favorite"]').click();
  await alice.getByRole("button", { name: "Запросить действие для click", exact: true }).click();
  const dialog = alice.getByRole("dialog");
  await dialog.getByLabel("Ключ действия", { exact: true }).fill("store.favorite");
  await dialog.getByLabel("Заголовок", { exact: true }).fill("Save a favorite");
  await dialog
    .getByLabel("Описание задачи", { exact: true })
    .fill("Save the selected item after an explicit click.");
  await dialog
    .getByLabel("Критерии приёмки, по одному в строке", { exact: true })
    .fill("Click saves the selected item\nDesign mode performs no action");
  for (const [name, max] of [
    ["id", "64"],
    ["title", "200"],
  ]) {
    await dialog.getByRole("button", { name: "Добавить аргумент", exact: true }).click();
    await dialog.getByLabel("Имя аргумента", { exact: true }).last().fill(name);
    await dialog.getByLabel("Обязательный", { exact: true }).last().check();
    await dialog.getByLabel("Максимум", { exact: true }).last().fill(max);
  }
  await dialog.getByRole("button", { name: "Сохранить черновик запроса", exact: true }).click();
  await expect(
    dialog.getByRole("button", { name: "Проверить передаваемый контекст", exact: true }),
  ).toBeVisible();
  const list = await read(
    author,
    "/capability-requests?environment=staging&key=store.favorite&status=draft",
  );
  expect(list.items).toHaveLength(1);
  const requestId = list.items[0].id;
  report.requestId = requestId;
  const hidden = await reviewer.request.get(
    cfg.baseUrl + "/api/v1/capability-requests/" + requestId,
    { headers: { "X-CMS-Project": cfg.project } },
  );
  expect(hidden.status()).toBe(404);
  await dialog
    .getByRole("button", { name: "Проверить передаваемый контекст", exact: true })
    .click();
  await expect(dialog.locator(".capability-context")).toBeVisible();
  await dialog
    .getByLabel("Передать этот context pack участникам проекта с code.propose", { exact: true })
    .check();
  await dialog.getByLabel("Подтверждаю замену цепочки", { exact: true }).check();
  const attempts = [];
  let lose = true;
  await alice.route("**/api/studio/api/v1/commands/submit-capability-request", async (route) => {
    attempts.push({
      body: route.request().postData(),
      key: route.request().headers()["idempotency-key"],
    });
    if (lose) {
      lose = false;
      await route.fetch();
      await route.abort("failed");
    } else await route.continue();
  });
  await dialog.getByRole("button", { name: "Отправить запрос", exact: true }).click();
  await dialog.getByRole("button", { name: "Повторить тот же запрос", exact: true }).click();
  await expect(
    dialog.getByRole("button", { name: "Скачать context pack", exact: true }),
  ).toBeVisible();
  expect(attempts).toHaveLength(2);
  expect(attempts[0]).toEqual(attempts[1]);
  await alice.unroute("**/api/studio/api/v1/commands/submit-capability-request");
  const submitted = await read(author, "/capability-requests/" + requestId);
  expect(submitted.status).toBe("submitted");
  expect(submitted.stale).toBe(false);
  expect(submitted.packs).toHaveLength(1);
  report.packHash = submitted.packs[0].hash;
  await admin("bind-human-role", {
    actorId: (await (await bob.request.get(cfg.studioOrigin + "/api/studio/auth/session")).json())
      .user.id,
    roleId: cfg.deliveryAdminRole,
  });
  const bobAccount = await (
    await bob.request.get(cfg.studioOrigin + "/api/studio/auth/session")
  ).json();
  await admin("unbind-human-role", { actorId: bobAccount.user.id, roleId: cfg.reviewerRole });
  await bob.reload();
  await expect(bob.getByRole("button", { name: "Запросы возможностей", exact: true })).toHaveCount(
    0,
  );
  await bob.getByRole("button", { name: "Получатель запросов", exact: true }).click();
  await bob.getByLabel("Получатель", { exact: true }).selectOption("engineering");
  await bob
    .getByLabel("Разрешить передачу после отдельного подтверждения автора", { exact: true })
    .check();
  await bob.getByRole("button", { name: "Сохранить получателя", exact: true }).click();
  await expect(bob.getByText("Ревизия: 1 · Готов", { exact: true })).toBeVisible();
  const target = await read(reviewer, "/capability-delivery-target?environment=staging");
  const receiverConfig = JSON.parse(await readFile(receiverFile, "utf8"));
  receiverConfig.bindings = [
    {
      projectId: cfg.projectId,
      environment: "staging",
      targetId: target.id,
      keys: { "local-v1": receiverConfig.key },
    },
  ];
  await writeFile(receiverFile, JSON.stringify(receiverConfig));
  const fingerprint = () =>
    JSON.parse(
      sql(
        `SELECT json_build_object('requestRevision',r.revision,'status',r.status,'seq',c.seq,'manifest',e.active_manifest_id,'packCount',(SELECT count(*) FROM capability_context_packs WHERE request_id=r.id),'requestAuditCount',(SELECT count(*) FROM capability_request_audit WHERE request_id=r.id)) FROM capability_requests r JOIN changesets c ON c.id=r.changeset_id JOIN environments e ON e.id=r.environment_id WHERE r.id='${requestId}'`,
      ),
    );
  const before = fingerprint();
  await alice.getByRole("button", { name: "Передать context pack", exact: true }).click();
  const external = alice.getByRole("dialog", {
    name: "Подтверждение внешней передачи",
    exact: true,
  });
  await expect(external.getByText(target.url, { exact: true })).toBeVisible();
  await expect(
    external.getByRole("button", { name: "Подтвердить передачу", exact: true }),
  ).toBeDisabled();
  await external
    .getByLabel("Разрешаю передать этот context pack на указанный внешний адрес", { exact: true })
    .check();
  await external.getByRole("button", { name: "Подтвердить передачу", exact: true }).click();
  await expect(external).toHaveCount(0);
  const journal = () =>
    read(author, `/capability-requests/${requestId}/deliveries?environment=staging`);
  await expect
    .poll(
      async () => {
        const j = await journal();
        return j.items[0]?.state;
      },
      { timeout: 20000 },
    )
    .toBe("retry_wait");
  const delivery = (await journal()).items[0];
  report.deliveryId = delivery.id;
  report.bodyHash = delivery.bodyHash;
  const inbox = new DatabaseSync(resolve(stand, "receiver-inbox.sqlite"), { readOnly: true });
  expect(inbox.prepare("SELECT count(*) AS count FROM inbox").get().count).toBe(1);
  const storedBefore = inbox
    .prepare("SELECT body_hash,body,calls FROM inbox WHERE delivery_id=?")
    .get(delivery.id);
  expect(storedBefore.calls).toBe(1);
  expect(storedBefore.body_hash).toBe(delivery.bodyHash);
  await restartCMS();
  await expect
    .poll(
      async () => {
        const j = await journal();
        return j.items[0]?.state;
      },
      { timeout: 70000 },
    )
    .toBe("delivered");
  await expect(alice.getByText("Пакет сохранён получателем · 2/18", { exact: true })).toBeVisible({
    timeout: 10000,
  });
  const detail = await read(author, `/capability-deliveries/${delivery.id}?environment=staging`);
  expect(detail.attempts).toHaveLength(2);
  expect(detail.attempts[0].receiptDeliveryId).toBe(delivery.id);
  expect(detail.attempts[0].receiptBodyHash).toBe(delivery.bodyHash);
  const storedAfter = inbox
    .prepare("SELECT body_hash,body,calls FROM inbox WHERE delivery_id=?")
    .get(delivery.id);
  expect(storedAfter.calls).toBe(2);
  expect(Buffer.from(storedAfter.body)).toEqual(Buffer.from(storedBefore.body));
  expect(inbox.prepare("SELECT count(*) AS count FROM inbox").get().count).toBe(1);
  expect(fingerprint()).toEqual(before);
  const sendAgain = await human("send-capability-context", {
    requestId,
    expectedRequestRevision: submitted.revision,
    expectedPackRevision: submitted.packs[0].revision,
    expectedPackHash: submitted.packs[0].hash,
    expectedSeq: submitted.seq,
    expectedManifestHash: initial.manifestHash,
    expectedTargetRevision: target.revision,
    expectedTargetFingerprint: target.fingerprint,
    confirmExternalShare: true,
  });
  expect(sendAgain.deduplicated).toBe(true);
  expect(sendAgain.delivery.id).toBe(delivery.id);
  // Create independent identities by explicitly updating the target. No saved
  // request is sent merely because a target was enabled or revised.
  await writeFile(modeFile, "hold");
  async function operator(name, payload) {
    const current = await (
      await bob.request.get(cfg.studioOrigin + "/api/studio/auth/session")
    ).json();
    const response = await reviewer.request.post(cfg.baseUrl + "/api/v1/commands/" + name, {
      headers: {
        "X-CMS-Project": cfg.project,
        Origin: cfg.studioOrigin,
        "X-CSRF-Token": current.csrfToken,
        "Idempotency-Key": randomUUID(),
      },
      data: { payload, reason: "Isolated delivery operator decision" },
    });
    const result = await response.json();
    if (!response.ok()) throw Error(name + " " + response.status() + " " + result.code);
    return result.result;
  }
  const nextTarget = await operator("set-capability-delivery-target", {
    environment: "staging",
    endpointId: "engineering",
    enabled: true,
    expectedRevision: 1,
  });
  expect((await journal()).items).toHaveLength(1);
  const preview = await read(
    author,
    `/capability-requests/${requestId}/delivery-preview?environment=staging`,
  );
  const payload = {
    requestId,
    expectedRequestRevision: preview.requestRevision,
    expectedPackRevision: preview.packRevision,
    expectedPackHash: preview.packHash,
    expectedSeq: preview.seq,
    expectedManifestHash: preview.manifestHash,
    expectedTargetRevision: nextTarget.revision,
    expectedTargetFingerprint: nextTarget.fingerprint,
    confirmExternalShare: true,
  };
  const held = (await human("send-capability-context", payload)).delivery;
  await expect
    .poll(
      () =>
        inbox.prepare("SELECT count(*) AS count FROM inbox WHERE delivery_id=?").get(held.id).count,
      { timeout: 10000 },
    )
    .toBe(1);
  const sending = await read(author, `/capability-deliveries/${held.id}?environment=staging`);
  expect(sending.state).toBe("sending");
  await operator("cancel-capability-delivery", {
    deliveryId: held.id,
    expectedDeliveryRevision: sending.revision,
  });
  await writeFile(modeFile, "normal");
  await expect
    .poll(
      async () => {
        const result = await read(author, `/capability-deliveries/${held.id}?environment=staging`);
        return result.attempts[0]?.outcome;
      },
      { timeout: 15000 },
    )
    .toBe("acknowledged_after_cancel");
  expect((await read(author, `/capability-deliveries/${held.id}?environment=staging`)).state).toBe(
    "cancelled",
  );
  // Revoke real DB roles while the next item is queued, then recover through
  // the normal HTTP/River path after restart. The receiver must see no request.
  let currentTarget = await operator("set-capability-delivery-target", {
    environment: "staging",
    endpointId: "engineering",
    enabled: true,
    expectedRevision: 2,
  });
  for (const mode of ["rights", "target", "stale"]) {
    sql("UPDATE river_queue SET paused_at=now() WHERE name='capability_delivery'");
    await restartCMS();
    const queued = (
      await human("send-capability-context", {
        ...payload,
        expectedTargetRevision: currentTarget.revision,
        expectedTargetFingerprint: currentTarget.fingerprint,
      })
    ).delivery;
    expect(queued.state).toBe("queued");
    if (mode === "rights")
      await admin("unbind-human-role", { actorId: account.user.id, roleId: cfg.authorRole });
    if (mode === "target")
      currentTarget = await operator("set-capability-delivery-target", {
        environment: "staging",
        endpointId: "engineering",
        enabled: true,
        expectedRevision: currentTarget.revision,
      });
    if (mode === "stale")
      await human("apply-operations", {
        changesetId: cs.id,
        expectedSeq: submitted.seq,
        operations: [
          {
            type: "node.rename",
            target: pageId,
            payload: { nodeId: "n_favorite", name: "User edit after consent" },
          },
        ],
      });
    sql("UPDATE river_queue SET paused_at=NULL WHERE name='capability_delivery'");
    await restartCMS();
    await expect
      .poll(
        async () => {
          const d = await read(reviewer, `/capability-deliveries/${queued.id}?environment=staging`);
          return d.state;
        },
        { timeout: 15000 },
      )
      .toBe("cancelled");
    expect(
      inbox.prepare("SELECT count(*) AS count FROM inbox WHERE delivery_id=?").get(queued.id).count,
    ).toBe(0);
    report[mode + "StopsBeforeHTTP"] = true;
    if (mode === "rights") {
      await admin("bind-human-role", { actorId: account.user.id, roleId: cfg.authorRole });
      currentTarget = await operator("set-capability-delivery-target", {
        environment: "staging",
        endpointId: "engineering",
        enabled: true,
        expectedRevision: currentTarget.revision,
      });
    }
  }
  const secondBoot = execFileSync(
    process.env.CMS_REVIEW_BINARY,
    ["bootstrap", "-slug", "delivery-other-project", "-name", "Isolated second project"],
    { env: process.env, encoding: "utf8", windowsHide: true },
  );
  const secondCredential = secondBoot.match(/cms_svc_[a-zA-Z0-9_-]+/)[0];
  async function secondAdmin(name, payload) {
    const response = await fetch(cfg.baseUrl + "/api/v1/commands/" + name, {
      method: "POST",
      headers: {
        Authorization: "Bearer " + secondCredential,
        "X-CMS-Project": "delivery-other-project",
        "Content-Type": "application/json",
        "Idempotency-Key": randomUUID(),
      },
      body: JSON.stringify({ payload, reason: "Second isolated project access fixture" }),
    });
    const value = await response.json();
    if (!response.ok) throw Error("Second project setup failed");
    return value.result;
  }
  const secondRole = await secondAdmin("create-role", {
    name: "other-author",
    capabilities: ["design.read", "capability.request", "capability.deliver"],
  });
  await secondAdmin("bind-human-role", { actorId: account.user.id, roleId: secondRole.id });
  const foreign = await author.request.get(
    cfg.baseUrl + `/api/v1/capability-deliveries/${delivery.id}?environment=staging`,
    { headers: { "X-CMS-Project": "delivery-other-project" } },
  );
  expect(foreign.status()).toBe(404);
  report.twoProjectIsolation = true;
  const review = await human("submit-changeset", {
    changesetId: cs.id,
    expectedSeq: submitted.seq + 1,
  });
  expect(JSON.stringify(review)).toContain("PENDING");
  const publish = await author.request.post(cfg.baseUrl + "/api/v1/commands/publish", {
    headers: {
      "X-CMS-Project": cfg.project,
      Origin: cfg.studioOrigin,
      "X-CSRF-Token": account.csrfToken,
      "Idempotency-Key": randomUUID(),
    },
    data: {
      payload: { changesetId: cs.id, environment: "staging", expectedSeq: submitted.seq + 1 },
      reason: "Pending publication must fail",
    },
  });
  expect(publish.ok()).toBe(false);
  expect(sql(`SELECT count(*) FROM published_pointers WHERE object_id='${pageId}'`)).toBe("0");
  report.pendingPublicationBlocked = true;
  await dialog.getByRole("button", { name: "Закрыть", exact: true }).click();
  await alice.getByLabel("Язык", { exact: true }).selectOption("en");
  await alice.setViewportSize({ width: 375, height: 900 });
  await alice.getByRole("button", { name: "Capability requests", exact: true }).click();
  await alice
    .getByRole("button", { name: "Save a favorite · store.favorite · submitted", exact: true })
    .click();
  await expect(
    alice.getByRole("heading", { name: "Developer handoff", exact: true }),
  ).toBeVisible();
  expect(
    await alice.evaluate(
      () => globalThis.document.documentElement.scrollWidth <= globalThis.window.innerWidth,
    ),
  ).toBe(true);
  await alice.screenshot({ path: resolve(output, "delivery-mobile.png"), fullPage: true });
  report.mobile375English = true;
  report.cancelInFlight = true;
  inbox.close();
  Object.assign(report, {
    durableInboxRows: 2,
    firstDeliveryInboxRows: 1,
    identicalBodyAfterRestart: true,
    lostReceiptRecovered: true,
    attempts: 2,
    unchangedRequestAndChangeset: true,
    adminPanelWithoutDesignRead: true,
    explicitExternalConsent: true,
    realHMACReceiver: true,
    worktreeDirty:
      execFileSync("git", ["status", "--porcelain"], { cwd: repo, encoding: "utf8" }).trim() !== "",
  });
  await writeFile(resolve(output, "delivery-acceptance.json"), JSON.stringify(report, null, 2));
  console.log(
    "PASS STU-275..294: signed durable receiver, lost receipt, CMS restart, immutable retry and in-flight cancellation.",
  );
} catch (error) {
  for (const context of browser.contexts().slice(1)) {
    for (const page of context.pages()) {
      console.error("Operator page: " + (await page.locator("body").innerText()).slice(-1200));
    }
  }
  const page = browser.contexts().flatMap((c) => c.pages())[0];
  if (page) {
    console.error((await page.locator("body").innerText()).slice(-3500));
    await page.screenshot({ path: resolve(output, "delivery-failure.png"), fullPage: true });
  }
  throw error;
} finally {
  await browser.close();
  if (restarted && restarted.exitCode === null) restarted.kill();
}
