import { expect, test } from "@playwright/test";
const origin = "http://127.0.0.1:18084",
  id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11";
test.beforeEach(async ({ request }) => {
  await request.post(origin + "/control", { data: { mode: "ready" } });
  await request.post(origin + "/studio-control", { data: { reset: true } });
});
test("STU-052: Ctrl+Z после выбора на холсте отменяет действие Studio", async ({ page }) => {
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  await page.getByRole("treeitem", { name: "◇ Heading", exact: true }).click();
  await page.getByLabel("text", { exact: true }).fill("Canvas edit");
  await page.getByRole("button", { name: "Сохранить text", exact: true }).click();
  const heading = page
    .frameLocator(".frame-wrap iframe")
    .getByRole("heading", { name: "Canvas edit", exact: true });
  await expect(heading).toBeVisible();
  await heading.click();
  await expect(page.getByRole("treeitem", { name: "◇ Heading", exact: true })).toBeFocused();
  await page.keyboard.press("Control+z");
  await expect(
    page
      .frameLocator(".frame-wrap iframe")
      .getByRole("heading", { name: "Заголовок черновика", exact: true }),
  ).toBeVisible();
});
test("STU-050/051/052: история, атомарная отмена текста и повтор с настоящим preview", async ({
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  const frame = page.frameLocator(".frame-wrap iframe");
  await page.getByRole("treeitem", { name: "◇ Heading", exact: true }).click();
  await page.getByLabel("text", { exact: true }).fill("Текст для отмены");
  await page.getByRole("button", { name: "Сохранить text", exact: true }).click();
  await expect(frame.getByRole("heading", { name: "Текст для отмены" })).toBeVisible();
  const history = page.getByRole("region", { name: "История черновика", exact: true });
  await expect(history.getByText("Изменение текста", { exact: true })).toBeVisible();
  await expect(history.locator("li")).toHaveCount(1);
  await history.getByRole("button", { name: "Отменить", exact: true }).click();
  await expect(frame.getByRole("heading", { name: "Заголовок черновика" })).toBeVisible();
  await history.getByRole("button", { name: "Повторить", exact: true }).click();
  await expect(frame.getByRole("heading", { name: "Текст для отмены" })).toBeVisible();
  await page.getByRole("treeitem", { name: "◇ Heading", exact: true }).click();
  await page.keyboard.press("Control+z");
  await expect(frame.getByRole("heading", { name: "Заголовок черновика" })).toBeVisible();
  await page.keyboard.press("Control+Shift+z");
  await expect(frame.getByRole("heading", { name: "Текст для отмены" })).toBeVisible();
  await page.reload();
  await expect(frame.getByRole("heading", { name: "Текст для отмены" })).toBeVisible();
  await expect(history.locator("li")).toHaveCount(5);
  await page.getByLabel("Язык", { exact: true }).selectOption("en");
  await expect(frame.getByRole("heading", { name: "Draft heading", exact: true })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("studio-history.png"), fullPage: true });
});
test("STU-052: отмена ввода в поле не вызывает серверную отмену", async ({ page }) => {
  const commands: string[] = [];
  page.on("request", (req) => {
    if (/\/commands\/(undo|redo)$/.test(req.url())) commands.push(req.url());
  });
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  await page.getByRole("treeitem", { name: "◇ Heading", exact: true }).click();
  await page.getByLabel("text", { exact: true }).fill("Saved");
  await page.getByRole("button", { name: "Сохранить text", exact: true }).click();
  await expect(
    page.frameLocator(".frame-wrap iframe").getByRole("heading", { name: "Saved" }),
  ).toBeVisible();
  const input = page.getByLabel("text", { exact: true });
  await input.click();
  await input.press("End");
  await input.pressSequentially(" draft");
  await input.press("Control+z");
  expect(commands).toEqual([]);
  await expect(
    page.frameLocator(".frame-wrap iframe").getByRole("heading", { name: "Saved" }),
  ).toBeVisible();
});
test("STU-053: потерянный ответ undo восстанавливает preview и повторяет тот же ключ/seq", async ({
  page,
  request,
}) => {
  const commands: { key: string; body: string }[] = [];
  page.on("request", (req) => {
    if (req.url().endsWith("/commands/undo"))
      commands.push({ key: req.headers()["idempotency-key"]!, body: req.postData()! });
  });
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  await page.getByRole("treeitem", { name: "◇ Heading", exact: true }).click();
  await page.getByLabel("text", { exact: true }).fill("Saved");
  await page.getByRole("button", { name: "Сохранить text", exact: true }).click();
  await expect(
    page.frameLocator(".frame-wrap iframe").getByRole("heading", { name: "Saved" }),
  ).toBeVisible();
  await request.post(origin + "/studio-control", { data: { fault: "history-lost" } });
  await page.getByRole("button", { name: "Отменить", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Проверить отмену или повтор", exact: true }),
  ).toBeVisible();
  await expect(
    page.frameLocator(".frame-wrap iframe").getByRole("heading", { name: "Заголовок черновика" }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Сохранить text", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Проверить отмену или повтор", exact: true }).click();
  await expect(page.getByRole("button", { name: "Повторить", exact: true })).toBeEnabled();
  expect(commands).toHaveLength(2);
  expect(commands[0]).toEqual(commands[1]);
  await expect(page.locator(".history-panel li")).toHaveCount(2);
});
test("STU-053/054: отказ и конфликт undo не заменяют текущий текст", async ({ page, request }) => {
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  await page.getByRole("treeitem", { name: "◇ Heading", exact: true }).click();
  await page.getByLabel("text", { exact: true }).fill("Saved");
  await page.getByRole("button", { name: "Сохранить text", exact: true }).click();
  await expect(
    page.frameLocator(".frame-wrap iframe").getByRole("heading", { name: "Saved" }),
  ).toBeVisible();
  for (const [fault, code] of [
    ["history-reject", "FORBIDDEN"],
    ["history-conflict", "CHANGESET_SEQ_CONFLICT"],
  ]) {
    await request.post(origin + "/studio-control", { data: { fault } });
    await page.getByRole("button", { name: "Отменить", exact: true }).click();
    await expect(page.getByRole("alert").filter({ hasText: code! })).toBeVisible();
    await expect(
      page.frameLocator(".frame-wrap iframe").getByRole("heading", { name: "Saved" }),
    ).toBeVisible();
  }
  await request.post(origin + "/studio-control", { data: { state: "in_review", fault: "" } });
  await page.getByRole("button", { name: "Перечитать черновик", exact: true }).click();
  await expect(page.getByRole("button", { name: "Отменить", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Повторить", exact: true })).toBeDisabled();
});
test("STU-040/041/042/044/131/133: открытие страницы и нового черновика без ручного ввода ID", async ({
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(origin + "/editor");
  await expect(page.getByRole("heading", { name: "Страницы", exact: true })).toBeVisible();
  await expect(page.locator(".pages-table")).toContainText("Store");
  await page.getByRole("button", { name: "Выбрать страницу /store", exact: true }).click();
  await expect(page.locator(".frame-wrap iframe")).toHaveCount(0);
  await page
    .getByLabel("Мой черновик", { exact: true })
    .selectOption({ label: "Studio browser draft" });
  await page.getByRole("button", { name: "Открыть", exact: true }).click();
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Новый черновик", exact: true }).click();
  await page.getByLabel("Название черновика", { exact: true }).fill("Новая работа");
  await page.getByRole("button", { name: "Создать и открыть", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Новая работа" })).toBeVisible();
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  await page.getByRole("treeitem", { name: "◇ Heading", exact: true }).click();
  await page.getByLabel("text", { exact: true }).fill("Работа из нового черновика");
  await page.getByRole("button", { name: "Сохранить text", exact: true }).click();
  await expect(
    page
      .frameLocator(".frame-wrap iframe")
      .getByRole("heading", { name: "Работа из нового черновика" }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page
      .frameLocator(".frame-wrap iframe")
      .getByRole("heading", { name: "Работа из нового черновика" }),
  ).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("studio-workspace.png"), fullPage: true });
});
test("STU-043: повтор создания после потерянного ответа не создаёт дубль", async ({
  page,
  request,
}) => {
  const keys: string[] = [];
  page.on("request", (req) => {
    if (req.url().endsWith("/commands/create-changeset"))
      keys.push(req.headers()["idempotency-key"]!);
  });
  await page.goto(origin + "/editor");
  await page.getByLabel("Страница", { exact: true }).selectOption({ label: "/store · Store" });
  await page.getByRole("button", { name: "Новый черновик", exact: true }).click();
  await page.getByLabel("Название черновика", { exact: true }).fill("Повторяемая работа");
  await request.post(origin + "/studio-control", { data: { fault: "create-lost" } });
  await page.getByRole("button", { name: "Создать и открыть", exact: true }).click();
  await expect(page.getByText(/второй черновик не создастся/)).toBeVisible();
  await page.getByRole("button", { name: "Повторить создание", exact: true }).click();
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  expect(keys).toHaveLength(2);
  expect(keys[0]).toBe(keys[1]);
  await expect(page.getByLabel("Мой черновик", { exact: true }).locator("option")).toHaveCount(3);
});
test("STU-001/003/010/021/026/032/033, SDK-042: Studio сохраняет черновик и переживает перезагрузку", async ({
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  const frame = page.frameLocator(".frame-wrap iframe");
  await expect(frame.getByRole("heading", { name: "Заголовок черновика" })).toBeVisible();
  await frame.getByRole("heading", { name: "Заголовок черновика" }).click();
  await expect(page.getByRole("treeitem", { name: "◇ Heading", exact: true })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(page.locator(".selection-frame")).toHaveCount(1);
  await page.getByLabel("text", { exact: true }).fill("Сохранённый текст Studio");
  await page.getByRole("button", { name: "Сохранить text", exact: true }).click();
  await expect(frame.getByRole("heading", { name: "Сохранённый текст Studio" })).toBeVisible();
  await page.getByLabel("Внутренний отступ", { exact: true }).fill("12");
  await page.getByRole("button", { name: "Сохранить Внутренний отступ", exact: true }).click();
  await expect(frame.getByRole("heading", { name: "Сохранённый текст Studio" })).toHaveCSS(
    "padding-top",
    "12px",
  );
  await page.reload();
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  await expect(frame.getByRole("heading", { name: "Сохранённый текст Studio" })).toHaveCSS(
    "padding-top",
    "12px",
  );
  const favorite = frame.getByRole("button", { name: "В избранное: Ваза «Линия»" });
  await favorite.click();
  await expect(page.getByRole("treeitem", { name: "◇ StoreProduct", exact: true })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(page.getByText("Экземпляр повторения; правка меняет шаблон")).toBeVisible();
  await page.getByRole("button", { name: "Взаимодействие", exact: true }).click();
  await favorite.click();
  await expect(frame.getByRole("status")).toContainText("добавлен в избранное");
  await page.getByLabel("Язык", { exact: true }).selectOption("en");
  await expect(page.getByRole("heading", { name: "Properties", exact: true })).toBeVisible();
  await expect(frame.getByRole("heading", { name: "Draft heading", exact: true })).toBeVisible();
  await page.getByLabel("Preview width", { exact: true }).selectOption("375");
  await expect(page.locator(".frame-wrap")).toHaveCSS("width", "375px");
  const pageText = await page.locator("body").innerText();
  expect(pageText).not.toContain("cms_svc_");
  expect(await page.evaluate(() => JSON.stringify({ ...localStorage }))).not.toMatch(
    /cms_svc_|Preview|fixture/,
  );
  expect(errors).toEqual([]);
  await page.getByRole("button", { name: "Design", exact: true }).click();
  await page.getByRole("treeitem", { name: "◇ Heading", exact: true }).click();
  await frame.getByRole("heading", { name: "Draft heading", exact: true }).scrollIntoViewIfNeeded();
  await expect(page.locator(".selection-frame")).toHaveCount(1);
  await expect(page.locator(".selection-frame")).toBeInViewport();
  await page.screenshot({ path: testInfo.outputPath("studio.png"), fullPage: true });
});
test("STU-021: отказ, конфликт и потерянный ответ восстанавливают фактический черновик", async ({
  page,
  request,
}) => {
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  await page.getByRole("treeitem", { name: "◇ Heading", exact: true }).click();
  const frame = page.frameLocator(".frame-wrap iframe");
  for (const [fault, code] of [
    ["reject", "FORBIDDEN"],
    ["conflict", "CHANGESET_SEQ_CONFLICT"],
  ] as const) {
    await request.post(origin + "/studio-control", { data: { fault } });
    await page.getByLabel("Внутренний отступ", { exact: true }).fill("24");
    await page.getByRole("button", { name: "Сохранить Внутренний отступ", exact: true }).click();
    await expect(page.getByRole("alert").filter({ hasText: code })).toBeVisible();
    await expect(frame.getByRole("heading", { name: "Заголовок черновика" })).not.toHaveCSS(
      "padding-top",
      "24px",
    );
  }
  await request.post(origin + "/studio-control", { data: { fault: "lost" } });
  await page.getByLabel("Внутренний отступ", { exact: true }).fill("36");
  await page.getByRole("button", { name: "Сохранить Внутренний отступ", exact: true }).click();
  await expect(frame.getByRole("heading", { name: "Заголовок черновика" })).toHaveCSS(
    "padding-top",
    "36px",
  );
  await expect(page.getByRole("alert").filter({ hasText: "STUDIO_RESPONSE_LOST" })).toBeVisible();
  await request.post(origin + "/studio-control", { data: { state: "in_review" } });
  await page.getByRole("button", { name: "Перечитать черновик", exact: true }).click();
  await expect(
    page.getByText("Этот черновик доступен только для чтения", { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Сохранить level", exact: true })).toBeDisabled();
});
