import type { IncomingMessage, ServerResponse } from "node:http";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { createStudioHandler } from "../../../apps/studio/src/handler.js";
import type { PageResponse } from "@cms/core";
type IrDocument = PageResponse["document"];
import type {
  HistoryAction,
  AgentRun,
  ContentRecord,
  ContentOperation,
} from "../../../apps/studio/src/types.js";
import { createHash } from "node:crypto";
import { applyOperations } from "../../../apps/studio/src/operations.js";
import { catalog } from "../scripts/catalog.js";
import { storeDocument } from "../src/composition.js";
import { identityBoundary } from "./identity-boundary.js";
export async function studioFixture(id: string, previewToken: string) {
  const previewTokens = new Set([previewToken]);
  const credential = "cms_svc_e2e_local_studio_actor";
  const manifest = JSON.parse(
    await readFile(new URL("../.cms/manifest.json", import.meta.url), "utf8"),
  );
  manifest.schemas = {
    ...manifest.schemas,
    Article: {
      version: 1,
      title: "Статья",
      fields: {
        title: { type: "text", localized: true, required: true },
        body: { type: "richText" },
        cover: { type: "asset", assetKind: "image" },
      },
    },
  };
  const entityId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c55",
    assetId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c66",
    newEntityId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c77",
    uploadId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c88";
  let content: Record<string, ContentRecord> = {};
  const contentKeys = new Map<string, { body: string; result: unknown }>(),
    uploadKeys = new Map<string, { body: string; result: unknown }>();
  let upload:
    | {
        assetId: string;
        status: string;
        expiresAt: string;
        fileHash?: string;
        errorCode?: string;
        uploadUrl?: string;
      }
    | undefined;
  let uploadExpected: { sha256: string; size: number } | undefined, uploaded: Buffer | undefined;
  const studio = createStudioHandler(
    {
      baseUrl: "http://127.0.0.1:18084",
      studioOrigin: "http://127.0.0.1:18084",
      appOrigin: "http://127.0.0.1:18083",
      project: "local-demo",
      environment: "staging",
      credential,
      assetUploadOrigin: "http://127.0.0.1:18084",
    },
    fileURLToPath(new URL("../../../apps/studio/dist/client", import.meta.url)),
  );
  let active = false,
    document: IrDocument,
    seq = 1,
    fault = "",
    state = "open";
  let human = false;
  const decisions: {
    approverId: string;
    decision: string;
    comment: string | null;
    valid: boolean;
    createdAt: string;
  }[] = [];
  const workflowKeys = new Map<string, { body: string; result: unknown }>();
  const draftTitles = new Map<string, string>(),
    createKeys = new Map<string, string>();
  const createdId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c99";
  const identity = identityBoundary(id, createdId);
  const userStudio = createStudioHandler(
    {
      baseUrl: "http://127.0.0.1:18084",
      studioOrigin: "http://127.0.0.1:18084",
      appOrigin: "http://127.0.0.1:18083",
      project: "local-demo",
      environment: "staging",
      authMode: "oidc",
      assetUploadOrigin: "http://127.0.0.1:18084",
    },
    fileURLToPath(new URL("../../../apps/studio/dist/client", import.meta.url)),
  );
  type Edit = {
    before: IrDocument;
    after: IrDocument;
    operations: HistoryAction["operations"];
    contentBefore?: Record<string, ContentRecord>;
    contentAfter?: Record<string, ContentRecord>;
  };
  const undoStack: Edit[] = [],
    redoStack: Edit[] = [],
    actions: HistoryAction[] = [];
  const historyKeys = new Map<string, { body: string; seq: number }>();
  let agentRuns: AgentRun[] = [];
  const agentKeys = new Map<string, string>();
  let historyId = 0;
  const leases = new Map<
    string,
    {
      actorId: string;
      actorName: string;
      changesetId: string;
      changesetTitle: string;
      expires: number;
    }
  >();
  let revision = 0,
    fingerprint = "";
  function logHistory(kind: HistoryAction["kind"], operations: HistoryAction["operations"]) {
    const firstSeq = seq + 1;
    seq += operations.length;
    actions.unshift({
      id: "0192f1c7-4b1e-7c2b-9d10-" + (++historyId).toString(16).padStart(12, "0"),
      firstSeq,
      lastSeq: seq,
      actorId: id,
      actorName: "Studio actor",
      createdAt: "2026-10-12T08:00:00.000Z",
      reason: null,
      kind,
      operations,
    });
  }
  function reset(enabled: boolean) {
    leases.clear();
    agentRuns = [];
    agentKeys.clear();
    active = enabled;
    seq = 1;
    fault = "";
    state = "open";
    human = false;
    identity.reset();
    decisions.length = 0;
    workflowKeys.clear();
    undoStack.length = 0;
    redoStack.length = 0;
    actions.length = 0;
    historyKeys.clear();
    contentKeys.clear();
    uploadKeys.clear();
    upload = undefined;
    uploaded = undefined;
    uploadExpected = undefined;
    content = {
      [entityId]: {
        id: entityId,
        versionId: entityId,
        kind: "entity",
        schema: "Article",
        schemaVersion: 1,
        deleted: false,
        data: {
          title: { ru: "Статья", en: "Keep" },
          body: {
            type: "doc",
            content: [{ type: "paragraph", content: [{ type: "text", text: "Rich before" }] }],
          },
        },
      },
      [assetId]: {
        id: assetId,
        versionId: assetId,
        kind: "asset",
        deleted: false,
        data: {
          title: "Picture",
          alt: { ru: "До", en: "Keep" },
          assetKind: "image",
          mimeType: "image/png",
          fileHash: "a".repeat(64),
        },
      },
    };
    historyId = 0;
    draftTitles.clear();
    createKeys.clear();
    draftTitles.set(id, "Studio browser draft");
    const config = structuredClone(catalog);
    config.heroAsset = { assetId: id };
    config.products.forEach((p) => {
      p.asset = { assetId: id };
    });
    delete config.products[0]!.priceRub;
    document = storeDocument(config);
    document.nodes.n_store!.children!.push("n_title");
    document.nodes.n_title = {
      id: "n_title",
      type: "Heading",
      props: { level: 2 },
      bindings: { text: "$local.t_title" },
    };
    document.localContent = {
      t_title: { type: "text", value: { ru: "Заголовок черновика", en: "Draft heading" } },
    };
    document.nodes.n_store!.children!.push("n_rich");
    document.nodes.n_rich = {
      id: "n_rich",
      type: "RichText",
      bindings: { value: "$data.article.body" },
    };
  }
  reset(false);
  const diffSnapshot = () =>
    "sha256:" +
    createHash("sha256").update(JSON.stringify({ seq, document, content })).digest("hex");
  const changeset = (draftId = id) => ({
    id: draftId,
    title: draftTitles.get(draftId),
    state,
    kind: "standard",
    seq,
    targets: ["staging"],
  });
  return {
    acceptsPreview: (authorization: string | undefined) =>
      typeof authorization === "string" &&
      authorization.startsWith("Preview ") &&
      previewTokens.has(authorization.slice(8)),
    reset,
    get document() {
      return active ? document : null;
    },
    get contentData() {
      return active && !content[entityId]!.deleted ? { article: content[entityId]!.data } : {};
    },
    async handle(req: IncomingMessage, res: ServerResponse): Promise<boolean> {
      const path = new URL(req.url ?? "/", "http://127.0.0.1:18084").pathname;
      if (path === `/upload/${uploadId}` && req.method === "PUT") {
        if (
          !upload ||
          new URL(req.url!, "http://127.0.0.1").searchParams.get("sig") !== "test-presigned" ||
          req.headers.authorization ||
          req.headers.cookie
        ) {
          res.writeHead(403);
          res.end();
          return true;
        }
        const chunks: Buffer[] = [];
        for await (const chunk of req) chunks.push(Buffer.from(chunk));
        uploaded = Buffer.concat(chunks);
        res.end();
        return true;
      }
      if (
        path === "/editor" ||
        path.startsWith("/api/studio/") ||
        (path.startsWith("/assets/") && /\.(js|css)$/.test(path))
      ) {
        await (identity.enabled ? userStudio : studio)(req, res);
        return true;
      }
      if (path === "/studio-control" && req.method === "POST") {
        let body = "";
        for await (const chunk of req) body += String(chunk);
        const control = JSON.parse(body) as {
          reset?: boolean;
          fault?: string;
          state?: string;
          human?: boolean;
          identity?: boolean;
          expired?: boolean;
          user?: string;
          remoteTitle?: string;
        };
        if (control.reset) reset(true);
        if (control.remoteTitle !== undefined) {
          document.localContent!.t_title!.value = { ru: control.remoteTitle, en: "Remote heading" };
          seq++;
        }
        fault = control.fault ?? "";
        state = control.state ?? state;
        human = control.human ?? human;
        identity.configure(control, res);
        res.end("ok");
        return true;
      }
      if (identity.handle(req, res)) return true;
      if (!path.startsWith("/api/v1/")) return false;
      const who = identity.user(req);
      res.setHeader("Content-Type", "application/json");
      res.setHeader("Cache-Control", "no-store");
      const fail = (status: number, code: string) => {
        res.writeHead(status);
        res.end(JSON.stringify({ code, detail: credential }));
      };
      if (
        (identity.enabled ? !who : req.headers.authorization !== "Bearer " + credential) ||
        req.headers["x-cms-project"] !== "local-demo"
      ) {
        fail(401, "UNAUTHENTICATED");
        return true;
      }
      const denied = identity.enabled ? identity.denial(req) : null;
      if (denied) {
        fail(denied.status, denied.code);
        return true;
      }
      const owner = !identity.enabled || who?.id === id;
      if (path === "/api/v1/agent-config") {
        res.end(
          JSON.stringify({
            provider: "openrouter",
            model: "fixture/model",
            configured: fault !== "agent-unconfigured",
            canDelegate: identity.enabled && owner,
            monthlyTokens: 1000000,
            userMonthlyTokens: 100000,
          }),
        );
        return true;
      }
      if (path === "/api/v1/agent-runs") {
        res.end(JSON.stringify({ items: owner ? agentRuns : [] }));
        return true;
      }
      if (path === "/api/v1/events") {
        const query = new URL(req.url!, "http://127.0.0.1").searchParams;
        let cursor = query.get("after") ?? "";
        res.writeHead(200, { "Content-Type": "text/event-stream" });
        const emit = (event: string, value: unknown, revision?: string) =>
          res.write(
            `${revision === undefined ? "" : "id: " + revision + "\n"}event: ${event}\ndata: ${JSON.stringify(value)}\n\n`,
          );
        const snapshot = () => {
          const next = JSON.stringify({ seq, state, document, content, drafts: [...draftTitles] });
          if (next !== fingerprint) {
            fingerprint = next;
            revision++;
          }
          const current = String(revision);
          if (cursor !== current) {
            emit("invalidate", { revision: current }, current);
            cursor = current;
          } else emit("sync", { revision: current }, current);
          const items = new Map<
            string,
            Omit<typeof leases extends Map<string, infer V> ? V : never, "expires">
          >();
          for (const [key, lease] of leases) {
            if (lease.expires <= Date.now()) {
              leases.delete(key);
              continue;
            }
            const { expires: _expires, ...item } = lease;
            items.set(lease.actorId + lease.changesetId, item);
          }
          emit("presence", { items: [...items.values()] });
          emit("agent", {
            items: owner
              ? agentRuns.map((r) => ({ id: r.id, state: r.state, stepSeq: r.stepSeq }))
              : [],
          });
        };
        snapshot();
        // Only the HTTP boundary is modelled here; Go tests verify storage/TTL.
        const poll = setInterval(snapshot, 100),
          timeout = setTimeout(() => res.end(), 25_000);
        res.once("close", () => {
          clearInterval(poll);
          clearTimeout(timeout);
        });
        return true;
      }
      if (path === "/api/v1/content-catalog") {
        const query = new URL(req.url!, "http://127.0.0.1").searchParams;
        res.end(
          JSON.stringify({
            items: Object.values(content)
              .filter(
                (r) =>
                  r.kind === query.get("kind") &&
                  (!query.get("schema") || r.schema === query.get("schema")),
              )
              .map((r) => ({
                id: r.id,
                kind: r.kind,
                schema: r.schema,
                title:
                  r.kind === "entity"
                    ? String(
                        (r.data.title as Record<string, unknown> | undefined)?.ru ?? "Новая статья",
                      )
                    : String(r.data.title ?? "Файл"),
                deleted: r.deleted,
                assetKind: r.data.assetKind,
                mimeType: r.data.mimeType,
              })),
            nextCursor: null,
            locales: ["ru", "en"],
            defaultLocale: "ru",
          }),
        );
        return true;
      }
      if (path.startsWith("/api/v1/content/")) {
        const record = content[path.split("/").at(-1)!];
        if (!record) {
          fail(404, "NOT_FOUND");
          return true;
        }
        res.end(JSON.stringify(record));
        return true;
      }
      if (path === `/api/v1/asset-uploads/${uploadId}`) {
        if (!upload) {
          fail(404, "NOT_FOUND");
          return true;
        }
        res.end(JSON.stringify(upload));
        return true;
      }
      if (path === "/api/v1/work-items") {
        res.end(
          JSON.stringify({
            items:
              owner || ["in_review", "approved"].includes(state)
                ? [
                    {
                      id,
                      title: draftTitles.get(id),
                      documentId: id,
                      path: "/store",
                      ownerId: id,
                      ownerName: "Alice",
                      own: owner,
                      state,
                    },
                  ]
                : [],
            nextCursor: null,
          }),
        );
        return true;
      }
      if (path === "/api/v1/manifest") {
        res.end(JSON.stringify({ manifest, manifestHash: "sha256:" + "a".repeat(64) }));
        return true;
      }
      if (/\/changesets\/[^/]+\/diff(?:\/[^/]+)?$/.test(path)) {
        const query = new URL(req.url!, "http://127.0.0.1").searchParams;
        const csId = path.split("/")[4]!;
        const index = {
          changesetId: csId,
          seq,
          snapshot: diffSnapshot(),
          environment: "staging",
          targets: ["staging"],
          complete: true,
          objects: [
            {
              id,
              kind: "document",
              docKind: "page",
              title: "Store",
              beforePath: "/store",
              afterPath: "/store",
              baseVersionId: id,
              workingVersionId: createdId,
              headVersionId: id,
              publishedVersionId: id,
              baseStale: false,
              summaries: {
                base: { effect: "modified", changeCount: 1 },
                published: { effect: "modified", changeCount: 1 },
              },
            },
          ],
        };
        if (path.endsWith("/diff")) res.end(JSON.stringify(index));
        else if (query.get("snapshot") !== index.snapshot) fail(409, "DIFF_SNAPSHOT_STALE");
        else
          res.end(
            JSON.stringify({
              ...index,
              objectId: id,
              baseline: query.get("baseline"),
              totalChanges: 1,
              nextCursor: null,
              items: [
                {
                  key: "0",
                  category: "property",
                  kind: "replace",
                  pointer: "/nodes/n_title/props/text",
                  nodeId: "n_title",
                  before: { present: true, value: "Before" },
                  after: {
                    present: true,
                    value:
                      (document.nodes.n_title?.props as Record<string, unknown> | undefined)
                        ?.text ?? "After",
                  },
                },
              ],
            }),
          );
        return true;
      }
      if (path.endsWith("/review")) {
        res.end(
          JSON.stringify({
            changeset: changeset(path.split("/").at(-2)!),
            risk: state === "open" ? null : "medium",
            needsAttention: state === "failed",
            canEdit: state === "open" && owner,
            requiredApprovals: human || identity.enabled ? 1 : 0,
            approvals: decisions.filter((d) => d.valid && d.decision === "approve").length,
            history: decisions,
            objects: [{ id, kind: "page", path: "/store", deleted: false }],
            actions: {
              submit: !owner ? "FORBIDDEN" : state === "open" ? null : "CHANGESET_NOT_OPEN",
              reopen: ["failed", "approved", "in_review", "changes_requested"].includes(state)
                ? owner
                  ? null
                  : "FORBIDDEN"
                : "CHANGESET_STATE_INVALID",
              approve:
                state !== "in_review"
                  ? "CHANGESET_STATE_INVALID"
                  : identity.enabled && owner
                    ? "APPROVAL_SELF_FORBIDDEN"
                    : human || identity.enabled
                      ? null
                      : "APPROVAL_FORBIDDEN_ACTOR",
              "request-changes":
                state !== "in_review"
                  ? "CHANGESET_STATE_INVALID"
                  : identity.enabled && owner
                    ? "APPROVAL_SELF_FORBIDDEN"
                    : human || identity.enabled
                      ? null
                      : "APPROVAL_FORBIDDEN_ACTOR",
              publish: state === "approved" ? null : "CHANGESET_STATE_INVALID",
            },
            checks:
              state === "open"
                ? []
                : [
                    {
                      stage: "ir",
                      status: state === "failed" ? "failed" : "passed",
                      blocking: true,
                      details:
                        state === "failed"
                          ? {
                              environments: {
                                staging: {
                                  documents: {
                                    [id]: [
                                      {
                                        code: "PROP_REQUIRED",
                                        severity: "error",
                                        pointer: "/nodes/n_title/props/text",
                                      },
                                    ],
                                  },
                                },
                              },
                            }
                          : {},
                    },
                    { stage: "policy", status: "passed", blocking: true, details: {} },
                  ],
          }),
        );
        return true;
      }
      if (path.endsWith("/history")) {
        const before = Number(
          new URL(req.url!, "http://127.0.0.1").searchParams.get("beforeSeq") ?? 2147483647,
        );
        const items = actions.filter((item) => item.lastSeq < before);
        const unsupportedUndo = undoStack
          .at(-1)
          ?.operations.some((op) => ["entity.create", "asset.create"].includes(op.type));
        const canUndo = state === "open" && undoStack.length > 0 && !unsupportedUndo,
          canRedo = state === "open" && redoStack.length > 0;
        res.end(
          JSON.stringify({
            seq,
            items: items.slice(0, 20),
            nextBeforeSeq: items.length > 20 ? items[19]!.lastSeq : null,
            canUndo,
            canRedo,
            undoReason: canUndo
              ? null
              : state !== "open"
                ? "CHANGESET_STATE_INVALID"
                : unsupportedUndo
                  ? "UNDO_NOT_SUPPORTED"
                  : "NOTHING_TO_UNDO",
            redoReason: canRedo
              ? null
              : state !== "open"
                ? "CHANGESET_STATE_INVALID"
                : "NOTHING_TO_REDO",
          }),
        );
        return true;
      }
      if (path === "/api/v1/pages") {
        res.end(
          JSON.stringify({
            items: [{ id, path: "/store", title: "Store", hasHead: true }],
            nextCursor: null,
          }),
        );
        return true;
      }
      if (path === "/api/v1/working-drafts") {
        res.end(
          JSON.stringify({
            items: state === "open" && owner ? [...draftTitles.keys()].map(changeset) : [],
            nextCursor: null,
          }),
        );
        return true;
      }
      if (path.startsWith("/api/v1/changesets/") && draftTitles.has(path.split("/").at(-1)!)) {
        res.end(JSON.stringify(changeset(path.split("/").at(-1)!)));
        return true;
      }
      if (path === `/api/v1/documents/${id}`) {
        res.end(JSON.stringify({ id, kind: "page", path: "/store", body: document }));
        return true;
      }
      let body = "";
      for await (const chunk of req) body += String(chunk);
      const envelope = JSON.parse(body);
      if (path === "/api/v1/commands/start-agent-run") {
        if (!identity.enabled || !owner) {
          fail(403, "POLICY_DENIED");
          return true;
        }
        const p = envelope.payload,
          key = String(req.headers["idempotency-key"]);
        if (!agentKeys.has(key)) {
          if (p.expectedSeq !== seq || p.documentId !== id || p.changesetId !== id) {
            fail(409, "AGENT_SCOPE_CHANGED");
            return true;
          }
          const runId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4caa";
          agentKeys.set(key, runId);
          const task: AgentRun = {
            id: runId,
            documentId: id,
            changesetId: id,
            environment: "staging",
            model: "fixture/model",
            prompt: p.prompt,
            state: fault === "agent-running" ? "running" : "completed",
            stepSeq: 3,
            steps: [
              { seq: 1, kind: "tool", data: { name: "set_local_content" } },
              { seq: 2, kind: "message", data: { text: "<script>Готово</script>" } },
              { seq: 3, kind: "finished", data: {} },
            ],
            tokens: 120,
            reserved: 0,
            usageKnown: true,
          };
          agentRuns = [task];
          if (task.state === "completed") {
            const before = structuredClone(document);
            document.localContent!.t_title!.value = { ru: "Текст агента", en: "Draft heading" };
            const operations = [{ type: "localContent.set", target: id, status: "applied" }];
            undoStack.push({ before, after: structuredClone(document), operations });
            redoStack.length = 0;
            logHistory("edit", operations);
            actions[0]!.actorName = "Studio Agent";
          }
          if (fault === "agent-lost") {
            fault = "";
            fail(503, "STUDIO_RESPONSE_LOST");
            return true;
          }
        }
        res.end(JSON.stringify({ result: { id: agentKeys.get(key) } }));
        return true;
      }
      if (path === "/api/v1/commands/cancel-agent-run") {
        if (!identity.enabled || !owner) {
          fail(403, "POLICY_DENIED");
          return true;
        }
        for (const run of agentRuns)
          if (run.id === envelope.payload.runId && ["queued", "running"].includes(run.state))
            run.state = "cancelled";
        res.end(JSON.stringify({ result: { id: envelope.payload.runId } }));
        return true;
      }
      if (path.includes("/commands/presence-")) {
        const p = envelope.payload,
          actorId = who?.id ?? id;
        const key = actorId + p.leaseId;
        if (path.endsWith("presence-leave")) leases.delete(key);
        else
          leases.set(key, {
            actorId,
            actorName: who?.name ?? "Studio actor",
            changesetId: p.changesetId,
            changesetTitle: draftTitles.get(p.changesetId) ?? "Draft",
            expires: Date.now() + 30_000,
          });
        res.end(JSON.stringify({ result: { ttlSeconds: 30 } }));
        return true;
      }
      if (
        !owner &&
        ["apply-operations", "undo", "redo", "submit-changeset", "reopen-changeset"].some(
          (command) => path.endsWith("/" + command),
        )
      ) {
        fail(403, "FORBIDDEN");
        return true;
      }
      if (
        identity.enabled &&
        owner &&
        ["approve-changeset", "request-changes"].some((command) => path.endsWith("/" + command))
      ) {
        fail(403, "APPROVAL_SELF_FORBIDDEN");
        return true;
      }
      if (
        [
          "submit-changeset",
          "reopen-changeset",
          "approve-changeset",
          "request-changes",
          "publish",
        ].some((command) => path.endsWith("/" + command))
      ) {
        const key = String(req.headers["idempotency-key"]),
          prior = workflowKeys.get(key);
        if (prior) {
          if (prior.body !== body) fail(409, "IDEMPOTENCY_KEY_REUSED");
          else res.end(JSON.stringify({ result: prior.result }));
          return true;
        }
        if (fault === "workflow-reject") {
          fail(403, "FORBIDDEN");
          return true;
        }
        if (fault === "workflow-stale") seq++;
        if (envelope.payload.expectedSeq !== seq) {
          fail(409, "CHANGESET_SEQ_CONFLICT");
          return true;
        }
        if (
          envelope.payload.expectedDiffSnapshot &&
          envelope.payload.expectedDiffSnapshot !== diffSnapshot()
        ) {
          fail(409, "DIFF_SNAPSHOT_STALE");
          return true;
        }
        let result: unknown;
        if (path.endsWith("submit-changeset"))
          state =
            fault === "submit-invalid"
              ? "failed"
              : human || identity.enabled
                ? "in_review"
                : "approved";
        else if (path.endsWith("reopen-changeset")) {
          state = "open";
          decisions.forEach((d) => {
            d.valid = false;
          });
        } else if (path.endsWith("approve-changeset") || path.endsWith("request-changes")) {
          if (!human && !identity.enabled) {
            fail(403, "APPROVAL_FORBIDDEN_ACTOR");
            return true;
          }
          const approve = path.endsWith("approve-changeset");
          state = approve ? "approved" : "changes_requested";
          decisions.push({
            approverId: createdId,
            decision: approve ? "approve" : "request_changes",
            comment: envelope.payload.comment ?? null,
            valid: true,
            createdAt: "2026-10-13T00:00:00Z",
          });
        } else {
          if (state !== "approved") {
            fail(409, "CHANGESET_STATE_INVALID");
            return true;
          }
          state = "merged";
          result = {
            id: createdId,
            changesetId: id,
            environment: "staging",
            createdAt: "2026-10-13T00:00:00Z",
          };
        }
        result ??= { changeset: changeset() };
        workflowKeys.set(key, { body, result });
        if (fault === "workflow-lost") {
          fault = "";
          fail(503, "STUDIO_RESPONSE_LOST");
          return true;
        }
        res.end(JSON.stringify({ result }));
        return true;
      }
      if (["/api/v1/commands/undo", "/api/v1/commands/redo"].includes(path)) {
        const key = String(req.headers["idempotency-key"]),
          prior = historyKeys.get(key);
        if (prior) {
          if (prior.body !== body) {
            fail(409, "IDEMPOTENCY_KEY_REUSED");
            return true;
          }
          res.end(JSON.stringify({ result: { changeset: { ...changeset(), seq: prior.seq } } }));
          return true;
        }
        if (fault === "history-reject") {
          fail(403, "FORBIDDEN");
          return true;
        }
        if (fault === "history-conflict") {
          seq++;
          fail(409, "CHANGESET_SEQ_CONFLICT");
          return true;
        }
        if (envelope.payload.expectedSeq !== seq) {
          fail(409, "CHANGESET_SEQ_CONFLICT");
          return true;
        }
        if (state !== "open") {
          fail(409, "CHANGESET_STATE_INVALID");
          return true;
        }
        const undo = path.endsWith("undo"),
          edit = (undo ? undoStack : redoStack).pop();
        if (!edit) {
          fail(409, undo ? "NOTHING_TO_UNDO" : "NOTHING_TO_REDO");
          return true;
        }
        document = structuredClone(undo ? edit.before : edit.after);
        const snapshot = undo ? edit.contentBefore : edit.contentAfter;
        if (snapshot) content = structuredClone(snapshot);
        (undo ? redoStack : undoStack).push(edit);
        logHistory(undo ? "undo" : "redo", edit.operations);
        historyKeys.set(key, { body, seq });
        if (fault === "history-lost") {
          fault = "";
          fail(503, "STUDIO_RESPONSE_LOST");
          return true;
        }
        res.end(JSON.stringify({ result: { changeset: changeset() } }));
        return true;
      }
      if (path === "/api/v1/commands/create-changeset") {
        const key = String(req.headers["idempotency-key"]);
        const prior = createKeys.get(key);
        if (!prior) {
          createKeys.set(key, createdId);
          draftTitles.set(createdId, envelope.payload.title);
        }
        if (fault === "create-lost" && !prior) {
          fault = "";
          fail(503, "STUDIO_RESPONSE_LOST");
          return true;
        }
        res.end(JSON.stringify({ result: changeset(prior ?? createdId) }));
        return true;
      }
      if (path === "/api/v1/commands/create-preview-token") {
        const claims = JSON.parse(
          Buffer.from(previewToken.split(".")[1]!, "base64url").toString("utf8"),
        );
        claims.cs = envelope.payload.changesetId;
        const token =
          "header." + Buffer.from(JSON.stringify(claims)).toString("base64url") + ".fixture";
        previewTokens.add(token);
        res.end(
          JSON.stringify({
            result: {
              token,
              expiresAt: new Date(Date.now() + 900_000).toISOString(),
            },
          }),
        );
        return true;
      }
      if (path.endsWith("create-asset-upload") || path.endsWith("complete-asset-upload")) {
        const key = String(req.headers["idempotency-key"]),
          prior = uploadKeys.get(key);
        if (prior) {
          if (prior.body !== body) {
            fail(409, "IDEMPOTENCY_KEY_CONFLICT");
            return true;
          }
          res.end(JSON.stringify({ result: prior.result }));
          return true;
        }
        if (path.endsWith("create-asset-upload")) {
          uploadExpected = envelope.payload;
          upload = {
            assetId: uploadId,
            status: "pending",
            expiresAt: new Date(Date.now() + 900000).toISOString(),
            uploadUrl: `http://127.0.0.1:18084/upload/${uploadId}?sig=test-presigned`,
          };
        } else {
          const valid =
            uploaded &&
            uploaded.length === uploadExpected?.size &&
            createHash("sha256").update(uploaded).digest("hex") === uploadExpected?.sha256;
          upload = {
            ...upload!,
            status: valid ? "ready" : "rejected",
            ...(valid
              ? { fileHash: createHash("sha256").update(uploaded!).digest("hex") }
              : { errorCode: "ASSET_HASH_MISMATCH" }),
          };
        }
        uploadKeys.set(key, { body, result: structuredClone(upload) });
        if (fault === "upload-lost") {
          fault = "";
          fail(503, "STUDIO_RESPONSE_LOST");
          return true;
        }
        res.end(JSON.stringify({ result: upload }));
        return true;
      }
      if (path !== "/api/v1/commands/apply-operations") {
        fail(404, "NOT_FOUND");
        return true;
      }
      const contentOps = envelope.payload.operations as ContentOperation[];
      if (contentOps.some((op) => op.type.startsWith("entity.") || op.type.startsWith("asset."))) {
        const key = String(req.headers["idempotency-key"]),
          prior = contentKeys.get(key);
        if (prior) {
          if (prior.body !== body) {
            fail(409, "IDEMPOTENCY_KEY_CONFLICT");
            return true;
          }
          res.end(JSON.stringify({ result: prior.result }));
          return true;
        }
        if (fault === "reject") {
          fail(403, "FORBIDDEN");
          return true;
        }
        if (fault === "conflict") {
          seq++;
          fail(409, "CHANGESET_SEQ_CONFLICT");
          return true;
        }
        if (!owner || state !== "open" || envelope.payload.expectedSeq !== seq) {
          fail(
            409,
            !owner
              ? "CHANGESET_NOT_OWNER"
              : state !== "open"
                ? "CHANGESET_STATE_INVALID"
                : "CHANGESET_SEQ_CONFLICT",
          );
          return true;
        }
        const contentBefore = structuredClone(content),
          before = structuredClone(document);
        if (
          contentOps.some(
            (op) => op.expectedVersionId && content[op.target!]?.versionId !== op.expectedVersionId,
          )
        ) {
          fail(409, "CONTENT_VERSION_CONFLICT");
          return true;
        }
        const operations = contentOps.map((op) => {
          const target = op.target ?? (op.type === "entity.create" ? newEntityId : uploadId);
          if (op.type === "entity.create")
            content[target] = {
              id: target,
              versionId: target,
              kind: "entity",
              schema: String(op.payload.schema),
              schemaVersion: 1,
              deleted: false,
              data: structuredClone(op.payload.data as Record<string, unknown>),
            };
          else if (op.type === "asset.create")
            content[target] = {
              id: target,
              versionId: target,
              kind: "asset",
              deleted: false,
              data: { fileHash: upload!.fileHash, mimeType: "image/png", assetKind: "image" },
            };
          else if (op.type === "entity.delete" || op.type === "entity.restore")
            content[target]!.deleted = op.type === "entity.delete";
          else {
            const data = content[target]!.data;
            for (const [path, v] of Object.entries((op.payload.set as object) ?? {})) {
              const [name, locale] = path.split(".");
              if (locale) (data[name!] ??= {}) as object;
              if (locale) (data[name!] as Record<string, unknown>)[locale] = v;
              else data[name!] = v;
            }
            for (const path of (op.payload.unset as string[]) ?? []) {
              const [name, locale] = path.split(".");
              if (locale) delete (data[name!] as Record<string, unknown>)[locale];
              else delete data[name!];
            }
          }
          return { type: op.type, target, status: "applied" };
        });
        undoStack.push({
          before,
          after: structuredClone(document),
          operations,
          contentBefore,
          contentAfter: structuredClone(content),
        });
        redoStack.length = 0;
        logHistory("edit", operations);
        const result = {
          changeset: changeset(),
          operations: operations.map(({ type, target }) => ({ type, target })),
        };
        contentKeys.set(key, { body, result });
        if (fault === "lost") {
          fault = "";
          fail(503, "STUDIO_RESPONSE_LOST");
          return true;
        }
        res.end(JSON.stringify({ result }));
        return true;
      }
      if (fault === "reject") {
        fail(403, "FORBIDDEN");
        return true;
      }
      if (fault === "conflict") {
        seq++;
        fail(409, "CHANGESET_SEQ_CONFLICT");
        return true;
      }
      if (envelope.payload.expectedSeq !== seq) {
        fail(409, "CHANGESET_SEQ_CONFLICT");
        return true;
      }
      if (state !== "open") {
        fail(409, "CHANGESET_STATE_INVALID");
        return true;
      }
      const before = structuredClone(document);
      document = applyOperations(document, envelope.payload.operations);
      const operations = envelope.payload.operations.map(
        (op: { type: string; target: string }) => ({
          type: op.type,
          target: op.target,
          status: "applied",
        }),
      );
      undoStack.push({ before, after: structuredClone(document), operations });
      redoStack.length = 0;
      logHistory("edit", operations);
      if (fault === "lost") {
        fail(503, "STUDIO_RESPONSE_LOST");
        return true;
      }
      res.end(JSON.stringify({ result: { changeset: changeset() } }));
      return true;
    },
  };
}
