/* eslint-disable @typescript-eslint/no-explicit-any -- untyped JSON of the public API, checked by assertions */
// STU-440: the MVP acceptance scenario §2 (docs/spec/14-mvp-plan.md) as one
// story against the real stand: ten steps in order, one project, one database,
// one Change Set for steps 2–7. Public interfaces only: Studio, the command and
// Delivery APIs, MCP and the CLI.
import { execFileSync } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import { mkdirSync, readdirSync, readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { test, expect, type Browser, type FrameLocator } from "@playwright/test";
import { startStand, repo, type Stand } from "../../../../scripts/e2e-stand.mjs";
import { catalog } from "../../scripts/catalog.js";
import {
  admin,
  adminRead,
  command,
  mcpAgent,
  read,
  signIn,
  until,
  Metrics,
  type Person,
} from "./support.js";

const directory = resolve(
  process.env.E2E_DIR ?? join(repo, "examples/react-store/test-results/scenario-stand"),
);
const evidence = join(directory, "evidence");
mkdirSync(evidence, { recursive: true });
const metrics = new Metrics();

let stand: Stand, alice: Person, bob: Person, erin: Person;
const state: Record<string, any> = {};

test.beforeAll(async ({ browser }: { browser: Browser }) => {
  test.setTimeout(5 * 60_000);
  stand = await startStand({ directory });
  await Promise.all([stand.startStore("staging"), stand.startStore("production")]);
  alice = await signIn(stand, browser, "alice", [stand.roles.designer]);
  bob = await signIn(stand, browser, "bob", [stand.roles.lead, stand.roles.admin]);
  erin = await signIn(stand, browser, "erin", [stand.roles.editor]);
  for (const person of [alice, bob, erin])
    // Screenshots stream frames like video: the load bench keeps only DOM snapshots.
    await person.context.tracing.start({
      screenshots: process.env.E2E_VIDEO !== "off",
      snapshots: true,
    });
});

test.afterAll(async () => {
  for (const person of [alice, bob, erin].filter(Boolean))
    await person.context.tracing.stop({ path: join(directory, `trace-${person.name}.zip`) });
  metrics.write(directory);
  stand?.stop();
});

const snap = (person: Person, step: number) =>
  person.page.screenshot({
    path: join(evidence, `step-${String(step).padStart(2, "0")}-${person.name}.png`),
    fullPage: true,
  });

const cli = (...args: string[]) =>
  execFileSync(process.execPath, [join(repo, "packages/component-registry/bin/cms.mjs"), ...args], {
    cwd: repo,
    encoding: "utf8",
    windowsHide: true,
  });

const push = (environment: string, deployed?: string) =>
  cli(
    "manifest",
    "push",
    "--dir",
    join(repo, "examples/react-store/.cms"),
    "--base-url",
    stand.apiOrigin,
    "--project",
    stand.project,
    "--env",
    environment,
    "--token-file",
    stand.tokenFile,
    ...(deployed ? ["--wait-for-deploy", deployed] : []),
  );

/** The schema Change Set of a registration goes through review like any other. */
async function publishSchemas(environment: string) {
  const open = (await adminRead(stand, "/changesets")).items.filter(
    (c: any) => c.kind === "schema" && !["merged", "abandoned"].includes(c.state),
  );
  for (const cs of open) {
    const claimed = await command(stand, alice, "claim-schema-changeset", {
      changesetId: cs.id,
      expectedSeq: cs.seq,
    });
    await command(stand, alice, "submit-changeset", {
      changesetId: cs.id,
      expectedSeq: claimed.seq,
    });
    await command(stand, bob, "approve-changeset", {
      changesetId: cs.id,
      expectedSeq: claimed.seq,
    });
    await command(stand, alice, "publish", {
      changesetId: cs.id,
      environment,
      expectedSeq: claimed.seq,
    });
  }
}

// The stand has no object storage: the catalogue of the page carries no assets.
const { heroAsset: _hero, ...catalogWithoutHero } = catalog;
const storeCatalog = {
  ...catalogWithoutHero,
  products: catalog.products.slice(0, 3).map(({ asset: _asset, ...product }) => product),
};

const purges = async (): Promise<{ at: number; keys: string[] }[]> =>
  (
    await fetch(stand.purge.origin + "/purges", {
      headers: { Authorization: "Bearer " + stand.purge.token },
    })
  ).json();
const productionPurges = (list: { keys: string[] }[]) =>
  list.flatMap((p) => p.keys).filter((k) => k.includes(":production:")).length;

const buildStore = () =>
  execFileSync("pnpm", ["--filter", "@cms/store-example", "build"], {
    cwd: repo,
    stdio: "pipe",
    shell: process.platform === "win32",
  });

const preview = (person: Person): FrameLocator => person.page.frameLocator("iframe[title]").first();

test("§2: MVP acceptance scenario", async () => {
  await test.step("1. CI pushes the application manifest", async () => {
    for (const environment of ["staging", "production"]) {
      push(environment);
      await publishSchemas(environment);
      const active = await adminRead(stand, "/manifest?environment=" + environment);
      expect(Object.keys(active.manifest.components)).toEqual(
        expect.arrayContaining(["ProductCard", "Storefront"]),
      );
      expect(active.manifest.schemas.Collection).toBeDefined();
      expect(active.manifest.tokens.spacing.md).toBe("16px");
    }
    await alice.page.reload({ waitUntil: "domcontentloaded" });
    await snap(alice, 1);
  });

  await test.step("2. Designer builds ProductShowcase on tokens", async () => {
    const cs = await command(stand, alice, "create-changeset", {
      title: "ProductShowcase",
      targets: ["staging"],
    });
    state.cs = cs.id;
    const seq = async () => (await read(stand, alice, "/changesets/" + cs.id)).seq;
    const page = await command(stand, alice, "apply-operations", {
      changesetId: cs.id,
      expectedSeq: 0,
      operations: [
        {
          type: "document.create",
          payload: {
            kind: "page",
            path: "/store",
            root: {
              id: "n_root",
              type: "Storefront",
              props: { config: { base: storeCatalog } },
              children: [
                {
                  id: "n_grid",
                  type: "Grid",
                  design: { columns: { base: ["1fr"], md: ["3fr", "2fr"] }, gap: "md" },
                  children: [
                    {
                      id: "n_stack",
                      type: "Stack",
                      design: { gap: "sm", padding: "md" },
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
                    { id: "n_image", type: "Image", props: { decorative: true } },
                  ],
                },
              ],
            },
          },
        },
      ],
    });
    state.page = page.operations[0].target;
    const texts = {
      t_title: { ru: "Витрина недели", en: "Showcase of the week" },
      t_desc: { ru: "Подборка товаров", en: "Selected products" },
      t_cta: { ru: "В каталог", en: "To catalog" },
    };
    state.texts = texts;
    await command(stand, alice, "apply-operations", {
      changesetId: cs.id,
      expectedSeq: await seq(),
      operations: Object.entries(texts).flatMap(([key, value]) =>
        Object.entries(value).map(([locale, text]) => ({
          type: "localContent.set",
          target: state.page,
          payload: { key, type: "text", locale, value: text },
        })),
      ),
    });
    // FR-010/DS-020: design values are tokens, never raw literals.
    const body = (await read(stand, alice, `/documents/${state.page}?changesetId=${cs.id}`)).body;
    expect(JSON.stringify(Object.values(body.nodes).map((n: any) => n.design ?? {}))).not.toContain(
      '"raw"',
    );
    await alice.page.goto(stand.studioOrigin + `/editor#document=${state.page}&changeset=${cs.id}`);
    await alice.page.reload({ waitUntil: "domcontentloaded" });
    const frame = preview(alice);
    await expect(frame.getByRole("heading", { name: texts.t_title.ru, exact: true })).toBeVisible();
    const columns = () =>
      frame
        .locator("[data-cms-node='n_grid']")
        .first()
        .evaluate((el) => getComputedStyle(el).gridTemplateColumns.split(" ").map(parseFloat));
    await alice.page.getByLabel("Ширина предпросмотра", { exact: true }).selectOption("375");
    await expect.poll(async () => (await columns()).length).toBe(1);
    await snap(alice, 2);
    await alice.page.getByLabel("Ширина предпросмотра", { exact: true }).selectOption("1024");
    await expect.poll(async () => (await columns()).length).toBe(2);
    const [wide, narrow] = await columns();
    expect(wide! / narrow!).toBeCloseTo(1.5, 1);
  });

  await test.step("3. Designer saves the block as a component", async () => {
    const page = alice.page;
    await page.locator('[role="treeitem"][title="n_grid"]').click();
    await page.getByRole("button", { name: "Сохранить как компонент", exact: true }).click();
    const wizard = page.getByRole("dialog", { name: "Сохранить как компонент", exact: true });
    await wizard.getByLabel("Имя компонента", { exact: true }).fill("ProductShowcase");
    await wizard.getByRole("button", { name: "Далее", exact: true }).click();
    await wizard.getByLabel("Сделать входом Button.label", { exact: true }).uncheck();
    await wizard.getByLabel("Имя входа Heading.text", { exact: true }).fill("title");
    await wizard.getByLabel("Имя входа Text.text", { exact: true }).fill("description");
    await wizard.getByLabel("Имя нового входа", { exact: true }).fill("products");
    const types = await wizard
      .getByLabel("Тип нового входа", { exact: true })
      .locator("option")
      .evaluateAll((options) =>
        options.map((o) => [(o as HTMLOptionElement).value, o.textContent ?? ""] as const),
      );
    // Source contracts follow manifest order: the first list is store.collectionProducts items.
    await wizard
      .getByLabel("Тип нового входа", { exact: true })
      .selectOption(
        types.find(([v, t]) => v.startsWith("source:") && t.trim().startsWith("list"))![0],
      );
    await wizard.getByRole("button", { name: "Добавить вход", exact: true }).click();
    await wizard.getByRole("button", { name: "Далее", exact: true }).click();
    await wizard.getByLabel("Имя слота", { exact: true }).fill("footer");
    const parents = await wizard
      .getByLabel("Родитель", { exact: true })
      .locator("option")
      .evaluateAll((options) =>
        options.map((o) => [(o as HTMLOptionElement).value, o.textContent ?? ""] as const),
      );
    await wizard
      .getByLabel("Родитель", { exact: true })
      .selectOption(parents.find(([, t]) => t === "Stack")![0]!);
    await wizard.getByLabel("Первый узел", { exact: true }).selectOption("2");
    await wizard.getByLabel("Последний узел", { exact: true }).selectOption("2");
    await wizard.getByRole("button", { name: "Добавить слот", exact: true }).click();
    await wizard.getByRole("button", { name: "Далее", exact: true }).click();
    await wizard.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(wizard).not.toBeVisible();
    // FR-011/IR-060…063: a component document and a Composed instance on the page.
    const body = (await read(stand, alice, `/documents/${state.page}?changesetId=${state.cs}`))
      .body;
    const instance = Object.values(body.nodes).find((n: any) => n.type === "Composed") as any;
    expect(instance).toBeDefined();
    expect(instance.slots).toEqual({ footer: ["n_cta"] });
    state.instance = instance.id;
    state.component = instance.ref.component;
    const component = (
      await read(stand, alice, `/documents/${state.component}?changesetId=${state.cs}`)
    ).body;
    expect(component.kind).toBe("component");
    expect(Object.keys(component.inputs).sort()).toEqual(["description", "products", "title"]);
    expect(Object.keys(component.slotDefs)).toEqual(["footer"]);
    await expect(preview(alice).locator("[data-cms-instance]").first()).toBeVisible();
    await snap(alice, 3);
  });

  await test.step("4. Editor creates a Collection; the block repeats its products", async () => {
    // The editor publishes the entity through her own Change Set.
    const content = await command(stand, erin, "create-changeset", {
      title: "Коллекция недели",
      targets: ["staging"],
    });
    const created = await command(stand, erin, "apply-operations", {
      changesetId: content.id,
      expectedSeq: 0,
      operations: [
        {
          type: "entity.create",
          payload: {
            schema: "Collection",
            environment: "staging",
            data: {
              title: { ru: "Коллекция недели", en: "Collection of the week" },
              handle: "all",
            },
          },
        },
      ],
    });
    state.entity = created.operations[0].target;
    state.entityChangeset = content.id;
    await command(stand, erin, "submit-changeset", { changesetId: content.id, expectedSeq: 1 });
    await command(stand, bob, "approve-changeset", { changesetId: content.id, expectedSeq: 1 });
    const published = await command(stand, erin, "publish", {
      changesetId: content.id,
      environment: "staging",
      expectedSeq: 1,
    });
    state.entityPublication = published.publication?.id ?? published.publicationId ?? published.id;
    // The designer binds the page to the entity and repeats its products in the component.
    const seq = async () => (await read(stand, alice, "/changesets/" + state.cs)).seq;
    await command(stand, alice, "apply-operations", {
      changesetId: state.cs,
      expectedSeq: await seq(),
      operations: [
        {
          type: "document.setContent",
          target: state.page,
          payload: {
            content: { schema: "Collection", resolve: { by: "entity", entityId: state.entity } },
          },
        },
        {
          type: "document.setDataSource",
          target: state.page,
          payload: {
            key: "featured",
            use: {
              source: "store.collectionProducts",
              params: { collection: "$content.handle", limit: { lit: 3 } },
            },
          },
        },
        {
          type: "node.setBinding",
          target: state.page,
          payload: { nodeId: state.instance, prop: "title", binding: "$content.title" },
        },
        {
          type: "node.setBinding",
          target: state.page,
          payload: { nodeId: state.instance, prop: "products", binding: "$data.featured.items" },
        },
        {
          type: "node.insert",
          target: state.component,
          payload: {
            parentId: "n_stack",
            index: 2,
            subtree: {
              id: "n_cards",
              type: "Repeat",
              props: { key: "id", limit: 3 },
              bindings: { items: "$props.products" },
              children: [
                {
                  id: "n_card",
                  type: "ProductCard",
                  props: { variant: "default" },
                  bindings: { product: "$item" },
                },
              ],
            },
          },
        },
      ],
    });
    const frame = preview(alice);
    await alice.page.reload({ waitUntil: "domcontentloaded" });
    await expect(
      frame.getByRole("heading", { name: "Коллекция недели", exact: true }),
    ).toBeVisible();
    await expect.poll(() => frame.locator("[data-cms-node='n_card']").count()).toBe(3);
    // IR-044: an entity field edit is visible in the preview; measured, fails only at 2x (§6).
    for (let i = 1; i <= 10; i++) {
      const title = "Коллекция " + i;
      await command(stand, alice, "apply-operations", {
        changesetId: state.cs,
        expectedSeq: await seq(),
        operations: [
          {
            type: "entity.setFields",
            target: state.entity,
            payload: { set: { title: { ru: title, en: "Collection " + i } } },
          },
        ],
      });
      const started = performance.now();
      await expect(frame.getByRole("heading", { name: title, exact: true })).toBeVisible({
        timeout: 5000,
      });
      metrics.add("ir044PreviewMs", performance.now() - started);
    }
    expect(
      metrics.p95("ir044PreviewMs"),
      JSON.stringify(metrics.samples.ir044PreviewMs),
    ).toBeLessThanOrEqual(2000);
    state.title = "Коллекция 10";
    await snap(alice, 4);
  });

  await test.step("5. An agent edits the block through MCP", async () => {
    const agent = await admin(stand, "register-agent", {
      name: "showcase-agent",
      description: "§2 scripted agent",
    });
    const secret = "cms_agt_" + randomBytes(32).toString("base64url");
    await command(stand, alice, "start-agent-session", {
      tokenHash: createHash("sha256").update(secret).digest("base64"),
      agentId: agent.id,
      changesetIds: [state.cs],
      rights: ["design.read", "design.compose"],
      ttlSeconds: 3600,
      opsQuota: 10,
    });
    state.agentToken = secret;
    state.agentId = agent.id;
    const call = mcpAgent(stand, secret);
    const structure = await call("get_page_structure", {
      changesetId: state.cs,
      documentId: state.component,
    });
    expect(structure.result?.isError).toBe(false);
    expect(JSON.stringify(structure.result?.structuredContent)).toContain("n_card");
    const before = (await read(stand, alice, "/changesets/" + state.cs)).seq;
    for (const [tool, payload] of [
      ["update_design", { nodeId: "n_grid", set: { gap: { base: "md", lg: "lg" } } }],
      ["update_props", { nodeId: "n_card", set: { variant: "compact" } }],
    ] as const) {
      const seq = (await read(stand, alice, "/changesets/" + state.cs)).seq;
      const result = await call(tool, {
        changesetId: state.cs,
        documentId: state.component,
        expectedSeq: seq,
        reason: "§2 step 5",
        payload,
      });
      expect(result.result?.isError, JSON.stringify(result)).toBe(false);
    }
    // FR-050/051, AGT-022: the agent's operations, on behalf of the designer, in the same Change Set.
    const ops = (await read(stand, alice, `/changesets/${state.cs}/operations?afterSeq=${before}`))
      .items;
    expect(ops.map((o: any) => [o.type, o.source, o.actorId, o.onBehalfOf])).toEqual([
      ["node.setDesign", "agent", agent.id, alice.id],
      ["node.setProps", "agent", agent.id, alice.id],
    ]);
    const index = await read(stand, alice, `/changesets/${state.cs}/diff?environment=staging`);
    const diff = await read(
      stand,
      alice,
      `/changesets/${state.cs}/diff/${state.component}?environment=staging&baseline=base&snapshot=${encodeURIComponent(index.snapshot)}&limit=100`,
    );
    const changes = JSON.stringify(diff.items);
    expect(changes).toContain('"lg":"lg"');
    expect(changes).toContain('"compact"');
    await alice.page.reload({ waitUntil: "domcontentloaded" });
    await expect.poll(() => preview(alice).locator(".product-card-compact").count()).toBe(3);
    await snap(alice, 5);
  });

  await test.step("6. Studio preview: canvas selection and document.patch", async () => {
    const page = alice.page;
    const frame = preview(alice);
    // FR-040/041: a click in the iframe selects the node in the tree.
    await frame.getByRole("heading", { name: state.title, exact: true }).click();
    await expect(page.locator('[role="treeitem"][aria-selected="true"]')).toHaveAttribute(
      "title",
      state.instance,
    );
    // NFR-005: a design change reaches the preview; measured, fails only at 2x (§6).
    // Under the load of `pnpm load` the strict 400 ms applies (load tests, decision §11.5).
    await page.locator('[role="treeitem"][title="n_root"]').click();
    const target = frame.locator("[data-cms-node='n_root']").first();
    const padding = () => target.evaluate((el) => getComputedStyle(el).paddingTop);
    const tokens = ["sm", "md", "lg", "xl"];
    for (let i = 0; i < 20; i++) {
      const token = tokens[i % tokens.length]!;
      const previous = await padding();
      await page.getByLabel("Внутренний отступ", { exact: true }).fill(token);
      const started = performance.now();
      await page.getByRole("button", { name: "Сохранить Внутренний отступ", exact: true }).click();
      await expect.poll(padding, { timeout: 5000, intervals: [10] }).not.toBe(previous);
      metrics.add("nfr005PatchMs", performance.now() - started);
    }
    expect(
      metrics.p95("nfr005PatchMs"),
      JSON.stringify(metrics.samples.nfr005PatchMs),
    ).toBeLessThanOrEqual(Number(process.env.E2E_NFR005_MAX_MS ?? 800));
    await snap(alice, 6);
  });

  await test.step("7. Review: the author and the agent cannot approve; the lead does", async () => {
    await alice.page.goto(stand.studioOrigin + "/editor#work=" + state.cs);
    await alice.page.reload({ waitUntil: "domcontentloaded" });
    const panel = alice.page.getByRole("region", { name: "Проверки и публикация", exact: true });
    await panel.getByRole("button", { name: "Отправить на проверку", exact: true }).click();
    // STU-496: stages 8–9 run in the check runner before the review opens.
    await expect
      .poll(async () => (await read(stand, alice, "/changesets/" + state.cs)).state, {
        timeout: 90_000,
      })
      .not.toMatch(/^(open|checking)$/);
    const submitted = await read(stand, alice, `/changesets/${state.cs}/review`);
    expect(
      submitted.changeset.state,
      JSON.stringify(submitted.checks.filter((c: any) => c.status !== "passed")),
    ).toBe("in_review");
    await expect(panel.getByText("На согласовании", { exact: true })).toBeVisible();
    await expect(panel.getByRole("button", { name: "Согласовать", exact: true })).toBeDisabled();
    const seq = (await read(stand, alice, "/changesets/" + state.cs)).seq;
    // PUB-002/003: neither the author nor the agent approves.
    await expect(
      command(stand, alice, "approve-changeset", { changesetId: state.cs, expectedSeq: seq }),
    ).rejects.toMatchObject({ status: 403, code: "APPROVAL_SELF" });
    const agentApproval = await fetch(stand.apiOrigin + "/api/v1/commands/approve-changeset", {
      method: "POST",
      headers: {
        Authorization: "Bearer " + state.agentToken,
        "X-CMS-Project": stand.project,
        "Content-Type": "application/json",
        "Idempotency-Key": randomBytes(8).toString("hex"),
      },
      body: JSON.stringify({
        payload: { changesetId: state.cs, expectedSeq: seq },
        reason: "agent approval",
      }),
    });
    expect(agentApproval.status).toBe(403);
    expect((await agentApproval.json()).code).toBe("POLICY_DENIED");
    const tool = await mcpAgent(stand, state.agentToken)("approve_changeset", {
      changesetId: state.cs,
    });
    expect(tool.error).toBeDefined();
    await bob.page.goto(stand.studioOrigin + "/editor#work=" + state.cs);
    await bob.page.reload({ waitUntil: "domcontentloaded" });
    const lead = bob.page.getByRole("region", { name: "Проверки и публикация", exact: true });
    await lead.getByRole("button", { name: "Согласовать", exact: true }).click();
    await expect(lead.getByText("Готов к публикации", { exact: true })).toBeVisible();
    // STU-571, 587: stage 10 does not hold the review; it finishes in the background.
    await expect
      .poll(
        async () =>
          (await read(stand, bob, `/changesets/${state.cs}/review`)).checks.find(
            (c: any) => c.stage === "visual",
          )?.status,
        { timeout: 120_000 },
      )
      .not.toMatch(/^(queued|running)$/);
    // Pipeline stages 1–10 and 13 pass; axe findings below serious only warn (STU-491);
    // Lighthouse is for high risk only (STU-579).
    const review = await read(stand, bob, `/changesets/${state.cs}/review`);
    for (const stage of ["ir", "policy", "preview", "axe", "visual", "lighthouse"])
      expect(review.checks.map((c: any) => c.stage)).toContain(stage);
    for (const check of review.checks)
      expect(check.status, check.stage + " " + JSON.stringify(check.details)).toMatch(
        check.stage === "axe"
          ? /^(passed|warning)$/
          : check.stage === "lighthouse"
            ? /^skipped$/
            : /^passed$/,
      );
    expect(
      review.checks.filter((c: any) => c.stage === "preview").map((c: any) => c.environment),
    ).toEqual(["staging"]);
    await snap(bob, 7);
  });

  await test.step("8. Publication to staging and promotion to production", async () => {
    await alice.page.reload({ waitUntil: "domcontentloaded" });
    const panel = alice.page.getByRole("region", { name: "Проверки и публикация", exact: true });
    await panel.getByRole("button", { name: "Опубликовать", exact: true }).click();
    await panel.getByRole("button", { name: "Подтвердить публикацию", exact: true }).click();
    await expect(panel).toContainText("Публикация подтверждена: staging");
    const staging = await (await fetch(stand.stores.staging.origin + "/store")).text();
    expect(staging).toContain(state.title);
    const publications = (await read(stand, bob, "/publications?environment=staging")).items;
    const ofShowcase = publications.find((p: any) => p.changesetId === state.cs);
    const ofEntity = publications.find((p: any) => p.changesetId === state.entityChangeset);
    expect(ofEntity && ofShowcase, JSON.stringify(publications)).toBeTruthy();
    const before = (await purges()).length;
    // STU-477: Bob promotes both staging publications in Studio; the page needs the entity first.
    for (const publication of [ofEntity, ofShowcase]) {
      await bob.page.goto(
        stand.studioOrigin + "/editor#publication=" + publication.id + "&environment=staging",
      );
      await bob.page.reload({ waitUntil: "domcontentloaded" });
      const history = bob.page.getByRole("region", { name: "История публикаций", exact: true });
      await history.getByRole("button", { name: "Перенести в production…", exact: true }).click();
      await expect(history.getByText(/Будет перенесено: [1-9]/)).toBeVisible();
      await history.getByRole("button", { name: "Перенести в production", exact: true }).click();
      const dialog = bob.page.getByRole("dialog");
      await dialog.locator("textarea").fill("Релиз витрины §2");
      await dialog.getByRole("button", { name: "Подтвердить перенос", exact: true }).click();
      await expect(history.getByText(/Перенос выполнен: production/)).toBeVisible();
    }
    // FR-031/PUB-020: visible in production within 10 s (strict gate, §6).
    const visible = await until(async () => {
      const html = await (await fetch(stand.stores.production.origin + "/store")).text();
      return html.includes(state.title) && html.includes("product-card-compact");
    }, 10_000);
    metrics.add("productionPublishMs", visible);
    expect(visible).toBeLessThanOrEqual(10_000);
    // PUB-021: Surrogate-Key purge of production objects reached the CDN receiver.
    await expect
      .poll(async () => productionPurges((await purges()).slice(before)))
      .toBeGreaterThan(0);
    state.promotion = (await read(stand, bob, "/publications?environment=production")).items[0];
    state.pageHead = (await read(stand, bob, `/documents/${state.page}`)).versionId;
    await snap(alice, 8);
  });

  await test.step("9. Production rollback", async () => {
    const before = (await purges()).length;
    // STU-477: Bob rolls back the production promotion in Studio.
    await bob.page.goto(
      stand.studioOrigin + "/editor#publication=" + state.promotion.id + "&environment=production",
    );
    await bob.page.reload({ waitUntil: "domcontentloaded" });
    const history = bob.page.getByRole("region", { name: "История публикаций", exact: true });
    await history
      .getByRole("button", { name: "Посмотреть последствия отката", exact: true })
      .click();
    await history.getByRole("button", { name: "Отменить публикацию…", exact: true }).click();
    const dialog = bob.page.getByRole("dialog");
    await dialog.locator("textarea").fill("Откат релиза §2");
    await dialog.getByRole("button", { name: "Подтвердить откат", exact: true }).click();
    await expect(history.getByText("Публикация отменена. Черновики сохранены.")).toBeVisible();
    // FR-032/PUB-030: the previous version is visible within 10 s; no new versions.
    const back = await until(async () => {
      const html = await (await fetch(stand.stores.production.origin + "/store")).text();
      return !html.includes("product-card-compact");
    }, 10_000);
    metrics.add("productionRollbackMs", back);
    expect(back).toBeLessThanOrEqual(10_000);
    expect((await read(stand, bob, `/documents/${state.page}`)).versionId).toBe(state.pageHead);
    await expect
      .poll(async () => productionPurges((await purges()).slice(before)))
      .toBeGreaterThan(0);
  });

  await test.step("10. Missing Capability: quick view through a pull request", async () => {
    const cs = await command(stand, alice, "create-changeset", {
      title: "Быстрый просмотр",
      targets: ["staging"],
    });
    const seq = async () => (await read(stand, alice, "/changesets/" + cs.id)).seq;
    await command(stand, alice, "apply-operations", {
      changesetId: cs.id,
      expectedSeq: 0,
      operations: [
        {
          type: "node.insert",
          target: state.page,
          payload: {
            parentId: "n_root",
            index: 1,
            subtree: {
              id: "n_quick",
              type: "Button",
              zone: { id: "z_quick", mode: "CODE" },
              bindings: { label: "$local.t_quick" },
            },
          },
        },
        {
          type: "localContent.set",
          target: state.page,
          payload: {
            key: "t_quick",
            type: "text",
            value: { ru: "Быстрый просмотр", en: "Quick view" },
          },
        },
      ],
    });
    const page = alice.page;
    await page.goto(stand.studioOrigin + `/editor#document=${state.page}&changeset=${cs.id}`);
    await page.reload({ waitUntil: "domcontentloaded" });
    await page.locator('[role="treeitem"][title="n_quick"]').click();
    await page.getByRole("button", { name: "Запросить действие для click", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("Ключ действия", { exact: true }).fill("store.quickView");
    await dialog.getByLabel("Заголовок", { exact: true }).fill("Quick view");
    await dialog
      .getByLabel("Описание задачи", { exact: true })
      .fill("Open a product preview without leaving the page.");
    await dialog
      .getByLabel("Критерии приёмки, по одному в строке", { exact: true })
      .fill("Click opens the quick view");
    await dialog.getByRole("button", { name: "Добавить аргумент", exact: true }).click();
    await dialog.getByLabel("Имя аргумента", { exact: true }).last().fill("id");
    await dialog.getByLabel("Обязательный", { exact: true }).last().check();
    await dialog.getByLabel("Максимум", { exact: true }).last().fill("64");
    await dialog.getByRole("button", { name: "Сохранить черновик запроса", exact: true }).click();
    await dialog
      .getByRole("button", { name: "Проверить передаваемый контекст", exact: true })
      .click();
    await dialog
      .getByLabel("Передать этот context pack участникам проекта с code.propose", { exact: true })
      .check();
    await dialog.getByLabel("Подтверждаю замену цепочки", { exact: true }).check();
    await dialog.getByRole("button", { name: "Отправить запрос", exact: true }).click();
    await expect(
      dialog.getByRole("button", { name: "Скачать context pack", exact: true }),
    ).toBeVisible();
    await dialog.getByRole("button", { name: "Закрыть", exact: true }).click();
    const request = (
      await read(stand, alice, "/capability-requests?environment=staging&key=store.quickView")
    ).items[0];
    expect(request.status).toBe("submitted");
    // FR-052: publication with the placeholder is blocked.
    let review = await command(stand, alice, "submit-changeset", {
      changesetId: cs.id,
      expectedSeq: await seq(),
    });
    expect(review.changeset.state).toBe("failed");
    expect(JSON.stringify(review.checks)).toContain("ACTION_PENDING_CAPABILITY");
    await command(stand, alice, "reopen-changeset", {
      changesetId: cs.id,
      expectedSeq: await seq(),
    });
    // Engineering: the request goes to a pull request; GitHub facts come from the fixture.
    const config = await read(stand, bob, "/capability-github-config?environment=staging");
    await command(stand, bob, "set-capability-github-config", {
      environment: "staging",
      configId: "local",
      enabled: true,
      expectedConfigRevision: config.revision,
    });
    let revision = (await read(stand, bob, "/capability-requests/" + request.id)).revision;
    for (const transition of ["accepted", "in_progress", "pr_open"])
      await command(stand, bob, "update-capability-request", {
        id: request.id,
        expectedRevision: revision++,
        transition,
        ...(transition === "pr_open" ? { prUrl: "https://github.com/local/store/pull/1" } : {}),
      });
    const patch = join(repo, "examples/react-store/e2e/scenario/quick-view.patch");
    execFileSync("git", ["apply", "--whitespace=nowarn", patch], { cwd: repo });
    try {
      buildStore();
      const head = execFileSync("git", ["rev-parse", "HEAD"], {
        cwd: repo,
        encoding: "utf8",
      }).trim();
      await fetch(stand.github.origin + "/control", {
        method: "POST",
        headers: { Authorization: "Bearer " + stand.github.control },
        body: JSON.stringify({ merged: true, merge: head }),
      });
      const github = await read(stand, bob, "/capability-github-config?environment=staging");
      await command(stand, bob, "link-capability-github-pr", {
        requestId: request.id,
        pullNumber: 1,
        expectedRequestRevision: (await read(stand, bob, "/capability-requests/" + request.id))
          .revision,
        expectedLinkRevision: 0,
        expectedConfigRevision: github.revision,
      });
      await stand.startStore("staging");
      push("staging", stand.stores.staging.origin);
    } finally {
      execFileSync("git", ["apply", "-R", "--whitespace=nowarn", patch], { cwd: repo });
      // The baseline build again: the workspace stays as before the scenario.
      buildStore();
    }
    // AGT-060…081: the merged PR is built and pushed; the designer confirms the
    // argument; fulfilment replaces the placeholder (cms-system) and the request is fulfilled.
    const fulfillment = () => read(stand, alice, `/capability-requests/${request.id}/fulfillment`);
    await expect
      .poll(async () => (await fulfillment()).anchors?.[0]?.state, { timeout: 90_000 })
      .toBe("awaiting_args");
    await page.reload({ waitUntil: "domcontentloaded" });
    await page.locator('[role="treeitem"][title="n_quick"]').click();
    await page.getByRole("button", { name: "Открыть запрос для click", exact: true }).click();
    const card = page.getByRole("dialog");
    const section = card.getByRole("region", { name: "Выполнение", exact: true });
    await section.getByLabel("Значение параметра id", { exact: true }).selectOption("literal");
    await section.getByLabel("Литерал id", { exact: true }).fill("vase");
    await section
      .getByRole("button", { name: "Подтвердить аргументы выполнения", exact: true })
      .click();
    await expect(section.getByText("аргументы подтверждены")).toBeVisible();
    await card.getByRole("button", { name: "Закрыть", exact: true }).click();
    await expect
      .poll(async () => (await fulfillment()).anchors[0].state, { timeout: 90_000 })
      .toBe("replaced");
    expect((await read(stand, alice, "/capability-requests/" + request.id)).status).toBe(
      "fulfilled",
    );
    review = await command(stand, alice, "submit-changeset", {
      changesetId: cs.id,
      expectedSeq: await seq(),
    });
    await expect
      .poll(async () => (await read(stand, alice, "/changesets/" + cs.id)).state, {
        timeout: 90_000,
      })
      .not.toBe("checking");
    review = await read(stand, alice, `/changesets/${cs.id}/review`);
    expect(review.changeset.state, JSON.stringify(review.checks)).toBe("in_review");
    await command(stand, bob, "approve-changeset", {
      changesetId: cs.id,
      expectedSeq: await seq(),
    });
    await command(stand, alice, "publish", {
      changesetId: cs.id,
      environment: "staging",
      expectedSeq: await seq(),
    });
    await snap(alice, 10);
  });
});

// STU-496: a serious axe violation on the store page fails stage 9 and the Change Set.
// It reuses the page of the scenario; the storefront serves only its own route.
test("§2: a page with a nameless button fails the accessibility stage", async () => {
  const cs = await command(stand, alice, "create-changeset", {
    title: "Нарушение доступности",
    targets: ["staging"],
  });
  await command(stand, alice, "apply-operations", {
    changesetId: cs.id,
    expectedSeq: 0,
    operations: [
      {
        type: "node.insert",
        target: state.page,
        payload: {
          parentId: "n_root",
          index: 0,
          subtree: { id: "n_blank", type: "Button", bindings: { label: "$local.t_blank" } },
        },
      },
      {
        type: "localContent.set",
        target: state.page,
        payload: { key: "t_blank", type: "text", value: { ru: " ", en: " " } },
      },
    ],
  });
  const seq = (await read(stand, alice, "/changesets/" + cs.id)).seq;
  await command(stand, alice, "submit-changeset", { changesetId: cs.id, expectedSeq: seq });
  await expect
    .poll(async () => (await read(stand, alice, "/changesets/" + cs.id)).state, {
      timeout: 90_000,
    })
    .not.toMatch(/^(open|checking)$/);
  const review = await read(stand, alice, `/changesets/${cs.id}/review`);
  expect(review.changeset.state, JSON.stringify(review.checks)).toBe("failed");
  const preview = review.checks.find((c: any) => c.stage === "preview");
  const axe = review.checks.find((c: any) => c.stage === "axe");
  expect(preview.status, JSON.stringify(preview.details)).toBe("passed");
  expect(axe.status, JSON.stringify(axe.details)).toBe("failed");
  expect(JSON.stringify(axe.details)).toContain("button-name");
  await expect(
    command(stand, bob, "approve-changeset", { changesetId: cs.id, expectedSeq: seq }),
  ).rejects.toMatchObject({ status: 409 });
});

// STU-576…581, 587: a high-risk change of a published page gets screenshots against the
// published version and Lighthouse scores with a report.
test("§2: stages 10–11 compare screenshots and score the changed page", async () => {
  const cs = await command(stand, alice, "create-changeset", {
    title: "Строгая зона",
    targets: ["staging"],
  });
  await command(stand, alice, "apply-operations", {
    changesetId: cs.id,
    expectedSeq: 0,
    operations: [
      {
        type: "node.insert",
        target: state.page,
        payload: {
          parentId: "n_root",
          index: 0,
          subtree: {
            id: "n_strict",
            type: "Button",
            zone: { id: "z_strict", mode: "STRICT", requiredApprovalRole: "lead-designer" },
            bindings: { label: "$local.t_strict" },
          },
        },
      },
      {
        type: "localContent.set",
        target: state.page,
        payload: { key: "t_strict", type: "text", value: { ru: "Строгая кнопка", en: "Strict" } },
      },
    ],
  });
  const seq = (await read(stand, alice, "/changesets/" + cs.id)).seq;
  const submitted = await command(stand, alice, "submit-changeset", {
    changesetId: cs.id,
    expectedSeq: seq,
  });
  expect(submitted.risk).toBe("high");
  const done = (c: any) => c && !["queued", "running"].includes(c.status);
  await expect
    .poll(
      async () => {
        const checks = (await read(stand, alice, `/changesets/${cs.id}/review`)).checks;
        return ["visual", "lighthouse"].every((s) => done(checks.find((c: any) => c.stage === s)));
      },
      { timeout: 240_000, intervals: [2000] },
    )
    .toBe(true);
  const review = await read(stand, alice, `/changesets/${cs.id}/review`);
  const visual = review.checks.find((c: any) => c.stage === "visual");
  expect(visual.status, JSON.stringify(visual.details)).toBe("passed");
  const page = visual.details.pages.find((p: any) => p.objectId === state.page);
  expect(page.widths.map((w: any) => w.width)).toEqual([375, 768, 1440]);
  const changed = page.widths.find((w: any) => w.changedRatio > 0);
  expect(changed, JSON.stringify(page)).toMatchObject({
    baseline: "published",
    artifacts: { before: expect.any(String), after: expect.any(String), diff: expect.any(String) },
  });
  // Read through the Studio adapter with the signed-in browser session (STU-575).
  const diff = await alice.context.request.get(
    `${stand.studioOrigin}/api/studio/api/v1/changesets/${cs.id}/checks/${visual.id}/artifacts/${changed.artifacts.diff}`,
  );
  expect(diff.status()).toBe(200);
  expect(diff.headers()["content-type"]).toBe("image/png");
  const lighthouse = review.checks.find((c: any) => c.stage === "lighthouse");
  expect(lighthouse.blocking).toBe(true);
  const scored = lighthouse.details.pages.find((p: any) => p.objectId === state.page);
  expect(scored, JSON.stringify(lighthouse.details)).toMatchObject({
    scores: { performance: expect.any(Number), accessibility: expect.any(Number) },
    report: expect.stringMatching(/-lighthouse\.html$/),
  });
  expect(lighthouse.status).toMatch(
    scored.scores.accessibility >= 0.9 ? /^(passed|warning)$/ : /^failed$/,
  );
  await command(stand, alice, "reopen-changeset", { changesetId: cs.id });
});

// SEC-005/OPS-022, STU-515: the stand's logs carry no tokens, preview JWTs or cookies.
test("§2: stand logs carry no secrets", async () => {
  const patterns = [
    /cms_(?:svc|pub)_[A-Za-z0-9_-]{20,}/,
    /cms_preview=[A-Za-z0-9._-]{20,}/,
    /eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}/,
    /Bearer\s+[A-Za-z0-9._-]{20,}/,
  ];
  const logs = readdirSync(directory).filter((f) => f.endsWith(".log"));
  expect(logs.length).toBeGreaterThan(3);
  for (const file of logs) {
    const text = readFileSync(join(directory, file), "utf8");
    for (const pattern of patterns) expect(pattern.test(text), `${file}: ${pattern}`).toBe(false);
  }
});
