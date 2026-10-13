import { useInfiniteQuery } from "@tanstack/react-query";
import { useStore } from "zustand";
import type { Api } from "./types.js";
import type { Editor } from "./editor.js";
export function WorkItems({ api, editor }: { api: Api; editor: Editor }) {
  const state = useStore(editor.store),
    ru = state.locale === "ru";
  const list = useInfiniteQuery({
    queryKey: ["studio-work-items"],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ signal, pageParam }) => api.workItems(pageParam, signal),
    getNextPageParam: (p) => p.nextCursor ?? undefined,
    retry: false,
  });
  const busy =
    ["loading", "saving", "uncertain"].includes(state.status) || !!state.dirtyFields.json;
  const states: Record<string, string> = ru
    ? {
        open: "В работе",
        failed: "Проверки не пройдены",
        changes_requested: "Нужны правки",
        in_review: "На согласовании",
        approved: "Готов к публикации",
      }
    : {
        open: "In progress",
        failed: "Checks failed",
        changes_requested: "Changes requested",
        in_review: "In review",
        approved: "Ready to publish",
      };
  return (
    <section className="work-items" aria-label={ru ? "Работы и согласование" : "Work and review"}>
      <details>
        <summary>{ru ? "Мои работы и согласование" : "My work and reviews"}</summary>
        <button
          disabled={busy || list.isFetching}
          onClick={() => {
            void list.refetch();
          }}
        >
          {ru ? "Обновить работы" : "Refresh work"}
        </button>
        {list.isPending && <p role="status">{ru ? "Загрузка…" : "Loading…"}</p>}
        {list.isError && (
          <p role="alert">{ru ? "Не удалось загрузить работы" : "Could not load work"}</p>
        )}
        {list.isSuccess && list.data.pages.every((p) => p.items.length === 0) && (
          <p>{ru ? "Нет доступных работ" : "No available work"}</p>
        )}
        <ul>
          {list.data?.pages
            .flatMap((p) => p.items)
            .map((i) => (
              <li key={i.id}>
                <strong>{i.title}</strong> · {i.path} · {i.ownerName} · {states[i.state]} ·{" "}
                {i.own
                  ? ru
                    ? "Моя работа"
                    : "My work"
                  : ru
                    ? "Работа другого автора"
                    : "Another author's work"}{" "}
                <button
                  disabled={busy}
                  onClick={async () => {
                    await editor.open(i.documentId, i.id);
                    const s = editor.store.getState();
                    if (s.status === "ready" && s.session?.changeset.id === i.id)
                      window.location.hash = new URLSearchParams({
                        document: i.documentId,
                        changeset: i.id,
                      }).toString();
                  }}
                >
                  {ru ? "Открыть работу" : "Open work"}
                </button>
              </li>
            ))}
        </ul>
        {list.hasNextPage && (
          <button
            disabled={busy || list.isFetching}
            onClick={() => {
              void list.fetchNextPage();
            }}
          >
            {ru ? "Ещё работы" : "More work"}
          </button>
        )}
      </details>
    </section>
  );
}
