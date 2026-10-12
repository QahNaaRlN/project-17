import { expect, test } from "@playwright/test";
const origin = "http://127.0.0.1:18084",
  id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11";
test.beforeEach(async ({ page, request }) => {
  await request.post(origin + "/control", { data: { mode: "ready" } });
  await request.post(origin + "/studio-control", { data: { reset: true } });
  await page.setViewportSize({ width: 1500, height: 1200 });
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
});
test("STU-060/063/064: собрать Stack с Heading/Text, duplicate/delete, undo/redo и reload", async ({
  page,
}, info) => {
  const palette = page.getByRole("region", { name: "Палитра", exact: true });
  await palette.getByLabel("Компонент", { exact: true }).selectOption("Stack");
  await palette.getByRole("button", { name: "Добавить блок", exact: true }).click();
  await expect(page.getByRole("treeitem", { name: "◇ Stack", exact: true })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await palette.getByLabel("Компонент", { exact: true }).selectOption("Heading");
  await palette.getByRole("button", { name: "Добавить блок", exact: true }).click();
  const frame = page.frameLocator(".frame-wrap iframe");
  await expect(frame.getByRole("heading", { name: "Heading", exact: true })).toBeVisible();
  await page.getByLabel("text", { exact: true }).fill("Новый заголовок");
  await page.getByRole("button", { name: "Сохранить text", exact: true }).click();
  await expect(frame.getByRole("heading", { name: "Новый заголовок", exact: true })).toBeVisible();
  await page.getByRole("treeitem", { name: "◇ Stack", exact: true }).click();
  await palette.getByLabel("Компонент", { exact: true }).selectOption("Text");
  await palette.getByRole("button", { name: "Добавить блок", exact: true }).click();
  await page.getByLabel("text", { exact: true }).fill("Новый абзац");
  await page.getByRole("button", { name: "Сохранить text", exact: true }).click();
  await expect(frame.getByText("Новый абзац", { exact: true })).toBeVisible();
  const structure = page.getByRole("region", { name: "Структура", exact: true });
  const places = structure.getByLabel("Место вставки", { exact: true });
  const first = await places
    .locator("option")
    .evaluateAll((options) =>
      options
        .find((o) => o.textContent === "Stack · Дочерние элементы · позиция 1")
        ?.getAttribute("value"),
    );
  await places.selectOption(first!);
  await structure.getByRole("button", { name: "Переместить", exact: true }).click();
  await expect(page.getByText("Перемещение блока", { exact: true })).toBeVisible();
  await structure.getByRole("button", { name: "Дублировать", exact: true }).click();
  await expect(frame.getByText("Новый абзац", { exact: true })).toHaveCount(2);
  const selected = page.locator('[role="treeitem"][aria-selected="true"]');
  await selected.focus();
  await page.keyboard.press("Delete");
  await expect(frame.getByText("Новый абзац", { exact: true })).toHaveCount(1);
  await page.getByRole("button", { name: "Отменить", exact: true }).click();
  await expect(frame.getByText("Новый абзац", { exact: true })).toHaveCount(2);
  await page.getByRole("button", { name: "Повторить", exact: true }).click();
  await expect(frame.getByText("Новый абзац", { exact: true })).toHaveCount(1);
  await page.reload();
  await expect(frame.getByText("Новый абзац", { exact: true })).toHaveCount(1);
  await expect(frame.getByRole("heading", { name: "Новый заголовок", exact: true })).toBeVisible();
  await page.screenshot({ path: info.outputPath("studio-structure.png"), fullPage: true });
});
test("STU-020/062: pointer drag показывает разрешённые места и перемещает в Stack", async ({
  page,
}) => {
  const palette = page.getByRole("region", { name: "Палитра", exact: true });
  await palette.getByLabel("Компонент", { exact: true }).selectOption("Stack");
  await palette.getByRole("button", { name: "Добавить блок", exact: true }).click();
  await expect(page.getByRole("treeitem", { name: "◇ Stack", exact: true })).toBeVisible();
  const source = page.getByRole("treeitem", { name: "◇ Heading", exact: true });
  await source.scrollIntoViewIfNeeded();
  const box = (await source.boundingBox())!;
  await page.mouse.move(box.x + 50, box.y + 18);
  await page.mouse.down();
  await page.mouse.move(box.x + 70, box.y + 18, { steps: 5 });
  const target = page
    .locator(".drop-place")
    .filter({ hasText: "Stack · Дочерние элементы · позиция 1" });
  await expect(target).toBeVisible();
  await target.scrollIntoViewIfNeeded();
  const drop = (await target.boundingBox())!;
  await page.mouse.move(drop.x + 50, drop.y + 12, { steps: 12 });
  await page.mouse.up();
  await expect(source).toHaveAttribute("aria-level", "3");
  await expect(page.getByText("Перемещение блока", { exact: true })).toBeVisible();
  await expect(
    page.frameLocator(".frame-wrap iframe").getByRole("heading", { name: "Заголовок черновика" }),
  ).toBeVisible();
});
test("STU-062/064: Esc отменяет drag, Ctrl+D копирует, поля сохраняют локальную клавиатуру", async ({
  page,
}) => {
  const source = page.getByRole("treeitem", { name: "◇ Heading", exact: true });
  await source.click();
  await page.keyboard.press("Control+d");
  await expect(
    page
      .frameLocator(".frame-wrap iframe")
      .getByRole("heading", { name: "Заголовок черновика", exact: true }),
  ).toHaveCount(2);
  const box = (await page.locator('[role="treeitem"][aria-selected="true"]').boundingBox())!;
  await page.mouse.move(box.x + 50, box.y + 18);
  await page.mouse.down();
  await page.mouse.move(box.x + 70, box.y + 18);
  await expect(page.locator(".drop-places")).toBeVisible();
  await page.keyboard.press("Escape");
  await page.mouse.up();
  await expect(page.locator('.layers > p[role="status"]')).toHaveText("Перемещение отменено");
  await expect(page.locator('[id^="DndLiveRegion-"]')).toHaveText("Перемещение отменено");
  await page.getByLabel("Язык", { exact: true }).selectOption("en");
  await expect(page.getByRole("region", { name: "Palette", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Add block", exact: true })).toBeVisible();
});
test("STU-064: отказ сервера не удаляет блок ни в дереве, ни в preview", async ({
  page,
  request,
}) => {
  await request.post(origin + "/studio-control", { data: { fault: "reject" } });
  await page.getByRole("treeitem", { name: "◇ Heading", exact: true }).click();
  await page.getByRole("button", { name: "Удалить", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "FORBIDDEN" })).toBeVisible();
  await expect(page.getByRole("treeitem", { name: "◇ Heading", exact: true })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(
    page.frameLocator(".frame-wrap iframe").getByRole("heading", { name: "Заголовок черновика" }),
  ).toBeVisible();
});
test("STU-064: отказ вставки не ломает UI, потерянный ответ восстанавливает добавленный блок", async ({
  page,
  request,
}) => {
  await request.post(origin + "/studio-control", { data: { fault: "reject" } });
  const palette = page.getByRole("region", { name: "Палитра", exact: true });
  await palette.getByRole("button", { name: "Добавить блок", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "FORBIDDEN" })).toBeVisible();
  await expect(page.getByRole("treeitem", { name: "◇ Stack", exact: true })).toHaveCount(0);
  await request.post(origin + "/studio-control", { data: { fault: "lost" } });
  await palette.getByLabel("Компонент", { exact: true }).selectOption("Heading");
  await palette.getByRole("button", { name: "Добавить блок", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "STUDIO_RESPONSE_LOST" })).toBeVisible();
  await expect(
    page.frameLocator(".frame-wrap iframe").getByRole("heading", { name: "Heading", exact: true }),
  ).toHaveCount(1);
  await page.reload();
  await expect(
    page.frameLocator(".frame-wrap iframe").getByRole("heading", { name: "Heading", exact: true }),
  ).toHaveCount(1);
});
