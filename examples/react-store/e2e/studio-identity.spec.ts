import { expect, test, type Page } from "@playwright/test";
const origin = "http://127.0.0.1:18084";
async function login(page: Page, name: string) {
  await page.getByRole("link", { name: "Войти", exact: true }).click();
  await page.getByRole("link", { name: "Войти как " + name, exact: true }).click();
  await expect(page.locator(".person-bar")).toContainText(name);
}
async function openWork(page: Page) {
  await page.getByRole("button", { name: "Работы", exact: true }).click();
  await page.getByRole("button", { name: "Открыть работу", exact: true }).click();
  await page.getByRole("button", { name: "Открыть редактор", exact: true }).click();
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
}
test.beforeEach(async ({ request }) => {
  await request.post(origin + "/control", { data: { mode: "ready" } });
  await request.post(origin + "/studio-control", { data: { reset: true, identity: true } });
});
test("STU-080/081/083/084: two people open, submit, approve and explicitly publish without IDs", async ({
  page,
}, info) => {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.goto(origin + "/editor");
  await login(page, "Alice");
  expect(await page.evaluate(() => document.cookie)).not.toContain("cms_session");
  await openWork(page);
  await page.getByRole("treeitem", { name: "◇ Heading", exact: true }).click();
  await page.getByLabel("text", { exact: true }).fill("Работа двух пользователей");
  await page.getByRole("button", { name: "Сохранить text", exact: true }).click();
  const panel = page.getByRole("region", { name: "Проверки и публикация", exact: true });
  await panel.getByRole("button", { name: "Отправить на проверку", exact: true }).click();
  await expect(panel.getByText("На согласовании", { exact: true })).toBeVisible();
  await expect(panel.getByRole("button", { name: "Согласовать", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Выйти", exact: true }).click();
  await expect(page.getByRole("link", { name: "Войти", exact: true })).toBeVisible();
  expect(new URL(page.url()).hash).toBe("");
  await login(page, "Bob");
  await openWork(page);
  await expect(page.getByRole("region", { name: "Работы и согласование" })).toContainText(
    "Работа другого автора",
  );
  await page.getByRole("treeitem", { name: "◇ Heading", exact: true }).click();
  await expect(page.getByRole("button", { name: "Сохранить text", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Работы", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Работы и согласование", exact: true }),
  ).toBeVisible();
  await panel.getByRole("button", { name: "Согласовать", exact: true }).click();
  await expect(panel.getByText("Готов к публикации", { exact: true })).toBeVisible();
  await panel.getByRole("button", { name: "Опубликовать", exact: true }).click();
  await panel.getByRole("button", { name: "Подтвердить публикацию", exact: true }).click();
  await expect(panel).toContainText("Публикация подтверждена: staging");
  await page.screenshot({ path: info.outputPath("studio-two-users.png"), fullPage: true });
});
test("STU-081/082: expired session destroys the old editor", async ({ page, request }) => {
  await page.goto(origin + "/editor");
  await login(page, "Alice");
  await openWork(page);
  await request.post(origin + "/studio-control", { data: { expired: true } });
  // A pending comparison read can detect expiry and unmount the editor first.
  // Trigger the explicit refresh synchronously only while its control exists.
  await page.evaluate(() => {
    [...document.querySelectorAll<HTMLButtonElement>("button")]
      .find((button) => button.textContent === "Перечитать черновик")
      ?.click();
  });
  await expect(page.getByRole("link", { name: "Войти", exact: true })).toBeVisible();
  await expect(page.locator(".frame-wrap iframe")).toHaveCount(0);
  expect(new URL(page.url()).hash).toBe("");
});
test("STU-082: another-tab actor change clears editor and does not replay a command", async ({
  page,
}) => {
  await page.goto(origin + "/editor");
  await login(page, "Alice");
  await openWork(page);
  const writes: string[] = [];
  page.on("request", (r) => {
    if (r.method() === "POST" && r.url().includes("/commands/")) writes.push(r.url());
  });
  await page.context().request.post(origin + "/studio-control", { data: { user: "bob" } });
  await page.getByRole("button", { name: "Перечитать черновик", exact: true }).click();
  await expect(page.locator(".person-bar")).toContainText("Bob");
  await expect(page.locator(".frame-wrap iframe")).toHaveCount(0);
  expect(new URL(page.url()).hash).toBe("");
  expect(writes).toEqual([]);
});
test("STU-080: invalid callback shows a generic retry message", async ({ page }) => {
  await page.goto(origin + "/api/studio/auth/callback?state=invalid&code=invalid");
  await expect(page.getByRole("alert")).toHaveText("Вход не завершён. Повторите попытку.");
  await expect(page.getByRole("link", { name: "Войти", exact: true })).toBeVisible();
});
