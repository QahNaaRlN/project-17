import { expect, test, type Page } from "@playwright/test";
import { applyDocumentPatch, documentHash } from "@cms/runtime";
import type { StoreModel } from "../src/model.js";
const appOrigin = "http://127.0.0.1:18083";
async function messages(page: Page, type: string) {
  return page.evaluate(
    (type) =>
      (
        window as unknown as {
          previewMessages: { type: string; payload: Record<string, unknown> }[];
        }
      ).previewMessages.filter((item) => item.type === type),
    type,
  );
}
async function send(page: Page, type: string, payload: Record<string, unknown>, v = 1) {
  await page.evaluate(
    ({ type, payload, v }) =>
      (document.getElementById("preview") as HTMLIFrameElement).contentWindow!.postMessage(
        { cms: "preview", v, id: "studio-test", type, payload },
        "http://127.0.0.1:18083",
      ),
    { type, payload, v },
  );
}
test.beforeEach(async ({ request }) => {
  expect(
    (await request.post("http://127.0.0.1:18084/control", { data: { mode: "ready" } })).ok(),
  ).toBe(true);
});
test("SDK-030/031/032/033/040/042: настоящий iframe выбирает Repeat, меняет режим, применяет patch и fallback", async ({
  page,
  request,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("http://127.0.0.1:18084/studio");
  await expect.poll(async () => (await messages(page, "ready")).length).toBe(1);
  const frame = page.frameLocator("#preview");
  await expect(frame.locator(".product-card")).toHaveCount(3);
  const app = page
    .frames()
    .find(
      (item) => item.url().startsWith(appOrigin + "/") && !item.url().includes("/api/cms/preview"),
    )!;
  const model = await app.evaluate(
    () => JSON.parse(document.getElementById("store-model")!.textContent!) as StoreModel,
  );
  expect(model.preview).toBe(true);
  expect(JSON.stringify(model)).not.toMatch(/server-only-e2e-delivery-key|header\..*\.fixture/);
  const cookie = (await page.context().cookies()).find((item) => item.name === "cms_preview")!;
  expect(cookie.httpOnly).toBe(true);
  expect(cookie.secure).toBe(true);
  expect(cookie.sameSite).toBe("None");
  await send(page, "init", {
    sessionId: "test",
    changesetId: model.bridge!.changesetId,
    locale: "ru",
    mode: "design",
  });
  await expect.poll(async () => (await messages(page, "ready")).length).toBe(2);
  const favorite = frame.getByRole("button", { name: "В избранное: Ваза «Линия»" });
  await favorite.focus();
  await favorite.press("Enter");
  await expect.poll(async () => (await messages(page, "node.click")).length).toBe(1);
  expect((await messages(page, "node.click"))[0]!.payload).toMatchObject({
    nodeId: "n_product",
    instance: "n_products:string%3Avase/n_product",
  });
  await expect(frame.locator(".action-notice")).toHaveCount(0);
  const rogue = page.frames().find((item) => item.url().endsWith("/rogue"))!;
  await rogue.evaluate(() =>
    (parent.document.getElementById("preview") as HTMLIFrameElement).contentWindow!.postMessage(
      { cms: "preview", v: 1, id: "rogue", type: "mode", payload: { mode: "interact" } },
      "http://127.0.0.1:18083",
    ),
  );
  await favorite.click();
  await expect(frame.locator(".action-notice")).toHaveCount(0);
  await send(page, "mode", { mode: "interact" });
  await favorite.click();
  await expect(frame.getByRole("status")).toHaveText("«Ваза «Линия»» добавлен в избранное");
  const patch = [{ op: "add", path: "/nodes/n_about/design", value: { padding: { raw: 12 } } }];
  const next = applyDocumentPatch(model.bridge!.document, patch),
    hash = await documentHash(next);
  await send(page, "document.patch", {
    documentId: model.bridge!.documentId,
    baseHash: model.bridge!.hash,
    hash,
    patch,
  });
  await expect(frame.locator(".about")).toHaveCSS("padding-top", "12px");
  await expect
    .poll(async () => (await messages(page, "rendered")).some((item) => item.payload.hash === hash))
    .toBe(true);
  await expect
    .poll(async () =>
      (await messages(page, "layout")).some(
        (item) => (item.payload.nodes as Record<string, unknown[]>)?.n_product?.length === 3,
      ),
    )
    .toBe(true);
  await request.post("http://127.0.0.1:18084/control", {
    data: { mode: "ready", title: "Серверный черновик" },
  });
  await send(page, "document.patch", {
    documentId: model.bridge!.documentId,
    baseHash: model.bridge!.hash,
    hash,
    patch,
  });
  await expect(frame.getByRole("heading", { level: 1 })).toContainText("Серверный черновик");
  await expect(frame.locator(".about")).not.toHaveCSS("padding-top", "12px");
  await send(page, "init", { sessionId: "test", locale: "ru", mode: "design" }, 2);
  await expect
    .poll(async () =>
      (await messages(page, "error")).some(
        (item) => item.payload.code === "CMS_PREVIEW_PROTOCOL_UNSUPPORTED",
      ),
    )
    .toBe(true);
  expect(errors).toEqual([]);
});
test("SDK-030/032: published запрещает iframe и метаданные, плохой token не выдаёт cookie", async ({
  request,
}) => {
  const published = await request.get("/");
  expect(published.headers()["content-security-policy"]).toContain("frame-ancestors 'none'");
  expect(await published.text()).not.toContain("data-cms-node");
  const bad = await request.get("/api/cms/preview?token=bad&path=//evil.example");
  expect(bad.status()).toBe(400);
  expect(bad.headers()["set-cookie"]).toBeUndefined();
});
