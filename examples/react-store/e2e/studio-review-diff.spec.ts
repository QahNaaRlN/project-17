import { expect, test } from "@playwright/test";
const origin = "http://127.0.0.1:18084",
  id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11";
test("STU-149/153/157/158: comparison bases, node navigation, mobile layout and protected confirmation", async ({
  page,
  request,
}) => {
  await request.post(origin + "/control", { data: { mode: "ready" } });
  await request.post(origin + "/studio-control", { data: { reset: true, state: "approved" } });
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  const diff = page.getByRole("region", { name: "Изменения работы", exact: true });
  await expect(diff).toContainText('"Before"');
  await expect(diff).toContainText('"After"');
  await diff.getByRole("button", { name: "Изменения в окружении · staging", exact: true }).click();
  await expect(diff).toContainText("Опубликовано в окружении");
  await diff.getByRole("button", { name: "Выбрать блок", exact: true }).click();
  await expect(page.locator('[role="treeitem"][aria-selected="true"]')).toHaveAttribute(
    "title",
    "n_title",
  );
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  const commands: unknown[] = [];
  page.on("request", (r) => {
    if (r.url().endsWith("/commands/publish")) commands.push(r.postDataJSON());
  });
  const panel = page.getByRole("region", { name: "Проверки и публикация", exact: true });
  await panel.getByRole("button", { name: "Опубликовать", exact: true }).click();
  expect(commands).toHaveLength(0);
  await panel.getByRole("button", { name: "Подтвердить публикацию", exact: true }).click();
  await expect(panel).toContainText("Публикация подтверждена");
  expect(commands).toHaveLength(1);
  expect(commands[0]).toMatchObject({
    payload: { expectedDiffSnapshot: expect.stringMatching(/^sha256:[a-f0-9]{64}$/) },
  });
});
