// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { NewWork } from "../src/new-work.js";
import { Studio } from "../src/ui.js";
import { createEditor, type Editor } from "../src/editor.js";
import { createApi } from "../src/api.js";
import { StudioError, type Api, type WorkContext } from "../src/types.js";
import {
  canRetryWork,
  checkCreatedWork,
  clearWorkAttempt,
  loadWorkAttempt,
  makeWorkAttempt,
  saveWorkAttempt,
  workCreationWindow,
  workStorageKey,
  workTitle,
} from "../src/work-creation.js";
import { mockApi, fixture, changesetId, documentId } from "./fixture.js";

const scope = { actor: documentId, project: "store", environment: "staging" };
const created = () => ({ ...fixture().changeset, seq: 0 });
let root: Root | undefined, editor: Editor | undefined, client: QueryClient | undefined;
afterEach(async () => {
  await act(async () => root?.unmount());
  root = undefined;
  editor?.dispose();
  client?.clear();
  document.body.innerHTML = "";
  sessionStorage.clear();
  window.history.replaceState({}, "", "/");
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 30));
  });
}
async function click(name: string) {
  const b = [...document.querySelectorAll("button")].find((b) => b.textContent === name);
  expect(b, name).toBeTruthy();
  expect(b!.disabled).toBe(false);
  await act(async () => b!.click());
  await flush();
}
async function input(value: string) {
  const el = document.querySelector("input")!;
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });
}
async function mount(api?: Api, onCreated = vi.fn(async () => true), ru = true) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("crypto", webcrypto);
  const a = api ?? mockApi().api;
  a.createWork ??= vi.fn(async () => created());
  document.body.innerHTML = '<div id="root"></div>';
  root = createRoot(document.getElementById("root")!);
  await act(async () =>
    root!.render(<NewWork api={a} scope={scope} ru={ru} disabled={false} onCreated={onCreated} />),
  );
  return { api: a, onCreated };
}
it("STU-191: Unicode title boundaries and standard creation response", () => {
  expect(workTitle("  hello  ")).toBe("hello");
  expect(workTitle("🪻".repeat(200))).toHaveLength(400);
  for (const v of [" ", "🪻".repeat(201)]) expect(() => workTitle(v)).toThrow("TITLE_INVALID");
  expect(checkCreatedWork(created(), "staging").seq).toBe(0);
  for (const value of [
    { ...created(), targets: ["production"] },
    { ...created(), seq: 1 },
    { ...created(), kind: "preview" },
    { ...created(), id: "wrong" },
    { ...created(), state: "merged" },
  ])
    expect(() => checkCreatedWork(value, "staging")).toThrow("RESPONSE_INVALID");
});
it("STU-192: durable attempt is scoped, preserves exact body and expires conservatively", () => {
  const a = makeWorkAttempt(scope, "  Draft  ");
  saveWorkAttempt(a);
  expect(loadWorkAttempt(scope)).toEqual(a);
  expect(a.body).toEqual({
    payload: { title: "Draft", targets: ["staging"] },
    reason: "Studio: create content work",
  });
  expect(loadWorkAttempt({ ...scope, actor: changesetId })).toBeNull();
  expect(canRetryWork(a, a.firstSentAt + workCreationWindow - 1)).toBe(true);
  expect(canRetryWork(a, a.firstSentAt + workCreationWindow)).toBe(false);
  expect(canRetryWork(a, a.firstSentAt - 1)).toBe(false);
  expect(canRetryWork({ ...a, firstSentAt: NaN })).toBe(false);
  expect(canRetryWork({ ...a, lastSeenAt: a.firstSentAt + 10 }, a.firstSentAt)).toBe(false);
  clearWorkAttempt(scope);
  expect(loadWorkAttempt(scope)).toBeNull();
  saveWorkAttempt(a);
  clearWorkAttempt();
  expect(loadWorkAttempt(scope)).toBeNull();
});
it("STU-192: malformed storage fails closed; no command is sent", async () => {
  sessionStorage.setItem(workStorageKey(scope), "{bad");
  const { api } = await mount();
  await input("Draft");
  await click("Создать работу");
  expect(api.createWork).not.toHaveBeenCalled();
  expect(document.body.textContent).toContain("хранилища");
});
it("STU-190/191: cancel before send, reject invalid title, then create without page", async () => {
  const { api, onCreated } = await mount();
  await click("Новая работа");
  await click("Закрыть");
  expect(api.createWork).not.toHaveBeenCalled();
  await click("Новая работа");
  await click("Создать работу");
  expect(document.body.textContent).toContain("от 1 до 200");
  await input("  Content only  ");
  await click("Создать работу");
  expect(api.createWork).toHaveBeenCalledTimes(1);
  expect(vi.mocked(api.createWork!).mock.calls[0]![0].body.payload.title).toBe("Content only");
  expect(onCreated).toHaveBeenCalledWith(changesetId);
  expect(loadWorkAttempt(scope)).toBeNull();
});
it("STU-192: unknown creation survives remount; explicit retry keeps key and body", async () => {
  const { api } = mockApi();
  api.createWork = vi
    .fn()
    .mockRejectedValueOnce(new StudioError("NETWORK_UNCERTAIN", true))
    .mockResolvedValue(created());
  await mount(api);
  await click("Новая работа");
  await input("Recover");
  await click("Создать работу");
  const first = structuredClone(vi.mocked(api.createWork).mock.calls[0]![0]);
  expect(document.body.textContent).toContain("Результат создания неизвестен");
  expect(window.location.hash).toBe("#work=");
  await act(async () => root!.unmount());
  await mount(api);
  expect(api.createWork).toHaveBeenCalledTimes(1);
  await click("Проверить / повторить");
  const second = vi.mocked(api.createWork).mock.calls[1]![0];
  expect(second.key).toBe(first.key);
  expect(second.body).toEqual(first.body);
});
it("STU-192: confirmed ID survives failed opening and does not create again", async () => {
  const opened = vi.fn().mockResolvedValueOnce(false).mockResolvedValue(true);
  const { api } = await mount(undefined, opened);
  await click("Новая работа");
  await input("Open later");
  await click("Создать работу");
  expect(loadWorkAttempt(scope)?.result?.id).toBe(changesetId);
  await click("Открыть созданную работу");
  expect(api.createWork).toHaveBeenCalledTimes(1);
});
it("STU-192/199: losing write access during recovery does not erase an unknown commit", async () => {
  const saved = makeWorkAttempt(scope, "Unknown before revocation");
  saveWorkAttempt(saved);
  const { api } = mockApi();
  api.createWork = vi.fn().mockRejectedValue(new StudioError("FORBIDDEN"));
  await mount(api);
  await click("Проверить / повторить");
  expect(loadWorkAttempt(scope)?.key).toBe(saved.key);
  expect(document.body.textContent).toContain("Результат создания неизвестен");
  expect(document.querySelector("input")!.disabled).toBe(true);
});
it("STU-192: two submits while the response is pending send one command", async () => {
  const { api } = mockApi();
  let finish!: (value: ReturnType<typeof created>) => void;
  api.createWork = vi.fn(
    () =>
      new Promise<ReturnType<typeof created>>((resolve) => {
        finish = resolve;
      }),
  );
  await mount(api);
  await click("Новая работа");
  await input("One request");
  await act(async () => {
    const form = document.querySelector("form")!;
    form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  });
  expect(api.createWork).toHaveBeenCalledTimes(1);
  await act(async () => finish(created()));
});
it("STU-192/201: storage unavailable blocks request and preserves title", async () => {
  const { api } = await mount();
  await click("Новая работа");
  await input("Keep title");
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw Error("quota");
  });
  await click("Создать работу");
  expect(api.createWork).not.toHaveBeenCalled();
  expect(document.querySelector("input")!.value).toBe("Keep title");
});
it("STU-192: expired attempt requires explicit duplicate warning; it is never replayed", async () => {
  saveWorkAttempt(makeWorkAttempt(scope, "Expired", Date.now() - workCreationWindow - 1));
  const { api } = await mount();
  expect(
    [...document.querySelectorAll("button")].find((b) => b.textContent === "Проверить / повторить")!
      .disabled,
  ).toBe(true);
  const confirm = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValue(true);
  await click("Создать другую работу");
  expect(loadWorkAttempt(scope)).not.toBeNull();
  await click("Создать другую работу");
  expect(confirm).toHaveBeenCalledTimes(2);
  expect(loadWorkAttempt(scope)).toBeNull();
  expect(api.createWork).not.toHaveBeenCalled();
});
it("STU-192: definitive refusal allows corrected fresh request; English UI", async () => {
  const { api } = mockApi();
  api.createWork = vi
    .fn()
    .mockRejectedValueOnce(new StudioError("FORBIDDEN"))
    .mockResolvedValue(created());
  await mount(api, undefined, false);
  await click("New work");
  await input("First");
  await click("Create work");
  expect(loadWorkAttempt(scope)).toBeNull();
  await input("Corrected");
  await click("Create work");
  expect(vi.mocked(api.createWork).mock.calls[0]![0].key).not.toBe(
    vi.mocked(api.createWork).mock.calls[1]![0].key,
  );
});
it("STU-191/192: API freezes target and reason; environment mismatch sends no command", async () => {
  const calls: { path: string; init?: RequestInit | undefined }[] = [];
  const fetcher = vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
    calls.push({ path: String(url), init });
    return Response.json(
      String(url).endsWith("/config") ? fixture().config : { result: created() },
    );
  });
  const api = createApi(fetcher as typeof fetch),
    a = makeWorkAttempt(scope, "API work");
  await api.createWork!(a);
  await api.createWork!(a);
  const commands = calls.filter((c) => c.init?.method === "POST");
  expect(commands).toHaveLength(2);
  expect(commands[0]!.init?.body).toBe(commands[1]!.init?.body);
  expect((commands[0]!.init!.headers as Record<string, string>)["Idempotency-Key"]).toBe(a.key);
  await expect(api.createWork!({ ...a, environment: "production" })).rejects.toMatchObject({
    code: "SESSION_CHANGED",
    uncertain: true,
  });
  expect(calls.filter((c) => c.init?.method === "POST")).toHaveLength(2);
});
it("STU-192/199: a different signed-in human cannot replay a stored attempt", async () => {
  const posts = vi.fn();
  const api = createApi((async (url: string | URL | Request, init?: RequestInit) => {
    if (init?.method === "POST") posts();
    return Response.json(
      String(url).endsWith("/config")
        ? { ...fixture().config, authMode: "oidc" }
        : {
            authenticated: true,
            user: { id: changesetId, name: "Other person" },
            csrfToken: "a".repeat(43),
            expiresAt: "2099-01-01T00:00:00Z",
            rights: ["content.read", "content.write"],
            hasAccess: true,
          },
    );
  }) as typeof fetch);
  await expect(api.createWork!(makeWorkAttempt(scope, "Bound to Alice"))).rejects.toMatchObject({
    code: "SESSION_CHANGED",
    uncertain: true,
  });
  expect(posts).not.toHaveBeenCalled();
});
it("STU-192: a failed config preflight cannot resolve an earlier unknown request", async () => {
  const api = createApi((async () =>
    Response.json({ code: "FORBIDDEN" }, { status: 403 })) as typeof fetch);
  await expect(api.createWork!(makeWorkAttempt(scope, "Preflight failure"))).rejects.toMatchObject({
    code: "FORBIDDEN",
    uncertain: true,
  });
});
it.each([false, true])(
  "STU-193/196/199: empty content work and repeated kind switches, entity write=%s",
  async (entityWrite) => {
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    const { api } = mockApi();
    const { record: _record, ...work } = fixture();
    void _record;
    const context: WorkContext = {
      ...work,
      changeset: created(),
      work: {
        item: {
          id: changesetId,
          title: "Empty work",
          ownerId: documentId,
          ownerName: "Alice",
          own: true,
          state: "open",
          seq: 0,
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
          objectCount: 0,
          deletedCount: 0,
          countsByKind: { page: 0, component: 0, entity: 0, asset: 0 },
        },
        objects: [],
      },
    };
    api.openWork = vi.fn(async () => context);
    api.workQueue = vi.fn(async () => ({ items: [], nextCursor: null }));
    api.createWork = vi.fn(async () => created());
    editor = createEditor(api);
    await editor.openWork(changesetId);
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    document.body.innerHTML = '<div id="root"></div>';
    root = createRoot(document.getElementById("root")!);
    await act(async () =>
      root!.render(
        <QueryClientProvider client={client!}>
          <Studio
            api={api}
            editor={editor!}
            actorId={documentId}
            rights={["content.read", "asset.write", ...(entityWrite ? ["content.write"] : [])]}
          />
        </QueryClientProvider>,
      ),
    );
    await flush();
    expect(document.body.textContent).toContain("В работе пока нет изменений");
    expect(
      [...document.querySelectorAll("button")].some((b) => b.textContent === "Создать сущность"),
    ).toBe(entityWrite);
    await click("Загрузить файл");
    expect(api.open).not.toHaveBeenCalled();
    expect(api.pages).not.toHaveBeenCalled();
    expect(api.preview).not.toHaveBeenCalled();
    expect(
      document.querySelector<HTMLSelectElement>('select[aria-label="Тип контента"]')?.value,
    ).toBe("asset");
    if (entityWrite) {
      await click("Создать сущность");
      expect(
        document.querySelector<HTMLSelectElement>('select[aria-label="Тип контента"]')!.value,
      ).toBe("entity");
      expect(document.body.textContent).toContain("нет схем сущностей");
      await click("Загрузить файл");
      expect(
        document.querySelector<HTMLSelectElement>('select[aria-label="Тип контента"]')!.value,
      ).toBe("asset");
    }
  },
);
