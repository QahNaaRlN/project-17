// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { StructurePanel } from "../src/structure-ui.js";
import { createEditor, type Editor } from "../src/editor.js";
import { fixture, mockApi, documentId, changesetId } from "./fixture.js";
import { StudioError, type Operation, type Session } from "../src/types.js";
import type { ExtractPlan } from "../src/component-extract.js";
import type { IrDocument } from "@cms/ir";

const componentId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c33";
let root: Root, editor: Editor;

function source(): Session {
  const s = fixture();
  s.record.body = {
    irVersion: "1.0",
    kind: "page",
    root: "n_root",
    localContent: { t_title: { type: "text", value: { ru: "Новинки" } } },
    nodes: {
      n_root: { id: "n_root", type: "Box", children: ["n_block"] },
      n_block: { id: "n_block", type: "Stack", children: ["n_title", "n_btn"] },
      n_title: { id: "n_title", type: "Heading", bindings: { text: "$local.t_title" } },
      n_btn: { id: "n_btn", type: "Button", props: { variant: "primary" } },
    },
  };
  return s;
}
function plan(overrides: Partial<ExtractPlan> = {}): ExtractPlan {
  return {
    nodeId: "n_block",
    blockers: [],
    candidates: [
      {
        nodeId: "n_title",
        nodeType: "Heading",
        prop: "text",
        type: { type: "text", content: true },
        current: "local",
        content: true,
        external: false,
        suggestedName: "text",
        expression: "$local.t_title",
      },
      {
        nodeId: "n_btn",
        nodeType: "Button",
        prop: "variant",
        type: { type: "enum", values: ["primary", "secondary"] },
        current: "literal",
        content: false,
        external: false,
        suggestedName: "variant",
        expression: "primary",
      },
    ],
    localKeys: [{ key: "t_title", nodeIds: ["n_title"], usedOutside: false }],
    slotRanges: [
      { parentId: "n_block", nodeIds: ["n_title", "n_btn"], types: ["Heading", "Button"] },
    ],
    slotPositions: [{ parentId: "n_block", maxIndex: 2 }],
    sourceListTypes: [],
    effectiveMode: "SYSTEM",
    ...overrides,
  };
}
// Server stand-in: the block becomes one Composed instance with the footer slot.
function extracted(session: Session, operations: Operation[]) {
  const op = operations[0]!;
  expect(op.type).toBe("component.extract");
  const body = session.record.body;
  body.nodes.n_inst = {
    id: "n_inst",
    type: "Composed",
    ref: { component: componentId, version: "live" },
    bindings: { text: "$local.t_title" },
    slots: { footer: ["n_btn"] },
  };
  body.nodes.n_root!.children = ["n_inst"];
  delete body.nodes.n_block;
  delete body.nodes.n_title;
  session.changeset.seq += 1;
  session.history.seq = session.changeset.seq;
}
async function setup(p = plan(), selected = "n_block") {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const { api, session } = mockApi(source());
  api.extractPlan = vi.fn(async () => structuredClone(p));
  api.components = vi.fn(
    async () =>
      ({
        [componentId]: {
          irVersion: "1.0",
          kind: "component",
          root: "n_block",
          meta: { title: "Showcase" },
          inputs: { text: { type: "text", content: true } },
          slotDefs: { footer: { allowedTypes: ["Button"] } },
          nodes: {
            n_block: { id: "n_block", type: "Stack", children: ["n_title", "n_slot"] },
            n_title: { id: "n_title", type: "Heading", bindings: { text: "$props.text" } },
            n_slot: { id: "n_slot", type: "Slot", props: { name: "footer" } },
          },
        },
      }) as Record<string, IrDocument>,
  );
  editor = createEditor(api);
  await editor.open(documentId, changesetId);
  editor.select(selected);
  document.body.innerHTML = "<div id='root'></div>";
  root = createRoot(document.getElementById("root")!);
  await act(async () => root.render(<StructurePanel editor={editor} />));
  await flush();
  return { api, session };
}
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});
const button = (name: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === name)!;
async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}
async function click(name: string) {
  await act(async () => button(name).click());
  await flush();
}
async function type(label: string, value: string) {
  const input = [
    ...document.querySelectorAll<HTMLInputElement | HTMLSelectElement>("input,select"),
  ].find(
    (i) =>
      i.getAttribute("aria-label") === label ||
      i.closest("label")?.textContent?.trim().startsWith(label),
  )!;
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      input.tagName === "SELECT" ? HTMLSelectElement.prototype : HTMLInputElement.prototype,
      "value",
    )!.set!.call(input, value);
    input.dispatchEvent(new Event("change", { bubbles: true }));
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
  await flush();
}

