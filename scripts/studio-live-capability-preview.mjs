// STU-334: isolated CMS/PostgreSQL/OIDC/Studio/React acceptance of preview/<ref>.
// Requires the capability baseline build and a review stand started with
// CMS_REVIEW_CAPABILITIES=true and CMS_REVIEW_CAPABILITY_PREVIEW=true.
// GitHub facts are recorded in the isolated database (no request reaches GitHub):
// the client and its provenance checks are accepted separately in STU-314.
import { readFile, writeFile, mkdir, open, cp, rm } from "node:fs/promises";
import { execFileSync, spawn } from "node:child_process";
import { createRequire } from "node:module";
import { resolve, dirname } from "node:path";
import { randomUUID } from "node:crypto";
import { fileURLToPath, pathToFileURL, URL, URLSearchParams } from "node:url";
import vm from "node:vm";
const { fetch } = globalThis;
const repo = fileURLToPath(new URL("..", import.meta.url));
const require = createRequire(new URL("../examples/react-store/package.json", import.meta.url));
const { chromium, expect } = require("@playwright/test");
const { manifestHash } = await import(pathToFileURL(require.resolve("@cms/manifest")).href);
for (const name of [
  "STUDIO_REVIEW_ADMIN_FILE",
  "STUDIO_REVIEW_OUTPUT",
  "CMS_PNPM_CLI",
  "PREVIEW_PG_CONTAINER",
  "PREVIEW_PG_DB",
])
  if (!process.env[name]) throw Error(name + " is required");
const cfg = JSON.parse(await readFile(process.env.STUDIO_REVIEW_ADMIN_FILE, "utf8"));
if (
  cfg.project !== "studio-work-review" ||
  !cfg.previewOrigin ||
  [cfg.baseUrl, cfg.studioOrigin, cfg.appOrigin, cfg.previewOrigin].some(
    (u) => new URL(u).hostname !== "127.0.0.1",
  )
)
  throw Error("Isolated loopback preview stand only");
const output = resolve(process.env.STUDIO_REVIEW_OUTPUT);
await mkdir(output, { recursive: true });
const standDir = dirname(process.env.STUDIO_REVIEW_ADMIN_FILE);
const prDir = resolve(repo, "examples/react-store/.pr-preview-acceptance");
const report = {
  requirements: "STU-315..334",
  sourceSha: execFileSync("git", ["rev-parse", "HEAD"], { cwd: repo, encoding: "utf8" }).trim(),
  githubFacts: "isolated database fixture; no GitHub request",
};
const sql = (text) =>
  execFileSync(
    "docker",
    [
      "exec",
      "-i",
      process.env.PREVIEW_PG_CONTAINER,
      "psql",
      "-v",
      "ON_ERROR_STOP=1",
      "-U",
      process.env.PREVIEW_PG_USER ?? "postgres",
      "-d",
      process.env.PREVIEW_PG_DB,
      "-At",
      "-c",
      text,
    ],
    { encoding: "utf8", windowsHide: true },
  ).trim();
async function call(name, payload, credential = cfg.credential) {
  const response = await fetch(cfg.baseUrl + "/api/v1/commands/" + name, {
    method: "POST",
    headers: {
      Authorization: "Bearer " + credential,
      "X-CMS-Project": cfg.project,
      "Content-Type": "application/json",
      "Idempotency-Key": randomUUID(),
    },
    body: JSON.stringify({ payload, reason: "Isolated preview acceptance fixture" }),
  });
  const data = await response.json();
  return { ok: response.ok, status: response.status, data };
}
async function admin(name, payload) {
  const r = await call(name, payload);
  if (!r.ok) throw Error(name + " " + r.status + " " + r.data.code);
  return r.data.result;
}
const build = () =>
  execFileSync(
    process.execPath,
    [process.env.CMS_PNPM_CLI, "--filter", "@cms/store-example", "build"],
    { cwd: repo, stdio: "pipe", windowsHide: true },
  );
