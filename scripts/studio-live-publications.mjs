// STU-175: real signed OIDC + CMS/PostgreSQL + Studio + storefront, without HTTP mocks.
// Run studio-live-review.mjs once on a fresh stand before this continuation.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { resolve, dirname } from "node:path";
import { randomUUID } from "node:crypto";
import { createRequire } from "node:module";
import { URL } from "node:url";
const require = createRequire(new URL("../examples/react-store/package.json", import.meta.url));
const { chromium, expect } = require("@playwright/test");
if (!process.env.STUDIO_REVIEW_ADMIN_FILE || !process.env.STUDIO_REVIEW_OUTPUT)
  throw Error("Review config and output are required");
const cfg = JSON.parse(await readFile(process.env.STUDIO_REVIEW_ADMIN_FILE, "utf8"));
const output = resolve(process.env.STUDIO_REVIEW_OUTPUT);
await mkdir(output, { recursive: true });
const prior = JSON.parse(await readFile(resolve(output, "acceptance.json"), "utf8"));
for (const origin of [cfg.baseUrl, cfg.studioOrigin, cfg.appOrigin])
  if (new URL(origin).hostname !== "127.0.0.1") throw Error("Only isolated loopback stand");
if (cfg.project !== "studio-work-review" || prior.project !== cfg.project)
  throw Error("Wrong local project");
const studio = cfg.studioOrigin,
  app = cfg.appOrigin;