it("STU-400/413/416: wizard saves one group with inputs, a slot and the component name", async () => {
  const { api, session } = await setup();
  vi.mocked(api.apply).mockImplementation(async (_s, operations) => extracted(session, operations));
  await click("Сохранить как компонент");
  expect(api.extractPlan).toHaveBeenCalledWith(expect.anything(), "n_block", expect.anything());
  expect(document.querySelector("[role=dialog]")).not.toBeNull();
  expect(button("Далее").disabled).toBe(true);
  await type("Имя компонента", "Showcase");
  await click("Далее");
  // Content property is an input by default; the literal variant is optional.
  const title = document.querySelector<HTMLInputElement>(
    '[aria-label="Сделать входом Heading.text"]',
  )!;
  expect(title.checked).toBe(true);
  await click("Далее");
  await type("Имя слота", "footer");
  await type("Первый узел", "1");
  await type("Последний узел", "1");
  await click("Добавить слот");
  expect(document.body.textContent).toContain("footer · Button");
  await click("Далее");
  expect(document.body.textContent).toContain("Slot · footer");
  await click("Сохранить");
  expect(api.apply).toHaveBeenCalledTimes(1);
  const [, operations, , , reason] = vi.mocked(api.apply).mock.calls[0]!;
  expect(operations).toEqual([
    {
      type: "component.extract",
      target: documentId,
      payload: {
        nodeId: "n_block",
        name: "Showcase",
        inputs: { text: { from: [{ nodeId: "n_title", prop: "text" }] } },
        slots: { footer: { nodeIds: ["n_btn"] } },
      },
    },
  ]);
  expect(reason).toBe("Studio: save as component «Showcase»");
  expect(document.querySelector("[role=dialog]")).toBeNull();
  // STU-414: the instance is selected and named after its component.
  expect(editor.store.getState().selected).toBe("n_inst");
  await flush();
  expect(editor.store.getState().components[componentId]).toBeDefined();
  expect(document.body.textContent).toContain("Showcase");
  expect(button("Открыть компонент")).toBeDefined();
});

it("STU-413: an uncertain outcome retries only the frozen body and key", async () => {
  const { api, session } = await setup();
  vi.mocked(api.apply)
    .mockRejectedValueOnce(new StudioError("NETWORK_UNCERTAIN", true))
    .mockImplementationOnce(async (_s, operations) => extracted(session, operations));
  await click("Сохранить как компонент");
  await type("Имя компонента", "Showcase");
  for (let i = 0; i < 3; i++) await click("Далее");
  await click("Сохранить");
  expect(document.body.textContent).toContain("Результат сохранения неизвестен");
  expect(button("Отмена").disabled).toBe(true);
  await click("Повторить сохранение");
  const calls = vi.mocked(api.apply).mock.calls;
  expect(calls).toHaveLength(2);
  expect(calls[1]![1]).toEqual(calls[0]![1]);
  expect(calls[1]![2]).toBe(calls[0]![2]);
  expect(calls[1]![0].changeset.seq).toBe(calls[0]![0].changeset.seq);
  expect(document.querySelector("[role=dialog]")).toBeNull();
});

