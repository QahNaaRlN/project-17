// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { CapabilityComposer, CapabilityCard, CapabilityQueue } from "../src/capability-panel.js";
import { createEditor, type Editor } from "../src/editor.js";
import { mockApi, documentId, changesetId, fixture } from "./fixture.js";
import { StudioError } from "../src/types.js";
import type { CapabilityRequest } from "../src/capabilities.js";
import { manifestHash } from "@cms/manifest";

let root: Root, editor: Editor;
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});
const button = (name: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find(
    (b) => b.textContent?.trim() === name,
  )!;
async function click(name: string) {
  await act(async () => button(name).click());
}
function field(label: string, index = 0) {
  const labels = [...document.querySelectorAll("label")].filter((l) =>
    l.textContent?.startsWith(label),
  );
  return labels[index]!.querySelector<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>(
    "input,select,textarea",
  )!;
}
async function change(label: string, value: string, index = 0) {
  const el = field(label, index);
  await act(async () => {
    const proto =
      el.tagName === "SELECT"
        ? HTMLSelectElement.prototype
        : el.tagName === "TEXTAREA"
          ? HTMLTextAreaElement.prototype
          : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, "value")!.set!.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.dispatchEvent(new Event("change", { bubbles: true }));
  });
}
async function check(label: string) {
  await act(async () => field(label).click());
}
async function submit() {
  await act(async () =>
    document
      .querySelector("form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })),
  );
}
const requestId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c33";
function request(): CapabilityRequest {
  return {
    id: requestId,
    kind: "action",
    environment: "staging",
    key: "store.favorite",
    title: "Favorite",
    description: "Safe task",
    proposedContract: { args: {} },
    acceptance: ["Explicit click works"],
    status: "draft",
    revision: 1,
    changesetId,
    documentId,
    nodeId: "n_heading",
    event: "click",
    ownerId: documentId,
    externalRefs: [],
    transitions: ["edit", "submit", "cancelled"],
    packs: [],
    audit: [],
  };
}
async function setup(mode: "composer" | "card" | "queue" = "composer", initial = request()) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("crypto", webcrypto);
  const session = fixture();
  session.record.body.nodes.n_heading!.type = "Button";
  session.review.capabilityPolicy = { defaultMode: "CODE", maxMode: "CODE" };
  session.codeIndexAvailable = true;
  const { api } = mockApi(session);
  let current = structuredClone(initial);
  api.capability = vi.fn(async () => structuredClone(current));
  api.capabilities = vi.fn(async () => ({ items: [structuredClone(current)], nextCursor: null }));
  const pack = {
    version: "1.0",
    request: { id: requestId, revision: initial.revision + 1 },
    pinned: {
      manifestHash: session.manifestHash,
      seq: session.changeset.seq + (initial.status === "draft" ? 1 : 0),
    },
  };
  const hash = await manifestHash(pack);
  api.capabilityContext = vi.fn(async () => ({ hash, pack }));
  api.capabilityCommand = vi.fn(async (attempt) => {
    current.revision++;
    if (attempt.name === "create-capability-request") {
      current = { ...current, ...attempt.payload, id: requestId, revision: 1 } as CapabilityRequest;
    }
    if (attempt.name === "submit-capability-request") {
      current.status = "submitted";
      current.transitions = ["refresh", "cancelled"];
      current.packs = [{ revision: 2, hash, changesetSeq: 2 }];
      session.record.body.nodes.n_heading!.on = { click: [{ action: "pending:" + requestId }] };
      session.changeset.seq = 2;
      session.history.seq = 2;
    }
    if (attempt.name === "resolve-capability-request") {
      current.status = "fulfilled";
      current.transitions = [];
      session.changeset.seq++;
      session.history.seq = session.changeset.seq;
    }
    if (attempt.name === "update-capability-request") {
      if (attempt.payload.draft) Object.assign(current, attempt.payload.draft);
      else {
        current.status = String(attempt.payload.transition);
        current.transitions =
          current.status === "accepted"
            ? ["in_progress", "rejected"]
            : current.status === "in_progress"
              ? ["pr_open", "rejected"]
              : [];
        if (attempt.payload.prUrl) current.externalRefs = [String(attempt.payload.prUrl)];
      }
    }
    return { request: structuredClone(current) };
  });
  editor = createEditor(api);
  await editor.open(documentId, changesetId);
  editor.select("n_heading");
  document.body.innerHTML = "<div id='root'></div>";
  root = createRoot(document.getElementById("root")!);
  await act(async () =>
    root.render(
      mode === "composer" ? (
        <CapabilityComposer {...{ api, editor }} />
      ) : mode === "card" ? (
        <CapabilityCard {...{ api, editor }} initial={initial} onClose={vi.fn()} />
      ) : (
        <CapabilityQueue {...{ api, editor }} />
      ),
    ),
  );
  return {
    api,
    session,
    pack,
    hash,
    setCurrent: (r: CapabilityRequest) => {
      current = r;
    },
  };
}

