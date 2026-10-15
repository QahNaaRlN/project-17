// STU-189: real loopback CMS/PostgreSQL/OIDC/S3 acceptance, no intercepted API.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { resolve, dirname } from "node:path";
import { randomUUID, createHash } from "node:crypto";
import { createRequire } from "node:module";
import { spawnSync } from "node:child_process";
import { fileURLToPath, URL } from "node:url";
const { fetch } = globalThis;
const require = createRequire(new URL("../examples/react-store/package.json", import.meta.url));
const { chromium, expect } = require("@playwright/test");
if (!process.env.STUDIO_REVIEW_ADMIN_FILE || !process.env.STUDIO_REVIEW_OUTPUT)
  throw Error("Local review config/output required");
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
const stamp = randomUUID().slice(0, 8),
  report = { project: cfg.project, stamp, works: [] };
async function admin(name, payload) {
  const r = await fetch(cfg.baseUrl + "/api/v1/commands/" + name, {
    method: "POST",
    headers: {
      Authorization: "Bearer " + cfg.credential,
      "X-CMS-Project": cfg.project,
      "Content-Type": "application/json",
      "Idempotency-Key": randomUUID(),
    },
    body: JSON.stringify({ payload, reason: "Local work queue acceptance setup" }),
  });
  const d = await r.json();
  if (!r.ok) throw Error(name + " " + r.status + " " + d.code);
  return d.result;
}
try {
  const author = await browser.newContext({ viewport: { width: 1440, height: 1100 } }),
    reviewer = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
  const alice = await author.newPage(),
    bob = await reviewer.newPage();
  async function login(page, name, roleId) {
    await page.goto(cfg.studioOrigin + "/editor");
    await page.getByRole("link", { name: "Войти", exact: true }).click();
    await page.getByRole("link", { name: "Войти как " + name, exact: true }).click();
    await page.waitForURL(cfg.studioOrigin + "/editor**");
    const person = await (
      await page.context().request.get(cfg.studioOrigin + "/api/studio/auth/session")
    ).json();
    if (!person.authenticated || person.user.name !== name) throw Error("OIDC identity required");
    await admin("bind-human-role", { actorId: person.user.id, roleId });
    await page.reload();
    await expect(page.locator(".person-bar")).toContainText(name);
    return person;
  }
  const a = await login(alice, "Alice", cfg.authorRole),
    b = await login(bob, "Bob", cfg.reviewerRole);
  async function command(context, person, name, payload) {
    const r = await context.request.post(cfg.baseUrl + "/api/v1/commands/" + name, {
      headers: {
        "X-CMS-Project": cfg.project,
        Origin: cfg.studioOrigin,
        "X-CSRF-Token": person.csrfToken,
        "Idempotency-Key": randomUUID(),
      },
      data: { payload, reason: "Local documentless work acceptance" },
    });
    const d = await r.json();
    if (!r.ok()) throw Error(name + " " + r.status() + " " + d.code + " " + (d.detail ?? ""));
    return d.result;
  }
  const own = (name, payload) => command(author, a, name, payload);
  async function read(context, path) {
    const r = await context.request.get(cfg.baseUrl + "/api/v1" + path, {
      headers: { "X-CMS-Project": cfg.project },
    });
    if (!r.ok()) throw Error(path + " " + r.status());
    return r.json();
  }
  async function newWork(title, operations) {
    const cs = await own("create-changeset", { title: title + " " + stamp, targets: ["staging"] });
    const result = await own("apply-operations", {
      changesetId: cs.id,
      expectedSeq: 0,
      operations,
    });
    return { cs, id: result.operations[0].target, seq: result.changeset.seq };
  }
  const entity = await newWork("Очередь сущности", [
    {
      type: "entity.create",
      payload: { schema: "Article", environment: "staging", data: { title: { ru: "До правки" } } },
    },
  ]);
  // More than two pages, created after the target: client-side filtering cannot find it.
  for (let i = 0; i < 105; i++)
    await own("create-changeset", {
      title: "Queue filler " + stamp + " " + i,
      targets: ["staging"],
    });
  async function open(page, work) {
    await page.getByRole("button", { name: "Работы", exact: true }).click();
    await page.getByLabel("Поиск работ", { exact: true }).fill(work.cs.title);
    await page.getByLabel("Поиск работ", { exact: true }).press("Enter");
    const row = page.locator(".work-catalogue tbody tr").filter({ hasText: work.cs.title });
    await expect(row).toHaveCount(1);
    await row.getByRole("button", { name: "Открыть работу", exact: true }).click();
    await expect(page.locator("#work-card-title")).toHaveText(work.cs.title);
  }
  await open(alice, entity);
  expect(new URL(alice.url()).hash).toBe("#work=" + entity.cs.id);
  await alice.reload();
  await expect(alice.locator("#work-card-title")).toHaveText(entity.cs.title);
  await alice.getByRole("button", { name: "Открыть контент", exact: true }).click();
  await alice.getByLabel("Content title ru", { exact: true }).fill("Entity saved without page");
  await alice.getByRole("button", { name: "Сохранить контент", exact: true }).click();
  await expect.poll(async () => (await read(author, "/changesets/" + entity.cs.id)).seq).toBe(2);
  const delivery = JSON.parse(
    await readFile(
      resolve(dirname(process.env.STUDIO_REVIEW_ADMIN_FILE), "studio-work-store.private.json"),
      "utf8",
    ),
  );
  async function publish(work, kind) {
    await open(alice, work);
    const authorPanel = alice.getByRole("region", { name: "Проверки и публикация", exact: true });
    await authorPanel.getByRole("button", { name: "Отправить на проверку", exact: true }).click();
    await expect(authorPanel.getByText("На согласовании", { exact: true })).toBeVisible();
    await open(bob, work);
    const panel = bob.getByRole("region", { name: "Проверки и публикация", exact: true });
    await expect(bob.getByRole("region", { name: "Изменения работы", exact: true })).toBeVisible();
    await panel.getByRole("button", { name: "Согласовать", exact: true }).click();
    await expect(panel.getByText("Готов к публикации", { exact: true })).toBeVisible();
    await panel.getByRole("button", { name: "Опубликовать", exact: true }).click();
    await expect(bob.locator(".publication-confirmation")).toContainText(work.cs.title);
    await panel.getByRole("button", { name: "Подтвердить публикацию", exact: true }).click();
    await expect(panel).toContainText("Публикация подтверждена: staging");
    const result = await fetch(
      cfg.baseUrl + "/delivery/v1/" + cfg.project + "/staging/" + kind + "/" + work.id,
      { headers: { Authorization: "Bearer " + delivery.deliveryKey } },
    );
    expect(result.status).toBe(200);
    const content = await result.json();
    report.works.push({
      id: work.cs.id,
      objectId: work.id,
      kind,
      publicationConfirmed: true,
      delivered: true,
    });
    return content;
  }
  const delivered = await publish(entity, "entity");
  expect(delivered.data.title.ru).toBe("Entity saved without page");
  const png = await alice.screenshot({ type: "png" });
  let upload = await own("create-asset-upload", {
    filename: "work-queue.png",
    mimeType: "image/png",
    size: png.length,
    sha256: createHash("sha256").update(png).digest("hex"),
  });
  const put = await fetch(upload.uploadUrl, { method: "PUT", body: png });
  expect(put.status).toBe(200);
  upload = await own("complete-asset-upload", { assetId: upload.assetId });
  await expect
    .poll(
      async () => {
        upload = await read(author, "/asset-uploads/" + upload.assetId);
        return upload.status;
      },
      { timeout: 30000 },
    )
    .toBe("ready");
  const asset = await newWork("Очередь ассета", [
    { type: "asset.create", payload: { uploadId: upload.assetId } },
  ]);
  await open(alice, asset);
  await alice.getByRole("button", { name: "Открыть контент", exact: true }).click();
  await alice.getByLabel("Content title", { exact: true }).fill("Asset saved without page");
  await alice.getByRole("button", { name: "Сохранить контент", exact: true }).click();
  await expect.poll(async () => (await read(author, "/changesets/" + asset.cs.id)).seq).toBe(2);
  const assetContent = await publish(asset, "asset");
  expect(assetContent.data.title).toBe("Asset saved without page");
  expect(assetContent.data.fileHash).toBe(upload.fileHash);
  report.asset = {
    id: asset.id,
    fileHash: upload.fileHash,
    uploadReady: true,
    metadataDelivered: true,
  };
  const component = await newWork("Очередь компонента", [
    {
      type: "document.create",
      payload: { kind: "component", root: { id: "n_root", type: "Box" } },
    },
  ]);
  await open(alice, component);
  await expect(alice.locator(".work-review")).toContainText("Компонент");
  expect(
    await alice
      .locator(".work-review")
      .getByRole("button", { name: /Открыть страницу/ })
      .count(),
  ).toBe(0);
  // A removed, previously committed page must remain discoverable and reviewable.
  const page = await newWork("Очередь удаления", [
    {
      type: "document.create",
      payload: {
        kind: "page",
        path: "/queue-delete-" + stamp,
        root: { id: "n_root", type: "Box" },
      },
    },
  ]);
  await publish(page, "document");
  const deleted = {
    cs: await own("create-changeset", {
      title: "Удаление страницы " + stamp,
      targets: ["staging"],
    }),
    id: page.id,
  };
  const imported = spawnSync(
    "go",
    [
      "run",
      fileURLToPath(new URL("./fixtures/work-queue-deletion.go", import.meta.url)),
      deleted.cs.id,
      page.id,
    ],
    {
      cwd: fileURLToPath(new URL("../apps/server", import.meta.url)),
      env: process.env,
      encoding: "utf8",
      windowsHide: true,
    },
  );
  if (imported.status !== 0)
    throw Error("Local deletion import fixture failed: " + imported.stderr);
  await open(alice, deleted);
  await expect(alice.locator(".work-review")).toContainText("Удалено");
  expect(
    await alice
      .locator(".work-review")
      .getByRole("button", { name: /Открыть страницу/ })
      .count(),
  ).toBe(0);
  await bob.setViewportSize({ width: 375, height: 900 });
  const completed = await reviewer.request.get(
    cfg.baseUrl + "/api/v1/work-queue/" + asset.cs.id + "?environment=staging",
    { headers: { "X-CMS-Project": cfg.project } },
  );
  expect(completed.status()).toBe(404);
  await bob.getByRole("button", { name: "Обновить работы", exact: true }).click();
  expect(
    await bob.evaluate(
      () => globalThis.document.documentElement.scrollWidth <= globalThis.innerWidth,
    ),
  ).toBe(true);
  await bob.screenshot({ path: resolve(output, "work-queue-mobile.png"), fullPage: true });
  await alice.setViewportSize({ width: 375, height: 900 });
  await alice.locator(".work-card-heading").scrollIntoViewIfNeeded();
  expect(
    await alice.evaluate(
      () => globalThis.document.documentElement.scrollWidth <= globalThis.innerWidth,
    ),
  ).toBe(true);
  await alice.screenshot({ path: resolve(output, "work-queue-mobile-card.png") });
  await alice.setViewportSize({ width: 1440, height: 1100 });
  await alice.screenshot({ path: resolve(output, "work-queue-desktop.png"), fullPage: true });
  const contentRole = await admin("create-role", {
    name: "content-only-" + stamp,
    capabilities: ["content.read", "content.write", "asset.write"],
  });
  await admin("unbind-human-role", { actorId: b.user.id, roleId: cfg.reviewerRole });
  await admin("bind-human-role", { actorId: b.user.id, roleId: contentRole.id });
  await bob.goto(cfg.studioOrigin + "/editor");
  await expect(bob.locator(".person-bar")).toContainText("Bob");
  const contentDraft = await command(reviewer, b, "create-changeset", {
    title: "Content only " + stamp,
    targets: ["staging"],
  });
  await command(reviewer, b, "apply-operations", {
    changesetId: contentDraft.id,
    expectedSeq: 0,
    operations: [
      {
        type: "entity.create",
        payload: {
          schema: "Article",
          environment: "staging",
          data: { title: { ru: "Content only" } },
        },
      },
    ],
  });
  await bob.getByLabel("Поиск работ", { exact: true }).fill(contentDraft.title);
  await bob.getByLabel("Поиск работ", { exact: true }).press("Enter");
  await bob.getByRole("button", { name: "Открыть работу", exact: true }).click();
  await expect(bob.locator("#work-card-title")).toHaveText(contentDraft.title);
  await bob.getByRole("button", { name: "Открыть контент", exact: true }).click();
  await bob.getByLabel("Content title ru", { exact: true }).fill("Content-only saved");
  await bob.getByRole("button", { name: "Сохранить контент", exact: true }).click();
  await expect
    .poll(async () => (await read(reviewer, "/changesets/" + contentDraft.id)).seq)
    .toBe(2);
  report.contentOnly = { opened: true, saved: true, design: false };
  await admin("unbind-human-role", { actorId: b.user.id, roleId: contentRole.id });
  await admin("bind-human-role", { actorId: b.user.id, roleId: cfg.reviewerRole });
  report.searchBeyondFirstPage = true;
  report.componentWithoutPage = true;
  report.deletionOnly = true;
  report.deletionWorkId = deleted.cs.id;
  report.componentWorkId = component.cs.id;
  report.mobile375 = true;
  await writeFile(resolve(output, "work-queue-acceptance.json"), JSON.stringify(report, null, 2));
  const verified = spawnSync(
    "go",
    [
      "run",
      fileURLToPath(new URL("./fixtures/work-queue-verify.go", import.meta.url)),
      resolve(output, "work-queue-acceptance.json"),
    ],
    {
      cwd: fileURLToPath(new URL("../apps/server", import.meta.url)),
      env: process.env,
      encoding: "utf8",
      windowsHide: true,
    },
  );
  if (verified.status !== 0) throw Error("Committed/file verification failed: " + verified.stderr);
  console.log(
    "PASS: real server search beyond 100 CS; entity/asset edit-review-publish without pages; ready upload; component/deletion review; content-only save; mobile 375.",
  );
} finally {
  await browser.close();
}
