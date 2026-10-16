import { expect, test } from "@playwright/test";
const origin = "http://127.0.0.1:18084",
  id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11";
test.beforeEach(async ({ request }) => {
  await request.post(origin + "/control", { data: { mode: "ready" } });
  await request.post(origin + "/studio-control", { data: { reset: true } });
});
test("STU-071/073: edit → checks → explicit publication, receipt and read-only state", async ({
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  await page.getByRole("treeitem", { name: "◇ Heading", exact: true }).click();
  await page.getByLabel("text", { exact: true }).fill("Текст для публикации");
  await expect(
    page.getByRole("button", { name: "Отправить на проверку", exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByText("Сохраните введённые значения перед отправкой черновика.", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Сохранить text", exact: true }).click();
  await expect(
    page.frameLocator(".frame-wrap iframe").getByRole("heading", { name: "Текст для публикации" }),
  ).toBeVisible();
  const panel = page.getByRole("region", { name: "Проверки и публикация", exact: true });
  await panel.getByRole("button", { name: "Отправить на проверку", exact: true }).click();
  await expect(panel.getByText("Готов к публикации", { exact: true })).toBeVisible();
  await expect(
    panel.getByText("Страница и контракт окружения: Пройдена", { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Сохранить text", exact: true })).toBeDisabled();
  await panel.getByRole("button", { name: "Опубликовать", exact: true }).click();
  await expect(panel.locator(".publication-confirmation")).toBeFocused();
  await expect(panel.locator(".publication-confirmation")).toContainText("staging");
  await panel.getByRole("button", { name: "Подтвердить публикацию", exact: true }).click();
  await expect(panel).toContainText("Публикация подтверждена: staging");
  await expect(panel.getByRole("link", { name: "Открыть страницу приложения" })).toHaveAttribute(
    "href",
    "http://127.0.0.1:18083/store",
  );
  await expect(panel.getByRole("button", { name: "Опубликовать", exact: true })).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath("studio-publication.png"), fullPage: true });
});
test("STU-071/072: failed check selects its node; reopening and fixing allows another submission", async ({
  page,
  request,
}) => {
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  const panel = page.getByRole("region", { name: "Проверки и публикация", exact: true });
  await request.post(origin + "/studio-control", { data: { fault: "submit-invalid" } });
  await panel.getByRole("button", { name: "Отправить на проверку", exact: true }).click();
  await expect(panel.getByText("PROP_REQUIRED", { exact: true })).toBeVisible();
  await panel.getByRole("button", { name: "Выбрать блок", exact: true }).last().click();
  await expect(page.locator('[role="treeitem"][aria-selected="true"]')).toHaveAttribute(
    "title",
    "n_title",
  );
  await panel.getByRole("button", { name: "Вернуть в работу", exact: true }).click();
  await panel
    .getByRole("button", { name: "Вернуть в работу и сбросить согласования", exact: true })
    .click();
  await page.getByLabel("text", { exact: true }).fill("Исправленный текст");
  await page.getByRole("button", { name: "Сохранить text", exact: true }).click();
  await request.post(origin + "/studio-control", { data: { fault: "" } });
  await panel.getByRole("button", { name: "Отправить на проверку", exact: true }).click();
  await expect(panel.getByText("Готов к публикации", { exact: true })).toBeVisible();
});
test("STU-072: service actor cannot approve; human request for changes and approval are explicit", async ({
  page,
  request,
}) => {
  await request.post(origin + "/studio-control", { data: { state: "in_review" } });
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  const panel = page.getByRole("region", { name: "Проверки и публикация", exact: true });
  await expect(panel.getByRole("button", { name: "Согласовать", exact: true })).toBeDisabled();
  await expect(panel).toContainText("Согласование требует входа от имени человека");
  await request.post(origin + "/studio-control", { data: { human: true } });
  await page.getByRole("button", { name: "Перечитать черновик", exact: true }).click();
  await panel.getByRole("button", { name: "Запросить правки", exact: true }).click();
  const confirm = panel.locator(".publication-confirmation");
  await expect(
    confirm.getByRole("button", { name: "Запросить правки", exact: true }),
  ).toBeDisabled();
  await panel.getByLabel("Что нужно исправить").fill("Исправить заголовок");
  await confirm.getByRole("button", { name: "Запросить правки", exact: true }).click();
  await expect(panel.getByText("Нужны правки", { exact: true })).toBeVisible();
  await panel.getByRole("button", { name: "Вернуть в работу", exact: true }).click();
  await panel
    .getByRole("button", { name: "Вернуть в работу и сбросить согласования", exact: true })
    .click();
  await panel.getByRole("button", { name: "Отправить на проверку", exact: true }).click();
  await panel.getByRole("button", { name: "Согласовать", exact: true }).click();
  await expect(panel.getByText("Готов к публикации", { exact: true })).toBeVisible();
  await page.getByLabel("Язык", { exact: true }).selectOption("en");
  await expect(page.getByRole("region", { name: "Checks and publishing" })).toContainText(
    "Ready to publish",
  );
});
test("STU-074: lost committed publication retries exact key/body without another publication", async ({
  page,
  request,
}) => {
  await request.post(origin + "/studio-control", {
    data: { state: "approved", fault: "workflow-lost" },
  });
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  const commands: { key: string; body: string | null }[] = [];
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  page.on("request", (r) => {
    if (r.url().endsWith("/commands/publish"))
      commands.push({ key: r.headers()["idempotency-key"]!, body: r.postData() });
  });
  const panel = page.getByRole("region", { name: "Проверки и публикация", exact: true });
  await panel.getByRole("button", { name: "Опубликовать", exact: true }).click();
  await panel.getByRole("button", { name: "Подтвердить публикацию", exact: true }).click();
  await expect(
    panel.getByRole("button", { name: "Повторить тот же запрос", exact: true }),
  ).toBeVisible();
  await page.locator(".editor-selector > summary").click();
  await expect(page.getByRole("button", { name: "Открыть", exact: true })).toBeDisabled();
  await expect(panel).not.toContainText("Публикация подтверждена");
  await panel.getByRole("button", { name: "Повторить тот же запрос", exact: true }).click();
  await expect(panel).toContainText("Публикация подтверждена: staging");
  expect(commands).toHaveLength(2);
  expect(commands[0]).toEqual(commands[1]);
});
test("STU-074: concurrent edit refuses submit and reloads the new seq", async ({
  page,
  request,
}) => {
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  await request.post(origin + "/studio-control", { data: { fault: "workflow-stale" } });
  const panel = page.getByRole("region", { name: "Проверки и публикация", exact: true });
  await panel.getByRole("button", { name: "Отправить на проверку", exact: true }).click();
  await expect(panel).toContainText("Черновик изменился");
  await expect(panel.getByText("В работе", { exact: true })).toBeVisible();
  await expect(page.locator("footer")).toContainText("#2");
});
