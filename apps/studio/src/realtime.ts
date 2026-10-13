import { StudioError, type Api, type Session } from "./types.js";

export interface Presence {
  actorId: string;
  actorName: string;
  changesetId: string;
  changesetTitle: string;
}
export interface LiveEvent {
  type: "invalidate" | "sync" | "presence" | "agent";
  revision?: string;
  items?: Presence[];
}
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i;
export function parseEvent(block: string): LiveEvent | null {
  let kind = "",
    id = "";
  const data: string[] = [];
  for (const line of block.split(/\r?\n/)) {
    const colon = line.indexOf(":");
    if (colon < 0) continue;
    const name = line.slice(0, colon),
      value = line.slice(colon + 1).replace(/^ /, "");
    if (name === "event") kind = value;
    if (name === "id") id = value;
    if (name === "data") data.push(value);
  }
  if (!["invalidate", "sync", "presence", "agent"].includes(kind)) return null;
  let value;
  try {
    value = JSON.parse(data.join("\n"));
  } catch {
    throw new StudioError("RESPONSE_INVALID");
  }
  if (kind === "agent") {
    if (
      !Array.isArray(value?.items) ||
      value.items.length > 10 ||
      value.items.some(
        (r: { id: string; state: string; stepSeq: number }) =>
          !r ||
          !uuid.test(r.id) ||
          !["queued", "running", "completed", "failed", "cancelled", "interrupted"].includes(
            r.state,
          ) ||
          !Number.isSafeInteger(r.stepSeq) ||
          r.stepSeq < 0,
      )
    )
      throw new StudioError("RESPONSE_INVALID");
    return { type: "agent" };
  }
  if (kind === "presence") {
    if (
      !Array.isArray(value?.items) ||
      value.items.length > 100 ||
      value.items.some(
        (p: Presence) =>
          !p ||
          !uuid.test(p.actorId) ||
          !uuid.test(p.changesetId) ||
          typeof p.actorName !== "string" ||
          p.actorName.length > 1000 ||
          typeof p.changesetTitle !== "string" ||
          p.changesetTitle.length > 1000,
      )
    )
      throw new StudioError("RESPONSE_INVALID");
    return { type: "presence", items: value.items };
  }
  if (!/^(0|[1-9][0-9]{0,18})$/.test(id) || value?.revision !== id)
    throw new StudioError("RESPONSE_INVALID");
  return { type: kind as "sync" | "invalidate", revision: id };
}

export async function readEvents(
  response: Response,
  signal: AbortSignal,
  receive: (event: LiveEvent) => void,
) {
  if (!response.body) throw new StudioError("RESPONSE_INVALID");
  const reader = response.body.getReader(),
    decoder = new TextDecoder();
  let buffer = "";
  const cancel = () => {
    void reader.cancel().catch(() => {});
  };
  signal.addEventListener("abort", cancel, { once: true });
  try {
    while (!signal.aborted) {
      const chunk = await reader.read();
      if (chunk.done) break;
      buffer += decoder.decode(chunk.value, { stream: true });
      let boundary: RegExpExecArray | null;
      while ((boundary = /\r?\n\r?\n/.exec(buffer))) {
        if (boundary.index > 64 * 1024) throw new StudioError("RESPONSE_INVALID");
        const event = parseEvent(buffer.slice(0, boundary.index));
        buffer = buffer.slice(boundary.index + boundary[0].length);
        if (event && !signal.aborted) receive(event);
      }
      if (buffer.length > 64 * 1024) throw new StudioError("RESPONSE_INVALID");
    }
  } finally {
    signal.removeEventListener("abort", cancel);
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}

export function startRealtime(
  api: Api,
  session: Session,
  callbacks: {
    invalidate(): void;
    presence(items: Presence[]): void;
    status(value: "connecting" | "live" | "offline"): void;
    agent?(): void;
  },
) {
  if (!api.events || !api.presence) return () => {};
  const lifecycle = new AbortController(),
    signal = lifecycle.signal,
    leaseId = crypto.randomUUID();
  let cursor: string | undefined,
    retry: ReturnType<typeof setTimeout> | undefined,
    heartbeat: ReturnType<typeof setTimeout> | undefined,
    delay = 1000;
  async function beat() {
    if (signal.aborted) return;
    try {
      await api.presence!(session, leaseId, false, signal);
    } catch {
      /* The server expires an unrenewed lease; presence grants no rights. */
    }
    if (!signal.aborted)
      heartbeat = setTimeout(() => {
        void beat();
      }, 10_000);
  }
  async function connect() {
    if (signal.aborted) return;
    callbacks.status("connecting");
    let lostSession = false;
    try {
      const response = await api.events!(session, cursor, signal);
      if (signal.aborted) {
        await response.body?.cancel();
        return;
      }
      callbacks.status("live");
      await readEvents(response, signal, (event) => {
        delay = 1000;
        if (event.type === "presence") callbacks.presence(event.items!);
        else if (event.type === "agent") callbacks.agent?.();
        else {
          cursor = event.revision;
          if (event.type === "invalidate") callbacks.invalidate();
        }
      });
    } catch (cause) {
      lostSession = cause instanceof StudioError && cause.code === "UNAUTHENTICATED";
    }
    if (!signal.aborted) {
      callbacks.status("offline");
      callbacks.presence([]);
      if (lostSession) {
        lifecycle.abort();
        if (heartbeat !== undefined) clearTimeout(heartbeat);
        return;
      }
      retry = setTimeout(() => {
        void connect();
      }, delay);
      delay = Math.min(delay * 2, 10_000);
    }
  }
  void beat();
  void connect();
  return () => {
    lifecycle.abort();
    if (retry !== undefined) clearTimeout(retry);
    if (heartbeat !== undefined) clearTimeout(heartbeat);
    void api.presence!(session, leaseId, true, AbortSignal.timeout(3000)).catch(() => {});
  };
}
