// STU-219…236: real loopback CMS/OIDC/PostgreSQL/storage; sources and Repeat are created in UI.
// One response is deliberately dropped AFTER the real server commits, to verify exact retry.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { resolve, dirname } from "node:path";
import { randomUUID, createHash } from "node:crypto";
import { createRequire } from "node:module";
import { URL } from "node:url";
import vm from "node:vm";
import { execFileSync } from "node:child_process";
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
  report = {
    project: cfg.project,
    stamp,
    sourceSha: execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim(),
    sourceDirty: !!execFileSync("git", ["status", "--porcelain"], { encoding: "utf8" }).trim(),
    works: [],
    createdViaUI: true,
  };
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
  setupManifest.schemas.Collection = {
    version: 1,
    title: "Collection",
    fields: {
      title: { type: "text", localized: true },
      handle: { type: "string", required: true, localized: true, maxLength: 64 },
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
      expectedSeq: (await read(author, "/changesets/" + registration.changesetId)).seq,
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

  // Shared setup and helpers are copied from the established live acceptance script.
  const work = await newWork("Источники и повторения");
  const localeContract = await read(
    author,
    "/content-catalog?kind=entity&environment=staging&changesetId=" + work.cs.id,
  );
  expect(localeContract.locales, "The isolated project must declare RU and EN locales").toEqual(
    expect.arrayContaining(["ru", "en"]),
  );
  console.log("STU-236: work created through Studio.");
  const cover = await addAsset(work);
  const title = "Collection " + stamp;
  await alice.getByRole("button", { name: "Создать сущность", exact: true }).click();
  await alice.getByLabel("Схема контента", { exact: true }).selectOption("Collection");
  await alice.getByRole("button", { name: "Новая сущность", exact: true }).click();
  await alice.getByLabel("Content title ru", { exact: true }).fill(title);
  await alice.getByLabel("Content handle ru", { exact: true }).fill("all");
  await alice.getByLabel("Локаль контента", { exact: true }).selectOption("en");
  await alice.getByLabel("Content title en", { exact: true }).fill("English " + title);
  await alice.getByLabel("Content handle en", { exact: true }).fill("Керамика");
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
    const response = await author.request.post(cfg.baseUrl + "/api/v1/commands/" + name, {
      headers: {
        "X-CMS-Project": cfg.project,
        Origin: cfg.studioOrigin,
        "X-CSRF-Token": account.csrfToken,
        "Idempotency-Key": randomUUID(),
      },
      data: { payload, reason: "Local static acceptance layout" },
    });
    const result = await response.json();
    if (!response.ok()) throw Error(name + " " + response.status() + " " + result.code);
    return result.result;
  }
  const catalogSource = await readFile(
    new URL("../examples/react-store/scripts/catalog.ts", import.meta.url),
    "utf8",
  );
  const catalog = vm.runInNewContext(
    "(" + catalogSource.match(/export const catalog: Catalog = ([\s\S]*);/)[1] + ")",
  );
  catalog.products = catalog.products
    .slice(0, 2)
    .map((item) => ({ ...item, asset: { assetId: cover } }));
  const pageResult = await human("apply-operations", {
    changesetId: work.cs.id,
    expectedSeq: (await read(author, "/work-queue/" + work.cs.id + "?environment=staging")).item
      .seq,
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
            children: [{ id: "n_static", type: "Heading", props: { level: 2 } }],
          },
        },
      },
    ],
  });
  const pageId = pageResult.operations[0].target;
  await alice.goto(cfg.studioOrigin + "/editor#document=" + pageId + "&changeset=" + work.cs.id);
  await alice.reload();
  const primary = alice.getByRole("region", { name: "Данные страницы", exact: true });
  await alice.locator('[role="treeitem"][title="n_static"]').click();
  await alice.getByLabel("text", { exact: true }).fill("Independent section " + stamp);
  await alice.getByRole("button", { name: "Сохранить text", exact: true }).click();
  await expect(
    alice
      .getByRole("region", { name: "Источники данных", exact: true })
      .getByRole("button", { name: "Подключить источник", exact: true }),
  ).toBeEnabled();
  await alice.getByLabel("Язык", { exact: true }).selectOption("en");
  await alice.getByLabel("text", { exact: true }).fill("Independent section " + stamp);
  await alice.getByRole("button", { name: "Save text", exact: true }).click();
  await expect(
    alice
      .getByRole("region", { name: "Data sources", exact: true })
      .getByRole("button", { name: "Connect source", exact: true }),
  ).toBeEnabled();
  const staticBody = (await read(author, "/documents/" + pageId + "?changesetId=" + work.cs.id))
    .body;
  const staticKey = staticBody.nodes.n_static.bindings.text.slice(7);
  expect(staticBody.localContent[staticKey].value).toEqual({
    ru: "Independent section " + stamp,
    en: "Independent section " + stamp,
  });
  await alice.getByLabel("Language", { exact: true }).selectOption("ru");
  await primary.getByRole("button", { name: "Выбрать сущность", exact: true }).click();
  const primaryDialog = alice.getByRole("dialog", { name: "Основной контент", exact: true });
  await primaryDialog.getByLabel("Схема", { exact: true }).selectOption("Collection");
  await primaryDialog.getByLabel("Поиск сущности", { exact: true }).fill(title);
  await expect(
    primaryDialog.getByLabel("Сущность", { exact: true }).locator("option[value='" + entity + "']"),
  ).toHaveCount(1);
  await primaryDialog.getByLabel("Сущность", { exact: true }).selectOption(entity);
  await primaryDialog.getByRole("button", { name: "Подтвердить", exact: true }).click();
  await expect(primaryDialog).not.toBeVisible();
  const data = alice.getByRole("region", { name: "Источники данных", exact: true });
  console.log("STU-236: Collection, image and primary content connected.");
  await data.getByRole("button", { name: "Подключить источник", exact: true }).click();
  await data.getByLabel("Источник", { exact: true }).selectOption("store.collectionProducts");
  await data.getByLabel("Значение параметра collection", { exact: true }).selectOption("path");
  await data
    .getByLabel("Путь параметра collection", { exact: true })
    .selectOption("$content.handle");
  await data.getByLabel("Значение параметра locale", { exact: true }).selectOption("path");
  await data.getByLabel("Путь параметра locale", { exact: true }).selectOption("$context.locale");
  await data.getByLabel("Значение параметра limit", { exact: true }).selectOption("literal");
  await data.getByLabel("Литерал limit", { exact: true }).fill("2");
  const pattern = "**/api/v1/commands/apply-operations",
    attempts = [];
  await alice.route(pattern, async (route) => {
    attempts.push({
      key: route.request().headers()["idempotency-key"],
      body: route.request().postData(),
    });
    const response = await route.fetch();
    if (!response.ok())
      console.error("Local source save rejected:", response.status(), await response.json());
    expect(response.ok()).toBe(true);
    await route.abort("failed");
  });
  await data.getByRole("button", { name: "Сохранить данные", exact: true }).click();
  await expect(
    primary.getByRole("button", { name: "Повторить точное сохранение", exact: true }),
  ).toBeVisible();
  await alice.unroute(pattern);
  const retry = alice.waitForRequest((request) => request.url().endsWith("/apply-operations"));
  await primary.getByRole("button", { name: "Повторить точное сохранение", exact: true }).click();
  const replay = await retry;
  expect({ key: replay.headers()["idempotency-key"], body: replay.postData() }).toEqual(
    attempts[0],
  );
  await expect(
    primary.getByRole("button", { name: "Повторить точное сохранение", exact: true }),
  ).not.toBeVisible();
  console.log("STU-236: source parameters and exact retry passed.");
  await data.getByRole("button", { name: "Добавить повторение", exact: true }).click();
  await data.getByLabel("Ключ элемента", { exact: true }).selectOption("$item.id");
  await data.getByLabel("Предел элементов", { exact: true }).fill("2");
  await data.getByRole("button", { name: "Сохранить данные", exact: true }).click();
  await expect(
    data.getByRole("button", { name: "Сохранить данные", exact: true }),
  ).not.toBeVisible();
  async function documentBody() {
    return (await read(author, "/documents/" + pageId + "?changesetId=" + work.cs.id)).body;
  }
  let body = await documentBody();
  const repeat = Object.values(body.nodes).find((node) => node.type === "Repeat");
  expect(repeat).toBeTruthy();
  const template = repeat.children[0],
    empty = repeat.slots.empty[0];
  async function addBlock(type, parent) {
    await alice.locator('[role="treeitem"][title="' + parent + '"]').click();
    const palette = alice.getByRole("region", { name: "Палитра", exact: true });
    await palette.getByLabel("Компонент", { exact: true }).selectOption(type);
    const destination = palette.getByLabel("Палитра: Место вставки", { exact: true });
    const options = await destination
      .locator("option")
      .evaluateAll((options) => options.map((option) => option.value));
    const place = options
      .map((option) => JSON.parse(option))
      .filter((place) => place.parentId === parent && place.slot === "")
      .at(-1);
    if (!place) throw Error("No insertion destination for " + type);
    await destination.selectOption(JSON.stringify(place));
    const before = new Set(Object.keys((await documentBody()).nodes));
    await palette.getByRole("button", { name: "Добавить блок", exact: true }).click();
    await expect
      .poll(async () => Object.keys((await documentBody()).nodes).length)
      .toBeGreaterThan(before.size);
    const body = await documentBody();
    return Object.values(body.nodes).find((node) => !before.has(node.id) && node.type === type).id;
  }
  async function bind(node, prop, path) {
    await alice.locator('[role="treeitem"][title="' + node + '"]').click();
    await data.getByRole("button", { name: "Привязать данные к свойству", exact: true }).click();
    await data.getByLabel("Свойство блока", { exact: true }).selectOption(prop);
    await data.getByLabel("Поле результата или элемента", { exact: true }).selectOption(path);
    const confirm = data.getByLabel("Подтверждаю замену или снятие", { exact: true });
    if (await confirm.isVisible()) await confirm.check();
    await data.getByRole("button", { name: "Сохранить данные", exact: true }).click();
    await expect(
      data.getByRole("button", { name: "Сохранить данные", exact: true }),
    ).not.toBeVisible();
  }
  const text = await addBlock("Text", template);
  await bind(text, "text", "$item.title");
  const image = await addBlock("Image", template);
  await bind(image, "src", "$item.asset");
  await bind(image, "alt", "$item.title");
  const emptyText = await addBlock("Text", empty);
  await alice.getByLabel("text", { exact: true }).fill("Коллекция пуста");
  await alice.getByRole("button", { name: "Сохранить text", exact: true }).click();
  await expect(
    data.getByRole("button", { name: "Подключить источник", exact: true }),
  ).toBeEnabled();
  const frame = alice.frameLocator("iframe");
  await expect(frame.getByText(catalog.products[0].title, { exact: true })).toBeVisible();
  await expect
    .poll(() =>
      frame
        .getByAltText(catalog.products[0].title, { exact: true })
        .evaluate((img) => img.naturalWidth),
    )
    .toBeGreaterThan(0);
  await frame.getByText(catalog.products[0].title, { exact: true }).click();
  await expect(alice.locator('[role="treeitem"][title="' + text + '"]')).toHaveAttribute(
    "aria-selected",
    "true",
  );
  async function collectionParameter(value) {
    await data.getByRole("button", { name: "Настроить featured", exact: true }).click();
    if (value === null) {
      await data.getByLabel("Значение параметра collection", { exact: true }).selectOption("path");
      await data
        .getByLabel("Путь параметра collection", { exact: true })
        .selectOption("$content.handle");
    } else {
      await data
        .getByLabel("Значение параметра collection", { exact: true })
        .selectOption("literal");
      await data.getByLabel("Литерал collection", { exact: true }).fill(value);
    }
    await data.getByLabel("Подтверждаю изменение зависимостей", { exact: true }).check();
    await data.getByRole("button", { name: "Сохранить данные", exact: true }).click();
    await expect(
      data.getByRole("button", { name: "Сохранить данные", exact: true }),
    ).not.toBeVisible();
  }
  await collectionParameter("empty");
  await expect(frame.getByText("Коллекция пуста", { exact: true })).toBeVisible();
  await expect(frame.getByText(catalog.products[0].title, { exact: true })).not.toBeVisible();
  await collectionParameter("unavailable");
  await expect(frame.getByText("Данные недоступны", { exact: true })).toBeVisible();
  await expect(frame.getByText("Коллекция пуста", { exact: true })).not.toBeVisible();
  await expect(
    frame.getByRole("heading", { name: "Independent section " + stamp, exact: true }),
  ).toBeVisible();
  await collectionParameter(null);
  await expect(frame.getByText(catalog.products[1].title, { exact: true })).toBeVisible();
  const englishResponse = alice.waitForResponse((response) =>
    response.url().endsWith("/api/store?locale=en"),
  );
  await alice.getByLabel("Язык", { exact: true }).selectOption("en");
  const english = await englishResponse;
  expect(english.ok(), "English preview HTTP status").toBe(true);
  await expect(frame.getByText(catalog.products[0].title, { exact: true })).toBeVisible();
  await expect(frame.getByText(catalog.products[1].title, { exact: true })).not.toBeVisible();
  await expect
    .poll(() =>
      frame
        .getByAltText(catalog.products[0].title, { exact: true })
        .evaluate((img) => img.naturalWidth),
    )
    .toBeGreaterThan(0);
  await alice.getByLabel("Language", { exact: true }).selectOption("ru");
  await expect(frame.getByText(catalog.products[1].title, { exact: true })).toBeVisible();
  let started, release;
  const held = new Promise((resolve) => {
      started = resolve;
    }),
    released = new Promise((resolve) => {
      release = resolve;
    });
  const latePattern = "**/api/store?locale=en";
  await alice.route(latePattern, async (route) => {
    const response = await route.fetch();
    started();
    await released;
    await route.fulfill({ response }).catch(() => {});
  });
  const aborted = alice.waitForEvent("requestfailed", {
    predicate: (request) => request.url().endsWith("/api/store?locale=en"),
  });
  await alice.getByLabel("Язык", { exact: true }).selectOption("en");
  await held;
  const currentResponse = alice.waitForResponse((response) =>
    response.url().endsWith("/api/store?locale=ru"),
  );
  await alice.getByLabel("Language", { exact: true }).selectOption("ru");
  expect((await currentResponse).ok()).toBe(true);
  release();
  await aborted;
  await alice.unroute(latePattern);
  await expect(frame.getByText(catalog.products[1].title, { exact: true })).toBeVisible();
  report.lateResponseIgnored = true;
  await alice.getByRole("button", { name: "Отменить", exact: true }).click();
  await expect(frame.getByText("Данные недоступны", { exact: true })).toBeVisible();
  await alice.getByRole("button", { name: "Повторить", exact: true }).click();
  await expect(frame.getByText(catalog.products[0].title, { exact: true })).toBeVisible();
  await alice.setViewportSize({ width: 375, height: 1000 });
  expect(
    await alice.evaluate(
      () => globalThis.document.documentElement.scrollWidth <= globalThis.innerWidth,
    ),
  ).toBe(true);
  await alice.screenshot({ path: resolve(output, "data-sources-mobile.png"), fullPage: true });
  await alice.setViewportSize({ width: 1440, height: 1100 });
  body = await documentBody();
  expect(body.nodes[repeat.id].props.key).toBe("$item.id");
  expect(body.nodes[text].bindings.text).toBe("$item.title");
  expect(body.dataSources.featured.params.collection).toBe("$content.handle");
  expect(JSON.stringify(body)).not.toContain("?sig=");
  const before = await fetch(
    cfg.baseUrl + "/delivery/v1/" + cfg.project + "/staging/page?path=/store",
    { headers: { Authorization: "Bearer " + delivery.deliveryKey } },
  );
  expect(before.status).toBe(404);
  const published = await publish(work, [
    { id: pageId, kind: "document" },
    { id: entity, kind: "entity" },
    { id: cover, kind: "asset" },
  ]);
  expect(published[2].data.fileHash).toBe(createHash("sha256").update(png).digest("hex"));
  const publicContext = await browser.newContext(),
    page = await publicContext.newPage();
  await page.goto(cfg.appOrigin + "/store");
  await expect(page.getByText(catalog.products[0].title, { exact: true })).toBeVisible();
  await expect
    .poll(() =>
      page
        .getByAltText(catalog.products[1].title, { exact: true })
        .evaluate((img) => img.naturalWidth),
    )
    .toBeGreaterThan(0);
  expect(await page.locator("[data-cms-node]").count()).toBe(0);
  await page.screenshot({ path: resolve(output, "data-sources-published.png"), fullPage: true });
  Object.assign(report, {
    requirements: "STU-219..236",
    pageId,
    entity,
    cover,
    repeatId: repeat.id,
    templateId: template,
    emptyId: empty,
    emptyTextId: emptyText,
    createdViaUI: true,
    exactRetry: true,
    emptyDistinctFromError: true,
    locales: ["ru", "en"],
    realImages: true,
    undoRedo: true,
    mobileNoOverflow: true,
    publicBrowser: true,
    manifestHash: (
      await (await page.request.get(cfg.appOrigin + "/.well-known/cms-manifest-hash")).text()
    ).trim(),
  });
  await writeFile(resolve(output, "data-sources-acceptance.json"), JSON.stringify(report, null, 2));
  console.log(
    "PASS STU-236: two humans, real CMS/PostgreSQL/images, UI source/Repeat/item/empty, safe error, locales, exact retry, undo/redo and publication.",
  );
} catch (error) {
  const page = browser.contexts().flatMap((context) => context.pages())[0];
  if (page) {
    console.error((await page.locator("body").innerText()).slice(-6000));
    await page.screenshot({ path: resolve(output, "data-sources-failure.png"), fullPage: true });
  }
  throw error;
} finally {
  await browser.close();
}
