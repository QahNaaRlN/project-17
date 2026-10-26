import { useEffect, useState } from "react";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useStore } from "zustand";
import type { Api } from "./types.js";
import type { Editor } from "./editor.js";
import { Button, Notice } from "./components.js";

export function LegacyWorkItems({
  api,
  editor,
  compact = true,
  onOpened,
}: {
  api: Api;
  editor: Editor;
  compact?: boolean;
  onOpened?: () => void;
}) {
  const state = useStore(editor.store),
    ru = state.locale === "ru";
  const client = useQueryClient();
  const [search, setSearch] = useState(""),
    [filter, setFilter] = useState("all"),
    [owner, setOwner] = useState("all"),
    [openError, setOpenError] = useState(false);
  const list = useInfiniteQuery({
    queryKey: ["studio-work-items"],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ signal, pageParam }) => api.workItems(pageParam, signal),
    getNextPageParam: (p) => p.nextCursor ?? undefined,
    retry: false,
  });
  // Workflow and realtime reads may remove a work item from the actor's queue.
  const id = state.session?.changeset.id,
    seq = state.session?.changeset.seq,
    status = state.session?.changeset.state;
  useEffect(() => {
    void client.invalidateQueries({ queryKey: ["studio-work-items"] });
  }, [client, id, seq, status]);
  const busy =
    ["loading", "saving", "uncertain"].includes(state.status) ||
    Object.values(state.dirtyFields).some(Boolean);
  const states: Record<string, string> = ru
    ? {
        open: "В работе",
        failed: "Проверки не пройдены",
        changes_requested: "Нужны правки",
        checking: "Идут проверки страниц",
        in_review: "На согласовании",
        approved: "Готов к публикации",
        merged: "Опубликован",
        abandoned: "Закрыт",
      }
    : {
        open: "In progress",
        failed: "Checks failed",
        changes_requested: "Changes requested",
        checking: "Page checks running",
        in_review: "In review",
        approved: "Ready to publish",
        merged: "Published",
        abandoned: "Closed",
      };
  const items = [
    ...new Map(
      (list.data?.pages.flatMap((p) => p.items) ?? []).map((i) => [`${i.id}:${i.documentId}`, i]),
    ).values(),
  ];
  const query = search.trim().toLocaleLowerCase(state.locale);
  const visible = items.filter(
    (i) =>
      (filter === "all" || i.state === filter) &&
      (owner === "all" || (owner === "mine" ? i.own : !i.own)) &&
      `${i.title} ${i.path} ${i.ownerName}`.toLocaleLowerCase(state.locale).includes(query),
  );
  const content = (
    <>
      {!compact && (
        <div className="catalogue-heading">
          <div>
            <p className="studio-eyebrow">Project 17</p>
            <h1>{ru ? "Работы и согласование" : "Work and review"}</h1>
            <p>
              {ru
                ? "Продолжайте свои работы и проверяйте изменения других авторов."
                : "Continue your work and review changes by other authors."}
            </p>
          </div>
          <Button
            disabled={busy || list.isFetching}
            onClick={() => {
              void list.refetch();
            }}
          >
            {ru ? "Обновить работы" : "Refresh work"}
          </Button>
        </div>
      )}
      {!compact && (
        <div className="work-filters">
          <label>
            {ru ? "Поиск работ" : "Search work"}
            <input type="search" value={search} onChange={(e) => setSearch(e.target.value)} />
          </label>
          <label>
            {ru ? "Статус" : "Status"}
            <select value={filter} onChange={(e) => setFilter(e.target.value)}>
              <option value="all">{ru ? "Все статусы" : "All states"}</option>
              {Object.entries(states).map(([key, label]) => (
                <option key={key} value={key}>
                  {label}
                </option>
              ))}
            </select>
          </label>
          <label>
            {ru ? "Автор" : "Author"}
            <select value={owner} onChange={(e) => setOwner(e.target.value)}>
              <option value="all">{ru ? "Все авторы" : "All authors"}</option>
              <option value="mine">{ru ? "Мои работы" : "My work"}</option>
              <option value="others">{ru ? "Другие авторы" : "Other authors"}</option>
            </select>
          </label>
        </div>
      )}
      {compact && (
        <Button
          disabled={busy || list.isFetching}
          onClick={() => {
            void list.refetch();
          }}
        >
          {ru ? "Обновить работы" : "Refresh work"}
        </Button>
      )}
      {list.isPending && <Notice>{ru ? "Загрузка…" : "Loading…"}</Notice>}
      {list.isError && (
        <Notice error>
          {list.isFetchNextPageError
            ? ru
              ? "Не удалось загрузить следующие работы. Загруженные работы доступны."
              : "Could not load more work. Loaded work remains available."
            : ru
              ? "Не удалось загрузить работы"
              : "Could not load work"}
        </Notice>
      )}
      {list.isSuccess && !items.length && (
        <Notice>{ru ? "Нет доступных работ" : "No available work"}</Notice>
      )}
      {!!items.length && !visible.length && (
        <Notice>
          {ru ? "Среди загруженных работ нет совпадений" : "No matches in loaded work"}
        </Notice>
      )}
      {!!visible.length && (
        <div className="pages-table-scroll">
          <table className="pages-table work-table">
            <thead>
              <tr>
                <th>{ru ? "Работа / страница" : "Work / page"}</th>
                <th>{ru ? "Автор" : "Author"}</th>
                <th>{ru ? "Статус" : "Status"}</th>
                <th>
                  <span className="sr-only">{ru ? "Открыть работу" : "Open work"}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {visible.map((i) => (
                <tr
                  key={`${i.id}:${i.documentId}`}
                  data-selected={id === i.id && state.session?.record?.id === i.documentId}
                >
                  <td>
                    <strong>{i.title}</strong>
                    <code>{i.path}</code>
                  </td>
                  <td>
                    {i.ownerName}
                    <small>
                      {i.own
                        ? ru
                          ? "Моя работа"
                          : "My work"
                        : ru
                          ? "Работа другого автора"
                          : "Another author's work"}
                    </small>
                  </td>
                  <td>
                    <span
                      className={`studio-badge ${["approved", "in_review"].includes(i.state) ? "accent" : ""}`}
                    >
                      {states[i.state] ?? i.state}
                    </span>
                  </td>
                  <td>
                    <Button
                      disabled={busy}
                      onClick={async () => {
                        setOpenError(false);
                        await editor.open(i.documentId, i.id);
                        const s = editor.store.getState();
                        if (
                          s.status === "ready" &&
                          s.session?.changeset.id === i.id &&
                          s.session.record?.id === i.documentId
                        ) {
                          window.location.hash = new URLSearchParams({
                            document: i.documentId,
                            changeset: i.id,
                          }).toString();
                          onOpened?.();
                        } else setOpenError(true);
                      }}
                    >
                      {ru ? "Открыть работу" : "Open work"}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {openError && (
        <Notice error>
          {ru
            ? "Не удалось открыть работу. Попробуйте ещё раз."
            : "Could not open work. Try again."}
        </Notice>
      )}
      {!compact && (
        <p className="catalogue-count" role="status">
          {ru ? "Показано страниц работ" : "Work pages shown"}: {visible.length} / {items.length}.{" "}
          {ru
            ? "Поиск и фильтры применяются к загруженным работам."
            : "Search and filters apply to loaded work."}
        </p>
      )}
      {list.hasNextPage && (
        <Button
          disabled={busy || list.isFetching}
          onClick={() => {
            void list.fetchNextPage();
          }}
        >
          {ru ? "Ещё работы" : "More work"}
        </Button>
      )}
    </>
  );
  return (
    <section
      className={`work-items ${compact ? "compact" : "work-catalogue"}`}
      aria-label={ru ? "Работы и согласование" : "Work and review"}
    >
      {compact ? (
        <details>
          <summary>{ru ? "Мои работы и согласование" : "My work and reviews"}</summary>
          {content}
        </details>
      ) : (
        content
      )}
    </section>
  );
}
