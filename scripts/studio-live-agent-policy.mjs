// STU-439: isolated acceptance of conditional agent policies. Requires a review
// stand (scripts/studio-review-stand.mjs) and the stand's PostgreSQL container to
// read security events. The agent works through the real MCP endpoint.
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { createHash, randomBytes, randomUUID } from "node:crypto";
import { fileURLToPath, URL, URLSearchParams } from "node:url";
const { fetch } = globalThis;
const repo = fileURLToPath(new URL("..", import.meta.url));
const require = createRequire(new URL("../examples/react-store/package.json", import.meta.url));
const { chromium, expect } = require("@playwright/test");
for (const name of [
  "STUDIO_REVIEW_ADMIN_FILE",
  "STUDIO_REVIEW_OUTPUT",
  "PREVIEW_PG_CONTAINER",
  "PREVIEW_PG_DB",
])
  if (!process.env[name]) throw Error(name + " is required");
const cfg = JSON.parse(await readFile(process.env.STUDIO_REVIEW_ADMIN_FILE, "utf8"));
if (
  cfg.project !== "studio-work-review" ||
  [cfg.baseUrl, cfg.studioOrigin, cfg.appOrigin].some((u) => new URL(u).hostname !== "127.0.0.1")
)
  throw Error("Acceptance is restricted to the isolated loopback stand");
const output = resolve(process.env.STUDIO_REVIEW_OUTPUT);
await mkdir(output, { recursive: true });
const report = {
  requirements: "STU-420..439",
  sourceSha: execFileSync("git", ["rev-parse", "HEAD"], { cwd: repo, encoding: "utf8" }).trim(),
};
const sql = (text) =>
  execFileSync(
    "docker",
    [
      "exec",
      "-i",
      process.env.PREVIEW_PG_CONTAINER,
      "psql",
      "-v",
      "ON_ERROR_STOP=1",
      "-U",
      "postgres",
      "-d",
      process.env.PREVIEW_PG_DB,
      "-At",
      "-c",
      text,
    ],
    { encoding: "utf8", windowsHide: true },
  ).trim();
