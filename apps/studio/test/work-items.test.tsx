// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { WorkItems } from "../src/work-items.js";
import { createEditor, type Editor } from "../src/editor.js";
import { mockApi, documentId, changesetId } from "./fixture.js";
import type { Api, WorkItem } from "../src/types.js";
let root: Root, editor: Editor, client: QueryClient;
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  client?.clear();
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});
const item: WorkItem = {
  id: changesetId,
  documentId,
  title: "Новая витрина",
  path: "/store",
  ownerId: "alice",
  ownerName: "Alice",
  own: true,
  state: "open",
};
async function setup(load: Api["workItems"], compact = false, locale: "ru" | "en" = "ru") {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const { api, session } = mockApi();
  api.workItems = vi.fn(load);
  editor = createEditor(api);
  editor.locale(locale);
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  document.body.innerHTML = '<div id="root"></div>';
  root = createRoot(document.getElementById("root")!);
  const opened = vi.fn();
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <WorkItems api={api} editor={editor} compact={compact} onOpened={opened} />
      </QueryClientProvider>,
    ),
  );
  await flush();
  return { api, session, opened };
}
async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 15));
  });
}
const button = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === text)!;
async function change(el: HTMLInputElement | HTMLSelectElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      el.tagName === "SELECT" ? HTMLSelectElement.prototype : HTMLInputElement.prototype,
      "value",
    )!.set!.call(el, value);
    el.dispatchEvent(new Event(el.tagName === "SELECT" ? "change" : "input", { bubbles: true }));
  });
}
it("STU-144: loaded search/state/ownership filters preserve distinct pages of one work item", async () => {
  await setup(async () => ({
    items: [
      item,
      { ...item, documentId: "page2", path: "/about" },
      { ...item, id: "review", ownerName: "Bob", own: false, state: "in_review" },
    ],
    nextCursor: null,
  }));
  expect(document.querySelectorAll("tbody tr")).toHaveLength(3);
  await change(document.querySelector("input")!, "ABOUT");
  expect(document.querySelectorAll("tbody tr")).toHaveLength(1);
  await change(document.querySelector("input")!, "");
  const selects = document.querySelectorAll("select");
  await change(selects[1]!, "others");
  expect(document.querySelector("tbody")!.textContent).toContain("Bob");
  await change(selects[0]!, "approved");
  expect(document.body.textContent).toContain("Среди загруженных работ нет совпадений");
  await change(selects[0]!, "all");
  await change(selects[1]!, "mine");
  expect(document.querySelectorAll("tbody tr")).toHaveLength(2);
});
it("STU-144/145: pagination errors retain loaded work, retry uses cursor; opening never sends workflow", async () => {
  let fail = true;
  const { api, opened } = await setup(async (cursor) => {
    if (cursor && fail) throw Error("private detail");
    return {
      items: cursor ? [{ ...item, documentId: "second", path: "/second" }] : [item],
      nextCursor: cursor ? null : "next",
    };
  });
  await act(async () => button("Ещё работы").click());
  await flush();
  expect(document.body.textContent).toContain("Загруженные работы доступны");
  expect(document.body.textContent).not.toContain("private detail");
  fail = false;
  await act(async () => button("Ещё работы").click());
  await flush();
  expect(document.querySelectorAll("tbody tr")).toHaveLength(2);
  await act(async () => button("Открыть работу").click());
  await flush();
  expect(opened).toHaveBeenCalledOnce();
  expect(window.location.hash).toContain(changesetId);
  expect(api.workflow).not.toHaveBeenCalled();
});
it("STU-145: open failures, dirty navigation lock and fresh queue after workflow", async () => {
  const { api } = await setup(async () => ({ items: [item], nextCursor: null }));
  vi.mocked(api.open).mockRejectedValueOnce(Error("private detail"));
  await act(async () => button("Открыть работу").click());
  await flush();
  expect(document.body.textContent).toContain("Не удалось открыть работу");
  await act(async () => button("Открыть работу").click());
  await flush();
  await act(async () => editor.formDirty("text", true));
  expect(button("Открыть работу").disabled).toBe(true);
  expect(button("Обновить работы").disabled).toBe(true);
  await act(async () => editor.formDirty("text", false));
  const reads = vi.mocked(api.workItems).mock.calls.length;
  await act(async () => editor.workflow("submit"));
  await flush();
  expect(vi.mocked(api.workItems).mock.calls.length).toBeGreaterThan(reads);
});
it("STU-144: empty, failed and compact English queue expose refresh", async () => {
  const { api } = await setup(
    async () => {
      throw Error("private detail");
    },
    true,
    "en",
  );
  expect(document.body.textContent).toContain("Could not load work");
  expect(document.querySelector("summary")!.textContent).toBe("My work and reviews");
  vi.mocked(api.workItems).mockResolvedValue({ items: [], nextCursor: null });
  await act(async () => button("Refresh work").click());
  await flush();
  expect(document.body.textContent).toContain("No available work");
});