async function readBuild(dir) {
  return {
    manifest: JSON.parse(await readFile(resolve(dir, ".cms/manifest.json"), "utf8")),
    index: JSON.parse(await readFile(resolve(dir, ".cms/code-index.json"), "utf8")),
  };
}
// Register a PR build: upload its code index, then the manifest with headSha.
async function registerBuild(built, commit) {
  const index = structuredClone(built.index);
  index.repo.commit = commit;
  const indexHash = await manifestHash(index);
  const manifest = structuredClone(built.manifest);
  manifest.codeIndex = { ...manifest.codeIndex, hash: indexHash, uploaded: false };
  const hash = await manifestHash(manifest);
  const upload = await admin("upload-code-index", { manifestHash: hash, indexHash, index });
  const result = await admin("register-manifest", {
    manifest,
    environment: "preview/pr-1",
    codeIndexUploadId: upload.uploadId,
    headSha: commit,
    appUrl: cfg.previewOrigin + "/",
  });
  return { ...result, hash };
}
let prStore;
const browser = await chromium.launch(
  process.env.PLAYWRIGHT_EXECUTABLE_PATH
    ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH }
    : {},
);
try {
  // PR deployment: the real application with store.favorite, copied aside so the
  // staging deployment keeps serving the baseline build.
  build();
  await rm(prDir, { recursive: true, force: true });
  for (const part of ["dist", ".cms"])
    await cp(resolve(repo, "examples/react-store", part), resolve(prDir, part), {
      recursive: true,
    });
  execFileSync(process.execPath, [resolve(repo, "scripts/studio-capability-baseline.mjs")], {
    cwd: repo,
    stdio: "pipe",
    windowsHide: true,
    env: process.env,
  });
  const prBuild = await readBuild(prDir);
  const headA = prBuild.index.repo.commit;
  expect(headA).toMatch(/^[0-9a-f]{40}$/);
  const action = prBuild.manifest.actions["store.favorite"];
  expect(action).toBeDefined();

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
    return (await page.request.get(cfg.studioOrigin + "/api/studio/auth/session")).json();
  }
  const aliceAccount = await login(alice, "Alice", cfg.authorRole);
  const bobAccount = await login(bob, "Bob", cfg.reviewerRole);
  async function read(context, path) {
    const response = await context.request.get(cfg.baseUrl + "/api/v1" + path, {
      headers: { "X-CMS-Project": cfg.project },
    });
    if (!response.ok()) throw Error(path + " " + response.status());
    return response.json();
  }
  async function human(context, account, name, payload) {
    const response = await context.request.post(cfg.baseUrl + "/api/v1/commands/" + name, {
      headers: {
        "X-CMS-Project": cfg.project,
        Origin: cfg.studioOrigin,
        "X-CSRF-Token": account.csrfToken,
        "Idempotency-Key": randomUUID(),
      },
      data: { payload, reason: "Isolated preview acceptance" },
    });
    const data = await response.json();
    if (!response.ok()) throw Error(name + " " + response.status() + " " + data.code);
    return data.result;
  }
  // Operator enables the GitHub configuration explicitly (STU-295).
  await admin("bind-human-role", { actorId: bobAccount.user.id, roleId: cfg.deliveryAdminRole });
  const config = await human(reviewer, bobAccount, "set-capability-github-config", {
    environment: "staging",
    configId: "local",
    enabled: true,
    expectedConfigRevision: (await read(reviewer, "/capability-github-config?environment=staging"))
      .revision,
  });
  await admin("unbind-human-role", { actorId: bobAccount.user.id, roleId: cfg.deliveryAdminRole });
  expect(config.ready).toBe(true);

  // The requested event is in a CODE zone (STU-255); the stand starts in DESIGN.
  sql(
    `UPDATE projects SET settings=settings || '{"defaultMode":"CODE","maxMode":"CODE"}' WHERE slug='${cfg.project}'`,
  );
  const initial = await read(author, "/manifest?environment=staging");
  expect(initial.manifest.actions?.["store.favorite"]).toBeUndefined();
  const cs = await human(author, aliceAccount, "create-changeset", {
    title: "PR preview acceptance",
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
  const page = await human(author, aliceAccount, "apply-operations", {
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
  await human(author, aliceAccount, "apply-operations", {
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
  const created = await human(author, aliceAccount, "create-capability-request", {
    environment: "staging",
    expectedManifestHash: initial.manifestHash,
    changesetId: cs.id,
    expectedSeq: 2,
    documentId: pageId,
    nodeId: "n_favorite",
    event: "click",
    key: "store.favorite",
    title: "Save a favorite",
    description: "Save the selected item after an explicit click.",
    proposedContract: { args: action.args },
    acceptance: ["Click saves the selected item"],
  });
  const requestId = created.request.id;
  await human(author, aliceAccount, "submit-capability-request", {
    id: requestId,
    expectedRevision: 1,
    expectedSeq: 2,
    expectedManifestHash: initial.manifestHash,
  });
  let revision = 2;
  for (const transition of ["accepted", "in_progress", "pr_open"]) {
    await human(reviewer, bobAccount, "update-capability-request", {
      id: requestId,
      expectedRevision: revision++,
      transition,
      ...(transition === "pr_open" ? { prUrl: "https://example.test/local/store/pull/1" } : {}),
    });
  }
  Object.assign(report, { changesetId: cs.id, pageId, requestId, headA });

  // CI: preview environment bound to PR #1, then its build (STU-315/316).
  const environment = {
    name: "preview/pr-1",
    kind: "preview",
    appUrl: cfg.previewOrigin + "/",
    source: { baseEnvironment: "staging", pullNumber: 1 },
  };
  const env = await admin("create-environment", environment);
  expect(env.source.repositoryId).toBe("42");
  expect((await admin("create-environment", environment)).id).toBe(env.id);
  const mismatch = await call("register-manifest", {
    manifest: prBuild.manifest,
    environment: "preview/pr-1",
    headSha: "f".repeat(40),
  });
  expect(mismatch.data.code).toBe("PREVIEW_COMMIT_MISMATCH");
  const first = await registerBuild(prBuild, headA);
  expect(first.previewRegistration.seq).toBe(1);

  // Verified GitHub facts of PR #1 at head A (fixture in the isolated database).
  const configRow = sql(
    `SELECT id||','||revision||','||fingerprint FROM capability_github_configs WHERE environment='staging'`,
  ).split(",");
  const facts = (head) =>
    JSON.stringify({
      repositoryId: "42",
      prId: "1001",
      number: 1,
      state: "open",
      draft: false,
      headRepositoryId: "42",
      headSha: head,
      baseSha: "b".repeat(40),
      baseRef: "dev",
      integrationType: "unavailable",
      policyDigest: "local",
      ci: "success",
      results: [],
    });
  const link = randomUUID(),
    sync = randomUUID();
  sql(`INSERT INTO capability_github_links(id,project_id,request_id,config_id,config_revision,fingerprint,pull_number,initiated_by,state,sync_id)
    SELECT '${link}',project_id,id,'${configRow[0]}',${configRow[1]},'${configRow[2]}',1,'${bobAccount.user.id}','idle','${sync}' FROM capability_requests WHERE id='${requestId}'`);
  const observe = (head, offset) =>
    sql(`INSERT INTO capability_github_observations(id,project_id,request_id,link_id,generation,config_revision,fingerprint,sync_id,body,digest,observed_at,fresh_until)
      SELECT gen_random_uuid(),project_id,request_id,id,generation,config_revision,fingerprint,gen_random_uuid(),'${facts(head)}'::jsonb,'local',now()+interval '${offset} seconds',now()+interval '${offset + 900} seconds'
      FROM capability_github_links WHERE id='${link}'`);
  observe(headA, 0);
  let preview = await read(author, "/capability-requests/" + requestId + "/preview");
  expect(preview.items[0].state).toBe("ready");
  expect(preview.canOpen).toBe(true);
  const engineerView = await read(reviewer, "/capability-requests/" + requestId + "/preview");
  expect(engineerView.canOpen).toBe(false);
  expect(engineerView.items[0].state).toBe("ready");

  // Start the PR deployment against its base environment (STU-322).
  const storeConfig = resolve(standDir, "studio-work-store.private.json");
  const log = await open(resolve(output, "pr-store.log"), "a");
  prStore = spawn(process.execPath, [resolve(prDir, "dist/server/server.js")], {
    cwd: repo,
    env: {
      ...process.env,
      NODE_ENV: "production",
      PORT: new URL(cfg.previewOrigin).port,
      CMS_STORE_CONFIG_FILE: storeConfig,
      CMS_STUDIO_ORIGIN: cfg.studioOrigin,
      CMS_APP_ORIGIN: cfg.previewOrigin,
    },
    stdio: ["ignore", log.fd, log.fd],
    windowsHide: true,
  });
  await log.close();
  for (let i = 0; i < 60; i++) {
    try {
      if ((await fetch(cfg.previewOrigin + "/store")).status < 500) break;
    } catch {
      /* starting */
    }
    await new Promise((r) => setTimeout(r, 250));
  }

  // Studio: preview args, exact retry, open read-only, execute the PR action.
  await alice.goto(
    cfg.studioOrigin + "/editor#" + new URLSearchParams({ document: pageId, changeset: cs.id }),
  );
  await alice.reload();
  await alice.locator('[role="treeitem"][title="n_favorite"]').click();
  await alice.getByRole("button", { name: "Открыть запрос для click", exact: true }).click();
  const card = alice.getByRole("dialog");
  const section = card.getByRole("region", { name: "Предпросмотр", exact: true });
  await expect(section.getByText("Готов к просмотру")).toBeVisible();
  for (const [key, value] of [
    ["id", "preview-item"],
    ["title", "Preview favorite"],
  ]) {
    await section.getByLabel("Значение параметра " + key, { exact: true }).selectOption("literal");
    await section.getByLabel("Литерал " + key, { exact: true }).fill(value);
  }
  await section.getByLabel("Причина", { exact: true }).fill("Check the PR build");
  const attempts = [];
  let lose = true;
  await alice.route("**/api/studio/api/v1/commands/set-capability-preview-args", async (route) => {
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
  await section
    .getByRole("button", { name: "Сохранить аргументы предпросмотра", exact: true })
    .click();
  await section.getByRole("button", { name: "Повторить тот же запрос", exact: true }).click();
  await expect(section.getByText("Аргументы предпросмотра сохранены")).toBeVisible();
  expect(attempts).toHaveLength(2);
  expect(attempts[0]).toEqual(attempts[1]);
  await alice.unroute("**/api/studio/api/v1/commands/set-capability-preview-args");
  const beforeOpen = await read(author, "/capability-requests/" + requestId);
  await section.getByRole("button", { name: "Открыть в предпросмотре", exact: true }).click();
  await card.getByRole("button", { name: "Закрыть", exact: true }).click();
  await expect(alice.getByText("Режим предпросмотра PR #1: только просмотр")).toBeVisible();
  await expect(alice.locator("iframe")).toHaveAttribute(
    "src",
    new RegExp("^" + cfg.previewOrigin.replaceAll(".", "\\.") + "/api/cms/preview"),
  );
  const frame = alice.frameLocator("iframe");
  // Actions are enabled only after the preview finished its handshake.
  await expect(async () => {
    await frame.getByRole("button", { name: "Save favorite", exact: true }).click();
    await expect(frame.getByRole("status").filter({ hasText: "Preview favorite" })).toBeVisible({
      timeout: 1000,
    });
  }).toPass({ timeout: 20000 });
  await alice.screenshot({ path: resolve(output, "preview-pr.png"), fullPage: true });
  report.prActionExecutedInPreview = true;

  // The stored draft keeps pending; staging preview does not execute it (STU-322).
  const draft = await read(author, "/documents/" + pageId + "?changesetId=" + cs.id);
  expect(JSON.stringify(draft.body.nodes.n_favorite.on)).toContain("pending:" + requestId);
  const csPreview = await read(
    author,
    "/changesets/" + cs.id + "/preview?environment=preview/pr-1",
  );
  expect(csPreview.substitutions[0].substituted).toBe(true);
  await alice.getByRole("button", { name: "Вернуться в рабочее окружение", exact: true }).click();
  await expect(alice.locator("iframe")).toHaveAttribute(
    "src",
    new RegExp("^" + cfg.appOrigin.replaceAll(".", "\\.") + "/api/cms/preview"),
  );
  await alice.getByRole("button", { name: "Взаимодействие", exact: true }).click();
  await expect(alice.getByText("Предпросмотр подключён")).toBeVisible();
  await alice.waitForTimeout(1500);
  await alice
    .frameLocator("iframe")
    .getByRole("button", { name: "Save favorite", exact: true })
    .click();
  await alice.waitForTimeout(1500);
  await expect(
    alice.frameLocator("iframe").getByRole("status").filter({ hasText: "Preview favorite" }),
  ).toHaveCount(0);
  report.stagingKeepsPending = true;

  // MF-031: nothing is published into a preview environment.
  const publish = await call("publish", {
    changesetId: cs.id,
    environment: "preview/pr-1",
    expectedSeq: (await read(author, "/changesets/" + cs.id)).seq,
  });
  expect(publish.ok).toBe(false);
  report.publishIntoPreview = publish.data.code;

  // A newer head on GitHub makes the build outdated; a new build restores it.
  const token = await admin("create-preview-token", {
    environment: "preview/pr-1",
    changesetId: cs.id,
  });
  const headB = "c".repeat(40);
  observe(headB, 5);
  preview = await read(author, "/capability-requests/" + requestId + "/preview");
  expect(preview.items[0].state).toBe("commit_behind");
  const second = await registerBuild(prBuild, headB);
  expect(second.previewRegistration.seq).toBe(2);
  preview = await read(author, "/capability-requests/" + requestId + "/preview");
  expect(preview.items[0].state).toBe("ready");
  expect(preview.items[0].args.status).toBe("stale");
  const oldToken = await fetch(cfg.baseUrl + "/delivery/v1/" + cfg.project + "/staging/routes", {
    headers: { Authorization: "Preview " + token.token },
  });
  expect(oldToken.status).toBe(401);
  report.newHead = { headB, oldTokenStatus: oldToken.status, argsAfterNewBuild: "stale" };

  // EN and 375 px.
  await alice.setViewportSize({ width: 375, height: 900 });
  await alice.getByRole("button", { name: "Открыть запрос для click", exact: true }).click();
  await expect(card.getByRole("region", { name: "Предпросмотр", exact: true })).toBeVisible();
  await alice.screenshot({ path: resolve(output, "preview-mobile.png"), fullPage: true });
  // The preview section fits its card; the page itself is checked as in STU-274.
  const fits = await card
    .getByRole("region", { name: "Предпросмотр", exact: true })
    .evaluate(
      (e) => e.scrollWidth <= e.clientWidth && e.getBoundingClientRect().right <= innerWidth,
    );
  expect(fits).toBe(true);
  await card.getByRole("button", { name: "Закрыть", exact: true }).click();
  await expect
    .poll(() =>
      alice.evaluate(
        () => globalThis.document.documentElement.scrollWidth <= globalThis.innerWidth,
      ),
    )
    .toBe(true);
  await alice.setViewportSize({ width: 1440, height: 1100 });

  // CI deletes the environment: archive, name released, registration refused.
  const archived = await admin("delete-environment", {
    environment: "preview/pr-1",
    expectedRegistrationSeq: 2,
  });
  expect(archived.archiveReason).toBe("deleted");
  preview = await read(author, "/capability-requests/" + requestId + "/preview");
  expect(preview.items[0].state).toBe("environment_expired");
  const late = await call("register-manifest", {
    manifest: prBuild.manifest,
    environment: "preview/pr-1",
    headSha: headB,
  });
  expect(late.data.code).toBe("ENVIRONMENT_ARCHIVED");

  const after = await read(author, "/capability-requests/" + requestId);
  expect(after.status).toBe("pr_open");
  expect(after.revision).toBe(beforeOpen.revision);
  expect(after.fulfilledManifestId).toBeNull();
  const stagingNow = await read(author, "/manifest?environment=staging");
  expect(stagingNow.manifestHash).toBe(initial.manifestHash);
  Object.assign(report, {
    twoHumans: true,
    engineerSeesStateOnly: true,
    exactRetryArgs: true,
    readOnlyBanner: true,
    archived: true,
    requestUnchanged: true,
    stagingManifestUnchanged: true,
    mobile375: true,
  });
  await writeFile(resolve(output, "preview-acceptance.json"), JSON.stringify(report, null, 2));
  console.log("PASS STU-334: PR build previewed read-only, outdated on new head, archived.");
} catch (error) {
  const page = browser.contexts().flatMap((c) => c.pages())[0];
  if (page) {
    console.error((await page.locator("body").innerText()).slice(-3500));
    await page.screenshot({ path: resolve(output, "preview-failure.png"), fullPage: true });
  }
  throw error;
} finally {
  await browser.close();
  prStore?.kill();
  await rm(prDir, { recursive: true, force: true });
}
