// STU-374: isolated acceptance of a component request through automatic fulfilment.
// Requires the capability baseline build and a review stand started with
// CMS_REVIEW_CAPABILITIES=true and CMS_REVIEW_CAPABILITY_PREVIEW=true (GitHub
// catalogue). GitHub facts are recorded in the isolated database: no request
// reaches GitHub; the client is accepted separately in STU-314.
import { readFile, writeFile, mkdir, open } from "node:fs/promises";
import { execFileSync, spawn } from "node:child_process";
import { createRequire } from "node:module";
import { resolve, dirname } from "node:path";
import { randomUUID } from "node:crypto";
import { fileURLToPath, URL, URLSearchParams } from "node:url";
import vm from "node:vm";
const { fetch, setTimeout } = globalThis;
const repo = fileURLToPath(new URL("..", import.meta.url));
const require = createRequire(new URL("../examples/react-store/package.json", import.meta.url));
const { chromium, expect } = require("@playwright/test");
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
  !cfg.deliveryAdminRole ||
  [cfg.baseUrl, cfg.studioOrigin, cfg.appOrigin].some((u) => new URL(u).hostname !== "127.0.0.1")
)
  throw Error("Isolated loopback stand with the GitHub catalogue only");
const output = resolve(process.env.STUDIO_REVIEW_OUTPUT);
await mkdir(output, { recursive: true });
const standDir = dirname(process.env.STUDIO_REVIEW_ADMIN_FILE);
const report = {
  requirements: "STU-355..374",
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
      "postgres",
      "-d",
      process.env.PREVIEW_PG_DB,
      "-At",
      "-c",
      text,
    ],
    { encoding: "utf8", windowsHide: true },
  ).trim();
