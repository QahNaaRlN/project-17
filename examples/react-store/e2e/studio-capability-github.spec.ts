import { expect, test } from "@playwright/test";
const origin = "http://127.0.0.1:18084";
const id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11";
// This suite models the CMS HTTP boundary. It is not external GitHub acceptance.
test("STU-312: GitHub evidence is distinct, mobile-safe and guards unsaved input", async ({
  page,
  request,
}, info) => {
  await request.post(origin + "/control", { data: { mode: "ready" } });
  await request.post(origin + "/studio-control", { data: { reset: true, identity: true } });
  await page.route("**/api/studio/auth/session", async (route) => {
    const signedIn = (route.request().headers().cookie ?? "").includes(
      "cms_session=" + "a".repeat(43),
    );
    await route.fulfill({
      json: signedIn
        ? {
            authenticated: true,
            user: { id, name: "Alice" },
            rights: ["design.read", "content.read", "capability.request", "code.propose"],
            csrfToken: "c".repeat(43),
            expiresAt: new Date(Date.now() + 3600000).toISOString(),
            hasAccess: true,
          }
        : { authenticated: false, rights: [], hasAccess: false },
    });
  });
  const card = {
    id,
    kind: "action",
    environment: "staging",
    key: "store.favorite",
    title: "Favorite request",
    description: "Explicit action request",
    proposedContract: { args: {} },
    acceptance: ["Works"],
    status: "pr_open",
    revision: 4,
    changesetId: id,
    documentId: id,
    nodeId: "n_root",
    event: "click",
    ownerId: id,
    externalRefs: [],
    transitions: [],
    packs: [],
    audit: [],
  };
  const facts = {
    repositoryId: "42",
    prId: "43",
    number: 61,
    state: "merged",
    draft: false,
    headSha: "a".repeat(40),
    baseSha: "b".repeat(40),
    integrationSha: "c".repeat(40),
    integrationType: "merged_commit",
    ci: "success",
    results: [
      {
        source: "check_run",
        name: "Check (TS + Go)",
        target: "integration",
        exactSha: "c".repeat(40),
        outcome: "success",
      },
    ],
  };
  const observation = {
    id,
    generation: 1,
    configRevision: 1,
    observedAt: new Date().toISOString(),
    freshUntil: new Date(Date.now() + 900000).toISOString(),
    digest: "sha256:" + "a".repeat(64),
    facts,
  };
  const link = {
    id,
    requestId: id,
    revision: 1,
    generation: 1,
    active: true,
    pullNumber: 61,
    configRevision: 1,
    syncId: id,
    syncState: "idle",
    lastError: "",
    nextAllowedAt: new Date().toISOString(),
    nextAttemptAt: new Date().toISOString(),
    pollsLeft: 0,
    attempts: 0,
  };
  const view = {
    link,
    current: observation,
    lastKnown: observation,
    syncState: "idle",
    canManage: true,
    configRevision: 1,
    repository: { owner: "owner", name: "repo", baseBranch: "dev" },
    url: "https://github.com/owner/repo/pull/61",
  };
  const writes: unknown[] = [];
  await page.route("**/api/studio/api/v1/capability-requests**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    let body: unknown = card;
    if (path.endsWith("/github")) body = view;
    else if (path.endsWith("/github-observations"))
      body = { items: [observation], nextCursor: null };
    else if (path.endsWith("/deliveries")) body = { items: [], nextCursor: null };
    else if (path.endsWith("/capability-requests")) body = { items: [card], nextCursor: null };
    await route.fulfill({ json: body });
  });
  await page.route("**/api/studio/api/v1/commands/*capability-github*", async (route) => {
    writes.push(route.request().postDataJSON());
    await route.fulfill({ json: { result: link } });
  });
  await page.setViewportSize({ width: 375, height: 900 });
  await page.goto(origin + "/editor");
  await page.getByRole("link", { name: "Войти", exact: true }).click();
  await page.getByRole("link", { name: "Войти как Alice", exact: true }).click();
  await expect(page.locator(".person-bar")).toContainText("Alice");
  await page.getByRole("button", { name: "Запросы возможностей", exact: true }).click();
  await page
    .getByRole("button", { name: "Favorite request · store.favorite · pr_open", exact: true })
    .click();
  const modal = page.getByRole("dialog", { name: "Запрос возможности", exact: true });
  const github = modal.getByRole("region", { name: "GitHub", exact: true });
  await expect(github.getByText("Слит · CI: Успешно", { exact: true })).toBeVisible();
  await expect(
    github.getByText("Факты GitHub не подтверждают развёртывание или выполнение запроса."),
  ).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await github.getByLabel("Причина", { exact: true }).fill("Check latest commit");
  await page.keyboard.press("Escape");
  await expect(modal).toBeVisible();
  await github.getByRole("button", { name: "Проверить GitHub", exact: true }).click();
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0]).toMatchObject({
    payload: {
      requestId: id,
      expectedRequestRevision: 4,
      expectedLinkRevision: 1,
      expectedConfigRevision: 1,
    },
  });
  await expect(github.getByLabel("Причина", { exact: true })).toHaveValue("");
  await github.getByRole("button", { name: "История наблюдений", exact: true }).click();
  await expect(github.locator("details")).toHaveCount(1);
  await page.screenshot({ path: info.outputPath("github-375.png"), fullPage: true });
  await page.keyboard.press("Escape");
  await expect(modal).toHaveCount(0);
});
