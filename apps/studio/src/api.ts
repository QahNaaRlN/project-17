import { validateDocument } from "@cms/ir";
import {
  StudioError,
  type Api,
  type Changeset,
  type Config,
  type DocumentRecord,
  type Session,
  type List,
  type PageItem,
  type History,
} from "./types.js";
import { validateReview, problemDiagnostics, safeDiagnostics } from "./review.js";
import type { Review, Publication } from "./types.js";
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i;
export function createApi(fetcher: typeof fetch = fetch): Api {
  async function request<T>(
    path: string,
    signal?: AbortSignal,
    body?: unknown,
    key?: string,
  ): Promise<T> {
    let response: Response;
    try {
      response = await fetcher("/api/studio" + path, {
        method: body === undefined ? "GET" : "POST",
        credentials: "same-origin",
        cache: "no-store",
        ...(signal ? { signal } : {}),
        ...(body === undefined
          ? {}
          : {
              headers: { "Content-Type": "application/json", "Idempotency-Key": key! },
              body: JSON.stringify(body),
            }),
      });
    } catch {
      throw new StudioError("NETWORK_UNCERTAIN", body !== undefined);
    }
    if (!response.ok) {
      let code = "CMS_REQUEST_FAILED";
      let diagnostics: StudioError["diagnostics"] = [];
      try {
        const value = await response.json();
        if (typeof value.code === "string" && /^[A-Z0-9_]{1,80}$/.test(value.code))
          code = value.code;
        diagnostics = [...problemDiagnostics(value.params), ...safeDiagnostics(value.diagnostics)];
      } catch {
        /* no server detail/secrets in UI */
      }
      throw new StudioError(code, body !== undefined && response.status >= 500, diagnostics);
    }
    try {
      return (await response.json()) as T;
    } catch {
      throw new StudioError("RESPONSE_INVALID", body !== undefined);
    }
  }
  const api: Api = {
    async workflow(session, action, comment, key, signal) {
      const commands = {
        submit: "submit-changeset",
        reopen: "reopen-changeset",
        approve: "approve-changeset",
        "request-changes": "request-changes",
        publish: "publish",
      };
      const result = await request<{ result: Partial<Publication> & { changeset?: Changeset } }>(
        `/api/v1/commands/${commands[action]}`,
        signal,
        {
          payload: {
            changesetId: session.changeset.id,
            expectedSeq: session.changeset.seq,
            ...(action === "publish" ? { environment: session.config.environment } : {}),
            ...(action === "request-changes" ? { comment } : {}),
          },
          reason: `Studio: ${action} Change Set`,
        },
        key,
      );
      if (action !== "publish") {
        const cs = result?.result?.changeset;
        const states = {
          submit: ["failed", "in_review", "approved"],
          reopen: ["open"],
          approve: ["in_review", "approved"],
          "request-changes": ["changes_requested"],
        };
        if (
          !cs ||
          cs.id !== session.changeset.id ||
          cs.seq !== session.changeset.seq ||
          !states[action].includes(cs.state)
        )
          throw new StudioError("RESPONSE_INVALID", true);
        return null;
      }
      const p = result?.result;
      if (
        !p ||
        typeof p.id !== "string" ||
        !uuid.test(p.id) ||
        p.changesetId !== session.changeset.id ||
        p.environment !== session.config.environment ||
        typeof p.createdAt !== "string" ||
        !Number.isFinite(Date.parse(p.createdAt))
      )
        throw new StudioError("RESPONSE_INVALID", true);
      return {
        id: p.id,
        changesetId: p.changesetId,
        environment: p.environment,
        createdAt: p.createdAt,
      };
    },
    async history(session, beforeSeq, signal) {
      return readHistory(session.changeset.id, session.changeset.seq, beforeSeq, signal);
    },
    async changeHistory(session, action, key, signal) {
      await request(
        `/api/v1/commands/${action}`,
        signal,
        {
          payload: { changesetId: session.changeset.id, expectedSeq: session.changeset.seq },
          reason: `Studio: ${action} action`,
        },
        key,
      );
    },
    async pages(cursor, signal) {
      const config = await api.config(signal);
      const value = await listing<PageItem>(
        "/api/v1/pages",
        { environment: config.environment },
        cursor,
        signal,
      );
      if (
        value.items.some(
          (p) =>
            !p ||
            !uuid.test(p.id) ||
            typeof p.path !== "string" ||
            !p.path.startsWith("/") ||
            p.path.startsWith("//") ||
            typeof p.title !== "string" ||
            typeof p.hasHead !== "boolean",
        )
      )
        throw new StudioError("RESPONSE_INVALID");
      return value;
    },
    async drafts(documentId, cursor, signal) {
      if (!uuid.test(documentId)) throw new StudioError("IDENTIFIER_INVALID");
      const config = await api.config(signal);
      const value = await listing<Changeset>(
        "/api/v1/working-drafts",
        { environment: config.environment, documentId },
        cursor,
        signal,
      );
      if (
        value.items.some(
          (d) =>
            !d ||
            !uuid.test(d.id) ||
            typeof d.title !== "string" ||
            d.state !== "open" ||
            d.kind !== "standard" ||
            !Array.isArray(d.targets) ||
            !d.targets.includes(config.environment),
        )
      )
        throw new StudioError("RESPONSE_INVALID");
      return value;
    },
    async createDraft(title, key) {
      title = title.trim();
      if ([...title].length < 1 || [...title].length > 200) throw new StudioError("TITLE_INVALID");
      const config = await api.config();
      const response = await request<{ result: Changeset }>(
        "/api/v1/commands/create-changeset",
        undefined,
        { payload: { title, targets: [config.environment] }, reason: "Studio: create page draft" },
        key,
      );
      if (
        !uuid.test(response?.result?.id) ||
        response.result.kind !== "standard" ||
        response.result.state !== "open" ||
        !Array.isArray(response.result.targets) ||
        !response.result.targets.includes(config.environment)
      )
        throw new StudioError("RESPONSE_INVALID", true);
      return response.result;
    },
    config: (signal) => request<Config>("/config", signal),
    async open(documentId, changesetId, signal) {
      if (!uuid.test(documentId) || !uuid.test(changesetId))
        throw new StudioError("IDENTIFIER_INVALID");
      const config = await api.config(signal);
      const [changeset, record, active, history, rawReview] = await Promise.all([
        request<Changeset>(`/api/v1/changesets/${changesetId}`, signal),
        request<DocumentRecord>(
          `/api/v1/documents/${documentId}?changesetId=${changesetId}`,
          signal,
        ),
        request<{ manifest: Session["manifest"]; manifestHash: string }>(
          `/api/v1/manifest?environment=${encodeURIComponent(config.environment)}`,
          signal,
        ),
        readHistory(changesetId, undefined, undefined, signal),
        request<Review>(`/api/v1/changesets/${changesetId}/review`, signal),
      ]);
      if (
        record?.id !== documentId ||
        changeset?.id !== changesetId ||
        changeset.kind !== "standard" ||
        !Array.isArray(changeset.targets) ||
        !changeset.targets.includes(config.environment) ||
        !Number.isSafeInteger(changeset.seq) ||
        changeset.seq < 0 ||
        !validateDocument(record.body).valid ||
        record.kind !== "page" ||
        typeof record.path !== "string" ||
        !record.path.startsWith("/") ||
        record.path.startsWith("//") ||
        !/^sha256:[a-f0-9]{64}$/.test(active?.manifestHash) ||
        active.manifest?.manifestVersion !== "1.0" ||
        !active.manifest.tokens ||
        !active.manifest.breakpoints
      )
        throw new StudioError("SESSION_INVALID");
      if (history.seq !== changeset.seq) throw new StudioError("CHANGESET_SEQ_CONFLICT");
      const review = validateReview(rawReview, changesetId);
      if (review.changeset.seq !== changeset.seq || review.changeset.state !== changeset.state)
        throw new StudioError("CHANGESET_SEQ_CONFLICT");
      // Query API reads are separate requests; reject a snapshot crossing a write.
      const after = await request<Changeset>(`/api/v1/changesets/${changesetId}`, signal);
      if (
        after.id !== changeset.id ||
        after.seq !== changeset.seq ||
        after.state !== changeset.state
      )
        throw new StudioError("CHANGESET_SEQ_CONFLICT");
      return { config, changeset, record, history, review, ...active };
    },
    async apply(session, operations, key, signal) {
      await request(
        "/api/v1/commands/apply-operations",
        signal,
        {
          payload: {
            changesetId: session.changeset.id,
            expectedSeq: session.changeset.seq,
            operations,
            group: true,
          },
          reason: "Studio: edit page draft",
        },
        key,
      );
    },
    async preview(session, signal) {
      const result = await request<{ result: { token: string; expiresAt: string } }>(
        "/api/v1/commands/create-preview-token",
        signal,
        {
          payload: { environment: session.config.environment, changesetId: session.changeset.id },
          reason: "Studio: preview page draft",
        },
        crypto.randomUUID(),
      );
      if (
        typeof result.result.token !== "string" ||
        !Number.isFinite(Date.parse(result.result.expiresAt))
      )
        throw new StudioError("PREVIEW_INVALID");
      return result.result;
    },
  };
  async function readHistory(
    id: string,
    expected?: number,
    before?: number,
    signal?: AbortSignal,
  ): Promise<History> {
    if (before !== undefined && (!Number.isSafeInteger(before) || before < 1))
      throw new StudioError("PARAM_INVALID");
    const data = await request<History>(
      `/api/v1/changesets/${id}/history${before === undefined ? "" : `?beforeSeq=${before}`}`,
      signal,
    );
    const reason = (value: unknown) =>
      value === null || (typeof value === "string" && /^[A-Z0-9_]{1,80}$/.test(value));
    if (
      !data ||
      !Number.isSafeInteger(data.seq) ||
      data.seq < 0 ||
      !Array.isArray(data.items) ||
      typeof data.canUndo !== "boolean" ||
      typeof data.canRedo !== "boolean" ||
      !reason(data.undoReason) ||
      !reason(data.redoReason) ||
      data.canUndo !== (data.undoReason === null) ||
      data.canRedo !== (data.redoReason === null) ||
      data.items.length > 20 ||
      !(
        data.nextBeforeSeq === null ||
        (Number.isSafeInteger(data.nextBeforeSeq) && data.nextBeforeSeq > 0)
      ) ||
      data.items.some(
        (item, index) =>
          !item ||
          typeof item.id !== "string" ||
          !uuid.test(item.id) ||
          typeof item.actorId !== "string" ||
          !uuid.test(item.actorId) ||
          !Number.isSafeInteger(item.firstSeq) ||
          item.firstSeq < 1 ||
          !Number.isSafeInteger(item.lastSeq) ||
          item.lastSeq < item.firstSeq ||
          item.lastSeq > data.seq ||
          (before !== undefined && item.lastSeq >= before) ||
          (index > 0 && item.lastSeq >= data.items[index - 1]!.firstSeq) ||
          typeof item.actorName !== "string" ||
          typeof item.createdAt !== "string" ||
          !Number.isFinite(Date.parse(item.createdAt)) ||
          !["edit", "undo", "redo"].includes(item.kind) ||
          !Array.isArray(item.operations) ||
          item.operations.length === 0 ||
          item.operations.some(
            (op) =>
              !op ||
              typeof op.type !== "string" ||
              !(op.target === null || (typeof op.target === "string" && uuid.test(op.target))) ||
              !["applied", "conflict", "dropped"].includes(op.status),
          ),
      )
    )
      throw new StudioError("HISTORY_INVALID");
    if (
      data.nextBeforeSeq !== null &&
      (data.items.length === 0 ||
        data.nextBeforeSeq !== data.items.at(-1)?.lastSeq ||
        (before !== undefined && data.nextBeforeSeq >= before))
    )
      throw new StudioError("HISTORY_INVALID");
    if (expected !== undefined && data.seq !== expected)
      throw new StudioError("CHANGESET_SEQ_CONFLICT");
    return data;
  }
  async function listing<T>(
    path: string,
    params: Record<string, string>,
    cursor?: string,
    signal?: AbortSignal,
  ): Promise<List<T>> {
    if (cursor !== undefined && !uuid.test(cursor)) throw new StudioError("IDENTIFIER_INVALID");
    const query = new URLSearchParams(params);
    if (cursor) query.set("after", cursor);
    const value = await request<List<T>>(`${path}?${query}`, signal);
    if (
      !Array.isArray(value?.items) ||
      !(
        value.nextCursor === null ||
        (typeof value.nextCursor === "string" && uuid.test(value.nextCursor))
      )
    )
      throw new StudioError("RESPONSE_INVALID");
    return value;
  }
  return api;
}
