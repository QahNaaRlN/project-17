import { URL } from "node:url";
const { fetch } = globalThis;
// Real loopback CMS + PostgreSQL acceptance; no intercepted requests or CMS mocks.
import { readFile, mkdir } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import vm from "node:vm";
const require = createRequire(new URL("../examples/react-store/package.json", import.meta.url));
const { chromium, expect } = require("@playwright/test");
if (!process.env.STUDIO_REVIEW_ADMIN_FILE || !process.env.STUDIO_REVIEW_OUTPUT)
  throw Error("STUDIO_REVIEW_ADMIN_FILE and STUDIO_REVIEW_OUTPUT are required");
const cfg = JSON.parse(await readFile(process.env.STUDIO_REVIEW_ADMIN_FILE, "utf8"));
const studio = "http://127.0.0.1:18094",
  app = "http://127.0.0.1:18096";
if (cfg.baseUrl !== "http://127.0.0.1:18093" || cfg.project !== "studio-work-review")
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
  const a = await login(alice, "Alice", cfg.authorRole),
    b = await login(bob, "Bob", cfg.reviewerRole);
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
    if (!response.ok()) throw Error(name + " " + response.status() + " " + d.code);
    return d.result;
  };
  const cs = await human("create-changeset", {
    title: "Витрина — согласование двух пользователей",
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
  const self = await author.request.post(cfg.baseUrl + "/api/v1/commands/approve-changeset", {
    headers: {
      "X-CMS-Project": cfg.project,
      Origin: studio,
      "X-CSRF-Token": a.csrfToken,
      "Idempotency-Key": randomUUID(),
    },
    data: { payload: { changesetId: cs.id, expectedSeq: 4 } },
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
  await bob.screenshot({ path: resolve(output, "studio-work-review.png"), fullPage: true });
  await bob.setViewportSize({ width: 390, height: 844 });
  expect(
    await bob.evaluate(
      () => globalThis.document.documentElement.scrollWidth <= globalThis.innerWidth,
    ),
  ).toBe(true);
  await bob.screenshot({ path: resolve(output, "studio-work-mobile.png"), fullPage: true });
  await bob.setViewportSize({ width: 1440, height: 1100 });
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
  const published = await reviewer.newPage();
  await published.goto(app + "/store");
  await expect(
    published.getByRole("heading", { name: "Проверено двумя пользователями", exact: true }),
  ).toBeVisible();
  await bob.screenshot({ path: resolve(output, "studio-work-published.png"), fullPage: true });
  console.log(
    "PASS: signed OIDC Alice/Bob -> author save/live preview -> self-approval 403 -> Bob review -> explicit publish -> committed CMS and storefront; desktop/mobile screenshots captured.",
  );
} finally {
  await browser.close();
}
