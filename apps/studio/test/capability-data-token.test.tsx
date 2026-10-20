// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { DataTokenRequestComposer } from "../src/capability-data-token-panel.js";
import { createEditor, type Editor } from "../src/editor.js";
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
const requestId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c55";
function request(kind: "dataSource" | "token"): CapabilityRequest {
  return {
    id: requestId,
    kind,
    environment: "staging",
    key: kind === "token" ? "spacing.huge" : "store.recommendations",
    title: "Request",
    description: "Task",
    proposedContract:
      kind === "token"
        ? { category: "spacing", value: "48px" }
        : { params: {}, result: { type: "list", of: { type: "object", fields: {} } } },
    placement:
      kind === "token" ? { nodeId: "n_root", property: "padding" } : { dataSourceKey: "rec" },
    acceptance: ["Works"],
    status: "draft",
    revision: 1,
    changesetId,
    documentId,
    nodeId: kind === "token" ? "n_root" : "rec",
    event: kind === "token" ? "padding" : "",
    ownerId: documentId,
    externalRefs: [],
    transitions: ["edit", "submit", "cancelled"],
    packs: [],
    audit: [],
  };
}
async function setup(kind: "dataSource" | "token", taken = false, placeholders = false) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("crypto", webcrypto);
  const session = fixture();
  session.review.capabilityPolicy = { defaultMode: "CODE", maxMode: "CODE" };
  if (placeholders) {
    session.record.body.dataSources = {
      rec: { source: "store.recommendations", pending: requestId },
    } as never;
    session.record.body.nodes.n_root!.design = { padding: "pending:" + requestId } as never;
  }
  const { api } = mockApi(session);
  api.capability = vi.fn(async () => request(kind));
  api.openRequest = vi.fn(async () => ({ id: requestId, revision: 2 }) as never);
  api.fulfillmentCommand = vi.fn(async () => ({}) as never);
  api.capabilityCommand = vi.fn(async (attempt) => {
    if (taken) throw new StudioError("CAPABILITY_KEY_TAKEN");
    return { request: { ...request(kind), ...(attempt.payload as object) } as CapabilityRequest };
  });
  editor = createEditor(api);
  await editor.open(documentId, changesetId);
  editor.select("n_root");
  document.body.innerHTML = "<div id='root'></div>";
  root = createRoot(document.getElementById("root")!);
  await act(async () => root.render(<DataTokenRequestComposer {...{ api, editor }} />));
  return { api };
}
async function texts() {
  await change("Заголовок", "Request");
  await change("Описание задачи", "Task");
  await change("Критерии приёмки", "Works");
}

it("STU-380/381: the page requests a data source with params, values and result shape", async () => {
  const { api } = await setup("dataSource");
  await click("Нужен другой источник…");
  await change("Ключ на странице", "rec");
  await change("Ключ источника", "store.recommendations");
  await texts();
  await change("limit", "4");
  await change("Имя поля", "title");
  await submit();
  const attempt = vi.mocked(api.capabilityCommand!).mock.calls[0]![0];
  expect(attempt.payload).toMatchObject({
    kind: "dataSource",
    key: "store.recommendations",
    placement: { dataSourceKey: "rec" },
    initialProps: { limit: { lit: 4 } },
    proposedContract: {
      params: { limit: { type: "number" } },
      result: { type: "list", of: { type: "object", fields: { title: { type: "string" } } } },
    },
  });
});

it("STU-380/384: a node requests a spacing token; a taken key joins the open request", async () => {
  const { api } = await setup("token", true);
  await click("Нужен другой токен…");
  await change("Имя токена отступа", "huge");
  await change("Значение, например 48px", "48px");
  await change("Ширина экрана", "md");
  await texts();
  await submit();
  expect(vi.mocked(api.capabilityCommand!).mock.calls[0]![0].payload).toMatchObject({
    kind: "token",
    key: "spacing.huge",
    placement: { nodeId: "n_root", property: "padding", breakpoint: "md" },
    proposedContract: { category: "spacing", value: "48px" },
  });
  await click("Присоединить к открытому запросу");
  expect(vi.mocked(api.fulfillmentCommand!).mock.calls[0]![0].payload).toMatchObject({
    requestId,
    placement: { nodeId: "n_root", property: "padding", breakpoint: "md" },
  });
  expect(document.body.textContent).toContain("Заглушка присоединена к открытому запросу.");
});

it("STU-392: placeholders show their badges and open their requests", async () => {
  const { api } = await setup("token", false, true);
  expect(document.body.textContent).toContain("Ожидает источник $data.rec");
  expect(document.body.textContent).toContain("Ожидает токен");
  await click("Открыть запрос токена");
  expect(api.capability).toHaveBeenCalledWith(requestId);
});
