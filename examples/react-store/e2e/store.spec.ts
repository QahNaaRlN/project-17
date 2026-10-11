import { test, expect, type APIRequestContext } from "@playwright/test";
import { readFile } from "node:fs/promises";

async function control(request: APIRequestContext, mode: string, title?: string) {
  const response = await request.post("http://127.0.0.1:18084/control", { data: { mode, title } });
  expect(response.ok()).toBe(true);
}
test.beforeEach(async ({ request }) => {
  await control(request, "ready");
});
test("MF-005: running application serves the CLI artifact hash", async ({ request }) => {
  const response = await request.get("/.well-known/cms-manifest-hash");
  expect(response.status()).toBe(200);
  expect((await response.text()).trim()).toBe(
    (await readFile(".cms/manifest.hash", "utf8")).trim(),
  );
});

test("IR-040/SDK-011: CMS меняет порядок разделов и условие показа без изменения React", async ({
  page,
  request,
}) => {
  await page.goto("/");
  await expect(page.locator("main > section").first()).toHaveClass(/hero/);
  await control(request, "reorder");
  await page.getByRole("button", { name: "Обновить контент" }).click();
  await expect(page.locator("main > section").first()).toHaveClass(/about/);
  await expect(page.locator(".product-card")).toHaveCount(3);
  await control(request, "hideabout");
  await page.getByRole("button", { name: "Обновить контент" }).click();
  await expect(page.locator(".about")).toHaveCount(0);
  await expect(page.locator(".product-card")).toHaveCount(3);
});

test("DS-030/031: атомы из CMS применяются после обновления и реагируют на ширину", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 375, height: 900 });
  await page.goto("/");
  await control(request, "adaptive");
  await page.getByRole("button", { name: "Обновить контент" }).click();
  await expect(page.locator(".about")).toHaveCSS("padding-top", "12px");
  await page.setViewportSize({ width: 800, height: 900 });
  await expect(page.locator(".about")).toHaveCSS("padding-top", "24px");
  await control(request, "ready");
  await page.getByRole("button", { name: "Обновить контент" }).click();
  await expect(page.locator(".about")).not.toHaveCSS("padding-top", "24px");
});

test("SSR содержит CMS-контент и не раскрывает ключ; браузер гидратирует диалог", async ({
  page,
  request,
}) => {
  const response = await request.get("/");
  expect(response.status()).toBe(200);
  const html = await response.text();
  expect(html).toContain("Ваза «Линия»");
  expect(html).not.toContain("server-only-e2e-delivery-key");
  const policy = response.headers()["content-security-policy"]!;
  const scriptPolicy = policy
    .split(";")
    .find((directive) => directive.trim().startsWith("script-src "));
  expect(scriptPolicy).toContain("'self'");
  expect(scriptPolicy).not.toMatch(/'unsafe-inline'|'unsafe-eval'/);
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  await page.goto("/");
  const trigger = page.getByRole("button", { name: "Подробнее: Ваза «Линия»" });
  await trigger.click();
  await expect(page.getByRole("dialog", { name: "Ваза «Линия»" })).toBeVisible();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await expect(page.locator(".price").first()).toHaveText("Цена не указана");
  await expect(page.locator(".product-art img").first()).toHaveAttribute("srcset", /320w.*640w/);
  expect(errors).toEqual([]);
});

test("реальный повтор читает CMS и показывает загрузку, ошибку и восстановление", async ({
  page,
  request,
}) => {
  await page.goto("/");
  await control(request, "hold");
  await page.getByRole("button", { name: "Обновить контент" }).click();
  await expect(page.getByRole("status", { name: "Загрузка контента" })).toBeVisible();
  await control(request, "error");
  await expect(page.getByRole("alert")).toContainText("Не получилось загрузить страницу");
  await control(request, "ready", "Изменено в CMS");
  await page.getByRole("button", { name: "Попробовать снова" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toContainText("Изменено в CMS");
});

test("отсутствующая страница и пустая коллекция отличаются от ошибки", async ({
  page,
  request,
}) => {
  await control(request, "missing");
  const response = await page.goto("/");
  expect(response?.status()).toBe(404);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Страница ещё не создана");
  await control(request, "empty");
  await page.getByRole("button", { name: "Попробовать снова" }).click();
  await expect(page.getByText("Новые вещи скоро появятся")).toBeVisible();
  await control(request, "missing");
  await page.getByRole("button", { name: "Обновить контент" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Страница ещё не создана");
});

test("ошибка изображения сохраняет товары; контент не исполняет HTML", async ({
  page,
  request,
}) => {
  const title = "<script>window.storeXss=true</script>";
  await control(request, "badimage", title);
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1 })).toContainText(title);
  await expect(page.locator(".product-card")).toHaveCount(3);
  await expect(
    page.locator(".product-art").first().getByText("Изображение недоступно"),
  ).toBeVisible();
  expect(await page.evaluate(() => "storeXss" in window)).toBe(false);
});

for (const width of [320, 375, 640, 768, 1280]) {
  test(`адаптация ${width}px, обе темы и меню`, async ({ page, request }) => {
    await control(request, "ready", "ОченьДлинноеНазвание".repeat(10));
    await page.setViewportSize({ width, height: 900 });
    for (const colorScheme of ["light", "dark"] as const) {
      await page.emulateMedia({ colorScheme });
      await page.goto("/");
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      );
      await expect(page.locator(".product-card")).toHaveCount(3);
      const first = page.getByRole("button", { name: "Подробнее: Ваза «Линия»" });
      await first.click();
      const box = await page.getByRole("dialog").boundingBox();
      expect(box!.x).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width).toBeLessThanOrEqual(width);
      await page.keyboard.press("Escape");
    }
    if (width <= 640) {
      await page.getByRole("button", { name: "Открыть меню" }).click();
      await expect(page.getByRole("button", { name: "Закрыть меню" })).toHaveAttribute(
        "aria-expanded",
        "true",
      );
      await page
        .getByRole("navigation", { name: "Навигация телефона" })
        .getByText("О вещах")
        .click();
      await expect(page.getByRole("navigation", { name: "Навигация телефона" })).toHaveCount(0);
    }
  });
}
