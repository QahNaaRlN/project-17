import { expect, test, type Page } from "@playwright/test";
import { fileURLToPath } from "node:url";
const origin = "http://127.0.0.1:18084",
  id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11",
  entityId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c55",
  assetId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c66",
  newId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c77";
const button = (page: Page, name: string) => page.getByRole("button", { name, exact: true });
async function open(page: Page) {
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  await button(page, "Контент").click();
  await expect(button(page, "Статья · " + entityId.slice(-8))).toBeVisible();
}
async function read(page: Page, objectId: string) {
  return (
    await (
      await page.request.get(
        origin + `/api/studio/api/v1/content/${objectId}?environment=staging&changesetId=${id}`,
      )
    ).json()
  ).data;
}
test.beforeEach(async ({ request }) => {
  await request.post(origin + "/control", { data: { mode: "ready" } });
  await request.post(origin + "/studio-control", { data: { reset: true } });
});
test("STU-100/101/102/104: content → locale/rich blocks → exact preview → undo/redo/reload", async ({
  page,
}, info) => {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await open(page);
  await button(page, "Статья · " + entityId.slice(-8)).click();
  await page.getByLabel("Content title ru", { exact: true }).fill("Статья из Studio");
  await page
    .getByLabel("Content body/0/0 text", { exact: true })
    .fill("Rich <script>plain text</script>");
  await page.locator('.rich-node input[type="checkbox"]').first().check();
  const writes: unknown[] = [];
  page.on("request", (r) => {
    if (r.url().endsWith("/commands/apply-operations")) writes.push(r.postDataJSON().payload);
  });
  await expect(button(page, "Контент")).toBeDisabled();
  await button(page, "Сохранить контент").click();
  const frame = page.frameLocator(".frame-wrap iframe");
  await expect(frame.locator('[data-cms-node="n_rich"] strong')).toHaveText(
    "Rich <script>plain text</script>",
  );
  expect(writes).toHaveLength(1);
  expect(writes[0]).toMatchObject({
    group: true,
    operations: [{ type: "entity.setFields", target: entityId }],
  });
  expect((await read(page, entityId)).title).toEqual({ ru: "Статья из Studio", en: "Keep" });
  await button(page, "Отменить").click();
  await expect(frame.locator('[data-cms-node="n_rich"]')).toHaveText("Rich before");
  await button(page, "Повторить").click();
  await expect(frame.locator('[data-cms-node="n_rich"] strong')).toContainText("plain text");
  await page.reload();
  await expect(frame.locator('[data-cms-node="n_rich"] strong')).toContainText("plain text");
  await button(page, "Контент").click();
  await button(page, "Статья из Studio · " + entityId.slice(-8)).click();
  await expect(page.getByLabel("Content title ru", { exact: true })).toHaveValue(
    "Статья из Studio",
  );
  await page.screenshot({ path: info.outputPath("studio-content.png"), fullPage: true });
});
test("STU-101/104: создание после lost response повторяет прежний key/body без второй сущности", async ({
  page,
  request,
}) => {
  await open(page);
  await page.getByLabel("Схема контента", { exact: true }).selectOption("Article");
  await button(page, "Новая сущность").click();
  await page.getByLabel("Content title ru", { exact: true }).fill("Созданная статья");
  const calls: { key: string; body: unknown }[] = [];
  page.on("request", (r) => {
    if (r.url().endsWith("/commands/apply-operations"))
      calls.push({ key: r.headers()["idempotency-key"]!, body: r.postDataJSON() });
  });
  await request.post(origin + "/studio-control", { data: { fault: "lost" } });
  await button(page, "Сохранить контент").click();
  await expect(button(page, "Проверить сохранение контента")).toBeVisible();
  expect((await read(page, newId)).title).toEqual({ ru: "Созданная статья" });
  await expect(button(page, "Новая сущность")).toBeDisabled();
  await button(page, "Проверить сохранение контента").click();
  await expect(button(page, "Созданная статья · " + newId.slice(-8))).toBeVisible();
  expect(calls).toHaveLength(2);
  expect(calls[1]).toEqual(calls[0]);
  await page.reload();
  await button(page, "Контент").click();
  await expect(button(page, "Созданная статья · " + newId.slice(-8))).toHaveCount(1);
});
test("STU-103/104: metadata → upload retry → verified ready → explicit replace → file undo", async ({
  page,
  request,
}) => {
  await open(page);
  await page.getByLabel("Тип контента", { exact: true }).selectOption("asset");
  await button(page, "Picture · " + assetId.slice(-8)).click();
  await page.getByLabel("Content alt ru", { exact: true }).fill("Новый alt");
  await button(page, "Сохранить контент").click();
  await expect(button(page, "Сохранить контент")).toBeDisabled();
  expect((await read(page, assetId)).alt).toEqual({ ru: "Новый alt", en: "Keep" });
  const creates: { key: string; body: unknown }[] = [],
    attachments: unknown[] = [];
  page.on("request", (r) => {
    if (r.url().endsWith("/commands/create-asset-upload"))
      creates.push({ key: r.headers()["idempotency-key"]!, body: r.postDataJSON() });
    if (r.url().endsWith("/commands/apply-operations")) attachments.push(r.postDataJSON());
  });
  await expect(page.getByLabel("Файл ассета", { exact: true })).toBeEnabled();
  await page
    .getByLabel("Файл ассета", { exact: true })
    .setInputFiles(fileURLToPath(new URL("../assets/vase.png", import.meta.url)));
  await request.post(origin + "/studio-control", { data: { fault: "upload-lost" } });
  await button(page, "Загрузить / повторить").click();
  await expect(page.locator(".asset-upload")).toContainText("STUDIO_RESPONSE_LOST");
  await button(page, "Загрузить / повторить").click();
  await expect(button(page, "Заменить файл в черновике")).toBeVisible();
  expect(creates).toHaveLength(2);
  expect(creates[1]).toEqual(creates[0]);
  expect(attachments).toHaveLength(0);
  await button(page, "Заменить файл в черновике").click();
  await expect(button(page, "Загрузить / повторить")).toHaveCount(0);
  const hash = (await read(page, assetId)).fileHash;
  expect(hash).not.toBe("a".repeat(64));
  await button(page, "Отменить").click();
  await expect.poll(async () => (await read(page, assetId)).fileHash).toBe("a".repeat(64));
  expect((await read(page, assetId)).alt).toEqual({ ru: "Новый alt", en: "Keep" });
  await button(page, "Повторить").click();
  await expect.poll(async () => (await read(page, assetId)).fileHash).toBe(hash);
});
test("STU-103: готовая загрузка создаёт новый ассет только по явному действию", async ({
  page,
}) => {
  await open(page);
  await page.getByLabel("Тип контента", { exact: true }).selectOption("asset");
  await expect(page.getByLabel("Файл ассета", { exact: true })).toBeEnabled();
  await page
    .getByLabel("Файл ассета", { exact: true })
    .setInputFiles(fileURLToPath(new URL("../assets/vase.png", import.meta.url)));
  await button(page, "Загрузить / повторить").click();
  await expect(button(page, "Добавить ассет в черновик")).toBeVisible();
  const before = await (
    await page.request.get(origin + `/api/studio/api/v1/changesets/${id}`)
  ).json();
  expect(before.seq).toBe(1);
  await button(page, "Добавить ассет в черновик").click();
  await expect(page.getByLabel("Content title", { exact: true })).toBeVisible();
  await expect(button(page, "Отменить")).toBeDisabled();
  await page.getByLabel("Content title", { exact: true }).fill("Uploaded image");
  await button(page, "Сохранить контент").click();
  const uploadedId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c88";
  await expect.poll(async () => (await read(page, uploadedId)).title).toBe("Uploaded image");
  expect((await read(page, uploadedId)).mimeType).toBe("image/png");
  await button(page, "Отменить").click();
  await expect.poll(async () => (await read(page, uploadedId)).title).toBeUndefined();
  expect((await read(page, uploadedId)).fileHash).toMatch(/^[a-f0-9]{64}$/);
});
for (const fault of ["reject", "conflict"])
  test(`STU-104: ${fault} сохраняет ввод, перечитывает CS и требует явного сброса`, async ({
    page,
    request,
  }) => {
    await open(page);
    await button(page, "Статья · " + entityId.slice(-8)).click();
    await page.getByLabel("Content title ru", { exact: true }).fill("Unsaved");
    await request.post(origin + "/studio-control", { data: { fault } });
    await button(page, "Сохранить контент").click();
    await expect(page.getByLabel("Content title ru", { exact: true })).toHaveValue("Unsaved");
    expect((await read(page, entityId)).title.ru).toBe("Статья");
    if (fault === "conflict") await expect(button(page, "Сохранить контент")).toBeDisabled();
    await button(page, "Сбросить контент").click();
    await expect(page.getByLabel("Content title ru", { exact: true })).toHaveValue("Статья");
  });
test("STU-104: смена OIDC человека уничтожает прежний контентный буфер", async ({
  page,
  request,
}) => {
  await request.post(origin + "/studio-control", { data: { identity: true } });
  await page.goto(origin + "/editor");
  await page.getByRole("link", { name: "Войти", exact: true }).click();
  await page.getByRole("link", { name: "Войти как Alice", exact: true }).click();
  await page.getByRole("button", { name: "Работы", exact: true }).click();
  await button(page, "Открыть работу").click();
  await button(page, "Открыть редактор").click();
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  await button(page, "Контент").click();
  await button(page, "Статья · " + entityId.slice(-8)).click();
  await page.getByLabel("Content title ru", { exact: true }).fill("Private unsaved");
  let writes = 0;
  page.on("request", (r) => {
    if (r.url().includes("/commands/apply-operations")) writes++;
  });
  await page.context().request.post(origin + "/studio-control", { data: { user: "bob" } });
  await button(page, "Сбросить ввод и перечитать").click();
  await expect(page.locator(".person-bar")).toContainText("Bob");
  await expect(page.locator(".content-panel")).toHaveCount(0);
  expect(writes).toBe(0);
});
