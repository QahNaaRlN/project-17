import { afterEach, expect, it, vi } from "vitest";
import fc from "fast-check";
import { parseEvent, readEvents, startRealtime } from "../src/realtime.js";
import { createEditor } from "../src/editor.js";
import { createApi } from "../src/api.js";
import { StudioError } from "../src/types.js";
import { fixture, mockApi, documentId, changesetId } from "./fixture.js";
afterEach(() => vi.useRealTimers());
const presence = {
  actorId: documentId,
  actorName: "Редактор",
  changesetId,
  changesetTitle: "Рабочий CS",
};
const block = (type: string, data: unknown, id = "") =>
  `${id ? "id: " + id + "\n" : ""}event: ${type}\ndata: ${JSON.stringify(data)}\n\n`;
it("STU-112: arbitrary SSE payloads return a validated event or a stable error", () => {
  fc.assert(
    fc.property(
      fc.jsonValue(),
      fc.constantFrom("sync", "invalidate", "presence"),
      fc.string(),
      (value, kind, id) => {
        try {
          parseEvent(block(kind, value, id));
        } catch (error) {
          expect(error).toBeInstanceOf(StudioError);
          expect((error as StudioError).code).toBe("RESPONSE_INVALID");
        }
      },
    ),
    { seed: 112, numRuns: 100 },
  );
  expect(() => parseEvent("event: sync\ndata: {bad}\n\n")).toThrow("RESPONSE_INVALID");
});
it("STU-110/112: SSE parses fragmented UTF-8, CRLF and rejects oversized/invalid data", async () => {
  expect(parseEvent(": heartbeat")).toBeNull();
  expect(
    parseEvent(block("invalidate", { revision: "9007199254740993" }, "9007199254740993")),
  ).toEqual({ type: "invalidate", revision: "9007199254740993" });
  expect(parseEvent(block("sync", { revision: "1" }, "1"))).toEqual({
    type: "sync",
    revision: "1",
  });
  expect(parseEvent(block("presence", { items: [presence] }))).toEqual({
    type: "presence",
    items: [presence],
  });
  for (const value of [
    { items: null },
    { items: [null] },
    { items: [{ ...presence, actorId: "bad" }] },
    { items: [{ ...presence, actorName: null }] },
    { items: [{ ...presence, changesetTitle: "x".repeat(1001) }] },
    { items: Array(101).fill(presence) },
  ])
    expect(() => parseEvent(block("presence", value))).toThrow("RESPONSE_INVALID");
  expect(() => parseEvent(block("sync", { revision: "2" }, "1"))).toThrow();
  const bytes = new TextEncoder().encode(
    block("presence", { items: [presence] }).replaceAll("\n", "\r\n"),
  );
  const response = new Response(
    new ReadableStream({
      start(controller) {
        for (const byte of bytes) controller.enqueue(new Uint8Array([byte]));
        controller.close();
      },
    }),
  );
  const receive = vi.fn();
  await readEvents(response, new AbortController().signal, receive);
  expect(receive).toHaveBeenCalledWith({ type: "presence", items: [presence] });
  await expect(
    readEvents(new Response(null), new AbortController().signal, receive),
  ).rejects.toThrow();
  for (const text of ["x".repeat(65537), "x".repeat(65537) + "\n\n"])
    await expect(
      readEvents(new Response(text), new AbortController().signal, receive),
    ).rejects.toThrow("RESPONSE_INVALID");
});
it("STU-112/113: reconnect resumes revision; heartbeat/leave and abort have a bounded lifecycle", async () => {
  vi.useFakeTimers();
  const { api, session } = mockApi();
  let stream: ReadableStreamDefaultController<Uint8Array>;
  const events = vi
    .fn<NonNullable<typeof api.events>>()
    .mockResolvedValueOnce(new Response(block("invalidate", { revision: "3" }, "3")))
    .mockImplementation(
      async () =>
        new Response(
          new ReadableStream({
            start(controller) {
              stream = controller;
            },
          }),
        ),
    );
  api.events = events;
  const heartbeat = vi.fn<NonNullable<typeof api.presence>>(async () => {});
  api.presence = heartbeat;
  const callbacks = { invalidate: vi.fn(), presence: vi.fn(), status: vi.fn() };
  const stop = startRealtime(api, session, callbacks);
  await vi.advanceTimersByTimeAsync(1000);
  expect(events).toHaveBeenCalledTimes(2);
  expect(events.mock.calls[1]![1]).toBe("3");
  expect(callbacks.invalidate).toHaveBeenCalledTimes(1);
  stream!.enqueue(new TextEncoder().encode(block("presence", { items: [presence] })));
  await vi.advanceTimersByTimeAsync(9000);
  expect(callbacks.presence).toHaveBeenCalledWith([presence]);
  expect(api.presence).toHaveBeenCalledTimes(2);
  stop();
  await vi.advanceTimersByTimeAsync(30_000);
  expect(events).toHaveBeenCalledTimes(2);
  expect(api.presence).toHaveBeenLastCalledWith(
    session,
    expect.any(String),
    true,
    expect.any(AbortSignal),
  );
  expect(heartbeat.mock.calls[0]![1]).toBe(heartbeat.mock.calls.at(-1)![1]);
});
it("STU-112: offline retry backs off; lost sessions stop retries and heartbeat", async () => {
  vi.useFakeTimers();
  const { api, session } = mockApi();
  api.events = vi.fn(async () => {
    throw new Error("offline");
  });
  api.presence = vi.fn(async () => {
    throw new Error("offline");
  });
  const callbacks = { invalidate: vi.fn(), presence: vi.fn(), status: vi.fn() };
  const stop = startRealtime(api, session, callbacks);
  await vi.advanceTimersByTimeAsync(16_000);
  expect(api.events).toHaveBeenCalledTimes(5);
  expect(callbacks.status).toHaveBeenCalledWith("offline");
  expect(callbacks.presence).toHaveBeenCalledWith([]);
  stop();
  api.events = vi.fn(async () => {
    throw new StudioError("UNAUTHENTICATED");
  });
  const stopAuth = startRealtime(api, session, callbacks);
  await vi.advanceTimersByTimeAsync(30_000);
  expect(api.events).toHaveBeenCalledTimes(1);
  stopAuth();
  startRealtime(mockApi().api, session, callbacks)();
});
it("STU-114: remote refresh preserves every local form and uncertain writes", async () => {
  const { api, session } = mockApi();
  const editor = createEditor(api);
  await editor.open(documentId, changesetId);
  for (const field of ["json", "content", "property-title", "contentUpload"]) {
    editor.formDirty(field, true);
    editor.remoteChanged();
    await editor.refreshRemote();
    expect(editor.store.getState().remotePending).toBe(true);
    expect(api.open).toHaveBeenCalledTimes(1);
    editor.formDirty(field, false);
  }
  editor.store.setState({ status: "uncertain" });
  await editor.refreshRemote();
  expect(api.open).toHaveBeenCalledTimes(1);
  session.record.body.nodes.n_custom!.name = "Changed remotely";
  editor.store.setState({ status: "ready" });
  await editor.refreshRemote();
  expect(editor.store.getState().document?.nodes.n_custom!.name).toBe("Changed remotely");
  expect(editor.store.getState().remotePending).toBe(false);
  editor.dispose();
});
it("STU-114: input changed during a remote read is preserved", async () => {
  const { api, session } = mockApi(),
    editor = createEditor(api);
  await editor.open(documentId, changesetId);
  let complete: (value: typeof session) => void;
  vi.mocked(api.open).mockImplementationOnce(
    async () =>
      new Promise((resolve) => {
        complete = resolve;
      }),
  );
  editor.remoteChanged();
  const reading = editor.refreshRemote();
  editor.formDirty("property-title", true);
  const changed = structuredClone(session);
  changed.changeset.seq = 2;
  complete!(changed);
  await reading;
  expect(editor.store.getState().session?.changeset.seq).toBe(1);
  expect(editor.store.getState().dirtyFields["property-title"]).toBe(true);
  expect(editor.store.getState().remotePending).toBe(true);
  editor.dispose();
});
it("STU-110/113: API sends credentials in headers and presence via CSRF Command API", async () => {
  const fetcher = vi.fn<typeof fetch>(
    async () =>
      new Response(block("sync", { revision: "1" }, "1"), {
        headers: { "Content-Type": "text/event-stream" },
      }),
  );
  const api = createApi(fetcher),
    session = fixture(),
    signal = new AbortController().signal;
  await api.events!(session, "1", signal);
  expect(fetcher.mock.calls[0]![0]).toContain("after=1");
  expect(fetcher.mock.calls[0]![1]).toMatchObject({
    credentials: "same-origin",
    signal,
    headers: { Accept: "text/event-stream" },
  });
  fetcher.mockResolvedValueOnce(Response.json({ result: { ttlSeconds: 30 } }));
  await api.presence!(session, documentId, false, signal);
  expect(JSON.parse(String(fetcher.mock.calls.at(-1)![1]!.body)).payload).toMatchObject({
    documentId,
    changesetId,
    leaseId: documentId,
    environment: "staging",
  });
  fetcher.mockResolvedValueOnce(Response.json({ result: { left: true } }));
  await api.presence!(session, documentId, true, signal);
  expect(JSON.parse(String(fetcher.mock.calls.at(-1)![1]!.body)).payload).toEqual({
    leaseId: documentId,
  });
  for (const status of [401, 403, 503]) {
    fetcher.mockResolvedValueOnce(new Response(null, { status }));
    await expect(api.events!(session, undefined, signal)).rejects.toThrow(
      status === 503 ? "EVENTS_UNAVAILABLE" : "UNAUTHENTICATED",
    );
  }
  fetcher.mockResolvedValueOnce(Response.json({}));
  await expect(api.events!(session, undefined, signal)).rejects.toThrow("RESPONSE_INVALID");
});
