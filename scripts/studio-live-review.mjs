import { URL } from "node:url";
const { fetch } = globalThis;
// Real loopback CMS + PostgreSQL acceptance; no intercepted requests or CMS mocks.
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import { createRequire } from "node:module";
import { resolve, dirname } from "node:path";
import vm from "node:vm";
const require = createRequire(new URL("../examples/react-store/package.json", import.meta.url));
const { chromium, expect } = require("@playwright/test");
if (!process.env.STUDIO_REVIEW_ADMIN_FILE || !process.env.STUDIO_REVIEW_OUTPUT)
  throw Error("STUDIO_REVIEW_ADMIN_FILE and STUDIO_REVIEW_OUTPUT are required");
const cfg = JSON.parse(await readFile(process.env.STUDIO_REVIEW_ADMIN_FILE, "utf8"));
const studio = cfg.studioOrigin ?? "http://127.0.0.1:18094",
  app = cfg.appOrigin ?? "http://127.0.0.1:18096";
if (
  new URL(cfg.baseUrl).hostname !== "127.0.0.1" ||
  new URL(studio).hostname !== "127.0.0.1" ||
  new URL(app).hostname !== "127.0.0.1" ||
  cfg.project !== "studio-work-review"
)
  throw Error("Acceptance is restricted to the isolated local review project");
