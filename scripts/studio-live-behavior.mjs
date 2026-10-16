// STU-254: a real isolated CMS/PostgreSQL/OIDC/React journey. Only fixture setup
// uses the API; every new on/when below is configured through Studio forms.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { resolve } from "node:path";
import { randomUUID } from "node:crypto";
import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";
import vm from "node:vm";
import { URL } from "node:url";
import { documentHash } from "../packages/runtime/dist/index.js";
const { fetch } = globalThis;
const require = createRequire(new URL("../examples/react-store/package.json", import.meta.url));
const { chromium, expect } = require("@playwright/test");
if (!process.env.STUDIO_REVIEW_ADMIN_FILE || !process.env.STUDIO_REVIEW_OUTPUT)
  throw Error("Local config/output required");
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
const stamp = randomUUID().slice(0, 8);
const report = {
  requirements: "STU-237..254",
  stamp,
  sourceSha: execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim(),
  sourceDirty: !!execFileSync("git", ["status", "--porcelain"], { encoding: "utf8" }).trim(),
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
    body: JSON.stringify({ payload, reason: "Isolated behavior acceptance setup" }),
  });
  const body = await response.json();
  if (!response.ok) throw Error(name + " " + response.status + " " + body.code);
  return body.result;
}
try {
  const author = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
  const reviewer = await browser.newContext();
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
    expect(account.user.name).toBe(name);
    await admin("bind-human-role", { actorId: account.user.id, roleId });
    await page.reload();
    return account;
  }
  const aliceAccount = await login(alice, "Alice", cfg.authorRole);
  await login(bob, "Bob", cfg.reviewerRole);
  async function read(context, path) {
    const r = await context.request.get(cfg.baseUrl + "/api/v1" + path, {
      headers: { "X-CMS-Project": cfg.project },
    });
    if (!r.ok()) throw Error(path + " " + r.status());
    return r.json();
  }
  async function human(name, payload) {
    const r = await author.request.post(cfg.baseUrl + "/api/v1/commands/" + name, {
      headers: {
        "X-CMS-Project": cfg.project,
        Origin: cfg.studioOrigin,
        "X-CSRF-Token": aliceAccount.csrfToken,
        "Idempotency-Key": randomUUID(),
      },
      data: { payload, reason: "Isolated behavior acceptance fixture" },
    });
    const body = await r.json();
    if (!r.ok()) throw Error(name + " " + r.status() + " " + body.code);
    return body.result;
  }
  // Activate the stand's schema through the existing human schema workflow.
  const manifest = JSON.parse(
    await readFile(new URL("../examples/react-store/.cms/manifest.json", import.meta.url), "utf8"),
  );
  manifest.schemas = {
    ...manifest.schemas,
    Article: { version: 1, title: "Article", fields: { title: { type: "text", localized: true } } },
    Collection: {
      version: 1,
      title: "Collection",
      fields: {
        title: { type: "text", localized: true },
        handle: { type: "string", required: true, localized: true, maxLength: 64 },
      },
    },
  };
  const registration = await admin("register-manifest", { environment: "staging", manifest });
  if (registration.changesetId) {
    const role = await admin("create-role", {
      name: "behavior-schema-" + stamp,
      capabilities: ["schema.apply", "schema.read"],
    });
    await admin("bind-human-role", { actorId: aliceAccount.user.id, roleId: role.id });
    await admin("set-approval-policy", { low: 0, medium: 0, high: 0 });
    const cs = await read(author, "/changesets/" + registration.changesetId);
    const claimed = await human("claim-schema-changeset", {
      changesetId: cs.id,
      expectedSeq: cs.seq,
    });
    await human("submit-changeset", { changesetId: cs.id, expectedSeq: claimed.seq });
    await human("publish", {
      changesetId: cs.id,
      expectedSeq: claimed.seq,
      environment: "staging",
    });
    await admin("unbind-human-role", { actorId: aliceAccount.user.id, roleId: role.id });
  }
  await admin("set-approval-policy", { low: 1, medium: 1, high: 1 });
  const catalogSource = await readFile(
    new URL("../examples/react-store/scripts/catalog.ts", import.meta.url),
    "utf8",
  );
  const catalog = vm.runInNewContext(
    "(" + catalogSource.match(/export const catalog: Catalog = ([\s\S]*);/)[1] + ")",
  );
  delete catalog.heroAsset;
  catalog.products = catalog.products.slice(0, 2).map((p, i) => {
    const next = { ...p, title: "Behavior " + stamp + " item " + (i + 1) };
    delete next.asset;
    return next;
  });
  expect(catalog.products[0].category).not.toBe(catalog.products[1].category);
  const cs = await human("create-changeset", {
    title: "Действия и условия " + stamp,
    targets: ["staging"],
  });
  const entityResult = await human("apply-operations", {
    changesetId: cs.id,
    expectedSeq: 0,
    operations: [
      {
        type: "entity.create",
        payload: {
          schema: "Collection",
          environment: "staging",
          data: {
            title: { ru: "Поведение " + stamp, en: "Behavior " + stamp },
            handle: { ru: "all", en: catalog.products[0].category },
          },
        },
      },
    ],
  });
  const entity = entityResult.operations[0].target;
  const pageResult = await human("apply-operations", {
    changesetId: cs.id,
    expectedSeq: 1,
    operations: [
      {
        type: "document.create",
        payload: {
          kind: "page",
          path: "/store",
          content: { schema: "Collection", resolve: { by: "entity", entityId: entity } },
          root: {
            id: "n_store",
            type: "Storefront",
            props: { config: { base: catalog } },
            children: [
              { id: "n_static", type: "Text", bindings: { text: "$local.t_static" } },
              {
                id: "n_repeat",
                type: "Repeat",
                props: { key: "$item.id", limit: 2 },
                bindings: { items: "$data.featured.items" },
                children: [
                  {
                    id: "n_card",
                    type: "Stack",
                    children: [
                      { id: "n_title", type: "Text", bindings: { text: "$item.title" } },
                      {
                        id: "n_favorite",
                        type: "Button",
                        bindings: { label: "$local.t_favorite" },
                      },
                      { id: "n_target", type: "Text", bindings: { text: "$local.t_target" } },
                    ],
                  },
                ],
              },
              { id: "n_outer", type: "Text", bindings: { text: "$local.t_outer" } },
            ],
          },
        },
      },
    ],
  });
  const pageId = pageResult.operations[0].target;
  const texts = {
    t_static: { ru: "Независимый раздел", en: "Independent section" },
    t_favorite: { ru: "В избранное", en: "Favorite" },
    t_target: { ru: "Цель карточки", en: "Item target" },
    t_outer: { ru: "Общая цель", en: "Outer target" },
  };
  await human("apply-operations", {
    changesetId: cs.id,
    expectedSeq: 2,
    operations: [
      ...Object.entries(texts).map(([key, value]) => ({
        type: "localContent.set",
        target: pageId,
        payload: { key, type: "text", value },
      })),
      {
        type: "document.setDataSource",
        target: pageId,
        payload: {
          key: "featured",
          use: {
            source: "store.collectionProducts",
            params: { collection: "$content.handle", locale: "$context.locale", limit: { lit: 2 } },
          },
        },
      },
    ],
  });
  const editorUrl = cfg.studioOrigin + "/editor#document=" + pageId + "&changeset=" + cs.id;
  await alice.goto(editorUrl);
  await alice.reload();
  await alice.evaluate(() => {
    globalThis.reviewRendered = 0;
    globalThis.addEventListener("message", (event) => {
      if (event.data?.cms === "preview" && event.data.type === "rendered") {
        globalThis.reviewRendered++;
        globalThis.reviewRenderedHash = event.data.payload.hash;
      }
    });
  });
  const panel = alice.getByRole("region", { name: "Поведение и видимость", exact: true });
  const frame = alice.frameLocator("iframe");
  const select = (id) => alice.locator('[role="treeitem"][title="' + id + '"]').click();
  const behavior = () =>
    panel.getByRole("button", { name: "Настроить событие click", exact: true }).click();
  async function save() {
    await panel.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(panel.locator("form")).not.toBeVisible();
    const current = await read(author, "/documents/" + pageId + "?changesetId=" + cs.id);
    const hash = await documentHash(current.body);
    await expect.poll(() => alice.evaluate(() => globalThis.reviewRenderedHash)).toBe(hash);
  }
  async function mode(name) {
    await alice.getByRole("button", { name, exact: true }).click();
    await expect(alice.getByRole("button", { name, exact: true })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await frame
      .locator("body")
      .evaluate(
        () =>
          new Promise((resolve) =>
            globalThis.requestAnimationFrame(() => globalThis.requestAnimationFrame(resolve)),
          ),
      );
  }
  async function add(action) {
    await panel.getByLabel("Действие", { exact: true }).selectOption(action);
    await panel.getByRole("button", { name: "Добавить действие", exact: true }).click();
  }
  async function scrollTarget(id, index) {
    await panel
      .getByLabel("Источник аргумента target", { exact: true })
      .nth(index)
      .selectOption("target");
    await panel.getByLabel("Целевой узел target", { exact: true }).nth(index).selectOption(id);
  }
  await expect(frame.getByRole("button", { name: "В избранное", exact: true })).toHaveCount(2);
  await select("n_favorite");
  await behavior();
  await add("scrollTo");
  await scrollTarget("n_target", 0);
  await add("scrollTo");
  await scrollTarget("n_outer", 1);
  await save();
  // A safe DOM fixture observes the actual builtin handler and can fail its first
  // invocation. It performs no external request or application mutation.
  async function steps(fail = false) {
    await frame.locator("body").evaluate((_, fail) => {
      globalThis.reviewSteps = [];
      globalThis.reviewFail = fail;
      globalThis.HTMLElement.prototype.scrollIntoView = function () {
        globalThis.reviewSteps.push(this.id);
        if (globalThis.reviewFail) throw Error("private fixture failure");
      };
    }, fail);
  }
  await steps();
  const favorite = frame.getByRole("button", { name: "В избранное", exact: true });
  await favorite.nth(0).click();
  await favorite.nth(0).focus();
  await favorite.nth(0).press("Enter");
  expect(await frame.locator("body").evaluate(() => globalThis.reviewSteps)).toEqual([]);
  await mode("Взаимодействие");
  await favorite.nth(1).click();
  const secondTarget = (await favorite.nth(1).getAttribute("id")).replace(
    /n_favorite$/,
    "n_target",
  );
  expect(await frame.locator("body").evaluate(() => globalThis.reviewSteps)).toEqual([
    secondTarget,
    "n_outer",
  ]);
  await steps(true);
  await favorite.nth(0).click();
  expect(await frame.locator("body").evaluate(() => globalThis.reviewSteps)).toHaveLength(1);
  await expect(
    frame.getByText("Не получилось выполнить действие. Попробуйте снова.", { exact: true }),
  ).toBeVisible();
  await mode("Дизайн");
  await behavior();
  await panel.getByRole("button", { name: "Удалить действие 2", exact: true }).click();
  await panel.getByRole("button", { name: "Удалить действие 1", exact: true }).click();
  await add("store.favorite");
  for (const name of ["id", "title"]) {
    await panel.getByLabel("Значение параметра " + name, { exact: true }).selectOption("path");
    await panel.getByLabel("Путь параметра " + name, { exact: true }).selectOption("$item." + name);
  }
  await add("navigate");
  await panel.getByLabel("Источник аргумента to", { exact: true }).selectOption("anchor");
  await panel.getByLabel("Целевой узел to", { exact: true }).selectOption("n_target");
  const pattern = "**/api/v1/commands/apply-operations",
    attempts = [];
  await alice.route(pattern, async (route) => {
    attempts.push({
      key: route.request().headers()["idempotency-key"],
      body: route.request().postData(),
    });
    const response = await route.fetch();
    expect(response.ok()).toBe(true);
    await route.abort("failed");
  });
  await panel.getByRole("button", { name: "Сохранить", exact: true }).click();
  const retry = alice.getByRole("button", { name: "Повторить точное сохранение", exact: true });
  await expect(retry).toBeVisible();
  await alice.unroute(pattern);
  const request = alice.waitForRequest((r) => r.url().endsWith("/apply-operations"));
  await retry.click();
  const replay = await request;
  expect({ key: replay.headers()["idempotency-key"], body: replay.postData() }).toEqual(
    attempts[0],
  );
  await expect(panel.locator("form")).not.toBeVisible();
  await mode("Взаимодействие");
  await favorite.nth(1).click();
  await expect(
    frame.getByText("«" + catalog.products[1].title + "» добавлен в избранное", { exact: true }),
  ).toBeVisible();
  await expect
    .poll(() => frame.locator("body").evaluate(() => globalThis.location.hash))
    .toBe("#" + encodeURIComponent(secondTarget));
  // While a new locale is in flight, the old visible button must not execute.
  let started, release;
  const held = new Promise((resolve) => {
    started = resolve;
  });
  const released = new Promise((resolve) => {
    release = resolve;
  });
  const localePattern = "**/api/store?locale=en";
  await alice.route(localePattern, async (route) => {
    started();
    await released;
    // Keep this request in the browser so partitioned preview cookies stay scoped.
    await route.continue();
  });
  await alice.getByLabel("Язык", { exact: true }).selectOption("en");
  await held;
  await expect(
    frame.getByText("Предпросмотр обновляется. Действия временно недоступны.", { exact: true }),
  ).toBeVisible();
  await favorite.nth(0).click();
  expect(await frame.locator("body").evaluate(() => globalThis.location.hash)).toBe(
    "#" + encodeURIComponent(secondTarget),
  );
  await expect(
    frame.getByText("«" + catalog.products[0].title + "» добавлен в избранное", { exact: true }),
  ).not.toBeVisible();
  release();
  await expect(frame.getByRole("button", { name: "Favorite", exact: true })).toHaveCount(1);
  await alice.unroute(localePattern);
  await frame.getByRole("button", { name: "Favorite", exact: true }).click();
  await expect(
    frame.getByText("«" + catalog.products[0].title + "» добавлен в избранное", { exact: true }),
  ).toBeVisible();
  await alice.getByLabel("Language", { exact: true }).selectOption("ru");
  await expect(favorite).toHaveCount(2);
  await mode("Дизайн");
  await select("n_card");
  await panel.getByRole("button", { name: "Настроить условие", exact: true }).click();
  await panel.getByLabel("Оператор", { exact: true }).selectOption("eq");
  await panel.getByLabel("Поле условия", { exact: true }).selectOption("$item.category");
  await panel.getByLabel("Значение параметра Сравнение", { exact: true }).selectOption("literal");
  await panel.getByLabel("Литерал Сравнение", { exact: true }).fill(catalog.products[0].category);
  await save();
  await expect(favorite).toHaveCount(1);
  // A conditionally hidden template stays selectable and editable without a rectangle.
  await select("n_card");
  await expect(panel.getByRole("button", { name: "Настроить условие", exact: true })).toBeEnabled();
  await alice.getByRole("button", { name: "Отменить", exact: true }).click();
  await expect(favorite).toHaveCount(2);
  await alice.getByRole("button", { name: "Повторить", exact: true }).click();
  await expect(favorite).toHaveCount(1);
  await alice.getByLabel("Язык", { exact: true }).selectOption("en");
  await expect(frame.getByRole("button", { name: "Favorite", exact: true })).toHaveCount(1);
  await expect(frame.getByText("Independent section", { exact: true })).toBeVisible();
  await alice.getByLabel("Language", { exact: true }).selectOption("ru");
  await expect(frame.getByText("Независимый раздел", { exact: true })).toBeVisible();
  // A real concurrent write makes the open form stale; it must retain user input.
  await panel.getByRole("button", { name: "Настроить условие", exact: true }).click();
  const current = await read(author, "/changesets/" + cs.id);
  await human("apply-operations", {
    changesetId: cs.id,
    expectedSeq: current.seq,
    operations: [
      {
        type: "node.rename",
        target: pageId,
        payload: { nodeId: "n_static", name: "Concurrent fixture change" },
      },
    ],
  });
  await panel.getByLabel("Литерал Сравнение", { exact: true }).fill("retained after conflict");
  await panel.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(panel.getByLabel("Литерал Сравнение", { exact: true })).toHaveValue(
    "retained after conflict",
  );
  await expect(
    alice.getByRole("alert").filter({ hasText: "CHANGESET_SEQ_CONFLICT" }),
  ).toBeVisible();
  await panel.getByRole("button", { name: "Сбросить ввод", exact: true }).click();
  await alice.getByRole("button", { name: "Перечитать черновик", exact: true }).click();
  await expect(alice.locator(".editor-save-state")).toContainText("Черновик загружен");
  await alice.setViewportSize({ width: 375, height: 1000 });
  expect(
    await alice.evaluate(
      () => globalThis.document.documentElement.scrollWidth <= globalThis.innerWidth,
    ),
  ).toBe(true);
  await alice.screenshot({ path: resolve(output, "behavior-mobile.png"), fullPage: true });
  await alice.setViewportSize({ width: 1440, height: 1100 });
  const before = await browser.newContext(),
    publicBefore = await before.newPage();
  await publicBefore.goto(cfg.appOrigin + "/store");
  expect(await publicBefore.getByRole("button", { name: "В избранное", exact: true }).count()).toBe(
    0,
  );
  const body = (await read(author, "/documents/" + pageId + "?changesetId=" + cs.id)).body;
  expect(body.nodes.n_favorite.on.click[0].args).toEqual({ id: "$item.id", title: "$item.title" });
  expect(body.nodes.n_card.when.args[1]).toEqual({ lit: catalog.products[0].category });
  const reviewPanel = alice.getByRole("region", { name: "Проверки и публикация", exact: true });
  await reviewPanel.getByRole("button", { name: "Отправить на проверку", exact: true }).click();
  await expect(reviewPanel.getByText("На согласовании", { exact: true })).toBeVisible();
  await expect(
    reviewPanel.getByRole("button", { name: "Согласовать", exact: true }),
  ).toBeDisabled();
  await bob.goto(cfg.studioOrigin + "/editor#work=" + cs.id);
  await bob.reload();
  const bobReview = bob.getByRole("region", { name: "Проверки и публикация", exact: true });
  await bobReview.getByRole("button", { name: "Согласовать", exact: true }).click();
  await expect(bobReview.getByText("Готов к публикации", { exact: true })).toBeVisible();
  await bobReview.getByRole("button", { name: "Опубликовать", exact: true }).click();
  await bobReview.getByRole("button", { name: "Подтвердить публикацию", exact: true }).click();
  await expect(bobReview).toContainText("Публикация подтверждена: staging");
  const publicContext = await browser.newContext(),
    publicPage = await publicContext.newPage();
  await publicPage.goto(cfg.appOrigin + "/store");
  await expect(publicPage.getByRole("button", { name: "В избранное", exact: true })).toHaveCount(1);
  await publicPage.getByRole("button", { name: "В избранное", exact: true }).click();
  await expect(
    publicPage.getByText("«" + catalog.products[0].title + "» добавлен в избранное", {
      exact: true,
    }),
  ).toBeVisible();
  expect(await publicPage.locator("[data-cms-node]").count()).toBe(0);
  await publicPage.screenshot({ path: resolve(output, "behavior-published.png"), fullPage: true });
  const published = await read(author, "/changesets/" + cs.id);
  const delivery = await admin("create-delivery-key", {
    name: "behavior-evidence-" + stamp,
    environment: "staging",
  });
  const delivered = await fetch(
    cfg.baseUrl + "/delivery/v1/" + cfg.project + "/staging/page?path=/store&locale=ru",
    {
      headers: { Authorization: "Bearer " + delivery.key },
    },
  );
  expect(delivered.status).toBe(200);
  const deliveredPage = await delivered.json();
  Object.assign(report, {
    changesetId: cs.id,
    pageId,
    entityId: entity,
    seq: published.seq,
    pageVersionId: deliveredPage.page.versionId,
    contentVersions: deliveredPage.contentContext?.versions,
    createdBehaviorViaUI: true,
    createdConditionViaUI: true,
    exactRetry: true,
    staleHandlersBlocked: true,
    conflictRetained: true,
    sequentialEffects: true,
    stopsOnFirstError: true,
    designClickAndEnterNoEffects: true,
    repeatArgumentsAndTargets: true,
    locales: ["ru", "en"],
    undoRedo: true,
    mobileNoOverflow: true,
    twoHumanPublication: true,
    freshPublicBrowser: true,
    manifestHash: (
      await (await publicPage.request.get(cfg.appOrigin + "/.well-known/cms-manifest-hash")).text()
    ).trim(),
  });
  await writeFile(resolve(output, "behavior-acceptance.json"), JSON.stringify(report, null, 2));
  console.log(
    "PASS STU-254: UI on/when, two humans, Repeat/locale/design/interact, ordered failure, exact retry/conflict, undo/redo and fresh public browser.",
  );
} catch (error) {
  const page = browser.contexts().flatMap((c) => c.pages())[0];
  if (page) {
    console.error(
      "Editor controls: " +
        (await page
          .locator(".canvas-tools")
          .innerText()
          .catch(() => "")),
    );
    console.error(
      "Properties: " +
        (
          await page
            .locator(".properties")
            .innerText()
            .catch(() => "")
        ).slice(-2500),
    );
    console.error("Alerts: " + (await page.getByRole("alert").allTextContents()));
    console.error((await page.locator("body").innerText()).slice(-4500));
    await page.screenshot({ path: resolve(output, "behavior-failure.png"), fullPage: true });
  }
  throw error;
} finally {
  await browser.close();
}
