import { useEffect, useState } from "react";
import { useStore } from "zustand";
import { useQueryClient } from "@tanstack/react-query";
import type { Api } from "./types.js";
import type { Editor } from "./editor.js";
import { startRealtime, type Presence } from "./realtime.js";

export function PresencePanel({ api, editor }: { api: Api; editor: Editor }) {
  const state = useStore(editor.store),
    client = useQueryClient();
  const [items, setItems] = useState<Presence[]>([]),
    [status, setStatus] = useState<"connecting" | "live" | "offline">("connecting");
  const documentId = state.session?.record?.id,
    changesetId = state.session?.changeset.id;
  useEffect(() => {
    const session = editor.store.getState().session;
    if (!session?.record) return;
    setItems([]);
    return startRealtime(
      api,
      { ...session, record: session.record },
      {
        presence: setItems,
        status: setStatus,
        agent() {
          void client.invalidateQueries({ queryKey: ["agent-runs"] });
        },
        invalidate() {
          void client.invalidateQueries({
            predicate: (query) =>
              query.queryKey[0] !== "studio-config" && query.queryKey[0] !== "studio-account",
          });
          editor.remoteChanged();
          void editor.refreshRemote();
        },
      },
    );
  }, [api, editor, client, documentId, changesetId]);
  useEffect(() => {
    void editor.refreshRemote();
  }, [editor, state.remotePending, state.status, state.dirtyFields]);
  if (!state.session) return null;
  const ru = state.locale === "ru";
  return (
    <section aria-label={ru ? "Присутствие" : "Presence"} className="presence-panel">
      <h2>{ru ? "На этой странице" : "On this page"}</h2>
      <p role="status">
        {status === "live"
          ? ru
            ? "Обновления подключены"
            : "Live updates connected"
          : status === "offline"
            ? ru
              ? "Связь прервана. Переподключение…"
              : "Offline. Reconnecting…"
            : ru
              ? "Подключение обновлений…"
              : "Connecting live updates…"}
      </p>
      {state.remotePending && (
        <p role="status">
          {ru
            ? "Сохранённое состояние изменилось. Сохраните или сбросьте ввод, чтобы перечитать обновления."
            : "Saved state changed. Save or discard input to load updates."}
        </p>
      )}
      <ul>
        {items.map((item) => (
          <li key={item.actorId + item.changesetId}>
            {item.actorName} · {item.changesetTitle} · <code>{item.changesetId.slice(-8)}</code>
          </li>
        ))}
      </ul>
    </section>
  );
}
