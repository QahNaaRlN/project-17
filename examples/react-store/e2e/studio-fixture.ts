import type { IncomingMessage, ServerResponse } from "node:http";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { createStudioHandler } from "../../../apps/studio/src/handler.js";
import type { PageResponse } from "@cms/core";
type IrDocument = PageResponse["document"];
import type { HistoryAction } from "../../../apps/studio/src/types.js";
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
  const studio = createStudioHandler(
    {
      baseUrl: "http://127.0.0.1:18084",
      studioOrigin: "http://127.0.0.1:18084",
      appOrigin: "http://127.0.0.1:18083",
      project: "local-demo",
      environment: "staging",
      credential,
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
    },
    fileURLToPath(new URL("../../../apps/studio/dist/client", import.meta.url)),
  );
  type Edit = { before: IrDocument; after: IrDocument; operations: HistoryAction["operations"] };
  const undoStack: Edit[] = [],
    redoStack: Edit[] = [],
    actions: HistoryAction[] = [];
  const historyKeys = new Map<string, { body: string; seq: number }>();
  let historyId = 0;
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
  }
  reset(false);
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
    async handle(req: IncomingMessage, res: ServerResponse): Promise<boolean> {
      const path = new URL(req.url ?? "/", "http://127.0.0.1:18084").pathname;
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
        };
        if (control.reset) reset(true);
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
        const canUndo = state === "open" && undoStack.length > 0,
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
      if (path !== "/api/v1/commands/apply-operations") {
        fail(404, "NOT_FOUND");
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
