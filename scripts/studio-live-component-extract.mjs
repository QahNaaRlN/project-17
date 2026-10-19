// STU-419: MVP step 3 on a real isolated CMS/PostgreSQL/OIDC/React stand. Only the step 2
// block, its primary entity/image and the page data source are prepared through the API;
// the wizard, inputs, slot, component edit, input binding, review and publication use Studio.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { resolve, dirname } from "node:path";
import { randomUUID, createHash } from "node:crypto";
import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";
import { URL } from "node:url";
import vm from "node:vm";
const { fetch } = globalThis;
const require = createRequire(new URL("../examples/react-store/package.json", import.meta.url));
const { chromium, expect: baseExpect } = require("@playwright/test");
// Preview reloads of a real stand can take longer than Playwright's 5 s default.
const expect = baseExpect.configure({ timeout: 20000 });
if (!process.env.STUDIO_REVIEW_ADMIN_FILE || !process.env.STUDIO_REVIEW_OUTPUT)
  throw Error("Local config/output required");
const cfg = JSON.parse(await readFile(process.env.STUDIO_REVIEW_ADMIN_FILE, "utf8"));
if (
  cfg.project !== "studio-work-review" ||
  [cfg.baseUrl, cfg.studioOrigin, cfg.appOrigin].some((u) => new URL(u).hostname !== "127.0.0.1")
)
  throw Error("Isolated loopback stand only");
