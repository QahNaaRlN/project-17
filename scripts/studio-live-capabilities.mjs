// STU-274: isolated CMS/PostgreSQL/OIDC/CLI/React acceptance. Requires the
// capability baseline build and CMS_REVIEW_CAPABILITIES=true review stand.
import { readFile, writeFile, mkdir, open } from "node:fs/promises";
import { execFileSync, spawn } from "node:child_process";
import { createRequire } from "node:module";
import { resolve, dirname } from "node:path";
import { randomUUID } from "node:crypto";
import { fileURLToPath, URL, URLSearchParams } from "node:url";
import vm from "node:vm";
const { fetch } = globalThis;
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
  requirements: "STU-255..274",
  sourceSha: execFileSync("git", ["rev-parse", "HEAD"], { cwd: repo, encoding: "utf8" }).trim(),
};
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
  await dialog.getByRole("button", { name: "Закрыть", exact: true }).click();
  const frame = alice.frameLocator("iframe");
  await expect(frame.getByRole("button", { name: "Save favorite", exact: true })).toBeVisible();
  await alice.getByRole("button", { name: "Взаимодействие", exact: true }).click();
  await frame.getByRole("button", { name: "Save favorite", exact: true }).click();
  await expect(frame.locator(".action-notice")).toHaveCount(0);
  // Independent developer reads only the submitted projection and safe export.
  await bob.getByRole("button", { name: "Запросы возможностей", exact: true }).click();
  await bob
    .getByRole("button", { name: "Save a favorite · store.favorite · submitted", exact: true })
    .click();
  const developer = bob.getByRole("dialog");
  const downloadPromise = bob.waitForEvent("download");
  await developer.getByRole("button", { name: "Скачать context pack", exact: true }).click();
  const download = await downloadPromise;
  const packPath = resolve(output, "context-pack.json");
  await download.saveAs(packPath);
  const exported = JSON.parse(await readFile(packPath, "utf8"));
  expect(exported.pinned.manifestHash).toBe(initial.manifestHash);
  expect(JSON.stringify(exported)).not.toContain('"Save favorite"');
  await developer.getByRole("button", { name: "Принять запрос", exact: true }).click();
  await developer.getByRole("button", { name: "Начать работу", exact: true }).click();
  await developer
    .getByLabel("Ссылка на PR", { exact: true })
    .fill("https://example.test/manual-review/pull/1");
  await developer.getByRole("button", { name: "Добавить PR", exact: true }).click();
  await developer.getByRole("button", { name: "Закрыть", exact: true }).click();
  // Restore the real action by rebuilding the original registry, deploy locally,
  // and use the real CLI's exact deployed hash gate before registration.
  execFileSync(
    process.execPath,
    [process.env.CMS_PNPM_CLI, "--filter", "@cms/store-example", "build"],
    { cwd: repo, stdio: "pipe", windowsHide: true },
  );
  const processesFile = resolve(
    dirname(process.env.STUDIO_REVIEW_ADMIN_FILE),
    "studio-work-review.processes.json",
  );
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
        CMS_STORE_CONFIG_FILE: resolve(
          dirname(process.env.STUDIO_REVIEW_ADMIN_FILE),
          "studio-work-store.private.json",
        ),
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
  const realized = await read(author, "/manifest?environment=staging");
  expect(realized.manifestHash).not.toBe(initial.manifestHash);
  expect(realized.manifest.actions["store.favorite"]).toBeDefined();
  await alice.reload();
  await alice.locator('[role="treeitem"][title="n_favorite"]').click();
  await alice.getByRole("button", { name: "Открыть запрос для click", exact: true }).click();
  const ready = alice.getByRole("dialog");
  await expect(ready.getByText("pr_open", { exact: false })).toBeVisible();
  for (const [key, value] of [
    ["id", "accepted-item"],
    ["title", "Accepted favorite"],
  ]) {
    await ready.getByLabel("Значение параметра " + key, { exact: true }).selectOption("literal");
    await ready.getByLabel("Литерал " + key, { exact: true }).fill(value);
  }
  await ready.getByRole("button", { name: "Применить доступное действие", exact: true }).click();
  await expect(ready.getByText(/fulfilled/)).toBeVisible();
  await ready.getByRole("button", { name: "Закрыть", exact: true }).click();
  const fulfilled = await read(author, "/capability-requests/" + requestId);
  expect(fulfilled.status).toBe("fulfilled");
  expect(fulfilled.packs[0].hash).toBe(report.packHash);
  await alice.setViewportSize({ width: 375, height: 900 });
  await expect
    .poll(() =>
      alice.evaluate(
        () => globalThis.document.documentElement.scrollWidth <= globalThis.innerWidth,
      ),
    )
    .toBe(true);
  await alice.screenshot({ path: resolve(output, "capability-mobile.png"), fullPage: true });
  await alice.setViewportSize({ width: 1440, height: 1100 });
  await alice.getByRole("button", { name: "Отправить на проверку", exact: true }).click();
  await expect
    .poll(async () => (await read(author, "/changesets/" + cs.id)).state)
    .toBe("in_review");
  await bob.goto(cfg.studioOrigin + "/editor#work=" + cs.id);
  await bob.reload();
  await bob.getByRole("button", { name: "Согласовать", exact: true }).click();
  await expect
    .poll(async () => (await read(author, "/changesets/" + cs.id)).state)
    .toBe("approved");
  await alice.reload();
  await alice.getByRole("button", { name: "Опубликовать", exact: true }).click();
  await alice.getByRole("button", { name: "Подтвердить публикацию", exact: true }).click();
  await expect.poll(async () => (await read(author, "/changesets/" + cs.id)).state).toBe("merged");
  const publicContext = await browser.newContext();
  const publicPage = await publicContext.newPage();
  await publicPage.goto(cfg.appOrigin + "/store");
  await publicPage.getByRole("button", { name: "Save favorite", exact: true }).click();
  await expect(
    publicPage.getByRole("status").filter({ hasText: "Accepted favorite" }),
  ).toBeVisible();
  Object.assign(report, {
    draftCreatedViaUI: true,
    contextSharedExplicitly: true,
    exactRetry: true,
    pendingPreviewPreservesNode: true,
    pendingDoesNotExecute: true,
    twoHumanHandoff: true,
    realCLIIndexUpload: true,
    manualEngineeringTransitions: true,
    explicitResolve: true,
    immutableOldPack: true,
    mobile375: true,
    twoHumanPublication: true,
    freshPublicBrowser: true,
    manifestHash: realized.manifestHash,
  });
  await writeFile(resolve(output, "capability-acceptance.json"), JSON.stringify(report, null, 2));
  console.log(
    "PASS STU-274: real forms, immutable pack, two humans, CLI deployment, explicit resolve and independently approved publication.",
  );
} catch (error) {
  const page = browser.contexts().flatMap((c) => c.pages())[0];
  if (page) {
    console.error((await page.locator("body").innerText()).slice(-3500));
    await page.screenshot({ path: resolve(output, "capability-failure.png"), fullPage: true });
    for (const frame of page.frames().slice(1))
      console.error(
        "Preview: " +
          (
            await frame
              .locator("body")
              .innerText()
              .catch(() => "")
          ).slice(-1200),
      );
  }
  throw error;
} finally {
  await browser.close();
}
