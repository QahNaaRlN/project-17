import { expect, test, type Page } from "@playwright/test";
const origin = "http://127.0.0.1:18084";
async function openAgent(page: Page) {
  await page.goto(origin + "/editor");
  await expect(
    page.locator(".person-bar").or(page.getByRole("link", { name: "Войти", exact: true })),
  ).toBeVisible();
  if (await page.getByRole("link", { name: "Войти", exact: true }).isVisible()) {
    await page.getByRole("link", { name: "Войти", exact: true }).click();
    await page.getByRole("link", { name: "Войти как Alice", exact: true }).click();
  }
  await expect(page.locator(".person-bar")).toContainText("Alice");
  await page.getByRole("button", { name: "Работы", exact: true }).click();
  await page.getByText("Мои работы и согласование", { exact: true }).click();
  await page.getByRole("button", { name: "Открыть работу", exact: true }).click();
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Agent", exact: true }).click();
}
test.beforeEach(async ({ request }) => {
  await request.post(origin + "/control", { data: { mode: "ready" } });
  await request.post(origin + "/studio-control", { data: { reset: true, identity: true } });
});
test("STU-120/123/125: Agent persists steps, changes real preview and supports history undo", async ({
  page,
}) => {
  await openAgent(page);
  const panel = page.getByRole("region", { name: "Agent", exact: true });
  await panel.getByLabel("Задача для агента").fill("Измени текст заголовка");
  await panel.getByRole("button", { name: "Запустить задачу", exact: true }).click();
  await expect(panel.getByText("<script>Готово</script>", { exact: true })).toBeVisible();
  expect(await panel.locator("script").count()).toBe(0);
  const preview = page.frameLocator(".frame-wrap iframe");
  await expect(preview.getByRole("heading", { name: "Текст агента", exact: true })).toBeVisible();
  await page.reload();
  await page.getByRole("button", { name: "Agent", exact: true }).click();
  await expect(panel.getByText("Измени текст заголовка", { exact: true })).toBeVisible();
  await panel
    .getByRole("button", { name: "Отменить последнюю группу правок", exact: true })
    .click();
  await expect(
    preview.getByRole("heading", { name: "Заголовок черновика", exact: true }),
  ).toBeVisible();
});
test("STU-125: a lost start response repeats the same key and cancellation closes the task", async ({
  page,
  request,
}) => {
  await request.post(origin + "/studio-control", { data: { fault: "agent-lost" } });
  const calls: { key: string; body: string }[] = [];
  page.on("request", (r) => {
    if (r.url().endsWith("/commands/start-agent-run"))
      calls.push({ key: r.headers()["idempotency-key"]!, body: r.postData()! });
  });
  await openAgent(page);
  const panel = page.getByRole("region", { name: "Agent", exact: true });
  await panel.getByLabel("Задача для агента").fill("Один запуск");
  await panel.getByRole("button", { name: "Запустить задачу", exact: true }).click();
  await panel.getByRole("button", { name: "Повторить запрос", exact: true }).click();
  await expect(panel.getByRole("heading", { name: "Один запуск", exact: true })).toBeVisible();
  await expect.poll(() => calls.length).toBe(2);
  expect(calls[0]).toEqual(calls[1]);
  await request.post(origin + "/studio-control", {
    data: { reset: true, identity: true, fault: "agent-running" },
  });
  await openAgent(page);
  await panel.getByLabel("Задача для агента").fill("Долгая задача");
  await panel.getByRole("button", { name: "Запустить задачу", exact: true }).click();
  await panel.getByRole("button", { name: "Отменить задачу", exact: true }).click();
  await expect(panel.getByText("Отменено", { exact: true })).toBeVisible();
});