const browser = await chromium.launch(
  process.env.PLAYWRIGHT_EXECUTABLE_PATH
    ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH }
    : {},
);
try {
  const author = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
  const reviewer = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
  const alice = await author.newPage(),
    bob = await reviewer.newPage();
  const login = async (page, name) => {
    await page.goto(studio + "/editor");
    await page.getByRole("link", { name: "Войти", exact: true }).click();
    await page.getByRole("link", { name: "Войти как " + name, exact: true }).click();
    await page.waitForURL(studio + "/editor**");
    const account = await (
      await page.context().request.get(studio + "/api/studio/auth/session")
    ).json();
    expect(account.authenticated).toBe(true);
    expect(account.user.name).toBe(name);
    await expect(page.locator(".person-bar")).toContainText(name);
    return account;
  };
  const a = await login(alice, "Alice"),
    b = await login(bob, "Bob");
  expect(a.user.id).not.toBe(b.user.id);
  const read = async (context, path) => {
    const r = await context.request.get(cfg.baseUrl + "/api/v1" + path, {
      headers: { "X-CMS-Project": cfg.project },
    });
    if (!r.ok()) throw Error("Read failed " + r.status());
    return r.json();
  };
  const command = async (context, person, name, payload) => {
    const r = await context.request.post(cfg.baseUrl + "/api/v1/commands/" + name, {
      headers: {
        "X-CMS-Project": cfg.project,
        Origin: studio,
        "X-CSRF-Token": person.csrfToken,
        "Idempotency-Key": randomUUID(),
      },
      data: { payload, reason: "Real local publication history acceptance" },
    });
    const d = await r.json();
    if (!r.ok()) throw Error(name + " " + r.status() + " " + d.code);
    return d.result;
  };
  const before = await read(author, "/documents/" + prior.documentId + "?environment=staging");
  const key = before.body.nodes.n_title.bindings.text.slice(7);
  const cs = await command(author, a, "create-changeset", {
    title: "Публикация для проверки отката",
    targets: ["staging"],
  });
  const edit = await command(author, a, "apply-operations", {
    changesetId: cs.id,
    expectedSeq: 0,
    operations: [
      {
        type: "localContent.set",
        target: prior.documentId,
        payload: { key, type: "text", locale: "ru", value: "Новая версия — проверяем откат" },
      },
      {
        type: "entity.setFields",
        target: prior.entityId,
        payload: { set: { "title.ru": "Новая версия статьи" } },
      },
    ],
  });
  await command(author, a, "submit-changeset", {
    changesetId: cs.id,
    expectedSeq: edit.changeset.seq,
  });
  await bob.getByRole("button", { name: "Работы", exact: true }).click();
  await bob.getByRole("button", { name: "Обновить работы", exact: true }).click();
  const row = bob.locator(".work-catalogue tbody tr").filter({ hasText: cs.title });
  await row.getByRole("button", { name: "Открыть работу", exact: true }).click();
  const workflow = bob.getByRole("region", { name: "Проверки и публикация", exact: true });
  await workflow.getByRole("button", { name: "Согласовать", exact: true }).click();
  await expect(workflow.getByText("Готов к публикации", { exact: true })).toBeVisible();
  await workflow.getByRole("button", { name: "Опубликовать", exact: true }).click();
  await workflow.getByRole("button", { name: "Подтвердить публикацию", exact: true }).click();
  await expect(workflow).toContainText("Публикация подтверждена");
  const head = await read(author, "/documents/" + prior.documentId);
  const published = await read(author, "/documents/" + prior.documentId + "?environment=staging");
  const history = await read(reviewer, "/publication-history?environment=staging");
  const publication = history.items.find((p) => p.changesetId === cs.id);
  expect(publication).toBeTruthy();
  const draft = await command(author, a, "create-changeset", {
    title: "Черновик должен сохраниться",
    targets: ["staging"],
  });
  await command(author, a, "apply-operations", {
    changesetId: draft.id,
    expectedSeq: 0,
    operations: [
      {
        type: "node.rename",
        target: prior.documentId,
        payload: { nodeId: "n_title", name: "Несвязанный черновик" },
      },
    ],
  });
  const draftBefore = await read(
    author,
    "/documents/" + prior.documentId + "?changesetId=" + draft.id,
  );
  // A Studio preview cookie intentionally retains the working version after rollback.
  // Verify published delivery in a clean context, without the editor's preview session.
  const deliveryContext = await browser.newContext();
  const storefront = await deliveryContext.newPage();
  await storefront.goto(app + "/store");
  await expect(
    storefront.getByRole("heading", { name: "Новая версия — проверяем откат", exact: true }),
  ).toBeVisible();
  await bob.getByRole("button", { name: "Открыть публикацию", exact: true }).click();
  const panel = bob.getByRole("region", { name: "История публикаций", exact: true });
  await expect(panel).toContainText("Что изменилось при публикации");
  await expect(panel).toContainText("Новая версия — проверяем откат");
  await panel.getByRole("button", { name: "Посмотреть последствия отката", exact: true }).click();
  await expect(
    panel.getByRole("button", { name: "Отменить публикацию…", exact: true }),
  ).toBeEnabled();
  await bob.screenshot({
    path: resolve(output, "publication-history-desktop.png"),
    fullPage: true,
  });
  await bob.setViewportSize({ width: 375, height: 900 });
  expect(
    await bob.evaluate(
      () => globalThis.document.documentElement.scrollWidth <= globalThis.innerWidth,
    ),
  ).toBe(true);
  await bob.screenshot({ path: resolve(output, "publication-history-mobile.png"), fullPage: true });
  await bob.setViewportSize({ width: 1440, height: 1100 });
  await panel.getByRole("button", { name: "Отменить публикацию…", exact: true }).click();
  const dialog = bob.getByRole("dialog", { name: "Подтверждение отката", exact: true });
  await expect(dialog).toContainText("staging");
  await expect(dialog).toContainText("Черновики");
  await expect(
    dialog.getByRole("button", { name: "Подтвердить откат", exact: true }),
  ).toBeDisabled();
  await dialog
    .getByLabel("Причина отката", { exact: true })
    .fill("Проверенный возврат предыдущей версии");
  await expect(bob.getByRole("button", { name: "Страницы", exact: true })).toBeDisabled();
  await dialog.getByRole("button", { name: "Подтвердить откат", exact: true }).click();
  await expect(panel).toContainText("Публикация отменена. Черновики сохранены.");
  const restored = await read(author, "/documents/" + prior.documentId + "?environment=staging");
  expect(restored.versionId).toBe(before.versionId);
  expect((await read(author, "/documents/" + prior.documentId)).versionId).toBe(head.versionId);
  expect(await read(author, "/documents/" + prior.documentId + "?changesetId=" + draft.id)).toEqual(
    draftBefore,
  );
  const afterHistory = await read(reviewer, "/publication-history?environment=staging");
  const rollback = afterHistory.items.find(
    (p) => p.sourcePublicationId === publication.id && p.kind === "rollback",
  );
  expect(rollback.reason).toBe("Проверенный возврат предыдущей версии");
  const storeCfg = JSON.parse(
    await readFile(
      resolve(dirname(process.env.STUDIO_REVIEW_ADMIN_FILE), "studio-work-store.private.json"),
      "utf8",
    ),
  );
  const delivered = await reviewer.request.get(
    cfg.baseUrl + "/delivery/v1/" + cfg.project + "/staging/entity/" + prior.entityId,
    { headers: { Authorization: "Bearer " + storeCfg.deliveryKey } },
  );
  expect((await delivered.json()).data.title.ru).toBe("Article for comparison");
  await storefront.reload();
  await expect(
    storefront.getByRole("heading", { name: "Проверено двумя пользователями", exact: true }),
  ).toBeVisible();
  await bob.screenshot({
    path: resolve(output, "publication-history-rolled-back.png"),
    fullPage: true,
  });
  await writeFile(
    resolve(output, "publication-history-acceptance.json"),
    JSON.stringify(
      {
        project: cfg.project,
        studio,
        app,
        documentId: prior.documentId,
        changesetId: cs.id,
        draftId: draft.id,
        publicationId: publication.id,
        rollbackId: rollback.id,
        previousVersion: before.versionId,
        publishedVersion: published.versionId,
        restoredVersion: restored.versionId,
        headPreserved: true,
        draftPreserved: true,
        entityRestored: true,
        storefrontRestored: true,
        twoSignedUsers: true,
      },
      null,
      2,
    ),
  );
  console.log(
    "PASS: two signed OIDC users -> approved publication -> immutable history/diff -> rollback preview -> explicit guarded rollback -> head/draft preserved -> entity and storefront restored; desktop/mobile captured",
  );
} finally {
  await browser.close();
}
