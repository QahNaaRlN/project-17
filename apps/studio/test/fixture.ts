import { vi } from "vitest";
import type { Api, Operation, Session } from "../src/types.js";
import { structuralApply } from "../src/structure.js";
export const documentId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11",
  changesetId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c22";
export function fixture(): Session {
  const session: Omit<Session, "review"> = {
    history: {
      seq: 1,
      items: [],
      nextBeforeSeq: null,
      canUndo: false,
      canRedo: false,
      undoReason: "NOTHING_TO_UNDO",
      redoReason: "NOTHING_TO_REDO",
    },
    config: { project: "store", environment: "staging", appOrigin: "http://127.0.0.1:18083" },
    changeset: {
      id: changesetId,
      title: "Test draft",
      seq: 1,
      state: "open",
      kind: "standard",
      targets: ["staging"],
    },
    record: {
      id: documentId,
      kind: "page",
      path: "/store",
      body: {
        irVersion: "1.0",
        kind: "page",
        root: "n_root",
        nodes: {
          n_root: {
            id: "n_root",
            type: "Box",
            children: ["n_heading", "n_custom"],
            slots: { footer: ["n_footer"] },
          },
          n_heading: { id: "n_heading", type: "Heading", props: { level: 2 } },
          n_custom: {
            id: "n_custom",
            type: "Custom",
            props: { enabled: false, title: "original" },
          },
          n_footer: { id: "n_footer", type: "Text", bindings: { text: "$content.title" } },
        },
      },
    },
    manifestHash: "sha256:" + "a".repeat(64),
    manifest: {
      manifestVersion: "1.0",
      app: { id: "test", version: "1", framework: "react" },
      irVersions: ["1.0"],
      breakpoints: { sm: 640, md: 768 },
      tokens: { spacing: { sm: "12px" } },
      components: {
        Custom: {
          origin: "native",
          container: false,
          props: {
            enabled: { type: "boolean" },
            title: { type: "string", minLength: 1, maxLength: 10 },
            count: { type: "number", min: 0, max: 10 },
            choice: { type: "enum", values: ["one", "two"] },
            object: { type: "object", fields: {} },
            image: { type: "asset", content: true },
          },
        },
      },
    },
  };
  return {
    ...session,
    review: {
      canEdit: true,
      needsAttention: false,
      changeset: session.changeset,
      risk: null,
      requiredApprovals: 0,
      approvals: 0,
      checks: [],
      history: [],
      objects: [{ id: documentId, kind: "page", path: "/store", deleted: false }],
      actions: {
        submit: null,
        reopen: null,
        approve: null,
        "request-changes": null,
        publish: null,
      },
    },
  };
}
export function persist(session: Session, operations: Operation[]) {
  for (const operation of operations) {
    const p = operation.payload;
    if (operation.type === "localContent.set")
      (session.record.body.localContent ??= {})[String(p.key)] = {
        type: "text",
        value: p.locale ? { [String(p.locale)]: p.value } : (p.value as Record<string, unknown>),
      };
    else if (["node.insert", "node.remove", "node.move"].includes(operation.type))
      structuralApply(session.record.body, operation);
    else if (operation.type === "node.setBinding")
      (session.record.body.nodes[String(p.nodeId)]!.bindings ??= {})[String(p.prop)] =
        p.binding as string;
    else
      Object.assign(
        (session.record.body.nodes[String(p.nodeId)]![
          operation.type === "node.setDesign" ? "design" : "props"
        ] ??= {}),
        p.set,
      );
  }
  session.changeset.seq += operations.length;
  session.history.seq = session.changeset.seq;
}
export function mockApi(session = fixture()) {
  const api: Api = {
    account: vi.fn(async () => ({ authenticated: false, rights: [], hasAccess: false })),
    logout: vi.fn(async () => {}),
    workItems: vi.fn(async () => ({ items: [], nextCursor: null })),
    workflow: vi.fn<Api["workflow"]>(async (_session, action) => {
      session.changeset.state = {
        submit: "approved",
        reopen: "open",
        approve: "approved",
        "request-changes": "changes_requested",
        publish: "merged",
      }[action];
      return action === "publish"
        ? {
            id: documentId,
            changesetId,
            environment: "staging",
            createdAt: new Date().toISOString(),
          }
        : null;
    }),
    history: vi.fn(async () => structuredClone(session.history)),
    changeHistory: vi.fn(async () => {}),
    pages: vi.fn(async () => ({
      items: [{ id: documentId, path: "/store", title: "Store", hasHead: true }],
      nextCursor: null,
    })),
    drafts: vi.fn(async () => ({ items: [session.changeset], nextCursor: null })),
    createDraft: vi.fn(async () => session.changeset),
    config: vi.fn(async () => session.config),
    open: vi.fn(async () => structuredClone(session)),
    apply: vi.fn(async (_session, operations) => {
      persist(session, operations);
    }),
    preview: vi.fn(async () => ({
      token: "transient-preview-jwt",
      expiresAt: new Date(Date.now() + 900_000).toISOString(),
    })),
  };
  return { api, session };
}