it("STU-410/413: a server rejection returns to the step and keeps the answers", async () => {
  const { api } = await setup();
  const failure = new StudioError("OPERATION_INVALID");
  failure.operation = {
    code: "COMPONENT_NAME_TAKEN",
    diagnostics: [{ code: "COMPONENT_NAME_TAKEN", pointer: "/payload/name" }],
  };
  vi.mocked(api.apply).mockRejectedValueOnce(failure);
  await click("Сохранить как компонент");
  await type("Имя компонента", "Taken");
  for (let i = 0; i < 3; i++) await click("Далее");
  await click("Сохранить");
  expect(document.body.textContent).toContain("Компонент с таким именем уже есть");
  expect(document.querySelector<HTMLInputElement>("textarea")).not.toBeNull();
  expect([...document.querySelectorAll("input")].some((i) => i.value === "Taken")).toBe(true);
  expect(editor.store.getState().status).toBe("ready");
});

it("STU-400/401: blockers and the page root disable the action with a reason", async () => {
  await setup(plan({ blockers: [{ code: "COMPONENT_EXTRACT_PROTECTED" }] }));
  await click("Сохранить как компонент");
  expect(document.body.textContent).toContain("Блок содержит зону");
  expect(button("Далее").disabled).toBe(true);
  await click("Отмена");
  await act(async () => editor.select("n_root"));
  await flush();
  expect(button("Сохранить как компонент").disabled).toBe(true);
  expect(document.body.textContent).toContain("Корень страницы нельзя");
});

it("STU-404 (Q-4): text without an input is copied unless the page no longer needs it", async () => {
  const { api, session } = await setup();
  vi.mocked(api.apply).mockImplementation(async (_s, operations) => extracted(session, operations));
  await click("Сохранить как компонент");
  await type("Имя компонента", "Showcase");
  await click("Далее");
  await act(async () =>
    document.querySelector<HTMLInputElement>('[aria-label="Сделать входом Heading.text"]')!.click(),
  );
  await flush();
  expect(document.body.textContent).toContain("Текст станет общим");
  expect(document.querySelector<HTMLSelectElement>('[aria-label="Текст t_title"]')!.value).toBe(
    "copy",
  );
  await type("Текст t_title", "move");
  await click("Далее");
  await click("Далее");
  await click("Сохранить");
  const [, operations] = vi.mocked(api.apply).mock.calls[0]!;
  expect(operations[0]!.payload).toMatchObject({ localContent: { t_title: "move" } });
  expect(operations[0]!.payload.inputs).toBeUndefined();
});

it("STU-412/415: canvas clicks inside an instance select it; the component opens in Structure", async () => {
  const { api, session } = await setup();
  extracted(session, [{ type: "component.extract", target: documentId, payload: {} }]);
  await act(async () => editor.refresh());
  await flush();
  const frame = { postMessage: vi.fn() };
  editor.bindFrame(frame);
  const origin = session.config.appOrigin;
  const ready = {
    cms: "preview",
    v: 1,
    type: "ready",
    payload: { documentId, protocolVersion: 1 },
  };
  editor.receive({ origin, source: frame, data: ready });
  editor.receive({ origin, source: frame, data: ready });
  editor.receive({
    origin,
    source: frame,
    data: {
      cms: "preview",
      v: 1,
      type: "node.click",
      payload: { nodeId: "n_title", instance: "n_root/n_inst/n_title" },
    },
  });
  expect(editor.store.getState().selected).toBe("n_inst");
  const component = structuredClone(session);
  component.record = {
    id: componentId,
    kind: "component",
    path: null,
    body: (await api.components!(session, [componentId]))[componentId]!,
  };
  vi.mocked(api.open).mockImplementation(async (id) =>
    structuredClone(id === componentId ? component : session),
  );
  vi.mocked(api.preview).mockClear();
  await click("Открыть компонент");
  expect(editor.store.getState().session?.record?.id).toBe(componentId);
  expect(editor.store.getState().pageReturn).toBe(documentId);
  expect(api.preview).not.toHaveBeenCalled();
  expect(document.body.textContent).toContain("Компонент: Showcase");
  expect(button("Сохранить как компонент")).toBeUndefined();
  await click("Вернуться к странице");
  expect(editor.store.getState().session?.record?.id).toBe(documentId);
});
