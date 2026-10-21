// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { AgentPanel } from "../src/agent-panel.js";
import { createEditor } from "../src/editor.js";
import { createApi } from "../src/api.js";
import { parseEvent } from "../src/realtime.js";
import { StudioError, type AgentRun } from "../src/types.js";
import { mockApi, fixture, documentId, changesetId } from "./fixture.js";
const cfg = {
  provider: "openrouter" as const,
  model: "test/model",
  configured: true,
  canDelegate: true,
  monthlyTokens: 1000000,
  userMonthlyTokens: 100000,
};
const run: AgentRun = {
  id: documentId,
  changesetId,
  documentId,
  environment: "staging",
  model: "test/model",
  prompt: "<script>Task</script>",
  state: "running",
  stepSeq: 3,
  steps: [
    { seq: 1, kind: "tool", data: { name: "get_changeset" } },
    { seq: 2, kind: "message", data: { text: "<script>Reply</script>" } },
    {
      seq: 3,
      kind: "result",
      data: {
        name: "apply_operations",
        isError: true,
        denial: { ruleId: "no-checkout", condition: "pathPrefix=/checkout", operationIndex: 0 },
      },
    },
  ],
  tokens: 100,
  reserved: 12000,
  usageKnown: false,
};
it("STU-124/125: API carries only task metadata, parses safe state and preserves retry keys", async () => {
  const fetcher = vi.fn<typeof fetch>(async (input) =>
    Response.json(String(input).endsWith("/agent-config") ? cfg : { items: [run] }),
  );
  const api = createApi(fetcher);
  expect(await api.agentConfig!()).toEqual(cfg);
  expect(await api.agentRuns!(fixture())).toEqual([run]);
  await api.agentCommand!("start-agent-run", { prompt: "Task" }, "same-key");
  expect(fetcher.mock.calls.at(-1)![1]).toMatchObject({
    headers: { "Idempotency-Key": "same-key" },
    body: JSON.stringify({ payload: { prompt: "Task" }, reason: "Studio: explicit Agent task" }),
  });
  for (const invalid of [null, { ...cfg, configured: "yes" }, { ...cfg, monthlyTokens: null }])
    await expect(createApi(async () => Response.json(invalid)).agentConfig!()).rejects.toThrow(
      "RESPONSE_INVALID",
    );
  for (const invalid of [
    null,
    { items: [{ ...run, documentId: changesetId }] },
    { items: [{ ...run, steps: [{ seq: 1, kind: "message", data: { text: 4 } }] }] },
    { items: [{ ...run, steps: [{ seq: 1, kind: "result", data: { denial: { ruleId: {} } } }] }] },
    { items: [{ ...run, steps: [{ seq: 1, kind: "result", data: { denial: null } }] }] },
  ])
    await expect(
      createApi(async () => Response.json(invalid)).agentRuns!(fixture()),
    ).rejects.toThrow("RESPONSE_INVALID");
  expect(
    parseEvent(
      "event: agent\ndata: " +
        JSON.stringify({ items: [{ id: documentId, state: "running", stepSeq: 3 }] }) +
        "\n",
    ),
  ).toEqual({ type: "agent" });
  expect(() => parseEvent('event: agent\ndata: {"items":[{}]}')).toThrow("RESPONSE_INVALID");
});
it("STU-120/123/125: explicit start, dirty-form boundary, lost response retry, cancellation and escaped output", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("crypto", webcrypto);
  const { api, session } = mockApi(),
    editor = createEditor(api),
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    host = document.createElement("div"),
    root = createRoot(host);
  document.body.append(host);
  let items: AgentRun[] = [];
  api.agentConfig = vi.fn(async () => cfg);
  api.agentRuns = vi.fn(async () => items);
  const command = vi
    .fn<NonNullable<typeof api.agentCommand>>()
    .mockRejectedValueOnce(new StudioError("NETWORK_UNCERTAIN", true))
    .mockImplementation(async (name) => {
      items = name === "start-agent-run" ? [run] : [{ ...run, state: "cancelled" }];
    });
  api.agentCommand = command;
  const button = (name: string) =>
    [...host.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === name)!;
  const wait = async (check: () => void) =>
    vi.waitFor(async () => {
      await act(async () => {
        await Promise.resolve();
      });
      check();
    });
  try {
    await act(async () => {
      await editor.open(documentId, changesetId);
      root.render(
        <QueryClientProvider client={client}>
          <AgentPanel {...{ api, editor }} />
        </QueryClientProvider>,
      );
    });
    await wait(() => expect(host.textContent).toContain("test/model"));
    await act(async () => {
      const area = host.querySelector("textarea")!;
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(
        area,
        "Измени страницу",
      );
      area.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await act(async () => editor.formDirty("props", true));
    expect(button("Запустить задачу").disabled).toBe(true);
    await act(async () => editor.formDirty("props", false));
    await act(async () => button("Запустить задачу").click());
    await wait(() => expect(host.textContent).toContain("Ответ не дошёл"));
    await act(async () => button("Повторить запрос").click());
    await wait(() => expect(host.textContent).toContain("Task"));
    expect(command.mock.calls[0]).toEqual(command.mock.calls[1]);
    expect(command.mock.calls[0]![1]).toMatchObject({
      documentId,
      changesetId,
      expectedSeq: 1,
      prompt: "Измени страницу",
    });
    expect(host.querySelector("script")).toBeNull();
    expect(host.textContent).toContain("часть расхода неизвестна");
    // STU-434: the step names the rule and the condition of the refusal.
    expect(host.textContent).toContain(
      "Запрещено политикой: правило no-checkout, pathPrefix=/checkout",
    );
    await act(async () => button("Отменить задачу").click());
    await wait(() => expect(host.textContent).toContain("Отменено"));
    await act(async () => {
      editor.locale("en");
    });
    expect(host.textContent).toContain("Cancelled");
    await act(async () => button("Refresh state").click());
    session.history.canUndo = true;
    await act(async () => editor.store.setState({ session: { ...session } }));
    const undo = vi.spyOn(editor, "changeHistory").mockResolvedValue(undefined);
    await act(async () => button("Undo last change group").click());
    expect(undo).toHaveBeenCalledWith("undo");
    await act(async () => editor.store.setState({ session: null }));
    expect(host.textContent).toBe("");
  } finally {
    await act(async () => root.unmount());
    editor.dispose();
    client.clear();
    host.remove();
    vi.unstubAllGlobals();
  }
});
it("STU-124: unconfigured provider, missing delegation and query errors are visible", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const { api } = mockApi(),
    editor = createEditor(api),
    client = new QueryClient(),
    host = document.createElement("div"),
    root = createRoot(host);
  document.body.append(host);
  api.agentConfig = async () => ({ ...cfg, configured: false, canDelegate: false });
  api.agentRuns = async () => {
    throw new Error("failed");
  };
  try {
    await act(async () => {
      await editor.open(documentId, changesetId);
      root.render(
        <QueryClientProvider client={client}>
          <AgentPanel {...{ api, editor }} />
        </QueryClientProvider>,
      );
    });
    await vi.waitFor(async () => {
      await act(async () => {
        await Promise.resolve();
      });
      expect(host.textContent).toContain("OpenRouter не настроен");
      expect(host.textContent).toContain("Не удалось загрузить");
    });
    expect(host.querySelector("textarea")!.disabled).toBe(true);
    await act(async () => editor.locale("en"));
    expect(host.textContent).toContain("not configured");
  } finally {
    await act(async () => root.unmount());
    editor.dispose();
    client.clear();
    host.remove();
    vi.unstubAllGlobals();
  }
});
