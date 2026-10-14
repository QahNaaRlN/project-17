import { createStore } from "zustand/vanilla";
import { documentHash } from "@cms/runtime";
import type { IrDocument } from "@cms/ir";
import {
  StudioError,
  type Api,
  type Operation,
  type Rect,
  type Session,
  type WorkflowAction,
  type Publication,
  type ReviewDiagnostic,
} from "./types.js";
import { applyOperations } from "./operations.js";
import type { ContentOperation } from "./types.js";
export interface EditorState {
  remotePending: boolean;
  contentRetry: boolean;
  dirtyFields: Record<string, boolean>;
  workflowRetry: boolean;
  workflowDiagnostics: ReviewDiagnostic[];
  publication: Publication | null;
  canvasPick: number;
  historyRevision: number;
  historyRetry: boolean;
  session: Session | null;
  document: IrDocument | null;
  selected: string | null;
  instance: string | null;
  status: "idle" | "loading" | "ready" | "saving" | "uncertain" | "error";
  error: string | null;
  previewError: string | null;
  previewSrc: string;
  connected: boolean;
  mode: "design" | "interact";
  viewMode: "visual" | "structure" | "agent";
  locale: "ru" | "en";
  width: number;
  rects: Record<string, Rect[]>;
}
export function createEditor(
  api: Api,
  options: {
    now?: () => number;
    setTimer?: typeof setTimeout;
    clearTimer?: typeof clearTimeout;
  } = {},
) {
  const store = createStore<EditorState>(() => ({
    remotePending: false,
    contentRetry: false,
    dirtyFields: {},
    workflowRetry: false,
    workflowDiagnostics: [],
    publication: null,
    canvasPick: 0,
    historyRevision: 0,
    historyRetry: false,
    session: null,
    document: null,
    selected: null,
    instance: null,
    status: "idle",
    error: null,
    previewError: null,
    previewSrc: "",
    connected: false,
    mode: "design",
    viewMode: "visual",
    locale: "ru",
    width: 1024,
    rects: {},
  }));
  let lifecycle = new AbortController(),
    epoch = 0,
    frame: { postMessage(message: unknown, target: string): void } | null = null,
    initialized = false,
    counter = 0,
    seq = 0,
    timer: ReturnType<typeof setTimeout> | undefined,
    handshakeTimer: ReturnType<typeof setTimeout> | undefined;
  const now = options.now ?? Date.now,
    setTimer = options.setTimer ?? setTimeout,
    clearTimer = options.clearTimer ?? clearTimeout;
  function send(type: string, payload: Record<string, unknown>) {
    const state = store.getState();
    if (frame && state.session)
      frame.postMessage(
        { cms: "preview", v: 1, type, id: "studio-" + ++counter, payload },
        state.session.config.appOrigin,
      );
  }
  function invalidate() {
    const session = store.getState().session;
    if (session) send("invalidate", { seq: ++seq, objectIds: [session.record.id] });
  }
  function watchHandshake() {
    if (handshakeTimer !== undefined) clearTimer(handshakeTimer);
    if (frame)
      handshakeTimer = setTimer(() => {
        if (!store.getState().connected)
          store.setState({ previewError: "PREVIEW_HANDSHAKE_TIMEOUT" });
      }, 10_000);
  }
  async function preview(session: Session, stamp: number) {
    try {
      const credential = await api.preview(session, lifecycle.signal);
      if (stamp !== epoch || lifecycle.signal.aborted) return;
      const expires = Date.parse(credential.expiresAt),
        remaining = expires - now();
      if (!Number.isFinite(remaining) || remaining <= 0 || remaining > 15 * 60_000 + 5000)
        throw new StudioError("PREVIEW_INVALID");
      const url = new URL("/api/cms/preview", session.config.appOrigin);
      url.searchParams.set("token", credential.token);
      url.searchParams.set("path", session.record.path!);
      initialized = false;
      store.setState({ previewSrc: url.href, connected: false, previewError: null, rects: {} });
      watchHandshake();
      if (timer !== undefined) clearTimer(timer);
      timer = setTimer(
        () => {
          void preview(store.getState().session!, stamp);
        },
        Math.max(1000, remaining - 60_000),
      );
    } catch (error) {
      if (stamp === epoch && !lifecycle.signal.aborted)
        store.setState({
          previewError: error instanceof StudioError ? error.code : "PREVIEW_FAILED",
        });
    }
  }
  let pendingHistory: { session: Session; action: "undo" | "redo"; key: string } | null = null;
  let pendingContent: {
    session: Session;
    operations: ContentOperation[];
    key: string;
    result?: string[];
  } | null = null;
  const contentDirty = () =>
    !!(store.getState().dirtyFields.content || store.getState().dirtyFields.contentUpload);
  const fieldDirty = () =>
    Object.entries(store.getState().dirtyFields).some(
      ([key, dirty]) => dirty && (key.startsWith("property-") || key.startsWith("design-")),
    );
  let pendingWorkflow: {
    session: Session;
    action: WorkflowAction;
    comment: string;
    key: string;
  } | null = null;
  let remoteReading = false;
  function historyRead() {
    return { historyRevision: store.getState().historyRevision + 1 };
  }
  async function open(documentId: string, changesetId: string) {
    if (fieldDirty()) {
      store.setState({ error: "FIELD_DRAFT_PENDING" });
      return;
    }
    if (contentDirty()) {
      store.setState({ error: "CONTENT_DRAFT_PENDING" });
      return;
    }
    if (store.getState().dirtyFields.json) {
      store.setState({ error: "JSON_DRAFT_PENDING" });
      return;
    }
    if (["saving", "uncertain"].includes(store.getState().status)) return;
    lifecycle.abort();
    lifecycle = new AbortController();
    const stamp = ++epoch;
    if (timer !== undefined) clearTimer(timer);
    if (handshakeTimer !== undefined) clearTimer(handshakeTimer);
    initialized = false;
    pendingContent = null;
    store.setState({
      status: "loading",
      remotePending: false,
      dirtyFields: {},
      workflowRetry: false,
      contentRetry: false,
      workflowDiagnostics: [],
      publication: null,
      canvasPick: 0,
      session: null,
      document: null,
      selected: null,
      instance: null,
      previewSrc: "",
      rects: {},
      error: null,
      connected: false,
      previewError: null,
    });
    try {
      const session = await api.open(documentId, changesetId, lifecycle.signal);
      if (stamp !== epoch || lifecycle.signal.aborted) return;
      store.setState({
        ...historyRead(),
        session,
        document: structuredClone(session.record.body),
        selected: session.record.body.root,
        status: "ready",
      });
      await preview(session, stamp);
    } catch (error) {
      if (stamp === epoch && !lifecycle.signal.aborted)
        store.setState({
          status: "error",
          error: error instanceof StudioError ? error.code : "OPEN_FAILED",
        });
    }
  }
  async function refresh(remote = false) {
    const session = store.getState().session;
    if (!session || ["saving", "loading"].includes(store.getState().status)) return;
    if (!remote) store.setState({ status: "loading", error: null });
    const stamp = epoch;
    try {
      const current = await api.open(session.record.id, session.changeset.id, lifecycle.signal);
      if (stamp !== epoch || lifecycle.signal.aborted) return;
      // Input can become dirty while the asynchronous read is in flight.
      if (
        remote &&
        (Object.values(store.getState().dirtyFields).some(Boolean) ||
          store.getState().status !== "ready" ||
          store.getState().session?.changeset.seq !== session.changeset.seq)
      ) {
        store.setState({ remotePending: true });
        return;
      }
      const selected = store.getState().selected;
      store.setState({
        ...historyRead(),
        session: current,
        document: structuredClone(current.record.body),
        status: pendingHistory || pendingWorkflow || pendingContent ? "uncertain" : "ready",
        selected:
          selected && Object.hasOwn(current.record.body.nodes, selected)
            ? selected
            : current.record.body.root,
        instance: null,
      });
      invalidate();
    } catch (error) {
      if (stamp === epoch && !lifecycle.signal.aborted)
        store.setState({
          status: "uncertain",
          error: error instanceof StudioError ? error.code : "REFRESH_FAILED",
        });
    }
  }
  async function refreshRemote() {
    const state = store.getState();
    if (
      remoteReading ||
      !state.remotePending ||
      state.status !== "ready" ||
      Object.values(state.dirtyFields).some(Boolean)
    )
      return;
    remoteReading = true;
    store.setState({ remotePending: false });
    try {
      await refresh(true);
    } finally {
      remoteReading = false;
    }
    if (store.getState().remotePending) void refreshRemote();
  }
  async function apply(operations: Operation[], selection?: string, source?: "json") {
    if (contentDirty()) {
      store.setState({ error: "CONTENT_DRAFT_PENDING" });
      return;
    }
    if (store.getState().dirtyFields.json && source !== "json") {
      store.setState({ error: "JSON_DRAFT_PENDING" });
      return;
    }
    const state = store.getState(),
      session = state.session;
    if (
      !session ||
      !state.document ||
      !session.review.canEdit ||
      state.status !== "ready" ||
      session.changeset.state !== "open"
    )
      return;
    const stamp = epoch,
      before = state.document,
      structural = operations.some((op) =>
        ["node.insert", "node.remove", "node.move"].includes(op.type),
      );
    let next: IrDocument;
    try {
      next = applyOperations(before, operations);
    } catch (error) {
      store.setState({ error: error instanceof StudioError ? error.code : "FIELD_INVALID" });
      return;
    }
    store.setState({
      document: next,
      status: "saving",
      error: null,
      selected: next.nodes[selection ?? state.selected ?? ""]
        ? (selection ?? state.selected)
        : next.root,
      instance: structural ? null : state.instance,
    });
    try {
      // Local values need authenticated reload in the application; design/props can use fast patch.
      if (
        !operations.some((operation) =>
          [
            "localContent.set",
            "localContent.remove",
            "node.insert",
            "node.remove",
            "node.move",
          ].includes(operation.type),
        )
      ) {
        const ids = [...new Set(operations.map((op) => String(op.payload.nodeId)))];
        send("document.patch", {
          documentId: session.record.id,
          baseHash: await documentHash(before),
          hash: await documentHash(next),
          patch: ids.map((id) => ({
            op: "replace",
            path: "/nodes/" + id.replace(/~/g, "~0").replace(/\//g, "~1"),
            value: next.nodes[id],
          })),
        });
      }
      await api.apply(session, operations, crypto.randomUUID(), lifecycle.signal);
      const current = await api.open(session.record.id, session.changeset.id, lifecycle.signal);
      if (stamp !== epoch || lifecycle.signal.aborted) return;
      store.setState({
        ...historyRead(),
        session: current,
        document: structuredClone(current.record.body),
        status: "ready",
        selected: current.record.body.nodes[selection ?? state.selected ?? ""]
          ? (selection ?? state.selected)
          : current.record.body.root,
        instance: structural ? null : state.instance,
      });
      invalidate();
    } catch (error) {
      if (stamp !== epoch || lifecycle.signal.aborted) return;
      const code = error instanceof StudioError ? error.code : "SAVE_UNCERTAIN";
      // Every failed attempt must reread: a disconnected response may have committed.
      store.setState({
        document: before,
        status: "uncertain",
        error: code,
        selected: state.selected,
        instance: structural ? null : state.instance,
      });
      try {
        const current = await api.open(session.record.id, session.changeset.id, lifecycle.signal);
        if (stamp !== epoch || lifecycle.signal.aborted) return;
        store.setState({
          ...historyRead(),
          session: current,
          document: structuredClone(current.record.body),
          status: "ready",
          error: code,
          selected: current.record.body.nodes[state.selected ?? ""]
            ? state.selected
            : current.record.body.root,
          instance: structural ? null : state.instance,
        });
      } catch {
        /* explicit refresh is required before further writes */
      }
      invalidate();
    }
  }
  async function changeHistory(action: "undo" | "redo") {
    if (fieldDirty()) {
      store.setState({ error: "FIELD_DRAFT_PENDING" });
      return;
    }
    if (contentDirty()) {
      store.setState({ error: "CONTENT_DRAFT_PENDING" });
      return;
    }
    if (store.getState().dirtyFields.json) {
      store.setState({ error: "JSON_DRAFT_PENDING" });
      return;
    }
    const state = store.getState(),
      session = state.session;
    if (
      !session ||
      state.status !== "ready" ||
      session.changeset.state !== "open" ||
      !session.history[action === "undo" ? "canUndo" : "canRedo"]
    )
      return;
    pendingHistory = { session, action, key: crypto.randomUUID() };
    await sendHistory();
  }
  async function workflow(action: WorkflowAction, comment = "") {
    const state = store.getState(),
      session = state.session;
    if (
      !session ||
      state.status !== "ready" ||
      Object.values(state.dirtyFields).some(Boolean) ||
      session.review.actions[action] !== null ||
      (action === "request-changes" && (!comment.trim() || [...comment].length > 2000))
    )
      return;
    pendingWorkflow = {
      session: structuredClone(session),
      action,
      comment: comment.trim(),
      key: crypto.randomUUID(),
    };
    await sendWorkflow();
  }
  async function sendWorkflow() {
    const attempt = pendingWorkflow;
    if (
      !attempt ||
      ["saving", "loading"].includes(store.getState().status) ||
      lifecycle.signal.aborted
    )
      return;
    const stamp = epoch;
    store.setState({
      status: "saving",
      error: null,
      workflowRetry: false,
      workflowDiagnostics: [],
    });
    try {
      const publication = await api.workflow(
        attempt.session,
        attempt.action,
        attempt.comment,
        attempt.key,
        lifecycle.signal,
      );
      if (stamp !== epoch || lifecycle.signal.aborted) return;
      pendingWorkflow = null;
      store.setState({ publication });
    } catch (cause) {
      if (stamp !== epoch || lifecycle.signal.aborted) return;
      if (cause instanceof StudioError && !cause.uncertain) pendingWorkflow = null;
      store.setState({
        error: cause instanceof StudioError ? cause.code : "WORKFLOW_UNCERTAIN",
        workflowDiagnostics: cause instanceof StudioError ? cause.diagnostics : [],
      });
    }
    try {
      const current = await api.open(
        attempt.session.record.id,
        attempt.session.changeset.id,
        lifecycle.signal,
      );
      if (stamp !== epoch || lifecycle.signal.aborted) return;
      const selected = store.getState().selected;
      store.setState({
        ...historyRead(),
        session: current,
        document: structuredClone(current.record.body),
        status: pendingWorkflow || pendingContent ? "uncertain" : "ready",
        workflowRetry: !!pendingWorkflow,
        selected:
          selected && Object.hasOwn(current.record.body.nodes, selected)
            ? selected
            : current.record.body.root,
        instance: null,
      });
    } catch {
      if (stamp === epoch && !lifecycle.signal.aborted)
        store.setState({
          status: "uncertain",
          workflowRetry: !!pendingWorkflow,
          error: store.getState().error ?? "REVIEW_READ_FAILED",
        });
    }
    invalidate();
  }
  async function applyContent(operations: ContentOperation[], expectedSeq: number) {
    const state = store.getState(),
      session = state.session;
    if (
      !session ||
      state.status !== "ready" ||
      !session.review.canEdit ||
      session.changeset.state !== "open" ||
      Object.entries(state.dirtyFields).some(([key, value]) => key !== "content" && value)
    )
      return null;
    if (expectedSeq !== session.changeset.seq) {
      store.setState({ error: "CHANGESET_SEQ_CONFLICT" });
      return null;
    }
    if (session.changeset.targets[0] !== session.config.environment) {
      store.setState({ error: "CONTENT_TARGET_MISMATCH" });
      return null;
    }
    if (!operations.length || operations.length > 200) return null;
    pendingContent = {
      session: structuredClone(session),
      operations: structuredClone(operations),
      key: crypto.randomUUID(),
    };
    return sendContent();
  }
  async function sendContent(): Promise<string[] | null> {
    const attempt = pendingContent;
    if (!attempt || store.getState().status === "saving") return null;
    const stamp = epoch;
    store.setState({ status: "saving", error: null, contentRetry: false });
    let result = attempt.result,
      uncertain = false;
    try {
      result ??= await api.applyContent(
        attempt.session,
        attempt.operations,
        attempt.key,
        lifecycle.signal,
      );
      if (stamp !== epoch || lifecycle.signal.aborted) return null;
      attempt.result = result;
    } catch (cause) {
      if (stamp !== epoch || lifecycle.signal.aborted) return null;
      uncertain = !(cause instanceof StudioError) || cause.uncertain;
      if (!uncertain) pendingContent = null;
      store.setState({ error: cause instanceof StudioError ? cause.code : "SAVE_UNCERTAIN" });
    }
    try {
      const current = await api.open(
        attempt.session.record.id,
        attempt.session.changeset.id,
        lifecycle.signal,
      );
      if (stamp !== epoch || lifecycle.signal.aborted) return null;
      if (result) pendingContent = null;
      store.setState({
        ...historyRead(),
        session: current,
        document: structuredClone(current.record.body),
        status: uncertain ? "uncertain" : "ready",
        contentRetry: uncertain,
      });
    } catch {
      if (stamp !== epoch || lifecycle.signal.aborted) return null;
      store.setState({
        status: "uncertain",
        contentRetry: !!pendingContent,
        error: store.getState().error ?? "CONTENT_READ_FAILED",
      });
      return null;
    }
    invalidate();
    return result ?? null;
  }
  async function sendHistory() {
    const attempt = pendingHistory;
    if (!attempt || store.getState().status === "saving") return;
    const stamp = epoch;
    store.setState({ status: "saving", error: null, historyRetry: false });
    let uncertain = false;
    try {
      await api.changeHistory(attempt.session, attempt.action, attempt.key, lifecycle.signal);
      pendingHistory = null;
    } catch (cause) {
      if (stamp !== epoch || lifecycle.signal.aborted) return;
      uncertain = !(cause instanceof StudioError) || cause.uncertain;
      if (!uncertain) pendingHistory = null;
      store.setState({ error: cause instanceof StudioError ? cause.code : "HISTORY_UNCERTAIN" });
    }
    try {
      const current = await api.open(
        attempt.session.record.id,
        attempt.session.changeset.id,
        lifecycle.signal,
      );
      if (stamp !== epoch || lifecycle.signal.aborted) return;
      const selected = store.getState().selected;
      store.setState({
        ...historyRead(),
        session: current,
        document: structuredClone(current.record.body),
        status: uncertain ? "uncertain" : "ready",
        historyRetry: uncertain,
        selected:
          selected && Object.hasOwn(current.record.body.nodes, selected)
            ? selected
            : current.record.body.root,
        instance: null,
      });
    } catch {
      if (stamp === epoch && !lifecycle.signal.aborted)
        store.setState({
          status: "uncertain",
          historyRetry: !!pendingHistory,
          error: store.getState().error ?? "HISTORY_READ_FAILED",
        });
    }
    invalidate();
  }
  function select(nodeId: string, instance: string | null = null, source?: "json") {
    if (nodeId !== store.getState().selected && fieldDirty()) {
      store.setState({ error: "FIELD_DRAFT_PENDING" });
      return;
    }
    if (
      store.getState().dirtyFields.json &&
      source !== "json" &&
      nodeId !== store.getState().selected
    ) {
      store.setState({ error: "JSON_DRAFT_PENDING" });
      return;
    }
    if (!store.getState().document || !Object.hasOwn(store.getState().document!.nodes, nodeId))
      return;
    store.setState({ selected: nodeId, instance });
    send("select", { nodeId });
  }
  function receive(event: { origin: string; source: unknown; data: unknown }) {
    const state = store.getState(),
      session = state.session;
    if (!session || !frame || event.source !== frame || event.origin !== session.config.appOrigin)
      return;
    try {
      const data = event.data as {
        cms?: unknown;
        v?: unknown;
        type?: unknown;
        payload?: Record<string, unknown>;
      };
      if (
        !data ||
        data.cms !== "preview" ||
        data.v !== 1 ||
        !data.payload ||
        JSON.stringify(data).length > 256 * 1024
      )
        return;
      const p = data.payload;
      if (data.type === "ready") {
        if (p.documentId !== session.record.id || p.protocolVersion !== 1) {
          store.setState({ previewError: "PREVIEW_DOCUMENT_MISMATCH", connected: false });
          return;
        }
        if (!initialized) {
          initialized = true;
          send("init", {
            sessionId: "studio-" + epoch,
            changesetId: session.changeset.id,
            locale: state.locale,
            mode: state.mode,
          });
        } else {
          if (handshakeTimer !== undefined) clearTimer(handshakeTimer);
          store.setState({ connected: true, previewError: null });
          send("select", { nodeId: state.selected });
          invalidate();
        }
      } else if (initialized && data.type === "node.click" && typeof p.nodeId === "string") {
        select(
          p.nodeId,
          typeof p.instance === "string" && p.instance.length <= 2000 ? p.instance : null,
        );
        if (
          state.mode === "design" &&
          state.document &&
          Object.hasOwn(state.document.nodes, p.nodeId)
        )
          store.setState({ canvasPick: state.canvasPick + 1 });
      } else if (
        initialized &&
        data.type === "layout" &&
        p.nodes &&
        typeof p.nodes === "object" &&
        !Array.isArray(p.nodes)
      ) {
        const rects: Record<string, Rect[]> = {};
        for (const [id, items] of Object.entries(p.nodes)) {
          if (
            !Object.hasOwn(state.document!.nodes, id) ||
            !Array.isArray(items) ||
            items.length > 10000
          )
            continue;
          const valid = items.filter(
            (r: Rect) =>
              r &&
              [r.x, r.y, r.w, r.h].every(
                (n) => typeof n === "number" && Number.isFinite(n) && Math.abs(n) <= 1e7,
              ) &&
              r.w >= 0 &&
              r.h >= 0 &&
              (r.instance === undefined ||
                (typeof r.instance === "string" && r.instance.length <= 2000)),
          );
          rects[id] = valid;
        }
        store.setState({ rects });
      } else if (initialized && data.type === "error")
        store.setState({
          previewError:
            typeof p.code === "string" && /^[A-Z0-9_]{1,80}$/.test(p.code)
              ? p.code
              : "PREVIEW_FAILED",
        });
    } catch {
      /* Invalid cross-window messages do not affect the editor. */
    }
  }
  return {
    remoteChanged() {
      store.setState({ remotePending: true });
    },
    refreshRemote,
    formDirty(id: string, dirty: boolean) {
      const current = store.getState().dirtyFields;
      if (!!current[id] !== dirty) store.setState({ dirtyFields: { ...current, [id]: dirty } });
    },
    workflow,
    retryWorkflow: sendWorkflow,
    changeHistory,
    retryHistory: sendHistory,
    store,
    open,
    apply,
    applyContent,
    retryContent: sendContent,
    refresh,
    select,
    receive,
    bindFrame(value: typeof frame) {
      if (frame === value) return;
      frame = value;
      initialized = false;
      store.setState({ connected: false, rects: {} });
      watchHandshake();
    },
    mode(mode: EditorState["mode"]) {
      store.setState({ mode });
      send("mode", { mode });
    },
    viewMode(viewMode: EditorState["viewMode"]) {
      if (fieldDirty()) {
        store.setState({ error: "FIELD_DRAFT_PENDING" });
        return;
      }
      if (contentDirty()) {
        store.setState({ error: "CONTENT_DRAFT_PENDING" });
        return;
      }
      if (store.getState().dirtyFields.json) {
        store.setState({ error: "JSON_DRAFT_PENDING" });
        return;
      }
      store.setState({ viewMode });
    },
    locale(locale: EditorState["locale"]) {
      store.setState({ locale });
      if (store.getState().session) {
        initialized = false;
        send("init", {
          sessionId: "studio-" + epoch,
          changesetId: store.getState().session!.changeset.id,
          locale,
          mode: store.getState().mode,
        });
        initialized = true;
      }
    },
    width(width: number) {
      if ([375, 768, 1024, 1440].includes(width)) store.setState({ width });
    },
    retryPreview() {
      const session = store.getState().session;
      if (session) return preview(session, epoch);
      return Promise.resolve();
    },
    error(code: string) {
      store.setState({ error: code });
    },
    dispose() {
      ++epoch;
      lifecycle.abort();
      if (timer !== undefined) clearTimer(timer);
      if (handshakeTimer !== undefined) clearTimer(handshakeTimer);
      frame = null;
    },
  };
}
export type Editor = ReturnType<typeof createEditor>;