async function admin(name, payload) {
  const response = await fetch(cfg.baseUrl + "/api/v1/commands/" + name, {
    method: "POST",
    headers: {
      Authorization: "Bearer " + cfg.credential,
      "X-CMS-Project": cfg.project,
      "Content-Type": "application/json",
      "Idempotency-Key": randomUUID(),
    },
    body: JSON.stringify({ payload, reason: "Isolated agent policy acceptance fixture" }),
  });
  const data = await response.json();
  if (!response.ok) throw Error(name + " " + response.status + " " + data.code);
  return data.result;
}
const browser = await chromium.launch(
  process.env.PLAYWRIGHT_EXECUTABLE_PATH
    ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH }
    : {},
);
try {
  const author = await browser.newContext({ viewport: { width: 1440, height: 1100 } }),
    reviewer = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
  const alice = await author.newPage(),
    bob = await reviewer.newPage();
  for (const page of [alice, bob])
    page.on("pageerror", (error) => console.error("PAGE ERROR", error.message, error.stack));
  async function login(page, name, roleId) {
    await page.goto(cfg.studioOrigin + "/editor");
    await page.getByRole("link", { name: "Войти", exact: true }).click();
    await page.getByRole("link", { name: "Войти как " + name, exact: true }).click();
    await page.waitForURL(cfg.studioOrigin + "/editor**");
    const account = await (
      await page.request.get(cfg.studioOrigin + "/api/studio/auth/session")
    ).json();
    await admin("bind-human-role", { actorId: account.user.id, roleId });
    return account;
  }
  const aliceAccount = await login(alice, "Alice", cfg.authorRole);
  const bobAccount = await login(bob, "Bob", cfg.reviewerRole);
  const policyAdmin = await admin("create-role", {
    name: "policy-admin",
    capabilities: ["project.admin"],
  });
  const delegate = await admin("create-role", {
    name: "agent-delegate",
    capabilities: ["agent.delegate"],
  });
  await admin("bind-human-role", { actorId: bobAccount.user.id, roleId: policyAdmin.id });
  await admin("bind-human-role", { actorId: aliceAccount.user.id, roleId: delegate.id });
  await admin("set-approval-policy", { low: 1, medium: 1, high: 1 });
  const agent = await admin("register-agent", {
    name: "copy-assistant",
    description: "Copy editing assistant",
  });
  async function read(context, path) {
    const response = await context.request.get(cfg.baseUrl + "/api/v1" + path, {
      headers: { "X-CMS-Project": cfg.project },
    });
    if (!response.ok()) throw Error(path + " " + response.status());
    return response.json();
  }
  async function human(context, account, name, payload) {
    const response = await context.request.post(cfg.baseUrl + "/api/v1/commands/" + name, {
      headers: {
        "X-CMS-Project": cfg.project,
        Origin: cfg.studioOrigin,
        "X-CSRF-Token": account.csrfToken,
        "Idempotency-Key": randomUUID(),
      },
      data: { payload, reason: "Isolated agent policy acceptance" },
    });
    const data = await response.json();
    if (!response.ok())
      throw Error(name + " " + response.status() + " " + JSON.stringify(data).slice(0, 600));
    return data.result;
  }

  // The stand registers the store manifest; its schema Change Set activates it.
  const manifest = JSON.parse(
    await readFile(new URL("../examples/react-store/.cms/manifest.json", import.meta.url), "utf8"),
  );
  const registration = await admin("register-manifest", { environment: "staging", manifest });
  if (registration.changesetId) {
    const setup = await admin("create-role", {
      name: "schema-setup",
      capabilities: ["schema.apply", "schema.read"],
    });
    await admin("bind-human-role", { actorId: aliceAccount.user.id, roleId: setup.id });
    await admin("set-approval-policy", { low: 0, medium: 0, high: 0 });
    const claimed = await human(author, aliceAccount, "claim-schema-changeset", {
      changesetId: registration.changesetId,
      expectedSeq: 1,
    });
    await human(author, aliceAccount, "submit-changeset", {
      changesetId: registration.changesetId,
      expectedSeq: claimed.seq,
    });
    await human(author, aliceAccount, "publish", {
      changesetId: registration.changesetId,
      environment: "staging",
      expectedSeq: claimed.seq,
    });
    await admin("unbind-human-role", { actorId: aliceAccount.user.id, roleId: setup.id });
  }
  await admin("set-approval-policy", { low: 1, medium: 1, high: 1 });

  // Alice prepares a draft with two pages.
  const cs = await human(author, aliceAccount, "create-changeset", {
    title: "Agent policy",
    targets: ["staging"],
  });
  const page = (path) => ({
    type: "document.create",
    payload: {
      kind: "page",
      path,
      root: {
        id: "n_root",
        type: "Box",
        children: [{ id: "n_head", type: "Heading", props: { level: 1 } }],
      },
    },
  });
  const created = await human(author, aliceAccount, "apply-operations", {
    changesetId: cs.id,
    expectedSeq: 0,
    operations: [page("/checkout"), page("/store/promo")],
  });
  const [checkout, store] = created.operations.map((op) => op.target);
  report.changeset = cs.id;

  // Bob writes the policy with forms and checks it (STU-433/435).
  await bob.reload();
  await bob.getByRole("button", { name: "Политика агентов", exact: true }).click();
  await expect(bob.getByRole("heading", { name: "Политика агентов" })).toBeVisible();
  const rule = (id) =>
    bob.locator("fieldset", {
      has: bob.locator(":scope > legend", { hasText: new RegExp("^(Правило|Риск) " + id + "$") }),
    });
  async function addRule(id, effect, rights, prefix, extra) {
    await bob.getByRole("button", { name: "Добавить правило", exact: true }).click();
    const fresh = bob.locator("fieldset > legend", { hasText: /^Правило rule-\d+$/ }).last();
    const name = (await fresh.textContent()).trim();
    const box = bob.locator("fieldset", {
      has: bob.locator(":scope > legend", { hasText: new RegExp("^" + name + "$") }),
    });
    await box.getByLabel("Идентификатор", { exact: true }).fill(id);
    await rule(id)
      .getByLabel("Правило " + id + " · " + effect, { exact: true })
      .selectOption(rights);
    await rule(id).locator("summary").click();
    await rule(id)
      .getByLabel("Правило " + id + " · Префикс пути", { exact: true })
      .fill(prefix);
    if (extra) await extra(rule(id));
  }
  await addRule("no-checkout", "Запретить", ["design.compose"], "/checkout");
  await addRule(
    "staging-review",
    "Требовать согласования",
    ["design.compose"],
    "/store",
    async (box) =>
      box
        .getByLabel("Правило staging-review · Окружение", { exact: true })
        .selectOption(["staging"]),
  );
  await bob.getByRole("button", { name: "Добавить правило риска", exact: true }).click();
  const risk = bob.locator("fieldset", {
    has: bob.locator(":scope > legend", { hasText: /^Риск risk-1$/ }),
  });
  await risk.getByLabel("Идентификатор", { exact: true }).fill("agent-store-risk");
  await rule("agent-store-risk")
    .getByLabel("Риск agent-store-risk · Не ниже", { exact: true })
    .selectOption("high");
  await rule("agent-store-risk").locator("summary").click();
  await rule("agent-store-risk")
    .getByLabel("Риск agent-store-risk · Префикс пути", { exact: true })
    .fill("/store");
  await rule("agent-store-risk")
    .getByLabel("Риск agent-store-risk · Источник", { exact: true })
    .selectOption(["agent"]);
  const explain = bob.locator("form.agent-policy-explain");
  await explain.getByLabel("Агент", { exact: true }).selectOption("copy-assistant");
  await explain.getByLabel("Право", { exact: true }).selectOption("design.compose");
  await explain.getByLabel("Путь", { exact: true }).fill("/checkout/cart");
  await explain.getByRole("button", { name: "Проверить черновик", exact: true }).click();
  await expect(explain.getByRole("status")).toContainText("правило no-checkout");
  await expect(explain.getByRole("status")).toContainText("pathPrefix=/checkout");
  report.explain = (await explain.getByRole("status").textContent()).trim();
  await bob.getByLabel("Причина изменения", { exact: true }).fill("Запрет checkout для агентов");
  await bob.getByRole("button", { name: "Сохранить новую версию", exact: true }).click();
  await expect(bob.getByText("Версия: 1 · Bob")).toBeVisible();
  await bob.screenshot({ path: resolve(output, "agent-policy-v1.png"), fullPage: true });

  // Alice delegates an external MCP session (STU-425/426/434).
  const secret = "cms_agt_" + randomBytes(32).toString("base64url");
  const tokenHash = createHash("sha256").update(secret).digest("base64");
  await human(author, aliceAccount, "start-agent-session", {
    tokenHash,
    agentId: agent.id,
    changesetIds: [cs.id],
    rights: ["design.read", "design.compose"],
    ttlSeconds: 3600,
    opsQuota: 10,
  });
  let rpc = 0;
  async function tool(name, args) {
    const response = await fetch(cfg.baseUrl + "/mcp", {
      method: "POST",
      headers: {
        Authorization: "Bearer " + secret,
        "X-CMS-Project": cfg.project,
        Accept: "application/json, text/event-stream",
        "Content-Type": "application/json",
        "MCP-Protocol-Version": "2025-06-18",
      },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: ++rpc,
        method: "tools/call",
        params: { name, arguments: args },
      }),
    });
    if (!response.ok) throw Error("mcp " + response.status);
    return (await response.json()).result;
  }
  const seq = async () => (await read(author, "/changesets/" + cs.id)).seq;
  const edit = async (documentId) =>
    tool("update_props", {
      changesetId: cs.id,
      documentId,
      expectedSeq: await seq(),
      reason: "Agent heading level",
      payload: { nodeId: "n_head", set: { level: 2 } },
    });
  const refused = await edit(checkout);
  expect(refused.isError).toBe(true);
  expect(refused.structuredContent.code).toBe("POLICY_DENIED");
  expect(refused.structuredContent.diagnostics).toMatchObject({
    ruleId: "no-checkout",
    condition: "pathPrefix=/checkout",
    reason: "denied_by_rule",
  });
  report.refusal = refused.structuredContent;
  const event = JSON.parse(
    sql(
      `SELECT data FROM security_events WHERE actor_id='${agent.id}' ORDER BY created_at DESC LIMIT 1`,
    ),
  );
  expect(event.params.ruleId).toBe("no-checkout");
  expect(sql(`SELECT ops_used FROM agent_sessions WHERE agent_id='${agent.id}'`)).toBe("0");
  const allowed = await edit(store);
  expect(allowed.isError).toBe(false);
  expect(sql(`SELECT ops_used FROM agent_sessions WHERE agent_id='${agent.id}'`)).toBe("1");

  // Submission requires a human approval and a raised risk (STU-427/428).
  const review = await human(author, aliceAccount, "submit-changeset", {
    changesetId: cs.id,
    expectedSeq: await seq(),
  });
  expect(review.changeset.state).toBe("in_review");
  expect(review.changeset.policyApproval).toEqual(["staging-review"]);
  expect(review.risk).toBe("high");
  expect(review.requiredApprovals).toBeGreaterThanOrEqual(1);
  report.review = {
    state: review.changeset.state,
    risk: review.risk,
    policyApproval: review.changeset.policyApproval,
    requiredApprovals: review.requiredApprovals,
  };
  await alice.goto(
    cfg.studioOrigin + "/editor#" + new URLSearchParams({ document: store, changeset: cs.id }),
  );
  await alice.reload();
  await expect(
    alice.getByText("Требуется согласование человеком по политике агентов: staging-review"),
  ).toBeVisible();
  await alice.screenshot({ path: resolve(output, "agent-policy-review.png"), fullPage: true });

  // A refusing version makes the Change Set failed (STU-430).
  await addRule("no-store", "Запретить", ["design.compose"], "/store");
  await bob.getByLabel("Причина изменения", { exact: true }).fill("Заморозка витрины");
  await bob.getByRole("button", { name: "Сохранить новую версию", exact: true }).click();
  await expect(bob.getByText("Версия: 2 · Bob")).toBeVisible();
  await expect.poll(async () => (await read(author, "/changesets/" + cs.id)).state).toBe("failed");
  const history = bob.locator('section[aria-labelledby="agent-policy-history"]');
  await expect(history).toContainText("Версия 2");
  await expect(history).toContainText("+ rules/no-store");
  await expect(history).toContainText("Заморозка витрины");
  await expect(history).toContainText("Версия 1");
  await alice.reload();
  await expect(alice.getByText("node.setProps · design.compose · no-store")).toBeVisible();
  report.failed = true;

  // English and 375 px.
  await bob.locator(".studio-header-controls select").selectOption("en");
  await expect(bob.getByRole("heading", { name: "Agent policy" })).toBeVisible();
  await bob.setViewportSize({ width: 375, height: 900 });
  await expect
    .poll(() =>
      bob.evaluate(() => globalThis.document.documentElement.scrollWidth <= globalThis.innerWidth),
    )
    .toBe(true);
  await bob.screenshot({ path: resolve(output, "agent-policy-mobile.png"), fullPage: true });
  report.result = "PASS";
} finally {
  await writeFile(resolve(output, "agent-policy-report.json"), JSON.stringify(report, null, 2));
  await browser.close();
}
console.log(JSON.stringify(report, null, 2));
