// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { ComponentRequestComposer } from "../src/capability-component-panel.js";
import { createEditor, type Editor } from "../src/editor.js";
import { nodeContract } from "../src/composed.js";
import { mockApi, documentId, changesetId, fixture } from "./fixture.js";
import { StudioError } from "../src/types.js";
import type { CapabilityRequest } from "../src/capabilities.js";

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
  );
async function click(name: string) {
  await act(async () => button(name)!.click());
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
async function submit() {
  await act(async () =>
    document
      .querySelector("form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })),
  );
}
const requestId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c44";
function request(): CapabilityRequest {
  return {
    id: requestId,
    kind: "component",
    environment: "staging",
    key: "PromoBanner",
    title: "Promo",
    description: "A banner",
    proposedContract: { props: { title: { type: "string", required: true } }, container: false },
    placement: { parentId: "n_root", index: 2 },
    acceptance: ["Shows the title"],
    status: "draft",
    revision: 1,
    changesetId,
    documentId,
    nodeId: "n_promo",
    event: "",
    ownerId: documentId,
    externalRefs: [],
    transitions: ["edit", "submit", "cancelled"],
    packs: [],
    audit: [],
  };
}
async function setup(select = "n_root", taken = false) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("crypto", webcrypto);
  const session = fixture();
  session.review.capabilityPolicy = { defaultMode: "CODE", maxMode: "CODE" };
  session.record.body.nodes.n_promo = {
    id: "n_promo",
    type: "PromoBanner",
    pending: requestId,
    props: { title: "Sale" },
  };
  session.record.body.nodes.n_root!.children = [
    ...(session.record.body.nodes.n_root!.children ?? []),
    "n_promo",
  ];
  const { api } = mockApi(session);
  api.capability = vi.fn(async () => request());
  api.openRequest = vi.fn(async () => ({ id: requestId, revision: 2 }) as never);
  api.fulfillmentCommand = vi.fn(async () => ({}) as never);
  api.capabilityCommand = vi.fn(async (attempt) => {
    if (taken) throw new StudioError("CAPABILITY_KEY_TAKEN");
    return { request: { ...request(), ...(attempt.payload as object) } as CapabilityRequest };
  });
  editor = createEditor(api);
  await editor.open(documentId, changesetId);
  editor.select(select);
  document.body.innerHTML = "<div id='root'></div>";
  root = createRoot(document.getElementById("root")!);
  await act(async () => root.render(<ComponentRequestComposer {...{ api, editor }} />));
  return { api, session };
}

it("STU-359/360/372: a container in CODE requests a component with props and initial values", async () => {
  const { api } = await setup();
  await click("Нужен другой компонент…");
  await change("Имя компонента", "PromoBanner");
  await change("Заголовок", "Promo");
  await change("Описание задачи", "A banner");
  await change("Критерии приёмки", "Shows the title");
  await click("Добавить свойство");
  await change("Имя свойства", "title");
  await act(async () => (field("Обязательный") as HTMLInputElement).click());
  await change("title", "Sale");
  await submit();
  const attempt = vi.mocked(api.capabilityCommand!).mock.calls[0]![0];
  expect(attempt.name).toBe("create-capability-request");
  expect(attempt.payload).toMatchObject({
    kind: "component",
    key: "PromoBanner",
    placement: { parentId: "n_root", index: 3 },
    initialProps: { title: "Sale" },
    proposedContract: { props: { title: { type: "string", required: true } }, container: false },
  });
  expect(attempt.payload).not.toHaveProperty("nodeId");
});

it("STU-363: a taken key offers another instance of the open request", async () => {
  const { api } = await setup("n_root", true);
  await click("Нужен другой компонент…");
  await change("Имя компонента", "PromoBanner");
  await change("Заголовок", "Promo");
  await change("Описание задачи", "A banner");
  await change("Критерии приёмки", "Shows the title");
  await submit();
  await click("Вставить ещё экземпляр");
  const call = vi.mocked(api.fulfillmentCommand!).mock.calls[0]![0];
  expect(call.name).toBe("attach-capability-request");
  expect(call.payload).toMatchObject({ requestId, placement: { parentId: "n_root" } });
  expect(document.body.textContent).toContain("Экземпляр запрошенного компонента вставлен.");
});

it("STU-372: a placeholder shows its badge, request and proposed props", async () => {
  const { api } = await setup("n_promo");
  expect(document.body.textContent).toContain("Ожидает компонент PromoBanner");
  expect(button("Нужен другой компонент…")).toBeUndefined();
  await click("Открыть запрос компонента");
  expect(api.capability).toHaveBeenCalledWith(requestId);
  expect(document.body.textContent).toContain("в n_root");
  await vi.waitFor(() =>
    expect(
      nodeContract(
        editor.store.getState().document!.nodes.n_promo,
        editor.store.getState().session!.manifest,
      )?.props,
    ).toHaveProperty("title"),
  );
});