it("STU-255/256/258/260: draft has typed args and explicit context/replacement consent before submit", async () => {
  const { api, session } = await setup();
  await click("Запросить действие для click");
  expect(document.querySelector("[role=dialog]")).not.toBeNull();
  expect(document.activeElement).toBe(field("Ключ действия"));
  await change("Ключ действия", "store.favorite");
  await change("Заголовок", "Favorite");
  await change("Описание задачи", "Save the choice");
  await change("Критерии приёмки", "A click saves it\nNo automatic action");
  for (const [i, type] of [
    "string",
    "text",
    "number",
    "boolean",
    "enum",
    "reference",
    "link",
  ].entries()) {
    await click("Добавить аргумент");
    await change("Имя аргумента", "arg" + i, i);
    await change("Тип аргумента", type, i);
    if (type === "enum") await change("Варианты через запятую", "one, two");
    if (type === "reference") {
      await act(async () => {
        editor.store.getState().session!.manifest.schemas = {
          Article: { version: 1, fields: { title: { type: "string" } } },
        };
      });
      await change("Схема", "Article");
    }
    if (type === "number") {
      await change("Минимум", "0", 2);
      await change("Максимум", "10", 2);
    }
  }
  await submit();
  expect(api.capabilityCommand).toHaveBeenCalledTimes(1);
  const attempt = vi.mocked(api.capabilityCommand!).mock.calls[0]![0];
  expect(attempt.name).toBe("create-capability-request");
  expect(attempt.payload.acceptance).toEqual(["A click saves it", "No automatic action"]);
  expect(session.changeset.seq).toBe(1);
  await click("Проверить передаваемый контекст");
  expect(button("Отправить запрос").disabled).toBe(true);
  await check("Передать этот context pack");
  await check("Подтверждаю замену цепочки");
  await click("Отправить запрос");
  expect(api.capabilityCommand).toHaveBeenCalledTimes(2);
  expect(vi.mocked(api.capabilityCommand!).mock.calls[1]![0].payload).toMatchObject({
    id: requestId,
    expectedRevision: 1,
    expectedSeq: 1,
    confirmReplaceExisting: true,
  });
  expect(session.changeset.seq).toBe(2);
  expect(document.body.textContent).toContain("submitted");
  await click("Закрыть");
  expect(document.querySelector("[role=dialog]")).toBeNull();
});

it("STU-270/272: uncertain create freezes exact payload and blocks locale/selection/Escape until retry", async () => {
  const { api } = await setup();
  const original = api.capabilityCommand!;
  let first = true;
  api.capabilityCommand = vi.fn(async (a) => {
    if (first) {
      first = false;
      throw new StudioError("NETWORK_UNCERTAIN", true);
    }
    return original(a);
  });
  await click("Запросить действие для click");
  await change("Ключ действия", "store.favorite");
  await change("Заголовок", "Favorite");
  await change("Описание задачи", "Task");
  await change("Критерии приёмки", "works");
  await submit();
  expect(button("Сохранить черновик запроса").disabled).toBe(true);
  expect(editor.store.getState().status).toBe("uncertain");
  await act(async () => {
    editor.locale("en");
    editor.select("n_root");
    document
      .querySelector("[role=dialog]")!
      .dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  });
  expect(editor.store.getState().locale).toBe("ru");
  expect(editor.store.getState().selected).toBe("n_heading");
  expect(document.querySelector("[role=dialog]")).not.toBeNull();
  await click("Повторить тот же запрос");
  const calls = vi.mocked(api.capabilityCommand!).mock.calls;
  expect(calls[0]![0]).toEqual(calls[1]![0]);
});

it("STU-258: a delayed authorized projection enables the draft card after the create acknowledgement", async () => {
  const { api } = await setup();
  let resolveRead!: (value: CapabilityRequest) => void;
  api.capability = vi.fn(
    () =>
      new Promise<CapabilityRequest>((resolve) => {
        resolveRead = resolve;
      }),
  );
  const original = api.capabilityCommand!;
  api.capabilityCommand = vi.fn(async (attempt) => {
    const result = await original(attempt);
    delete result.request.transitions;
    return result;
  });
  await click("Запросить действие для click");
  await change("Ключ действия", "store.favorite");
  await change("Заголовок", "Favorite");
  await change("Описание задачи", "Task");
  await change("Критерии приёмки", "works");
  await submit();
  expect(button("Проверить передаваемый контекст")).toBeUndefined();
  await act(async () => resolveRead(request()));
  expect(button("Проверить передаваемый контекст")).toBeDefined();
});

