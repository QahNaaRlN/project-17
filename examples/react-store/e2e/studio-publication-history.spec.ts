import { expect, test } from "@playwright/test";
const origin = "http://127.0.0.1:18084",
  id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11",
  other = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c99";
const hash = "sha256:" + "a".repeat(64);
test.beforeEach(async ({ request }) => {
  await request.post(origin + "/control", { data: { mode: "ready" } });
  await request.post(origin + "/studio-control", { data: { reset: true } });
});
test("STU-162/170/174: history keyboard confirmation is explicit and fits mobile", async ({
  page,
}, info) => {
  const publication = {
    id,
    kind: "publish",
    environment: "staging",
    actorId: id,
    createdAt: "2026-10-15T00:00:00Z",
    changesetId: id,
    sourcePublicationId: null,
    reason: "Release",
    manifestChanged: false,
    previousManifestId: null,
    currentManifestId: null,
  };
  let rolledBack = false;
  await page.route("**/api/studio/api/v1/publication-history**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/publication-history"))
      return route.fulfill({
        json: { items: [{ ...publication, objectCount: 1 }], nextCursor: null },
      });
    const pid = url.pathname.includes(other) ? other : id;
    if (url.pathname.endsWith("/diff"))
      return route.fulfill({
        json: {
          publicationId: pid,
          objectId: id,
          snapshot: hash,
          totalChanges: 1,
          nextCursor: null,
          items: [
            {
              key: "0",
              category: "property",
              kind: "replace",
              pointer: "/title",
              before: { present: true, value: "New" },
              after: { present: true, value: "Previous" },
            },
          ],
        },
      });
    return route.fulfill({
      json: {
        publication: {
          ...publication,
          id: pid,
          kind: pid === other ? "rollback" : "publish",
          sourcePublicationId: pid === other ? id : null,
        },
        snapshot: hash,
        complete: true,
        canRollback: !rolledBack,
        blockers: [],
        objects: [
          {
            id,
            kind: "document",
            docKind: "page",
            title: "Collection",
            beforeVersionId: id,
            afterVersionId: other,
            beforePath: "/store",
            afterPath: "/store",
            summary: { effect: "modified", changeCount: 1 },
          },
        ],
      },
    });
  });
  const commands: unknown[] = [];
  await page.route("**/api/studio/api/v1/commands/rollback", async (route) => {
    commands.push(route.request().postDataJSON());
    rolledBack = true;
    return route.fulfill({
      json: { result: { ...publication, id: other, kind: "rollback", sourcePublicationId: id } },
    });
  });
  await page.setViewportSize({ width: 375, height: 900 });
  await page.goto(origin + "/editor#publication=" + id);
  const panel = page.getByRole("region", { name: "История публикаций", exact: true });
  await expect(panel).toContainText("Collection");
  await panel.getByRole("button", { name: "Посмотреть последствия отката", exact: true }).click();
  await panel.getByRole("button", { name: "Отменить публикацию…", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Подтверждение отката", exact: true });
  await expect(
    dialog.getByRole("button", { name: "Подтвердить откат", exact: true }),
  ).toBeDisabled();
  await expect(dialog.getByLabel("Причина отката", { exact: true })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  expect(commands).toHaveLength(0);
  await expect(
    panel.getByRole("button", { name: "Отменить публикацию…", exact: true }),
  ).toBeFocused();
  await panel.getByRole("button", { name: "Отменить публикацию…", exact: true }).click();
  await dialog.getByLabel("Причина отката", { exact: true }).fill("Confirmed rollback");
  await expect(page.getByRole("button", { name: "Страницы", exact: true })).toBeDisabled();
  await dialog.getByRole("button", { name: "Подтвердить откат", exact: true }).click();
  await expect(panel).toContainText("Публикация отменена. Черновики сохранены.");
  expect(commands).toEqual([
    {
      reason: "Confirmed rollback",
      payload: { publicationId: id, resetHead: false, expectedRollbackSnapshot: hash },
    },
  ]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({
    path: info.outputPath("publication-rollback-mobile.png"),
    fullPage: true,
  });
  await page.getByRole("combobox", { name: "Язык", exact: true }).selectOption("en");
  await expect(page.getByRole("heading", { name: "Publications", exact: true })).toBeVisible();
});
