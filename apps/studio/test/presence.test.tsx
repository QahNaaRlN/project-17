// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { expect, it, vi } from "vitest";
import { createEditor } from "../src/editor.js";
import { PresencePanel } from "../src/presence.js";
import { mockApi, documentId, changesetId } from "./fixture.js";

it("STU-025/114: присутствие/CS, отложенное обновление, переводы и отмена при закрытии", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const { api, session } = mockApi(),
    editor = createEditor(api),
    client = new QueryClient(),
    host = document.createElement("div"),
    root = createRoot(host);
  document.body.append(host);
  const invalidate = vi.spyOn(client, "invalidateQueries");
  let controller: ReadableStreamDefaultController<Uint8Array>;
  api.events = vi.fn(
    async () =>
      new Response(
        new ReadableStream({
          start(value) {
            controller = value;
          },
        }),
      ),
  );
  const heartbeat = vi.fn<NonNullable<typeof api.presence>>(async () => {});
  api.presence = heartbeat;
  try {
    await act(async () => {
      await editor.open(documentId, changesetId);
      root.render(
        <QueryClientProvider client={client}>
          <PresencePanel {...{ api, editor }} />
        </QueryClientProvider>,
      );
    });
    const push = async (data: string) => {
      await act(async () => {
        controller.enqueue(new TextEncoder().encode(data));
        await new Promise((resolve) => setTimeout(resolve, 0));
      });
    };
    await push(
      "event: presence\ndata: " +
        JSON.stringify({
          items: [
            {
              actorId: documentId,
              actorName: "<Редактор>",
              changesetId,
              changesetTitle: "Летний CS",
            },
          ],
        }) +
        "\n\n",
    );
    expect(host.textContent).toContain("<Редактор> · Летний CS");
    expect(host.querySelector("Редактор")).toBeNull();
    await act(async () => editor.formDirty("json", true));
    session.changeset.seq = 2;
    await push('id: 2\nevent: invalidate\ndata: {"revision":"2"}\n\n');
    expect(invalidate).toHaveBeenCalled();
    expect(editor.store.getState().session?.changeset.seq).toBe(1);
    expect(host.textContent).toContain("Сохранённое состояние изменилось");
    await act(async () => {
      editor.locale("en");
      expect(editor.store.getState().locale).toBe("ru");
      // Check the translated notice independently of the dirty navigation guard.
      editor.store.setState({ locale: "en" });
    });
    expect(host.textContent).toContain("Saved state changed");
    await act(async () => {
      editor.formDirty("json", false);
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    expect(editor.store.getState().session?.changeset.seq).toBe(2);
    expect(editor.store.getState().remotePending).toBe(false);
    await act(async () => {
      editor.store.setState({ session: null });
    });
    expect(host.textContent).toBe("");
    expect(heartbeat.mock.calls.at(-1)![2]).toBe(true);
  } finally {
    await act(async () => root.unmount());
    editor.dispose();
    client.clear();
    host.remove();
    vi.unstubAllGlobals();
  }
});
