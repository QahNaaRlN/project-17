// STU-203…218: real loopback CMS/OIDC/PostgreSQL/storage; CS and objects created only in UI.
// One response is deliberately dropped AFTER the real server commits, to verify exact retry.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { resolve, dirname } from "node:path";
import { randomUUID, createHash } from "node:crypto";
import { createRequire } from "node:module";
import { URL } from "node:url";
import vm from "node:vm";
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
const stamp = randomUUID().slice(0, 8),
  report = { project: cfg.project, stamp, works: [], createdViaUI: true };
async function admin(name, payload) {
  const r = await fetch(cfg.baseUrl + "/api/v1/commands/" + name, {
    method: "POST",
    headers: {
      Authorization: "Bearer " + cfg.credential,
      "X-CMS-Project": cfg.project,
      "Content-Type": "application/json",
      "Idempotency-Key": randomUUID(),
    },
    body: JSON.stringify({ payload, reason: "Local content work acceptance role setup" }),
  });
  const d = await r.json();
  if (!r.ok) throw Error(name + " " + r.status + " " + d.code);
  return d.result;
}
try {
  const author = await browser.newContext({ viewport: { width: 1440, height: 1100 } }),
    reviewer = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
  const alice = await author.newPage(),
    bob = await reviewer.newPage();
  async function login(page, name, roleId) {
    await page.goto(cfg.studioOrigin + "/editor");
    await page.getByRole("link", { name: "Войти", exact: true }).click();
    await page.getByRole("link", { name: "Войти как " + name, exact: true }).click();
    await page.waitForURL(cfg.studioOrigin + "/editor**");
    const person = await (
      await page.context().request.get(cfg.studioOrigin + "/api/studio/auth/session")
    ).json();
    if (!person.authenticated || person.user.name !== name) throw Error("OIDC identity required");
    await admin("bind-human-role", { actorId: person.user.id, roleId });
    await page.reload();
    await expect(page.locator(".person-bar")).toContainText(name);
    return person;
  }
  await login(alice, "Alice", cfg.authorRole);
  await login(bob, "Bob", cfg.reviewerRole);
  const setupManifest = JSON.parse(
    await readFile(new URL("../examples/react-store/.cms/manifest.json", import.meta.url), "utf8"),
  );
  setupManifest.schemas = {
    ...setupManifest.schemas,
    Article: {
      version: 1,
      title: "Article",
      fields: {
        title: { type: "text", localized: true },
        cover: { type: "asset", assetKind: "image" },
        alt: { type: "text", localized: true },
      },
    },
  };
  const registration = await admin("register-manifest", {
    environment: "staging",
    manifest: setupManifest,
  });
  if (registration.changesetId) {
    const setupRole = await admin("create-role", {
      name: "binding-schema-" + stamp,
      capabilities: ["schema.apply", "schema.read"],
    });
    const setupAccount = await (
      await author.request.get(cfg.studioOrigin + "/api/studio/auth/session")
    ).json();
    await admin("bind-human-role", { actorId: setupAccount.user.id, roleId: setupRole.id });
    await admin("set-approval-policy", { low: 0, medium: 0, high: 0 });
    const setupCommand = async (name, payload) => {
      const r = await author.request.post(cfg.baseUrl + "/api/v1/commands/" + name, {
        headers: {
          "X-CMS-Project": cfg.project,
          Origin: cfg.studioOrigin,
          "X-CSRF-Token": setupAccount.csrfToken,
          "Idempotency-Key": randomUUID(),
        },
        data: { payload, reason: "Local schema setup" },
      });
      const v = await r.json();
      if (!r.ok()) throw Error(name + " " + r.status() + " " + v.code);
      return v.result;
    };
    const claimed = await setupCommand("claim-schema-changeset", {
      changesetId: registration.changesetId,
      expectedSeq: 1,
    });
    await setupCommand("submit-changeset", {
      changesetId: registration.changesetId,
      expectedSeq: claimed.seq,
    });
    await setupCommand("publish", {
      changesetId: registration.changesetId,
      environment: "staging",
      expectedSeq: claimed.seq,
    });
    await admin("set-approval-policy", { low: 1, medium: 1, high: 1 });
    await admin("unbind-human-role", { actorId: setupAccount.user.id, roleId: setupRole.id });
    await alice.reload();
  }
  async function read(context, path) {
    const r = await context.request.get(cfg.baseUrl + "/api/v1" + path, {
      headers: { "X-CMS-Project": cfg.project },
    });
    if (!r.ok()) throw Error(path + " " + r.status());
    return r.json();
  }
  async function newWork(title, lost = false) {
    title += " " + stamp;
    await alice.getByRole("button", { name: "Работы", exact: true }).click();
    await alice.getByRole("button", { name: "Новая работа", exact: true }).click();
    const dialog = alice.getByRole("dialog", { name: "Новая работа с контентом", exact: true });
    await dialog.getByLabel("Название работы", { exact: true }).fill(title);
    let committed;
    const pattern = "**/api/v1/commands/create-changeset";
    if (lost)
      await alice.route(pattern, async (route) => {
        const response = await route.fetch();
        expect(response.ok()).toBe(true);
        committed = (await response.json()).result;
        await route.abort("failed");
      });
    await dialog.getByRole("button", { name: "Создать работу", exact: true }).click();
    if (lost) {
      await expect(dialog).toContainText("Результат создания неизвестен");
      await alice.unroute(pattern);
      await alice.reload();
      await expect(dialog).toBeVisible();
      await dialog.getByRole("button", { name: "Проверить / повторить", exact: true }).click();
    }
    await expect(alice.locator("#work-card-title")).toHaveText(title);
    const id = new URL(alice.url()).hash.slice("#work=".length);
    if (lost) {
      expect(id).toBe(committed.id);
      const queue = await read(
        author,
        "/work-queue?environment=staging&q=" + encodeURIComponent(title),
      );
      expect(queue.items.filter((i) => i.title === title)).toHaveLength(1);
      report.lostResponseRecovered = true;
    }
    expect((await read(author, "/work-queue/" + id + "?environment=staging")).objects).toHaveLength(
      0,
    );
    await expect(
      alice.getByRole("button", { name: "Отправить на проверку", exact: true }),
    ).toBeDisabled();
    return { cs: { id, title } };
  }
  const png = await alice.screenshot({ type: "png" });
  async function addAsset(work) {
    await alice.getByRole("button", { name: "Загрузить файл", exact: true }).click();
    await alice
      .getByLabel("Файл ассета", { exact: true })
      .setInputFiles({ name: "content-work.png", mimeType: "image/png", buffer: png });
    await alice.getByRole("button", { name: "Загрузить / повторить", exact: true }).click();
    const attach = alice.getByRole("button", { name: "Добавить ассет в черновик", exact: true });
    await expect
      .poll(
        async () => {
          if (await attach.isVisible()) return true;
          const check = alice.getByRole("button", { name: "Проверить обработку", exact: true });
          if ((await check.isVisible()) && (await check.isEnabled())) await check.click();
          return attach.isVisible();
        },
        { timeout: 30000 },
      )
      .toBe(true)
      .catch(async (error) => {
        console.error("Upload UI: " + (await alice.locator(".asset-upload").innerText()));
        throw error;
      });
    const before = await read(author, "/work-queue/" + work.cs.id + "?environment=staging");
    expect(before.objects.some((o) => o.kind === "asset")).toBe(false);
    await attach.click();
    await expect
      .poll(
        async () =>
          (await read(author, "/work-queue/" + work.cs.id + "?environment=staging")).objects.filter(
            (o) => o.kind === "asset",
          ).length,
      )
      .toBe(1);
    const card = await read(author, "/work-queue/" + work.cs.id + "?environment=staging");
    return card.objects.find((o) => o.kind === "asset").id;
  }
  const delivery = JSON.parse(
    await readFile(
      resolve(dirname(process.env.STUDIO_REVIEW_ADMIN_FILE), "studio-work-store.private.json"),
      "utf8",
    ),
  );
  async function open(page, work) {
    await page.goto(cfg.studioOrigin + "/editor#work=" + work.cs.id);
    await page.reload();
    try {
      await expect(page.locator("#work-card-title")).toHaveText(work.cs.title);
    } catch (error) {
      const account = await (
        await page.context().request.get(cfg.studioOrigin + "/api/studio/auth/session")
      ).json();
      const card = await page
        .context()
        .request.get(cfg.baseUrl + "/api/v1/work-queue/" + work.cs.id + "?environment=staging", {
          headers: { "X-CMS-Project": cfg.project },
        });
      console.error(
        JSON.stringify({
          user: account.user?.name,
          rights: account.rights,
          cardStatus: card.status(),
          ui: (await page.locator("body").innerText()).slice(-1800),
        }),
      );
      await page.screenshot({ path: resolve(output, "content-work-failure.png") });
      throw error;
    }
  }
  async function publish(work, ids) {
    await open(alice, work);
    const authorPanel = alice.getByRole("region", { name: "Проверки и публикация", exact: true });
    await authorPanel.getByRole("button", { name: "Отправить на проверку", exact: true }).click();
    await expect(authorPanel.getByText("На согласовании", { exact: true })).toBeVisible();
    await expect(
      authorPanel.getByRole("button", { name: "Согласовать", exact: true }),
    ).toBeDisabled();
    await open(bob, work);
    const panel = bob.getByRole("region", { name: "Проверки и публикация", exact: true });
    await expect(bob.getByRole("region", { name: "Изменения работы", exact: true })).toBeVisible();
    await panel.getByRole("button", { name: "Согласовать", exact: true }).click();
    await expect(panel.getByText("Готов к публикации", { exact: true })).toBeVisible();
    await panel.getByRole("button", { name: "Опубликовать", exact: true }).click();
    await panel.getByRole("button", { name: "Подтвердить публикацию", exact: true }).click();
    await expect(panel).toContainText("Публикация подтверждена: staging");
    const objects = [];
    for (const { id, kind } of ids) {
      const result = await fetch(
        cfg.baseUrl + "/delivery/v1/" + cfg.project + "/staging/" + kind + "/" + id,
        { headers: { Authorization: "Bearer " + delivery.deliveryKey } },
      );
      expect(result.status).toBe(200);
      objects.push(await result.json());
    }
    report.works.push({
      id: work.cs.id,
      objectIds: ids,
      objectId: ids[0].id,
      kind: ids.length === 1 ? ids[0].kind : "mixed",
      expectedCount: ids.length,
      publicationConfirmed: true,
      delivered: true,
    });
    return objects;
  }

  const title = "Binding story " + stamp,
    alt = "Cover " + stamp;
  const work = await newWork("Привязки страницы");
  const cover = await addAsset(work);
  await alice.getByRole("button", { name: "Создать сущность", exact: true }).click();
  await alice.getByLabel("Схема контента", { exact: true }).selectOption("Article");
  await alice.getByRole("button", { name: "Новая сущность", exact: true }).click();
  await alice.getByLabel("Content title ru", { exact: true }).fill(title);
  await alice.getByLabel("Content alt ru", { exact: true }).fill(alt);
  await alice.getByLabel("Content cover", { exact: true }).selectOption(cover);
  await alice.getByLabel("Локаль контента", { exact: true }).selectOption("en");
  await alice.getByLabel("Content title en", { exact: true }).fill("English " + title);
  await alice.getByLabel("Content alt en", { exact: true }).fill("English " + alt);
  await alice.getByRole("button", { name: "Сохранить контент", exact: true }).click();
  await expect
    .poll(
      async () =>
        (await read(author, "/work-queue/" + work.cs.id + "?environment=staging")).objects.filter(
          (o) => o.kind === "entity",
        ).length,
    )
    .toBe(1);
  const entity = (
    await read(author, "/work-queue/" + work.cs.id + "?environment=staging")
  ).objects.find((o) => o.kind === "entity").id;
  const account = await (
    await author.request.get(cfg.studioOrigin + "/api/studio/auth/session")
  ).json();
  async function human(name, payload) {
    const r = await author.request.post(cfg.baseUrl + "/api/v1/commands/" + name, {
      headers: {
        "X-CMS-Project": cfg.project,
        Origin: cfg.studioOrigin,
        "X-CSRF-Token": account.csrfToken,
        "Idempotency-Key": randomUUID(),
      },
      data: { payload, reason: "Local primary-content acceptance" },
    });
    const result = await r.json();
    if (!r.ok()) throw Error(name + " " + r.status() + " " + result.code);
    return result.result;
  }
  const catalogSource = await readFile(
    new URL("../examples/react-store/scripts/catalog.ts", import.meta.url),
    "utf8",
  );
  const catalog = vm.runInNewContext(
    "(" + catalogSource.match(/export const catalog: Catalog = ([\s\S]*);/)[1] + ")",
  );
  // Only the pre-existing page layout is setup through API. Article, cover and all
  // content declarations/bindings below are created and edited through Studio.
  const seq = (await read(author, "/work-queue/" + work.cs.id + "?environment=staging")).item.seq;
  const pageResult = await human("apply-operations", {
    changesetId: work.cs.id,
    expectedSeq: seq,
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
              { id: "n_heading", type: "Heading", props: { text: "Initial static title" } },
              { id: "n_image", type: "Image" },
            ],
          },
        },
      },
    ],
  });
  const pageId = pageResult.operations[0].target;
  await alice.goto(cfg.studioOrigin + "/editor#document=" + pageId + "&changeset=" + work.cs.id);
  await alice.reload();
  const data = alice.getByRole("region", { name: "Данные страницы", exact: true });
  await data.getByRole("button", { name: "Выбрать сущность", exact: true }).click();
  let dialog = alice.getByRole("dialog", { name: "Основной контент", exact: true });
  await dialog.getByLabel("Схема", { exact: true }).selectOption("Article");
  await dialog.getByLabel("Поиск сущности", { exact: true }).fill(title);
  await expect(
    dialog.getByLabel("Сущность", { exact: true }).locator("option[value='" + entity + "']"),
  ).toHaveCount(1);
  await dialog.getByLabel("Сущность", { exact: true }).selectOption(entity);
  await dialog.getByRole("button", { name: "Подтвердить", exact: true }).click();
  await expect(dialog).not.toBeVisible();
  async function bind(node, prop, path, lost = false) {
    await alice.locator('[role="treeitem"][title="' + node + '"]').click();
    await data.getByRole("button", { name: "Связать с контентом · " + prop, exact: true }).click();
    const picker = alice.getByRole("dialog", { name: "Привязка свойства", exact: true });
    await picker.getByLabel("Поле контента", { exact: true }).selectOption(path);
    const pattern = "**/api/v1/commands/apply-operations";
    const attempts = [];
    if (lost)
      await alice.route(pattern, async (route) => {
        attempts.push({
          key: route.request().headers()["idempotency-key"],
          body: route.request().postData(),
        });
        const response = await route.fetch();
        expect(response.ok()).toBe(true);
        await route.abort("failed");
      });
    await picker.getByRole("button", { name: "Подтвердить", exact: true }).click();
    if (lost) {
      await data
        .getByRole("button", { name: "Повторить точное сохранение", exact: true })
        .waitFor();
      await alice.unroute(pattern);
      const replayRequest = alice.waitForRequest((request) =>
        request.url().endsWith("/apply-operations"),
      );
      await data.getByRole("button", { name: "Повторить точное сохранение", exact: true }).click();
      const replay = await replayRequest;
      attempts.push({ key: replay.headers()["idempotency-key"], body: replay.postData() });
      await expect(
        data.getByRole("button", { name: "Повторить точное сохранение", exact: true }),
      ).not.toBeVisible();
      expect(attempts[1]).toEqual(attempts[0]);
      report.exactRetry = true;
    }
    await expect(picker).not.toBeVisible();
  }
  await bind("n_heading", "text", "$content.title", true);
  await bind("n_image", "src", "$content.cover");
  await bind("n_image", "alt", "$content.alt");
  const frame = alice.frameLocator("iframe");
  await expect(frame.getByRole("heading", { name: title, exact: true })).toBeVisible();
  await expect
    .poll(() => frame.getByAltText(alt, { exact: true }).evaluate((img) => img.naturalWidth))
    .toBeGreaterThan(0);
  const localeResponse = alice.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/store" && url.searchParams.get("locale") === "en";
  });
  await alice.getByLabel("Язык", { exact: true }).selectOption("en");
  expect((await localeResponse).status()).toBe(200);
  await expect(frame.getByRole("heading", { name: "English " + title, exact: true })).toBeVisible();
  await expect
    .poll(() =>
      frame.getByAltText("English " + alt, { exact: true }).evaluate((img) => img.naturalWidth),
    )
    .toBeGreaterThan(0);
  await alice.getByLabel("Language", { exact: true }).selectOption("ru");
  await alice.getByRole("button", { name: "Отменить", exact: true }).click();
  await expect(data.locator("code").filter({ hasText: "$content.alt" })).toHaveCount(0);
  await alice.getByRole("button", { name: "Повторить", exact: true }).click();
  await expect(data.locator("code").filter({ hasText: "$content.alt" })).toHaveCount(1);
  await alice.setViewportSize({ width: 375, height: 1000 });
  report.mobileNoOverflow = await alice.evaluate(
    () => globalThis.document.documentElement.scrollWidth <= globalThis.innerWidth,
  );
  expect(report.mobileNoOverflow).toBe(true);
  await alice.screenshot({ path: resolve(output, "bindings-mobile.png"), fullPage: true });
  await alice.setViewportSize({ width: 1440, height: 1100 });
  const doc = await read(author, "/documents/" + pageId + "?changesetId=" + work.cs.id);
  expect(doc.body.content.resolve.entityId).toBe(entity);
  expect(doc.body.nodes.n_image.bindings.src).toBe("$content.cover");
  expect(JSON.stringify(doc.body)).not.toContain("?sig=");
  const publishedObjects = await publish(work, [
    { id: pageId, kind: "document" },
    { id: entity, kind: "entity" },
    { id: cover, kind: "asset" },
  ]);
  expect(publishedObjects[2].data.fileHash).toBe(createHash("sha256").update(png).digest("hex"));
  const url = cfg.baseUrl + "/delivery/v1/" + cfg.project + "/staging/page?path=/store&locale=ru";
  const headers = { Authorization: "Bearer " + delivery.deliveryKey };
  let response = await fetch(url, { headers });
  expect(response.status).toBe(200);
  const before = await response.json(),
    etag = response.headers.get("etag");
  expect(before.data.content).toEqual({ title, cover: { assetId: cover }, alt });
  const publicContext = await browser.newContext(),
    published = await publicContext.newPage();
  await published.goto(cfg.appOrigin + "/store");
  await expect(published.getByRole("heading", { name: title, exact: true })).toBeVisible();
  await expect
    .poll(() => published.getByAltText(alt, { exact: true }).evaluate((img) => img.naturalWidth))
    .toBeGreaterThan(0);
  const edit = await newWork("Только правка сущности");
  await alice.getByRole("button", { name: "Создать сущность", exact: true }).click();
  await alice.getByLabel("Схема контента", { exact: true }).selectOption("Article");
  await alice.locator(".content-panel").getByRole("button").filter({ hasText: title }).click();
  await alice.getByLabel("Локаль контента", { exact: true }).selectOption("ru");
  await alice.getByLabel("Content title ru", { exact: true }).fill("Changed " + title);
  await alice.getByRole("button", { name: "Сохранить контент", exact: true }).click();
  await expect
    .poll(
      async () =>
        (await read(author, "/work-queue/" + edit.cs.id + "?environment=staging")).objects.length,
    )
    .toBe(1);
  response = await fetch(url, { headers: { ...headers, "If-None-Match": etag } });
  expect(response.status).toBe(304);
  await publish(edit, [{ id: entity, kind: "entity" }]);
  response = await fetch(url, { headers: { ...headers, "If-None-Match": etag } });
  expect(response.status).toBe(200);
  const after = await response.json();
  expect(after.page.versionId).toBe(before.page.versionId);
  expect(after.data.content.title).toBe("Changed " + title);
  expect(response.headers.get("etag")).not.toBe(etag);
  await published.reload();
  await expect(
    published.getByRole("heading", { name: "Changed " + title, exact: true }),
  ).toBeVisible();
  const publishedCover = published.getByAltText(alt, { exact: true });
  await publishedCover.scrollIntoViewIfNeeded();
  await expect.poll(() => publishedCover.evaluate((img) => img.naturalWidth)).toBeGreaterThan(0);
  await published.screenshot({ path: resolve(output, "bindings-published.png"), fullPage: true });
  report.primary = {
    pageId,
    entity,
    cover,
    locales: ["ru", "en"],
    realImage: true,
    undoRedo: true,
    entityOnlyPublication: true,
    etagChanged: true,
    pageVersionUnchanged: true,
  };
  await writeFile(
    resolve(output, "content-bindings-acceptance.json"),
    JSON.stringify(report, null, 2),
  );
  console.log(
    "PASS STU-218: two humans, real CMS/PostgreSQL/storage/React, UI content bindings, RU/EN image preview, undo, exact retry and entity-only ETag.",
  );
} catch (error) {
  const pages = browser.contexts().flatMap((context) => context.pages());
  if (pages[0]) {
    console.error((await pages[0].locator("body").innerText()).slice(-6000));
    await pages[0].screenshot({ path: resolve(output, "bindings-failure.png"), fullPage: true });
  }
  throw error;
} finally {
  await browser.close();
}
