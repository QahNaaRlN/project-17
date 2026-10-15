// STU-190…202: real loopback CMS/OIDC/PostgreSQL/storage; CS and objects created only in UI.
// One response is deliberately dropped AFTER the real server commits, to verify exact retry.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { resolve, dirname } from "node:path";
import { randomUUID, createHash } from "node:crypto";
import { createRequire } from "node:module";
import { URL } from "node:url";
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
  async function addEntity(work, text) {
    await alice.getByRole("button", { name: "Создать сущность", exact: true }).click();
    await alice.getByLabel("Схема контента", { exact: true }).selectOption("Article");
    await alice.getByRole("button", { name: "Новая сущность", exact: true }).click();
    await alice.getByLabel("Content title ru", { exact: true }).fill(text);
    await alice.getByRole("button", { name: "Сохранить контент", exact: true }).click();
    await expect
      .poll(
        async () =>
          (await read(author, "/work-queue/" + work.cs.id + "?environment=staging")).objects.filter(
            (o) => o.kind === "entity",
          ).length,
      )
      .toBe(1);
    const card = await read(author, "/work-queue/" + work.cs.id + "?environment=staging");
    return card.objects.find((o) => o.kind === "entity").id;
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
  const entity = await newWork("Контент без страницы", true);
  entity.id = await addEntity(entity, "Content created entirely in Studio " + stamp);
  await alice.reload();
  await expect(alice.locator("#work-card-title")).toHaveText(entity.cs.title);
  const [delivered] = await publish(entity, [{ id: entity.id, kind: "entity" }]);
  expect(delivered.data.title.ru).toBe("Content created entirely in Studio " + stamp);
  const asset = await newWork("Файл без страницы");
  asset.id = await addAsset(asset);
  const [assetContent] = await publish(asset, [{ id: asset.id, kind: "asset" }]);
  report.asset = {
    id: asset.id,
    fileHash: assetContent.data.fileHash,
    uploadReady: true,
    sourceHash: createHash("sha256").update(png).digest("hex"),
  };
  const mixed = await newWork("Смешанный контент");
  const entityId = await addEntity(mixed, "Mixed content " + stamp),
    assetId = await addAsset(mixed);
  await publish(mixed, [
    { id: entityId, kind: "entity" },
    { id: assetId, kind: "asset" },
  ]);
  await alice.setViewportSize({ width: 375, height: 900 });
  await alice.getByRole("button", { name: "Новая работа", exact: true }).click();
  await expect(alice.getByRole("dialog")).toBeVisible();
  report.mobileNoOverflow = await alice.evaluate(
    () => globalThis.document.documentElement.scrollWidth <= globalThis.innerWidth,
  );
  expect(report.mobileNoOverflow).toBe(true);
  await alice.screenshot({ path: resolve(output, "content-work-mobile.png") });
  await alice.getByRole("button", { name: "Закрыть", exact: true }).click();
  await alice.setViewportSize({ width: 1440, height: 1100 });
  await alice.screenshot({ path: resolve(output, "content-work-desktop.png") });
  await writeFile(resolve(output, "content-work-acceptance.json"), JSON.stringify(report, null, 2));
  console.log(
    "PASS: UI creates entity/asset/mixed work, lost-response recovery, reload, two-person publication and Delivery, 375 px.",
  );
} finally {
  await browser.close();
}
