import { refreshMountedEditor } from "./identity-controls.js";
import { expect, test, type Page } from "@playwright/test";
import type { IrDocument } from "@cms/ir";
const origin = "http://127.0.0.1:18084",
  id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11";
const textbox = (page: Page) => page.getByRole("textbox", { name: "Редактор JSON", exact: true });
async function record(page: Page) {
  return (
    await (
      await page.request.get(origin + `/api/studio/api/v1/documents/${id}?changesetId=${id}`)
    ).json()
  ).body as IrDocument;
}
async function input(page: Page, value: unknown) {
  await textbox(page).click();
  await textbox(page).press("ControlOrMeta+a");
  await page.keyboard.insertText(
    typeof value === "string" ? value : JSON.stringify(value, null, 2),
  );
}
async function open(page: Page) {
  await page.goto(`${origin}/editor#document=${id}&changeset=${id}`);
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  await page.locator('[role="treeitem"][title="n_title"]').click();
  await page.getByRole("button", { name: "Structure", exact: true }).click();
  await expect(textbox(page)).toBeVisible();
}
test.beforeEach(async ({ request }) => {
  await request.post(origin + "/control", { data: { mode: "ready" } });
  await request.post(origin + "/studio-control", { data: { reset: true } });
});
test("STU-090/091/093/094: full-page JSON → atomic history → preview → undo/redo/reload", async ({
  page,
}, info) => {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await open(page);
  await page.getByLabel("Область JSON", { exact: true }).selectOption("document");
  const doc = await record(page);
  doc.nodes.n_title!.name = "Заголовок JSON";
  doc.nodes.n_title!.props = { level: 3 };
  doc.localContent!.t_title!.value = { ru: "Текст из JSON", en: "JSON text" };
  const writes: {
    key: string | undefined;
    payload: { group: boolean; operations: { type: string }[] };
  }[] = [];
  page.on("request", (r) => {
    if (r.url().endsWith("/commands/apply-operations"))
      writes.push({ key: r.headers()["idempotency-key"], payload: r.postDataJSON().payload });
  });
  await input(page, doc);
  await expect(page.getByRole("button", { name: "Визуальный", exact: true })).toBeDisabled();
  await expect(page.locator(".json-diff")).toContainText("node.rename");
  await page.getByRole("button", { name: "Сохранить JSON", exact: true }).click();
  await expect(
    page
      .frameLocator(".frame-wrap iframe")
      .getByRole("heading", { name: "Текст из JSON", exact: true }),
  ).toBeVisible();
  expect(writes).toHaveLength(1);
  expect(writes[0]!.payload.group).toBe(true);
  expect(writes[0]!.payload.operations.map((o) => o.type)).toEqual([
    "node.setProps",
    "node.rename",
    "localContent.set",
  ]);
  await expect(page.getByRole("button", { name: "Сохранить JSON", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Отменить", exact: true }).click();
  await expect(
    page
      .frameLocator(".frame-wrap iframe")
      .getByRole("heading", { name: "Заголовок черновика", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Повторить", exact: true }).click();
  await expect(
    page
      .frameLocator(".frame-wrap iframe")
      .getByRole("heading", { name: "Текст из JSON", exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page
      .frameLocator(".frame-wrap iframe")
      .getByRole("heading", { name: "Текст из JSON", exact: true }),
  ).toBeVisible();
  await page.locator('[role="treeitem"][title="n_title"]').click();
  await page.getByRole("button", { name: "Structure", exact: true }).click();
  await expect(textbox(page)).toContainText("Заголовок JSON");
  await page.screenshot({ path: info.outputPath("studio-json-ready.png"), fullPage: true });
});
test("STU-090/091: structure JSON moves existing IDs and inserts a subtree with preview", async ({
  page,
}) => {
  await open(page);
  await page.getByLabel("Область JSON", { exact: true }).selectOption("document");
  const doc = await record(page);
  const root = doc.nodes[doc.root]!;
  root.children = ["n_title", ...root.children!.filter((n) => n !== "n_title"), "n_json_group"];
  doc.nodes.n_json_group = { id: "n_json_group", type: "Stack", children: ["n_json_heading"] };
  doc.nodes.n_json_heading = {
    id: "n_json_heading",
    type: "Heading",
    props: { level: 2 },
    bindings: { text: "$local.t_extra" },
  };
  doc.localContent!.t_extra = { type: "text", value: { ru: "Новый из JSON" } };
  await input(page, doc);
  await expect(page.locator(".json-diff")).toContainText("node.move");
  await expect(page.locator(".json-diff")).toContainText("node.insert");
  await page.getByRole("button", { name: "Сохранить JSON", exact: true }).click();
  await expect(
    page
      .frameLocator(".frame-wrap iframe")
      .getByRole("heading", { name: "Новый из JSON", exact: true }),
  ).toBeVisible();
  expect((await record(page)).nodes[doc.root]!.children).toEqual(root.children);
  await expect(page.locator('[role="treeitem"][title="n_json_heading"]')).toBeVisible();
});
test("STU-090/092: invalid syntax/ID never sends; Ctrl+Z stays inside input", async ({ page }) => {
  await open(page);
  let writes = 0;
  page.on("request", (r) => {
    if (r.url().includes("/commands/") && r.method() === "POST") writes++;
  });
  await input(page, "{");
  await expect(page.locator(".json-diagnostics")).toContainText("JSON_SYNTAX");
  await expect(page.getByRole("button", { name: "Сохранить JSON", exact: true })).toBeDisabled();
  await textbox(page).press("ControlOrMeta+z");
  await page.getByRole("button", { name: "Сбросить ввод", exact: true }).click();
  const node = (await record(page)).nodes.n_title!;
  node.id = "n_changed";
  await input(page, node);
  await expect(page.locator(".json-diagnostics")).toContainText("JSON_NODE_ID_IMMUTABLE");
  await page.locator('[role="treeitem"][title="n_store"]').click();
  await expect(page.locator('[role="treeitem"][aria-selected="true"]')).toHaveAttribute(
    "title",
    "n_title",
  );
  expect(writes).toBe(0);
  await page.getByRole("button", { name: "Сбросить ввод", exact: true }).click();
  await page.getByLabel("Язык", { exact: true }).selectOption("en");
  await expect(page.getByRole("textbox", { name: "JSON editor", exact: true })).toBeVisible();
});
for (const fault of ["reject", "conflict", "lost"]) {
  test(
    "STU-093: JSON preserves input after " + fault + " and rereads server",
    async ({ page, request }) => {
      await open(page);
      const node = (await record(page)).nodes.n_title!;
      node.name = "Попытка JSON";
      await input(page, node);
      await request.post(origin + "/studio-control", { data: { fault } });
      await page.getByRole("button", { name: "Сохранить JSON", exact: true }).click();
      await expect(page.locator("footer")).toContainText(
        fault === "reject"
          ? "FORBIDDEN"
          : fault === "conflict"
            ? "CHANGESET_SEQ_CONFLICT"
            : "STUDIO_RESPONSE_LOST",
      );
      await expect(textbox(page)).toContainText("Попытка JSON");
      expect((await record(page)).nodes.n_title!.name).toBe(
        fault === "lost" ? "Попытка JSON" : undefined,
      );
      if (fault !== "reject")
        await expect(
          page.getByRole("button", { name: "Сохранить JSON", exact: true }),
        ).toBeDisabled();
      await page.getByRole("button", { name: "Сбросить ввод", exact: true }).click();
    },
  );
}
test("STU-093: changed OIDC actor destroys unsaved JSON and cannot send it", async ({
  page,
  request,
}) => {
  await request.post(origin + "/studio-control", { data: { identity: true } });
  await page.goto(origin + "/editor");
  await page.getByRole("link", { name: "Войти", exact: true }).click();
  await page.getByRole("link", { name: "Войти как Alice", exact: true }).click();
  await page.getByRole("button", { name: "Работы", exact: true }).click();
  await page.getByRole("button", { name: "Открыть работу", exact: true }).click();
  await page.getByRole("button", { name: "Открыть страницу /store", exact: true }).click();
  await expect(page.getByText("Предпросмотр подключён", { exact: true })).toBeVisible();
  await page.locator('[role="treeitem"][title="n_title"]').click();
  await page.getByRole("button", { name: "Structure", exact: true }).click();
  const node = (await record(page)).nodes.n_title!;
  node.name = "Private unsaved JSON";
  await input(page, node);
  let writes = 0;
  page.on("request", (r) => {
    if (r.url().includes("/commands/apply-operations")) writes++;
  });
  await page.context().request.post(origin + "/studio-control", { data: { user: "bob" } });
  await refreshMountedEditor(page, "Сбросить ввод и перечитать");
  await expect(page.locator(".person-bar")).toContainText("Bob");
  await expect(page.locator(".cm-editor")).toHaveCount(0);
  expect(writes).toBe(0);
});