const output = resolve(process.env.STUDIO_REVIEW_OUTPUT);
await mkdir(output, { recursive: true });
const admin = async (name, payload) => {
  const r = await fetch(cfg.baseUrl + "/api/v1/commands/" + name, {
    method: "POST",
    headers: {
      Authorization: "Bearer " + cfg.credential,
      "X-CMS-Project": cfg.project,
      "Content-Type": "application/json",
      "Idempotency-Key": randomUUID(),
    },
    body: JSON.stringify({ payload, reason: "Local two-person acceptance setup" }),
  });
  const d = await r.json();
  if (!r.ok) throw Error(name + " " + r.status + " " + d.code);
  return d.result;
};
const browser = await chromium.launch({
  ...(process.env.PLAYWRIGHT_EXECUTABLE_PATH
    ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH }
    : {}),
});
try {
  const author = await browser.newContext({ viewport: { width: 1440, height: 1100 } }),
    reviewer = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
  const alice = await author.newPage(),
    bob = await reviewer.newPage();
  const login = async (page, name, roleId) => {
    await page.goto(studio + "/editor");
    await page.getByRole("link", { name: "Войти", exact: true }).click();
    await page.getByRole("link", { name: "Войти как " + name, exact: true }).click();
    await page.waitForURL(studio + "/editor**");
    const account = await (
      await page.context().request.get(studio + "/api/studio/auth/session")
    ).json();
    if (!account.authenticated || account.user.name !== name)
      throw Error("Signed OIDC identity not confirmed");
    await admin("bind-human-role", { actorId: account.user.id, roleId });
    await page.reload();
    await expect(page.locator(".person-bar")).toContainText(name);
    return account;
  };
  const assetRole = process.env.STUDIO_REVIEW_ASSET_ID
    ? await admin("create-role", {
        name: "review-asset-" + randomUUID().slice(0, 8),
        capabilities: ["asset.write"],
      })
    : null;
  const a = await login(alice, "Alice", cfg.authorRole),
    b = await login(bob, "Bob", cfg.reviewerRole);
  if (assetRole) await admin("bind-human-role", { actorId: a.user.id, roleId: assetRole.id });
  if (a.user.id === b.user.id) throw Error("Users must differ");
  const human = async (name, payload) => {
    const response = await author.request.post(cfg.baseUrl + "/api/v1/commands/" + name, {
      headers: {
        "X-CMS-Project": cfg.project,
        Origin: studio,
        "X-CSRF-Token": a.csrfToken,
        "Idempotency-Key": randomUUID(),
      },
      data: { payload, reason: "Local author acceptance" },
    });
    const d = await response.json();
    if (!response.ok())
      throw Error(name + " " + response.status() + " " + d.code + " " + String(d.detail ?? ""));
    return d.result;
  };
  const manifest = JSON.parse(
    await readFile(new URL("../examples/react-store/.cms/manifest.json", import.meta.url), "utf8"),
  );
  manifest.schemas = {
    ...manifest.schemas,
    Article: { version: 1, title: "Article", fields: { title: { type: "text", localized: true } } },
  };
  const registration = await admin("register-manifest", { environment: "staging", manifest });
  if (registration.changesetId) {
    const setupRole = await admin("create-role", {
      name: "schema-setup-" + randomUUID().slice(0, 8),
      capabilities: ["schema.apply", "schema.read"],
    });
    await admin("bind-human-role", { actorId: a.user.id, roleId: setupRole.id });
    await admin("set-approval-policy", { low: 0, medium: 0, high: 0 });
    const claimed = await human("claim-schema-changeset", {
      changesetId: registration.changesetId,
      expectedSeq: 1,
    });
    await human("submit-changeset", {
      changesetId: registration.changesetId,
      expectedSeq: claimed.seq,
    });
    await human("publish", {
      changesetId: registration.changesetId,
      environment: "staging",
      expectedSeq: claimed.seq,
    });
    await admin("set-approval-policy", { low: 1, medium: 1, high: 1 });
    await admin("unbind-human-role", { actorId: a.user.id, roleId: setupRole.id });
  }
  const cs = await human("create-changeset", {
    title: "Витрина — согласование " + randomUUID().slice(0, 8),
    targets: ["staging"],
  });
  const source = await readFile(
    new URL("../examples/react-store/scripts/catalog.ts", import.meta.url),
    "utf8",
  );
  const catalog = vm.runInNewContext(
    "(" + source.match(/export const catalog: Catalog = ([\s\S]*);/)[1] + ")",
  );
  const seeded = await human("apply-operations", {
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
            name: "Тихая форма",
            props: { config: { base: catalog } },
            children: [
              {
                id: "n_title",
                type: "Heading",
                name: "Заголовок",
                props: { level: 2, text: "До согласования" },
              },
              {
                id: "n_hero",
                type: "StoreHero",
                name: "Первый экран",
                bindings: {
                  title: "$context.store.title",
                  subtitle: "$context.store.subtitle",
                  description: "$context.store.description",
                  ctaLabel: "$context.store.ctaLabel",
                },
              },
            ],
          },
        },
      },
    ],
  });
  const doc = seeded.operations[0].target;
  const extra = await human("apply-operations", {
    changesetId: cs.id,
    expectedSeq: seeded.changeset.seq,
    operations: [
      {
        type: "document.create",
        payload: {
          kind: "page",
          path: "/comparison-secondary-" + cs.id,
          root: { id: "n_other", type: "Box", name: "Secondary page", children: [] },
        },
      },
      {
        type: "entity.create",
        payload: {
          schema: "Article",
          environment: "staging",
          data: { title: { ru: "Article for comparison" } },
        },
      },
      ...(process.env.STUDIO_REVIEW_ASSET_ID
        ? [
            {
              type: "asset.updateMeta",
              target: process.env.STUDIO_REVIEW_ASSET_ID,
              payload: { set: { title: "Reviewed picture" } },
            },
          ]
        : []),
    ],
  });
  const entity = extra.operations[1].target;

  await alice.getByRole("button", { name: "Работы", exact: true }).click();
  await alice.getByRole("button", { name: "Обновить работы", exact: true }).click();
  const row = alice.locator(".work-catalogue tbody tr").filter({ hasText: cs.title });
  await row.getByRole("button", { name: "Открыть работу", exact: true }).click();
  await expect(alice.locator(".work-card-heading")).toContainText(cs.title);
  await alice.getByRole("button", { name: "Открыть редактор", exact: true }).click();
  await alice.locator('[role="treeitem"][title="n_title"]').click();
  await alice.getByLabel("text", { exact: true }).fill("Проверено двумя пользователями");
  await alice.getByRole("button", { name: "Сохранить text", exact: true }).click();
  await expect(
    alice
      .frameLocator(".frame-wrap iframe")
      .getByRole("heading", { name: "Проверено двумя пользователями", exact: true }),
  ).toBeVisible();
  await alice.getByRole("button", { name: "Работы", exact: true }).click();
  const authorPanel = alice.getByRole("region", { name: "Проверки и публикация", exact: true });
  await authorPanel.getByRole("button", { name: "Отправить на проверку", exact: true }).click();
  await expect(authorPanel.getByText("На согласовании", { exact: true })).toBeVisible();
  await expect(
    authorPanel.getByRole("button", { name: "Согласовать", exact: true }),
  ).toBeDisabled();
  const selfSnapshot = await (
    await author.request.get(
      cfg.baseUrl + "/api/v1/changesets/" + cs.id + "/diff?environment=staging",
      { headers: { "X-CMS-Project": cfg.project } },
    )
  ).json();
  const self = await author.request.post(cfg.baseUrl + "/api/v1/commands/approve-changeset", {
    headers: {
      "X-CMS-Project": cfg.project,
      Origin: studio,
      "X-CSRF-Token": a.csrfToken,
      "Idempotency-Key": randomUUID(),
    },
    data: {
      payload: {
        changesetId: cs.id,
        expectedSeq: selfSnapshot.seq,
        expectedDiffSnapshot: selfSnapshot.snapshot,
      },
    },
  });
  expect(self.status()).toBe(403);
  expect((await self.json()).code).toBe("APPROVAL_SELF");
  await bob.getByRole("button", { name: "Работы", exact: true }).click();
  await bob.getByRole("button", { name: "Обновить работы", exact: true }).click();
  await bob.getByLabel("Поиск работ", { exact: true }).fill("Витрина");
  await bob
    .locator(".work-catalogue tbody tr")
    .filter({ hasText: cs.title })
    .getByRole("button", { name: "Открыть работу", exact: true })
    .click();
  const panel = bob.getByRole("region", { name: "Проверки и публикация", exact: true });
  await expect(panel.getByRole("button", { name: "Согласовать", exact: true })).toBeEnabled();
  const diff = panel.getByRole("region", { name: "Изменения работы", exact: true });
  await expect(diff.locator(".diff-object-list button")).toHaveCount(
    process.env.STUDIO_REVIEW_ASSET_ID ? 4 : 3,
  );
  await diff.locator(".diff-object-list button").filter({ hasText: "Article for comparison" }).click();
  await expect(diff).toContainText("Article for comparison");
  await diff.getByRole("button", { name: "Изменения в окружении · staging", exact: true }).click();
  await expect(diff).toContainText("Опубликовано в окружении");
  if (process.env.STUDIO_REVIEW_ASSET_ID) {
    await diff.getByRole("button", { name: "Изменения работы", exact: true }).click();
    await diff.locator(".diff-object-list button").filter({ hasText: "Reviewed picture" }).click();
    await expect(diff).toContainText("Reviewed picture");
    await expect(diff).toContainText('"Picture before"');
  }
  await diff.locator(".diff-object-list button").filter({ hasText: "Тихая форма" }).click();
  await expect(diff).toContainText("Проверено двумя пользователями");
  await bob.screenshot({ path: resolve(output, "studio-work-review.png"), fullPage: true });
  await bob.setViewportSize({ width: 390, height: 844 });
  expect(
    await bob.evaluate(
      () => globalThis.document.documentElement.scrollWidth <= globalThis.innerWidth,
    ),
  ).toBe(true);
  await bob.screenshot({ path: resolve(output, "studio-work-mobile.png"), fullPage: true });
  await bob.setViewportSize({ width: 1440, height: 1100 });
  const oldIndex = await (
    await reviewer.request.get(
      cfg.baseUrl + "/api/v1/changesets/" + cs.id + "/diff?environment=staging",
      { headers: { "X-CMS-Project": cfg.project } },
    )
  ).json();
  await admin("set-approval-policy", { low: 1, medium: 1, high: 2 });
  const stale = await reviewer.request.post(cfg.baseUrl + "/api/v1/commands/approve-changeset", {
    headers: {
      "X-CMS-Project": cfg.project,
      Origin: studio,
      "X-CSRF-Token": b.csrfToken,
      "Idempotency-Key": randomUUID(),
    },
    data: {
      payload: {
        changesetId: cs.id,
        expectedSeq: oldIndex.seq,
        expectedDiffSnapshot: oldIndex.snapshot,
      },
    },
  });
  expect(stale.status()).toBe(409);
  expect((await stale.json()).code).toBe("DIFF_SNAPSHOT_STALE");
  await admin("set-approval-policy", { low: 1, medium: 1, high: 1 });
  await panel.getByRole("button", { name: "Согласовать", exact: true }).click();
  await expect(panel.getByText("Готов к публикации", { exact: true })).toBeVisible();
  const before = await reviewer.request.get(
    cfg.baseUrl + "/api/v1/documents/" + doc + "?environment=staging",
    { headers: { "X-CMS-Project": cfg.project } },
  );
  expect(before.status()).toBe(404);
  await panel.getByRole("button", { name: "Опубликовать", exact: true }).click();
  await expect(bob.locator(".publication-confirmation")).toContainText(cs.title);
  await panel.getByRole("button", { name: "Подтвердить публикацию", exact: true }).click();
  await expect(panel).toContainText("Публикация подтверждена: staging");
  await expect(bob.locator(".work-catalogue tbody tr").filter({ hasText: cs.title })).toHaveCount(
    0,
  );
  const result = await reviewer.request.get(
    cfg.baseUrl + "/api/v1/documents/" + doc + "?environment=staging",
    { headers: { "X-CMS-Project": cfg.project } },
  );
  expect(result.ok()).toBe(true);
  expect((await result.json()).state).toBe("committed");
  const storeConfig = JSON.parse(
    await readFile(
      resolve(dirname(process.env.STUDIO_REVIEW_ADMIN_FILE), "studio-work-store.private.json"),
      "utf8",
    ),
  );
  const publishedEntity = await reviewer.request.get(
    cfg.baseUrl + "/delivery/v1/" + cfg.project + "/staging/entity/" + entity,
    { headers: { Authorization: "Bearer " + storeConfig.deliveryKey } },
  );
  expect(publishedEntity.ok()).toBe(true);
  expect((await publishedEntity.json()).data.title.ru).toBe("Article for comparison");
  const published = await reviewer.newPage();
  await published.goto(app + "/store");
  await expect(
    published.getByRole("heading", { name: "Проверено двумя пользователями", exact: true }),
  ).toBeVisible();
  await bob.screenshot({ path: resolve(output, "studio-work-published.png"), fullPage: true });
  await writeFile(
    resolve(output, "acceptance.json"),
    JSON.stringify(
      {
        project: cfg.project,
        studio,
        app,
        changesetId: cs.id,
        documentId: doc,
        entityId: entity,
        assetId: process.env.STUDIO_REVIEW_ASSET_ID ?? null,
        objects: process.env.STUDIO_REVIEW_ASSET_ID ? 4 : 3,
        seq: oldIndex.seq,
        selfApprovalStatus: self.status(),
        staleApprovalStatus: stale.status(),
        published: true,
      },
      null,
      2,
    ),
  );
  console.log(
    "PASS: signed OIDC Alice/Bob -> author save/live preview -> whole-work diff and stale snapshot 409 -> self-approval 403 -> Bob review -> explicit publish -> committed CMS and storefront; desktop/mobile screenshots captured.",
  );
} finally {
  await browser.close();
}