async function admin(name, payload) {
  const response = await fetch(cfg.baseUrl + "/api/v1/commands/" + name, {
    method: "POST",
    headers: {
      Authorization: "Bearer " + cfg.credential,
      "X-CMS-Project": cfg.project,
      "Content-Type": "application/json",
      "Idempotency-Key": randomUUID(),
    },
    body: JSON.stringify({ payload, reason: "Isolated component acceptance fixture" }),
  });
  const data = await response.json();
  if (!response.ok) throw Error(name + " " + response.status + " " + data.code);
  return data.result;
}
const browser = await chromium.launch(
  process.env.PLAYWRIGHT_EXECUTABLE_PATH
    ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH }
    : {},
);
try {
  const author = await browser.newContext({ viewport: { width: 1440, height: 1100 } }),
    reviewer = await browser.newContext();
  const alice = await author.newPage(),
    bob = await reviewer.newPage();
  for (const page of [alice, bob])
    page.on("pageerror", (error) => console.error("PAGE ERROR", error.message, error.stack));
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
  async function human(context, account, name, payload, allowFailure = false) {
    const response = await context.request.post(cfg.baseUrl + "/api/v1/commands/" + name, {
      headers: {
        "X-CMS-Project": cfg.project,
        Origin: cfg.studioOrigin,
        "X-CSRF-Token": account.csrfToken,
        "Idempotency-Key": randomUUID(),
      },
      data: { payload, reason: "Isolated component acceptance" },
    });
    const data = await response.json();
    if (!response.ok() && !allowFailure)
      throw Error(name + " " + response.status() + " " + data.code);
    return allowFailure ? { ok: response.ok(), data } : data.result;
  }
  sql(
    `UPDATE projects SET settings=settings || '{"defaultMode":"CODE","maxMode":"CODE"}' WHERE slug='${cfg.project}'`,
  );
  await admin("set-approval-policy", { low: 1, medium: 1, high: 1 });
  // Operator enables the GitHub configuration (STU-295).
  await admin("bind-human-role", { actorId: bobAccount.user.id, roleId: cfg.deliveryAdminRole });
  await human(reviewer, bobAccount, "set-capability-github-config", {
    environment: "staging",
    configId: "local",
    enabled: true,
    expectedConfigRevision: (await read(reviewer, "/capability-github-config?environment=staging"))
      .revision,
  });
  await admin("unbind-human-role", { actorId: bobAccount.user.id, roleId: cfg.deliveryAdminRole });

  const initial = await read(author, "/manifest?environment=staging");
  expect(initial.manifest.components?.PromoBanner).toBeUndefined();
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
  async function draft(title, path) {
    const cs = await human(author, aliceAccount, "create-changeset", {
      title,
      targets: ["staging"],
    });
    const page = await human(author, aliceAccount, "apply-operations", {
      changesetId: cs.id,
      expectedSeq: 0,
      operations: [
        {
          type: "document.create",
          payload: {
            kind: "page",
            path,
            root: { id: "n_store", type: "Storefront", props: { config: { base: catalog } } },
          },
        },
      ],
    });
    return { cs: cs.id, page: page.operations[0].target };
  }
  const one = await draft("Component first page", "/store");
  const two = await draft("Component second page", "/store-second");
  Object.assign(report, { changesets: [one.cs, two.cs] });
  const openEditor = async (d, node = "n_store") => {
    await alice.goto(
      cfg.studioOrigin + "/editor#" + new URLSearchParams({ document: d.page, changeset: d.cs }),
    );
    await alice.reload();
    await alice.locator(`[role="treeitem"][title="${node}"]`).click();
  };
  async function compose(dialog) {
    await dialog.getByLabel("Имя компонента", { exact: true }).fill("PromoBanner");
    await dialog.getByLabel("Заголовок", { exact: true }).fill("Promo banner");
    await dialog
      .getByLabel("Описание задачи", { exact: true })
      .fill("A promotional banner with a title and nested call to action.");
    await dialog
      .getByLabel("Критерии приёмки, по одному в строке", { exact: true })
      .fill("Shows the title on every width");
    await dialog.getByRole("button", { name: "Добавить свойство", exact: true }).click();
    await dialog.getByLabel("Имя свойства", { exact: true }).last().fill("title");
    await dialog.getByLabel("Обязательный", { exact: true }).last().check();
    await dialog.getByLabel("Максимум", { exact: true }).last().fill("200");
    await dialog.getByRole("button", { name: "Добавить свойство", exact: true }).click();
    await dialog.getByLabel("Имя свойства", { exact: true }).last().fill("tone");
    await dialog
      .getByLabel(/^Тип свойства/)
      .last()
      .selectOption("enum");
    await dialog.getByLabel("Варианты через запятую", { exact: true }).fill("plain, accent");
    await dialog.getByLabel("Принимает вложенные блоки", { exact: true }).check();
    await dialog.getByLabel("title", { exact: true }).fill("Летняя распродажа");
    await dialog.getByLabel(/^tone/).selectOption("accent");
    await dialog.getByRole("button", { name: "Сохранить черновик запроса", exact: true }).click();
  }

  // STU-359/360: the designer requests the missing component for the container.
  await openEditor(one);
  await alice.getByRole("button", { name: "Нужен другой компонент…", exact: true }).click();
  let dialog = alice.getByRole("dialog");
  await compose(dialog);
  await dialog
    .getByRole("button", { name: "Проверить передаваемый контекст", exact: true })
    .click();
  await dialog
    .getByLabel("Передать этот context pack участникам проекта с code.propose", { exact: true })
    .check();
  await dialog.getByRole("button", { name: "Отправить запрос", exact: true }).click();
  await expect(
    dialog.getByRole("button", { name: "Скачать context pack", exact: true }),
  ).toBeVisible();
  await dialog.getByRole("button", { name: "Закрыть", exact: true }).click();
  const requestId = (await read(author, "/capability-requests?environment=staging&key=PromoBanner"))
    .items[0].id;
  const request = await read(author, "/capability-requests/" + requestId);
  report.requestId = requestId;
  let page = await read(author, "/documents/" + one.page + "?changesetId=" + one.cs);
  expect(page.body.nodes[request.nodeId]).toMatchObject({
    type: "PromoBanner",
    pending: requestId,
    props: { title: "Летняя распродажа", tone: "accent" },
  });
  // STU-366: the live preview shows a neutral placeholder block.
  await alice.reload();
  await expect(
    alice.frameLocator("iframe").locator(`[data-cms-pending="${requestId}"]`),
  ).toBeVisible({ timeout: 30000 });
  await alice.locator(`[role="treeitem"][title="${request.nodeId}"]`).click();
  await expect(alice.getByText("Ожидает компонент PromoBanner")).toBeVisible();
  report.placeholderViaUI = true;

  // STU-363: the second page inserts another instance of the open request.
  await openEditor(two);
  await alice.getByRole("button", { name: "Нужен другой компонент…", exact: true }).click();
  dialog = alice.getByRole("dialog");
  await compose(dialog);
  await dialog.getByRole("button", { name: "Вставить ещё экземпляр", exact: true }).click();
  await expect(dialog.getByText("Экземпляр запрошенного компонента вставлен.")).toBeVisible();
  await dialog.getByRole("button", { name: "Закрыть", exact: true }).click();
  let fulfillment = await read(author, "/capability-requests/" + requestId + "/fulfillment");
  expect(fulfillment.anchorCount).toBe(2);
  report.instanceInsertedViaUI = true;

  // STU-358: a draft with a placeholder fails its checks and cannot be published.
  const failed = await human(author, aliceAccount, "submit-changeset", {
    changesetId: one.cs,
    expectedSeq: (await read(author, "/changesets/" + one.cs)).seq,
  });
  expect(JSON.stringify(failed)).toContain("COMPONENT_PENDING_CAPABILITY");
  const publish = await human(
    author,
    aliceAccount,
    "publish",
    {
      changesetId: one.cs,
      environment: "staging",
      expectedSeq: (await read(author, "/changesets/" + one.cs)).seq,
    },
    true,
  );
  expect(publish.ok).toBe(false);
  await human(author, aliceAccount, "reopen-changeset", {
    changesetId: one.cs,
    expectedSeq: (await read(author, "/changesets/" + one.cs)).seq,
  });
  report.pendingPublicationBlocked = true;

  // Engineering: accepted → in progress → PR open.
  let revision = (await read(reviewer, "/capability-requests/" + requestId)).revision;
  for (const transition of ["accepted", "in_progress", "pr_open"])
    await human(reviewer, bobAccount, "update-capability-request", {
      id: requestId,
      expectedRevision: revision++,
      transition,
      ...(transition === "pr_open" ? { prUrl: "https://example.test/local/store/pull/1" } : {}),
    });

  // The PR build with PromoBanner: the real registry.
  execFileSync(
    process.execPath,
    [process.env.CMS_PNPM_CLI, "--filter", "@cms/store-example", "build"],
    { cwd: repo, stdio: "pipe", windowsHide: true },
  );
  const built = JSON.parse(
    await readFile(resolve(repo, "examples/react-store/.cms/code-index.json"), "utf8"),
  );
  const mergeSha = built.repo.commit;
  const config = sql(
    `SELECT id||','||revision||','||fingerprint FROM capability_github_configs WHERE environment='staging'`,
  ).split(",");
  const facts = JSON.stringify({
    repositoryId: "42",
    prId: "1001",
    number: 1,
    state: "merged",
    draft: false,
    headRepositoryId: "42",
    headSha: "a".repeat(40),
    baseSha: "b".repeat(40),
    baseRef: "dev",
    integrationType: "merged_commit",
    integrationSha: mergeSha,
    policyDigest: "local",
    ci: "success",
    results: [],
  });
  const link = randomUUID();
  sql(`INSERT INTO capability_github_links(id,project_id,request_id,config_id,config_revision,fingerprint,pull_number,initiated_by,state,sync_id)
    SELECT '${link}',project_id,id,'${config[0]}',${config[1]},'${config[2]}',1,'${bobAccount.user.id}','idle',gen_random_uuid() FROM capability_requests WHERE id='${requestId}'`);
  sql(`INSERT INTO capability_github_observations(id,project_id,request_id,link_id,generation,config_revision,fingerprint,sync_id,body,digest)
    SELECT gen_random_uuid(),project_id,request_id,id,generation,config_revision,fingerprint,gen_random_uuid(),'${facts}'::jsonb,'local' FROM capability_github_links WHERE id='${link}'`);

  // Deploy the PR build and register its manifest with the real CLI.
  const processesFile = resolve(standDir, "studio-work-review.processes.json");
  const processes = JSON.parse(await readFile(processesFile, "utf8"));
  process.kill(processes.storePid);
  const log = await open(resolve(output, "realized-store.log"), "a");
  const store = spawn(
    process.execPath,
    [resolve(repo, "examples/react-store/dist/server/server.js")],
    {
      cwd: repo,
      env: {
        ...process.env,
        NODE_ENV: "production",
        PORT: new URL(cfg.appOrigin).port,
        CMS_STORE_CONFIG_FILE: resolve(standDir, "studio-work-store.private.json"),
        CMS_STUDIO_ORIGIN: cfg.studioOrigin,
        CMS_APP_ORIGIN: cfg.appOrigin,
      },
      stdio: ["ignore", log.fd, log.fd],
      windowsHide: true,
    },
  );
  store.unref();
  processes.storePid = store.pid;
  await writeFile(processesFile, JSON.stringify(processes));
  await log.close();
  const push = () =>
    execFileSync(
      process.execPath,
      [
        resolve(repo, "packages/component-registry/bin/cms.mjs"),
        "manifest",
        "push",
        "--dir",
        resolve(repo, "examples/react-store/.cms"),
        "--base-url",
        cfg.baseUrl,
        "--project",
        cfg.project,
        "--env",
        "staging",
        "--token-file",
        cfg.tokenFile,
        "--wait-for-deploy",
        cfg.appOrigin,
      ],
      { cwd: repo, stdio: "pipe", windowsHide: true },
    );
  push();
  const realized = await read(author, "/manifest?environment=staging");
  expect(realized.manifest.components.PromoBanner).toBeDefined();

  // STU-369/370: automatic fulfilment clears both instances' markers.
  await expect
    .poll(async () => (await read(author, "/capability-requests/" + requestId)).status, {
      timeout: 60000,
    })
    .toBe("fulfilled");
  await expect
    .poll(
      async () =>
        (await read(author, "/capability-requests/" + requestId + "/fulfillment")).anchors
          .map((a) => a.state)
          .join(","),
      { timeout: 60000 },
    )
    .toBe("replaced,replaced");
  fulfillment = await read(author, "/capability-requests/" + requestId + "/fulfillment");
  expect(fulfillment.mode).toBe("automatic");
  expect(fulfillment.commit).toBe(mergeSha);
  page = await read(author, "/documents/" + one.page + "?changesetId=" + one.cs);
  expect(page.body.nodes[request.nodeId].pending).toBeUndefined();
  expect(JSON.stringify(await read(author, "/changesets/" + one.cs + "/history"))).toContain(
    "cms-system",
  );
  report.automaticFulfillment = { commit: mergeSha, systemActorInHistory: true };

  // STU-346: undo in the second Change Set reverts; retry replaces again.
  await human(author, aliceAccount, "undo", {
    changesetId: two.cs,
    expectedSeq: (await read(author, "/changesets/" + two.cs)).seq,
  });
  await expect
    .poll(async () =>
      (await read(author, "/capability-requests/" + requestId + "/fulfillment")).anchors.some(
        (a) => a.state === "reverted",
      ),
    )
    .toBe(true);
  const reverted = (
    await read(author, "/capability-requests/" + requestId + "/fulfillment")
  ).anchors.find((a) => a.state === "reverted");
  await human(author, aliceAccount, "retry-capability-fulfillment", {
    requestId,
    anchorId: reverted.id,
    expectedAnchorRevision: reverted.revision,
  });
  await expect
    .poll(
      async () =>
        (await read(author, "/capability-requests/" + requestId + "/fulfillment")).anchors.every(
          (a) => a.state === "replaced",
        ),
      { timeout: 60000 },
    )
    .toBe(true);
  report.undoAndRetry = true;

  // 375 px.
  await openEditor(one);
  await alice.setViewportSize({ width: 375, height: 900 });
  await expect
    .poll(() =>
      alice.evaluate(
        () => globalThis.document.documentElement.scrollWidth <= globalThis.innerWidth,
      ),
    )
    .toBe(true);
  await alice.screenshot({ path: resolve(output, "component-mobile.png"), fullPage: true });
  await alice.setViewportSize({ width: 1440, height: 1100 });

  // Independent approval and publication of the first page; public browser.
  await openEditor(one);
  await alice.getByRole("button", { name: "Отправить на проверку", exact: true }).click();
  await expect
    .poll(async () => (await read(author, "/changesets/" + one.cs)).state)
    .toBe("in_review");
  await bob.goto(cfg.studioOrigin + "/editor#work=" + one.cs);
  await bob.reload();
  await bob.getByRole("button", { name: "Согласовать", exact: true }).click();
  await expect
    .poll(async () => (await read(author, "/changesets/" + one.cs)).state)
    .toBe("approved");
  await alice.reload();
  await alice.getByRole("button", { name: "Опубликовать", exact: true }).click();
  await alice.getByRole("button", { name: "Подтвердить публикацию", exact: true }).click();
  await expect.poll(async () => (await read(author, "/changesets/" + one.cs)).state).toBe("merged");
  const publicPage = await (await browser.newContext()).newPage();
  await publicPage.goto(cfg.appOrigin + "/store");
  await expect(
    publicPage.locator("section.promo-accent").getByRole("heading", { name: "Летняя распродажа" }),
  ).toBeVisible();
  report.publishedAndRendered = true;
  await writeFile(resolve(output, "component-acceptance.json"), JSON.stringify(report, null, 2));
  console.log(
    "PASS STU-374: requested, placeholder, second instance, merged, deployed, fulfilled, approved, published.",
  );
} catch (error) {
  const page = browser.contexts().flatMap((c) => c.pages())[0];
  if (page) {
    console.error((await page.locator("body").innerText()).slice(-3000));
    await page.screenshot({ path: resolve(output, "component-failure.png"), fullPage: true });
  }
  throw error;
} finally {
  await browser.close();
}
