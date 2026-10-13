import { expect, test, type Page } from "@playwright/test";
const origin = "http://127.0.0.1:18084",
  id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11";
async function open(page: Page, name: string) {
  await page.goto(origin + "/editor");
  await page.getByRole("link", { name: "Войти", exact: true }).click();
  await page.getByRole("link", { name: "Войти как " + name, exact: true }).click();
  await expect(page.locator(".person-bar")).toContainText(name);
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  await page.reload();
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  await expect(page.getByText("Обновления подключены", { exact: true })).toBeVisible();
}
test("STU-025/110/114: two users see CS presence and live preview; unsaved text survives external edits", async ({
  browser,
  request,
}) => {
  await request.post(origin + "/control", { data: { mode: "ready" } });
  await request.post(origin + "/studio-control", { data: { reset: true, identity: true } });
  const alice = await browser.newContext(),
    bob = await browser.newContext();
  try {
    const a = await alice.newPage(),
      b = await bob.newPage();
    await open(a, "Alice");
    await open(b, "Bob");
    const panel = a.getByRole("region", { name: "Присутствие", exact: true });
    await expect(panel).toContainText("Bob · Studio browser draft");
    await a.getByRole("treeitem", { name: "◇ Heading", exact: true }).click();
    const input = a.getByLabel("text", { exact: true });
    await input.fill("Мой несохранённый текст");
    await request.post(origin + "/studio-control", { data: { remoteTitle: "Внешняя правка" } });
    await expect(panel).toContainText("Сохранённое состояние изменилось");
    await expect(input).toHaveValue("Мой несохранённый текст");
    await expect(
      b.frameLocator("iframe").getByRole("heading", { name: "Внешняя правка", exact: true }),
    ).toBeVisible();
    // Returning the field to its saved value explicitly discards local input.
    await input.fill("Заголовок черновика");
    await expect(input).toHaveValue("Внешняя правка");
    await b.close();
    // Abrupt tab closure is covered by the server TTL, not an unload request.
    await expect(panel).toContainText("Обновления подключены");
  } finally {
    await alice.close();
    await bob.close();
  }
});
