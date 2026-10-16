import type { IrDocument } from "@cms/ir";
import { applyDocumentPatch, documentHash } from "@cms/runtime";
import { previewOrigin, previewPath } from "./preview-origin.js";
export interface PreviewSnapshot {
  documentId: string;
  document: IrDocument;
  hash: string;
  path: string;
}
export type PreviewMode = "design" | "interact";
export interface PreviewMessage {
  cms: "preview";
  v: 1;
  type: string;
  id: string;
  payload: Record<string, unknown>;
}
export interface PreviewControllerOptions {
  studioOrigin: string;
  parent: unknown;
  initial: PreviewSnapshot;
  manifestHash: string;
  changesetId?: string;
  locale?: string;
  send: (message: PreviewMessage, origin: string) => void;
  reload: (signal: AbortSignal, locale: string) => Promise<PreviewSnapshot>;
  render: (snapshot: PreviewSnapshot) => Promise<void>;
  onMode?: (mode: PreviewMode) => void;
  /** Synchronous gate for application-owned native events during plan transitions. */
  onReady?: (ready: boolean) => void;
  onSelect?: (node: string | null) => void;
  onHover?: (node: string | null) => void;
  onZones?: (enabled: boolean) => void;
  navigate?: (path: string) => Promise<void> | void;
  refreshToken?: (signal: AbortSignal) => Promise<void>;
  now?: () => number;
}
function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
function safeJson(input: unknown): unknown {
  // postMessage uses structured clone; reject remaining non-JSON values/accessors before callbacks.
  const seen = new Set<object>();
  let count = 0;
  function copy(value: unknown, depth: number): unknown {
    if (++count > 50_000 || depth > 32) throw new Error("CMS_PREVIEW_MESSAGE_INVALID");
    if (
      value === null ||
      typeof value === "string" ||
      typeof value === "boolean" ||
      (typeof value === "number" && Number.isFinite(value))
    )
      return value;
    if (typeof value !== "object" || seen.has(value))
      throw new Error("CMS_PREVIEW_MESSAGE_INVALID");
    if (
      Array.isArray(value)
        ? Object.getPrototypeOf(value) !== Array.prototype
        : ![null, Object.prototype].includes(Object.getPrototypeOf(value))
    )
      throw new Error("CMS_PREVIEW_MESSAGE_INVALID");
    const descriptors = Object.getOwnPropertyDescriptors(value);
    if (
      Reflect.ownKeys(value).some(
        (key) =>
          typeof key !== "string" ||
          !Object.hasOwn(descriptors[key]!, "value") ||
          ["__proto__", "constructor", "prototype"].includes(key as string),
      )
    )
      throw new Error("CMS_PREVIEW_MESSAGE_INVALID");
    seen.add(value);
    const result = Array.isArray(value)
      ? Array.from({ length: value.length }, (_, i) => copy(descriptors[i]?.value, depth + 1))
      : Object.fromEntries(
          Object.entries(value).map(([key, item]) => [key, copy(item, depth + 1)]),
        );
    seen.delete(value);
    return result;
  }
  const result = copy(input, 0);
  if (new TextEncoder().encode(JSON.stringify(result)).length > 256 * 1024)
    throw new Error("CMS_PREVIEW_MESSAGE_INVALID");
  return result;
}
function text(input: unknown, limit = 128): input is string {
  return typeof input === "string" && input.length > 0 && input.length <= limit;
}
const hash = (input: unknown) => typeof input === "string" && /^sha256:[a-f0-9]{64}$/.test(input);
async function checked(snapshot: PreviewSnapshot): Promise<PreviewSnapshot> {
  if (
    !text(snapshot.documentId) ||
    !hash(snapshot.hash) ||
    previewPath(snapshot.path) !== snapshot.path ||
    (await documentHash(snapshot.document)) !== snapshot.hash
  )
    throw new Error("CMS_PREVIEW_DOCUMENT_INVALID");
  return structuredClone(snapshot);
}
export async function createPreviewController(options: PreviewControllerOptions) {
  const origin = previewOrigin(options.studioOrigin);
  if (!hash(options.manifestHash)) throw new Error("CMS_PREVIEW_MANIFEST_INVALID");
  let snapshot = await checked(options.initial),
    mode: PreviewMode = "design",
    initialized = false,
    disposed = false;
  let locale = options.locale ?? "ru",
    requestedLocale = options.locale ?? "ru",
    revision = 0,
    contextReloadNeeded = false,
    sequence = -1,
    requestedSequence = -1,
    pending = 0,
    serial = Promise.resolve(),
    counter = 0;
  let activeReload: AbortController | undefined;
  let fresh = true;
  function freshness(value: boolean) {
    fresh = value;
    options.onReady?.(value);
  }
  const lifecycle = new AbortController(),
    now = options.now ?? Date.now;
  function emit(type: string, payload: Record<string, unknown>) {
    if (!disposed)
      options.send({ cms: "preview", v: 1, type, id: "app-" + ++counter, payload }, origin);
  }
  function ready() {
    emit("ready", {
      protocolVersion: 1,
      sdkVersion: "0.1.0",
      manifestHash: options.manifestHash,
      path: snapshot.path,
      documentId: snapshot.documentId,
    });
  }
  async function commit(next: PreviewSnapshot, expectedRevision = revision) {
    const value = await checked(next);
    if (disposed || expectedRevision !== revision) return;
    const started = now();
    await options.render(value);
    if (disposed || expectedRevision !== revision) return;
    snapshot = value;
    freshness(true);
    emit("rendered", { hash: snapshot.hash, durationMs: Math.max(0, now() - started) });
  }
  async function reload() {
    freshness(false);
    const controller = new AbortController(),
      expectedRevision = revision;
    activeReload = controller;
    try {
      const next = await options.reload(
        AbortSignal.any([lifecycle.signal, controller.signal]),
        locale,
      );
      if (disposed || controller.signal.aborted || expectedRevision !== revision) return false;
      await commit(next, expectedRevision);
      return true;
    } catch (cause) {
      if (disposed || controller.signal.aborted || expectedRevision !== revision) return false;
      throw cause;
    } finally {
      if (activeReload === controller) activeReload = undefined;
    }
  }
  function node(input: unknown): input is string | null {
    return (
      input === null || (typeof input === "string" && Object.hasOwn(snapshot.document.nodes, input))
    );
  }
  async function apply(data: unknown, expectedRevision: number) {
    let message: Record<string, unknown>;
    try {
      const value = safeJson(data);
      if (!record(value)) throw new Error();
      message = value;
    } catch {
      emit("error", { code: "CMS_PREVIEW_MESSAGE_INVALID" });
      return;
    }
    if (message.cms !== "preview") return;
    if (message.v !== 1) {
      emit("error", { code: "CMS_PREVIEW_PROTOCOL_UNSUPPORTED" });
      return;
    }
    if (!text(message.id) || !text(message.type) || !record(message.payload)) {
      emit("error", { code: "CMS_PREVIEW_MESSAGE_INVALID" });
      return;
    }
    const payload = message.payload;
    try {
      if (message.type === "init") {
        if (
          !text(payload.sessionId) ||
          (payload.changesetId ?? undefined) !== options.changesetId ||
          !text(payload.locale, 64) ||
          !["design", "interact"].includes(String(payload.mode))
        )
          throw new Error("CMS_PREVIEW_INIT_INVALID");
        initialized = true;
        mode = payload.mode as PreviewMode;
        options.onMode?.(mode);
        if (locale !== payload.locale || contextReloadNeeded) {
          locale = payload.locale;
          if (await reload()) contextReloadNeeded = false;
        }
        if (expectedRevision === revision) ready();
        return;
      }
      if (!initialized) throw new Error("CMS_PREVIEW_INIT_REQUIRED");
      switch (message.type) {
        case "mode":
          if (payload.mode !== "design" && payload.mode !== "interact")
            throw new Error("CMS_PREVIEW_MESSAGE_INVALID");
          mode = payload.mode;
          options.onMode?.(mode);
          break;
        case "select":
        case "hover":
          if (!node(payload.nodeId)) throw new Error("CMS_PREVIEW_MESSAGE_INVALID");
          if (message.type === "select") options.onSelect?.(payload.nodeId);
          else options.onHover?.(payload.nodeId);
          break;
        case "zones.show":
          if (typeof payload.enabled !== "boolean") throw new Error("CMS_PREVIEW_MESSAGE_INVALID");
          options.onZones?.(payload.enabled);
          break;
        case "navigate":
          if (!options.navigate) throw new Error("CMS_PREVIEW_NAVIGATE_UNAVAILABLE");
          await options.navigate(previewPath(payload.path));
          break;
        case "token.refresh":
          if (!options.refreshToken) throw new Error("CMS_PREVIEW_REFRESH_UNAVAILABLE");
          await options.refreshToken(lifecycle.signal);
          if (!disposed) await reload();
          break;
        case "invalidate":
          if (
            !Number.isSafeInteger(payload.seq) ||
            Number(payload.seq) < 0 ||
            !Array.isArray(payload.objectIds) ||
            payload.objectIds.length > 5000 ||
            !payload.objectIds.every((id) => text(id))
          )
            throw new Error("CMS_PREVIEW_MESSAGE_INVALID");
          if (Number(payload.seq) <= sequence) break;
          if (await reload()) sequence = Number(payload.seq);
          break;
        case "document.patch": {
          if (
            payload.documentId !== snapshot.documentId ||
            !hash(payload.baseHash) ||
            !hash(payload.hash)
          )
            throw new Error("CMS_PREVIEW_MESSAGE_INVALID");
          let next: PreviewSnapshot | undefined;
          if (payload.baseHash === snapshot.hash) {
            try {
              const document = applyDocumentPatch(snapshot.document, payload.patch);
              const sourcesChanged =
                JSON.stringify(document.dataSources) !==
                  JSON.stringify(snapshot.document.dataSources) ||
                JSON.stringify(document.localContent) !==
                  JSON.stringify(snapshot.document.localContent);
              if (!sourcesChanged && (await documentHash(document)) === payload.hash)
                next = { ...snapshot, document, hash: payload.hash as string };
            } catch {
              /* Reload the authenticated draft instead. */
            }
          }
          if (next) await commit(next);
          else await reload();
          break;
        }
        default:
          throw new Error("CMS_PREVIEW_MESSAGE_UNKNOWN");
      }
    } catch (cause) {
      const code =
        cause instanceof Error &&
        /^CMS_PREVIEW_(INIT_INVALID|INIT_REQUIRED|MESSAGE_INVALID|MESSAGE_UNKNOWN|NAVIGATE_UNAVAILABLE|REFRESH_UNAVAILABLE|DOCUMENT_INVALID)$/.test(
          cause.message,
        )
          ? cause.message
          : "CMS_PREVIEW_UPDATE_FAILED";
      emit("error", { code });
    }
  }
  freshness(true);
  ready();
  emit("rendered", { hash: snapshot.hash, durationMs: 0 });
  return {
    get snapshot() {
      return structuredClone(snapshot);
    },
    get mode() {
      return mode;
    },
    get interactive() {
      return !disposed && fresh && initialized && mode === "interact";
    },
    receive(event: { origin: string; source: unknown; data: unknown }): Promise<void> {
      if (disposed || event.origin !== origin || event.source !== options.parent || pending >= 32)
        return Promise.resolve();
      // Cancel immediately on a valid new locale, before the serial queue reaches init.
      // Invalid/foreign messages cannot interrupt a legitimate load.
      try {
        const data = safeJson(event.data);
        if (
          initialized &&
          record(data) &&
          data.cms === "preview" &&
          data.v === 1 &&
          text(data.id) &&
          record(data.payload)
        ) {
          const p = data.payload;
          const invalidation =
            data.type === "invalidate" &&
            Number.isSafeInteger(p.seq) &&
            Number(p.seq) > Math.max(sequence, requestedSequence) &&
            Array.isArray(p.objectIds) &&
            p.objectIds.length <= 5000 &&
            p.objectIds.every((id) => text(id));
          const patch =
            data.type === "document.patch" &&
            p.documentId === snapshot.documentId &&
            hash(p.baseHash) &&
            hash(p.hash);
          if (
            invalidation ||
            patch ||
            (data.type === "token.refresh" && options.refreshToken) ||
            (data.type === "navigate" &&
              options.navigate &&
              typeof p.path === "string" &&
              previewPath(p.path) === p.path)
          ) {
            if (invalidation) requestedSequence = Number(p.seq);
            revision++;
            activeReload?.abort();
            freshness(false);
          }
        }
        if (
          record(data) &&
          data.cms === "preview" &&
          data.v === 1 &&
          data.type === "init" &&
          text(data.id) &&
          record(data.payload)
        ) {
          const payload = data.payload;
          if (
            text(payload.sessionId) &&
            (payload.changesetId ?? undefined) === options.changesetId &&
            text(payload.locale, 64) &&
            ["design", "interact"].includes(String(payload.mode)) &&
            payload.locale !== requestedLocale
          ) {
            requestedLocale = payload.locale;
            revision++;
            contextReloadNeeded = true;
            freshness(false);
            activeReload?.abort();
          }
        }
      } catch {
        /* The queued validator reports malformed messages. */
      }
      const expectedRevision = revision;
      pending++;
      serial = serial
        .then(async () => {
          if (!disposed && expectedRevision === revision) await apply(event.data, expectedRevision);
        })
        .catch(() => {
          emit("error", { code: "CMS_PREVIEW_UPDATE_FAILED" });
        })
        .finally(() => {
          pending--;
        });
      return serial;
    },
    notify: emit,
    refresh(): Promise<void> {
      if (disposed) return Promise.resolve();
      revision++;
      activeReload?.abort();
      freshness(false);
      const expectedRevision = revision;
      serial = serial
        .then(async () => {
          if (!disposed && expectedRevision === revision) await reload();
        })
        .catch(() => {
          emit("error", { code: "CMS_PREVIEW_UPDATE_FAILED" });
        });
      return serial;
    },
    dispose() {
      disposed = true;
      freshness(false);
      lifecycle.abort();
      activeReload?.abort();
    },
  };
}
export type PreviewController = Awaited<ReturnType<typeof createPreviewController>>;