const output = resolve(process.env.STUDIO_REVIEW_OUTPUT);
await mkdir(output, { recursive: true });
const browser = await chromium.launch(
  process.env.PLAYWRIGHT_EXECUTABLE_PATH
    ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH }
    : {},
);
const stamp = randomUUID().slice(0, 8);
const report = {
  requirements: "STU-400..419",
  mvpStep: 3,
  stamp,
  sourceSha: execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim(),
  sourceDirty: !!execFileSync("git", ["status", "--porcelain"], { encoding: "utf8" }).trim(),
};
const delivery = JSON.parse(
  await readFile(
    resolve(dirname(process.env.STUDIO_REVIEW_ADMIN_FILE), "studio-work-store.private.json"),
    "utf8",
  ),
);
async function admin(name, payload) {
  const response = await fetch(cfg.baseUrl + "/api/v1/commands/" + name, {
    method: "POST",
    headers: {
      Authorization: "Bearer " + cfg.credential,
      "X-CMS-Project": cfg.project,
      "Content-Type": "application/json",
      "Idempotency-Key": randomUUID(),
    },
    body: JSON.stringify({ payload, reason: "Isolated component extract acceptance setup" }),
  });
  const body = await response.json();
  if (!response.ok) throw Error(name + " " + response.status + " " + body.code);
  return body.result;
}
async function deliveryGet(path) {
  return fetch(cfg.baseUrl + "/delivery/v1/" + cfg.project + "/staging/" + path, {
    headers: { Authorization: "Bearer " + delivery.deliveryKey },
  });
}
try {
  const author = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
  const reviewer = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
  const alice = await author.newPage(),
    bob = await reviewer.newPage();
  async function login(page, name, roleId) {
    await page.goto(cfg.studioOrigin + "/editor");
    await page.getByRole("link", { name: "Войти", exact: true }).click();
    await page.getByRole("link", { name: "Войти как " + name, exact: true }).click();
    await page.waitForURL(cfg.studioOrigin + "/editor**");
    const account = await (
      await page.request.get(cfg.studioOrigin + "/api/studio/auth/session")
    ).json();
    expect(account.user.name).toBe(name);
    await admin("bind-human-role", { actorId: account.user.id, roleId });
    await page.reload();
    return (await (await page.request.get(cfg.studioOrigin + "/api/studio/auth/session")).json())
      .csrfToken;
  }
  const aliceCsrf = await login(alice, "Alice", cfg.authorRole);
  await login(bob, "Bob", cfg.reviewerRole);
  async function read(context, path) {
    const r = await context.request.get(cfg.baseUrl + "/api/v1" + path, {
      headers: { "X-CMS-Project": cfg.project },
    });
    if (!r.ok()) throw Error(path + " " + r.status());
    return r.json();
  }
  async function human(name, payload, reason = "Local step 2 fixture") {
    const r = await author.request.post(cfg.baseUrl + "/api/v1/commands/" + name, {
      headers: {
        "X-CMS-Project": cfg.project,
        Origin: cfg.studioOrigin,
        "X-CSRF-Token": aliceCsrf,
        "Idempotency-Key": randomUUID(),
      },
      data: { payload, reason },
    });
    const v = await r.json();
    if (!r.ok())
      throw Error(
        name +
          " " +
          r.status() +
          " " +
          v.code +
          " " +
          (v.detail ?? "") +
          " " +
          JSON.stringify(v.params ?? {}).slice(0, 400),
      );
    return v.result;
  }

  // --- Fixture: Article schema applied in staging (same setup as the data-sources stand) ---
  const setupManifest = JSON.parse(
    await readFile(new URL("../examples/react-store/.cms/manifest.json", import.meta.url), "utf8"),
  );
  setupManifest.schemas = {
    ...setupManifest.schemas,
    Article: {
      version: 1,
      title: "Article",
      fields: {
        title: { type: "text", localized: true },
        cover: { type: "asset", assetKind: "image" },
        alt: { type: "text", localized: true },
      },
    },
  };
  const registration = await admin("register-manifest", {
    environment: "staging",
    manifest: setupManifest,
  });
  if (registration.changesetId) {
    const setupRole = await admin("create-role", {
      name: "component-schema-" + stamp,
      capabilities: ["schema.apply", "schema.read"],
    });
    const account = await (
      await author.request.get(cfg.studioOrigin + "/api/studio/auth/session")
    ).json();
    await admin("bind-human-role", { actorId: account.user.id, roleId: setupRole.id });
    await admin("set-approval-policy", { low: 0, medium: 0, high: 0 });
    const claimed = await human("claim-schema-changeset", {
      changesetId: registration.changesetId,
      expectedSeq: (await read(author, "/changesets/" + registration.changesetId)).seq,
    });
    await human("submit-changeset", {
      changesetId: registration.changesetId,
      expectedSeq: claimed.seq,
    });
    await human("publish", {
      changesetId: registration.changesetId,
      environment: "staging",
      expectedSeq: claimed.seq,
    });
    await admin("set-approval-policy", { low: 1, medium: 1, high: 1 });
    await admin("unbind-human-role", { actorId: account.user.id, roleId: setupRole.id });
    await alice.reload();
  }

  // --- Fixture: step 2 block, primary entity with a real image and a page source ---------
  const cs = await human("create-changeset", {
    title: "Сохранить как компонент " + stamp,
    targets: ["staging"],
  });
  const seq = async () => (await read(author, "/changesets/" + cs.id)).seq;
  // A unique image per run: identical files are deduplicated by the project.
  await alice.evaluate((text) => {
    const mark = globalThis.document.createElement("h1");
    mark.textContent = text;
    globalThis.document.body.prepend(mark);
  }, "Showcase " + stamp);
  const png = await alice.screenshot({ type: "png" });
  const upload = await human("create-asset-upload", {
    filename: "showcase.png",
    mimeType: "image/png",
    size: png.length,
    sha256: createHash("sha256").update(png).digest("hex"),
  });
  const put = await fetch(upload.uploadUrl, {
    method: "PUT",
    body: png,
    headers: { "Content-Type": "image/png" },
  });
  if (!put.ok) throw Error("upload PUT " + put.status);
  await human("complete-asset-upload", { assetId: upload.assetId });
  await expect
    .poll(async () => (await read(author, "/asset-uploads/" + upload.assetId)).status, {
      timeout: 60000,
    })
    .toBe("ready");
  let r = await human("apply-operations", {
    changesetId: cs.id,
    expectedSeq: await seq(),
    operations: [{ type: "asset.create", payload: { uploadId: upload.assetId } }],
  });
  const cover = r.operations[0].target;
  r = await human("apply-operations", {
    changesetId: cs.id,
    expectedSeq: await seq(),
    operations: [
      {
        type: "entity.create",
        payload: {
          schema: "Article",
          environment: "staging",
          data: {
            title: { ru: "Новинки " + stamp, en: "New " + stamp },
            cover: { assetId: cover },
          },
        },
      },
    ],
  });
  const entity = r.operations[0].target;
  // The storefront root carries the application catalog; the step 2 block is its child.
  const catalogSource = await readFile(
    new URL("../examples/react-store/scripts/catalog.ts", import.meta.url),
    "utf8",
  );
  const catalog = vm.runInNewContext(
    "(" + catalogSource.match(/export const catalog: Catalog = ([\s\S]*);/)[1] + ")",
  );
  catalog.products = catalog.products
    .slice(0, 3)
    .map((item) => ({ ...item, asset: { assetId: cover } }));
  r = await human("apply-operations", {
    changesetId: cs.id,
    expectedSeq: await seq(),
    operations: [
      {
        type: "document.create",
        payload: {
          kind: "page",
          path: "/store",
          content: { schema: "Article", resolve: { by: "entity", entityId: entity } },
          root: {
            id: "n_root",
            type: "Storefront",
            props: { config: { base: catalog } },
            children: [
              {
                id: "n_grid",
                type: "Grid",
                children: [
                  {
                    id: "n_stack",
                    type: "Stack",
                    children: [
                      {
                        id: "n_title",
                        type: "Heading",
                        props: { level: 2 },
                        bindings: { text: "$local.t_title" },
                      },
                      { id: "n_desc", type: "Text", bindings: { text: "$local.t_desc" } },
                      { id: "n_cta", type: "Button", bindings: { label: "$local.t_cta" } },
                    ],
                  },
                  {
                    id: "n_image",
                    type: "Image",
                    bindings: { src: "$content.cover", alt: "$local.t_alt" },
                  },
                ],
              },
            ],
          },
        },
      },
    ],
  });
  const pageId = r.operations[0].target;
  const texts = {
    t_title: { ru: "Витрина " + stamp, en: "Showcase " + stamp },
    t_desc: { ru: "Описание витрины", en: "Showcase description" },
    t_cta: { ru: "В каталог", en: "To catalog" },
    t_alt: { ru: "Обложка", en: "Cover" },
  };
  await human("apply-operations", {
    changesetId: cs.id,
    expectedSeq: await seq(),
    operations: [
      ...Object.entries(texts).flatMap(([key, value]) =>
        Object.entries(value).map(([locale, text]) => ({
          type: "localContent.set",
          target: pageId,
          payload: { key, type: "text", locale, value: text },
        })),
      ),
      {
        type: "document.setDataSource",
        target: pageId,
        payload: {
          key: "featured",
          use: { source: "store.products", params: { limit: { lit: 3 } } },
        },
      },
    ],
  });
  console.log("STU-419: step 2 fixture prepared through the API.");

  // --- Studio: block before extraction ----------------------------------------------------
  await alice.goto(cfg.studioOrigin + "/editor#document=" + pageId + "&changeset=" + cs.id);
  await alice.reload();
  const frame = alice.frameLocator("iframe");
  await expect(frame.getByRole("heading", { name: texts.t_title.ru, exact: true })).toBeVisible();
  async function block() {
    // Rendered block without preview attributes and instance-scoped ids (STU-412).
    return frame
      .locator("[data-cms-node='n_grid']")
      .first()
      .evaluate((el) => {
        const clone = el.cloneNode(true);
        for (const n of [clone, ...clone.querySelectorAll("*")]) {
          if (n.hasAttribute("style")) n.setAttribute("style", n.style.cssText);
          for (const a of ["id", "data-cms-node", "data-cms-instance", "src", "srcset"])
            n.removeAttribute(a);
        }
        return {
          html: clone.outerHTML,
          width: Math.round(el.getBoundingClientRect().width),
          height: Math.round(el.getBoundingClientRect().height),
        };
      });
  }
  const widths = {};
  for (const width of ["375", "1024"]) {
    await alice.getByLabel("Ширина предпросмотра", { exact: true }).selectOption(width);
    await expect(frame.getByRole("heading", { name: texts.t_title.ru, exact: true })).toBeVisible();
    widths[width] = await block();
    await alice.screenshot({ path: resolve(output, `before-${width}.png`) });
  }
  const bodyBefore = (await read(author, "/documents/" + pageId + "?changesetId=" + cs.id)).body;

  // --- Wizard STU-024 with a lost response and an exact retry -----------------------------
  await alice.locator('[role="treeitem"][title="n_grid"]').click();
  await alice.getByRole("button", { name: "Сохранить как компонент", exact: true }).click();
  const wizard = alice.getByRole("dialog", { name: "Сохранить как компонент", exact: true });
  const name = "ProductShowcase " + stamp;
  await wizard.getByLabel("Имя компонента", { exact: true }).fill(name);
  await wizard.getByRole("button", { name: "Далее", exact: true }).click();
  // Image.src depends on the page entity: forced input. Alt stays shared copied text (Q-4).
  await expect(wizard.getByLabel("Сделать входом Image.src", { exact: true })).toBeDisabled();
  await wizard.getByLabel("Сделать входом Image.alt", { exact: true }).uncheck();
  await wizard.getByLabel("Сделать входом Button.label", { exact: true }).uncheck();
  await wizard.getByLabel("Имя входа Heading.text", { exact: true }).fill("title");
  await wizard.getByLabel("Имя входа Text.text", { exact: true }).fill("description");
  await wizard.getByLabel("Имя входа Image.src", { exact: true }).fill("image");
  await expect(wizard.getByLabel("Текст t_alt", { exact: true })).toHaveValue("copy");
  await wizard.getByLabel("Имя нового входа", { exact: true }).fill("products");
  const listOption = await wizard
    .getByLabel("Тип нового входа", { exact: true })
    .locator("option")
    .evaluateAll((options) => options.map((o) => [o.value, o.textContent]));
  await wizard
    .getByLabel("Тип нового входа", { exact: true })
    .selectOption(
      listOption
        .filter(([v, t]) => v.startsWith("source:") && t.trim().startsWith("list"))
        .at(-1)[0],
    );
  await wizard.getByRole("button", { name: "Добавить вход", exact: true }).click();
  await wizard.getByRole("button", { name: "Далее", exact: true }).click();
  await wizard.getByLabel("Имя слота", { exact: true }).fill("footer");
  const parents = await wizard
    .getByLabel("Родитель", { exact: true })
    .locator("option")
    .evaluateAll((options) => options.map((o) => [o.value, o.textContent]));
  await wizard
    .getByLabel("Родитель", { exact: true })
    .selectOption(parents.find(([, t]) => t === "Stack")[0]);
  await wizard.getByLabel("Первый узел", { exact: true }).selectOption("2");
  await wizard.getByLabel("Последний узел", { exact: true }).selectOption("2");
  await wizard.getByRole("button", { name: "Добавить слот", exact: true }).click();
  await expect(wizard).toContainText("footer · Button");
  await wizard.getByRole("button", { name: "Далее", exact: true }).click();
  await expect(wizard).toContainText("Slot · footer");
  const pattern = "**/api/v1/commands/apply-operations";
  const attempts = [];
  await alice.route(pattern, async (route) => {
    attempts.push({
      key: route.request().headers()["idempotency-key"],
      body: route.request().postData(),
    });
    const response = await route.fetch();
    if (!response.ok())
      console.error("Extract rejected:", response.status(), await response.text());
    expect(response.ok()).toBe(true);
    await route.abort("failed");
  });
  await wizard.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(wizard).toContainText("Результат сохранения неизвестен");
  await alice.unroute(pattern);
  const retry = alice.waitForRequest((request) => request.url().endsWith("/apply-operations"));
  await wizard.getByRole("button", { name: "Повторить сохранение", exact: true }).click();
  const replay = await retry;
  expect({ key: replay.headers()["idempotency-key"], body: replay.postData() }).toEqual(
    attempts[0],
  );
  await expect(wizard).not.toBeVisible();
  report.exactRetry = true;
  console.log("STU-413: wizard saved one group; lost response retried with the same key/body.");

  // --- Server state: component document and Composed instance -----------------------------
  const pageBody = async () =>
    (await read(author, "/documents/" + pageId + "?changesetId=" + cs.id)).body;
  let body = await pageBody();
  const instance = Object.values(body.nodes).find((n) => n.type === "Composed");
  expect(instance.ref.version).toBe("live");
  expect(body.nodes.n_root.children).toEqual([instance.id]);
  expect(instance.slots).toEqual({ footer: ["n_cta"] });
  expect(Object.keys(instance.bindings).sort()).toEqual(["description", "image", "title"]);
  const componentId = instance.ref.component;
  const componentBody = async () =>
    (await read(author, "/documents/" + componentId + "?changesetId=" + cs.id)).body;
  let component = await componentBody();
  expect(component.kind).toBe("component");
  expect(component.meta.title).toBe(name);
  expect(Object.keys(component.inputs).sort()).toEqual([
    "description",
    "image",
    "products",
    "title",
  ]);
  expect(Object.keys(component.slotDefs)).toEqual(["footer"]);
  expect(component.localContent.t_alt.value).toEqual(texts.t_alt);
  expect(JSON.stringify(component)).not.toMatch(/\$(content|data|item)\b/);
  const history = await read(author, "/changesets/" + cs.id + "/history");
  expect(history.items[0].operations.map((o) => o.type)).toEqual(["component.extract"]);
  await expect(alice.getByText("Сохранено как компонент «" + name + "»")).toBeVisible();

  // --- STU-412: the same render before and after on 375 and 1024 px ------------------------
  // The preview must be re-rendered from the instance, not left with the old DOM.
  await expect
    .poll(() => frame.locator("[data-cms-instance]").count(), { timeout: 30000 })
    .toBeGreaterThan(0);
  for (const width of ["375", "1024"]) {
    await alice.getByLabel("Ширина предпросмотра", { exact: true }).selectOption(width);
    await expect(frame.getByRole("heading", { name: texts.t_title.ru, exact: true })).toBeVisible();
    const after = await block();
    expect(after.html).toBe(widths[width].html);
    expect(Math.abs(after.width - widths[width].width)).toBeLessThanOrEqual(1);
    expect(Math.abs(after.height - widths[width].height)).toBeLessThanOrEqual(1);
    await alice.screenshot({ path: resolve(output, `after-${width}.png`) });
  }
  await expect
    .poll(() =>
      frame.getByAltText(texts.t_alt.ru, { exact: true }).evaluate((img) => img.naturalWidth),
    )
    .toBeGreaterThan(0);
  report.renderEquivalent = ["375", "1024"];
  // Canvas click inside the instance selects the instance.
  await frame.getByRole("heading", { name: texts.t_title.ru, exact: true }).click();
  await expect(alice.locator('[role="treeitem"][aria-selected="true"]')).toHaveAttribute(
    "title",
    instance.id,
  );

  // --- STU-408: undo/redo and reload -----------------------------------------------------
  await alice.getByRole("button", { name: "Отменить", exact: true }).click();
  await expect.poll(async () => JSON.stringify(await pageBody())).toBe(JSON.stringify(bodyBefore));
  await expect
    .poll(
      async () =>
        (await read(author, "/documents/" + componentId + "?changesetId=" + cs.id).catch(
          () => null,
        )) === null,
    )
    .toBe(true);
  await alice.getByRole("button", { name: "Повторить", exact: true }).click();
  await expect
    .poll(async () => JSON.stringify(await componentBody().catch(() => null)))
    .toBe(JSON.stringify(component));
  await alice.reload();
  await expect(alice.locator('[role="treeitem"][title="' + instance.id + '"]')).toBeVisible();
  report.undoRedo = true;

  // --- STU-415: Repeat over $props.products in the component, then bind the instance -------
  await alice.locator('[role="treeitem"][title="' + instance.id + '"]').click();
  await alice.getByRole("button", { name: "Открыть компонент", exact: true }).click();
  await expect(alice.getByRole("region", { name: "Компонент", exact: true })).toContainText(name);
  const data = alice.getByRole("region", { name: "Источники данных", exact: true });
  await alice.locator('[role="treeitem"][title="n_stack"]').click();
  await data.getByRole("button", { name: "Добавить повторение", exact: true }).click();
  await data.getByLabel("Список для повторения", { exact: true }).selectOption("$props.products");
  await data.getByLabel("Ключ элемента", { exact: true }).selectOption("$item.id");
  await data.getByLabel("Предел элементов", { exact: true }).fill("3");
  await data.getByRole("button", { name: "Сохранить данные", exact: true }).click();
  await expect(
    data.getByRole("button", { name: "Сохранить данные", exact: true }),
  ).not.toBeVisible();
  component = await componentBody();
  const repeat = Object.values(component.nodes).find((n) => n.type === "Repeat");
  expect(repeat.bindings.items).toBe("$props.products");
  const palette = alice.getByRole("region", { name: "Палитра", exact: true });
  await alice.locator('[role="treeitem"][title="' + repeat.children[0] + '"]').click();
  await palette.getByLabel("Компонент", { exact: true }).selectOption("Text");
  const destination = palette.getByLabel("Палитра: Место вставки", { exact: true });
  const places = await destination.locator("option").evaluateAll((o) => o.map((x) => x.value));
  await destination.selectOption(
    places
      .map((p) => JSON.parse(p))
      .filter((p) => p.parentId === repeat.children[0])
      .map((p) => JSON.stringify(p))
      .at(-1),
  );
  const beforeNodes = new Set(Object.keys(component.nodes));
  await palette.getByRole("button", { name: "Добавить блок", exact: true }).click();
  await expect
    .poll(async () => Object.keys((await componentBody()).nodes).length)
    .toBeGreaterThan(beforeNodes.size);
  const card = Object.values((await componentBody()).nodes).find(
    (n) => !beforeNodes.has(n.id) && n.type === "Text",
  ).id;
  await alice.locator('[role="treeitem"][title="' + card + '"]').click();
  await data.getByRole("button", { name: "Привязать данные к свойству", exact: true }).click();
  await data.getByLabel("Свойство блока", { exact: true }).selectOption("text");
  await data
    .getByLabel("Поле результата или элемента", { exact: true })
    .selectOption("$item.title");
  const confirm = data.getByLabel("Подтверждаю замену или снятие", { exact: true });
  if (await confirm.isVisible()) await confirm.check();
  await data.getByRole("button", { name: "Сохранить данные", exact: true }).click();
  await expect(
    data.getByRole("button", { name: "Сохранить данные", exact: true }),
  ).not.toBeVisible();
  await alice.getByRole("button", { name: "Вернуться к странице", exact: true }).click();
  await alice.locator('[role="treeitem"][title="' + instance.id + '"]').click();
  await data.getByRole("button", { name: "Привязать данные к свойству", exact: true }).click();
  await data.getByLabel("Свойство блока", { exact: true }).selectOption("products");
  await data
    .getByLabel("Поле результата или элемента", { exact: true })
    .selectOption("$data.featured.items");
  await data.getByRole("button", { name: "Сохранить данные", exact: true }).click();
  await expect(
    data.getByRole("button", { name: "Сохранить данные", exact: true }),
  ).not.toBeVisible();
  body = await pageBody();
  expect(body.nodes[instance.id].bindings.products).toBe("$data.featured.items");
  const products = frame.locator("[data-cms-node='" + card + "']");
  await expect.poll(() => products.count()).toBe(3);
  const productTitles = await products.allInnerTexts();
  report.productsInPreview = productTitles.length;
  console.log("STU-415: Repeat over $props.products shows " + productTitles.length + " products.");

  // --- STU-418: EN and 375 px ------------------------------------------------------------
  await alice.getByLabel("Язык", { exact: true }).selectOption("en");
  await expect(alice.getByRole("button", { name: "Save as component", exact: true })).toBeVisible();
  await alice.setViewportSize({ width: 375, height: 1000 });
  expect(
    await alice.evaluate(
      () => globalThis.document.documentElement.scrollWidth <= globalThis.innerWidth,
    ),
  ).toBe(true);
  await alice.screenshot({ path: resolve(output, "component-mobile-en.png"), fullPage: true });
  await alice.setViewportSize({ width: 1440, height: 1100 });
  await alice.getByLabel("Language", { exact: true }).selectOption("ru");

  // --- STU-407/419: Bob cannot extract; Alice cannot approve; Bob approves and publishes ---
  const bobPlan = await bob.request.get(
    cfg.studioOrigin +
      `/api/studio/api/v1/documents/${pageId}/component-extract-plan?changesetId=${cs.id}&nodeId=${instance.id}&environment=staging`,
  );
  const bobBlockers = (await bobPlan.json()).blockers.map((b) => b.code);
  expect(bobBlockers).toEqual(expect.arrayContaining(["FORBIDDEN"]));
  expect((await deliveryGet("document/" + componentId)).status).toBe(404);
  await alice.goto(cfg.studioOrigin + "/editor#work=" + cs.id);
  await alice.reload();
  const authorPanel = alice.getByRole("region", { name: "Проверки и публикация", exact: true });
  await authorPanel.getByRole("button", { name: "Отправить на проверку", exact: true }).click();
  await expect(authorPanel.getByText("На согласовании", { exact: true })).toBeVisible();
  await expect(
    authorPanel.getByRole("button", { name: "Согласовать", exact: true }),
  ).toBeDisabled();
  await bob.goto(cfg.studioOrigin + "/editor#work=" + cs.id);
  await bob.reload();
  const panel = bob.getByRole("region", { name: "Проверки и публикация", exact: true });
  await panel.getByRole("button", { name: "Согласовать", exact: true }).click();
  await expect(panel.getByText("Готов к публикации", { exact: true })).toBeVisible();
  await panel.getByRole("button", { name: "Опубликовать", exact: true }).click();
  await panel.getByRole("button", { name: "Подтвердить публикацию", exact: true }).click();
  await expect(panel).toContainText("Публикация подтверждена: staging");
  const published = await deliveryGet("document/" + componentId);
  expect(published.status).toBe(200);
  expect((await published.json()).document.kind).toBe("component");

  // --- New public browser: the published block without preview attributes -----------------
  const publicContext = await browser.newContext(),
    page = await publicContext.newPage();
  await page.goto(cfg.appOrigin + "/store");
  await expect(page.getByRole("heading", { name: texts.t_title.ru, exact: true })).toBeVisible();
  await expect(page.getByText(texts.t_desc.ru, { exact: true })).toBeVisible();
  await expect(page.getByText(texts.t_cta.ru, { exact: true })).toBeVisible();
  await expect
    .poll(() =>
      page.getByAltText(texts.t_alt.ru, { exact: true }).evaluate((img) => img.naturalWidth),
    )
    .toBeGreaterThan(0);
  for (const title of productTitles)
    await expect(page.getByText(title, { exact: true }).first()).toBeVisible();
  expect(await page.locator("[data-cms-node]").count()).toBe(0);
  await page.screenshot({ path: resolve(output, "component-published.png"), fullPage: true });
  Object.assign(report, {
    changesetId: cs.id,
    pageId,
    componentId,
    instanceId: instance.id,
    entity,
    cover,
    inputs: Object.keys(component.inputs).sort(),
    slots: Object.keys(component.slotDefs),
    selfApprovalBlocked: true,
    reviewerApproved: true,
    published: true,
    publicBrowser: true,
    mobileNoOverflow: true,
    locales: ["ru", "en"],
    manifestHash: (
      await (await page.request.get(cfg.appOrigin + "/.well-known/cms-manifest-hash")).text()
    ).trim(),
    changesetSeq: (await read(author, "/changesets/" + cs.id)).seq,
  });
  await writeFile(
    resolve(output, "component-extract-acceptance.json"),
    JSON.stringify(report, null, 2),
  );
  console.log(
    "PASS STU-419: MVP step 3 — wizard, component document, Composed instance, equivalent render, undo/redo, $props Repeat, review and publication.",
  );
} catch (error) {
  const page = browser.contexts().flatMap((context) => context.pages())[0];
  if (page) {
    console.error((await page.locator("body").innerText()).slice(-6000));
    console.error(
      "DATA PANEL: " +
        (
          await page
            .locator("section.content-bindings")
            .last()
            .innerText()
            .catch(() => "")
        ).slice(0, 3000),
    );
    await page.evaluate(() => globalThis.scrollTo(0, 0));
    await page.screenshot({ path: resolve(output, "component-extract-failure-top.png") });
    await page.screenshot({
      path: resolve(output, "component-extract-failure.png"),
      fullPage: true,
    });
  }
  throw error;
} finally {
  await browser.close();
}