it("STU-260/265: sharing retains the reviewed seq/revision/hash after a live editor refresh", async () => {
  const { api, session } = await setup("card");
  await click("Проверить передаваемый контекст");
  await check("Передать этот context pack");
  await check("Подтверждаю замену цепочки");
  const reviewed = {
    expectedSeq: session.changeset.seq,
    expectedRevision: 1,
    expectedManifestHash: session.manifestHash,
  };
  await act(async () =>
    editor.store.setState({
      session: {
        ...session,
        changeset: { ...session.changeset, seq: session.changeset.seq + 1 },
        manifestHash: "sha256:" + "b".repeat(64),
      },
    }),
  );
  await click("Отправить запрос");
  expect(vi.mocked(api.capabilityCommand!).mock.calls[0]![0].payload).toMatchObject(reviewed);
});

it("STU-266/267: engineer transitions and PR URL stay manual, escaped and audited", async () => {
  const r = request();
  r.status = "submitted";
  r.transitions = ["accepted", "rejected"];
  r.description = "<script>untrusted</script>";
  r.audit = [
    { revision: 1, event: "submitted", reason: "Human consent", createdAt: "2026-10-16T00:00:00Z" },
  ];
  const { api } = await setup("card", r);
  expect(document.querySelector("script")).toBeNull();
  await click("Принять запрос");
  expect(document.body.textContent).toContain("accepted");
  await click("Начать работу");
  expect(button("Добавить PR").disabled).toBe(true);
  await change("Ссылка на PR", "https://example.test/pull/1");
  await click("Добавить PR");
  expect(vi.mocked(api.capabilityCommand!).mock.calls.at(-1)![0].payload).toMatchObject({
    transition: "pr_open",
    prUrl: "https://example.test/pull/1",
  });
  expect(document.querySelector("a")?.rel).toBe("noopener noreferrer");
});

it("STU-268/272: explicit resolve uses the existing typed argument form and latest seq/hash", async () => {
  const r = request();
  r.status = "pr_open";
  r.revision = 5;
  r.transitions = ["resolve", "cancelled"];
  r.stale = true;
  const { api } = await setup("card", r);
  await act(async () => {
    const session = editor.store.getState().session!;
    session.manifest.actions = { [r.key]: { args: { value: { type: "string", required: true } } } };
    editor.store.setState({ session: { ...session } });
  });
  expect(button("Применить доступное действие").disabled).toBe(true);
  await change("Значение параметра value", "literal");
  await change("Литерал value", "chosen");
  await click("Применить доступное действие");
  expect(vi.mocked(api.capabilityCommand!).mock.calls[0]![0].payload).toMatchObject({
    id: r.id,
    expectedRevision: 5,
    expectedSeq: 1,
    handlers: [{ action: r.key, args: { value: { lit: "chosen" } } }],
  });
  expect(document.body.textContent).toContain("fulfilled");
});

it("STU-272: draft edit, permission failure and explicit cancel retain input without a write on close", async () => {
  const { api } = await setup("card");
  await click("Изменить черновик");
  await change("Заголовок", "Changed");
  api.capabilityCommand = vi.fn(async () => {
    throw new StudioError("FORBIDDEN");
  });
  await submit();
  expect(field("Заголовок").value).toBe("Changed");
  expect(document.body.textContent).toContain("Права изменились");
  expect(editor.store.getState().status).toBe("ready");
  await click("Закрыть");
  expect(api.capabilityCommand).toHaveBeenCalledTimes(1);
});

it("STU-264/266: queue filters, pagination, safe JSON download and context failures", async () => {
  const r = request();
  r.status = "submitted";
  r.transitions = ["refresh", "cancelled"];
  const { api, hash, setCurrent } = await setup("queue", r);
  r.packs = [{ revision: 2, hash, changesetSeq: 2 }];
  setCurrent(r);
  await change("Ключ действия", "store.favorite");
  await change("Статус", "submitted");
  await submit();
  expect(api.capabilities).toHaveBeenLastCalledWith({ status: "submitted", key: "store.favorite" });
  await click("Favorite · store.favorite · submitted");
  vi.stubGlobal(
    "URL",
    class extends URL {
      static override createObjectURL = vi.fn(() => "blob:test");
      static override revokeObjectURL = vi.fn();
    },
  );
  const link = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
  await click("Скачать context pack");
  await vi.waitFor(() => expect(link).toHaveBeenCalledOnce());
  link.mockRestore();
  api.capabilityContext = vi.fn(async () => {
    throw new StudioError("CAPABILITY_INDEX_UNAVAILABLE");
  });
  await click("Проверить передаваемый контекст");
  expect(document.body.textContent).toContain("нет подтверждённого индекса");
  await click("Закрыть");
  expect(document.querySelector("[role=dialog]")).toBeNull();
});
