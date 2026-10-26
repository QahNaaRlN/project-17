import { useEffect, useState } from "react";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { useStore } from "zustand";
import type { Api, WorkQueueFilters } from "./types.js";
import { StudioError } from "./types.js";
import type { Editor } from "./editor.js";
import { Button, Notice } from "./components.js";
import { LegacyWorkItems } from "./legacy-work-items.js";
import { NewWork } from "./new-work.js";
type Props = {
  api: Api;
  editor: Editor;
  compact?: boolean;
  onOpened?: () => void;
  actorId?: string;
  rights?: string[] | undefined;
};
export function WorkItems(props: Props) {
  return props.api.workQueue ? <Queue {...props} /> : <LegacyWorkItems {...props} />;
}
function Queue({ api, editor, compact = true, onOpened, actorId = "service", rights }: Props) {
  const state = useStore(editor.store),
    ru = state.locale === "ru",
    client = useQueryClient();
  const [search, setSearch] = useState(""),
    [q, setQ] = useState(""),
    [status, setStatus] = useState("all"),
    [ownership, setOwnership] = useState("all"),
    [kind, setKind] = useState("all"),
    [openError, setOpenError] = useState(false);
  const filters: WorkQueueFilters = { q, state: status, ownership, kind };
  const config = useQuery({
    queryKey: ["studio-config"],
    queryFn: ({ signal }) => api.config(signal),
    retry: false,
  });
  const queryKey = [
    "studio-work-items",
    actorId,
    config.data?.project,
    config.data?.environment,
    filters,
  ];
  const list = useInfiniteQuery({
    queryKey,
    enabled: config.isSuccess,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ signal, pageParam }) => api.workQueue!(filters, pageParam, signal),
    getNextPageParam: (p) => p.nextCursor ?? undefined,
    retry: false,
    refetchInterval: 30000,
    refetchOnWindowFocus: "always",
  });
  useEffect(() => {
    const timer = setTimeout(() => setQ(search.trim()), 300);
    return () => clearTimeout(timer);
  }, [search]);
  const id = state.session?.changeset.id,
    seq = state.session?.changeset.seq,
    csState = state.session?.changeset.state;
  useEffect(() => {
    void client.invalidateQueries({ queryKey: ["studio-work-items"] });
  }, [client, id, seq, csState, state.publication]);
  const busy =
    ["loading", "saving", "uncertain"].includes(state.status) ||
    Object.values(state.dirtyFields).some(Boolean);
  const states = ru
    ? {
        open: "В работе",
        failed: "Проверки не пройдены",
        changes_requested: "Нужны правки",
        checking: "Идут проверки страниц",
        in_review: "На согласовании",
        approved: "Готов к публикации",
      }
    : {
        open: "In progress",
        failed: "Checks failed",
        changes_requested: "Changes requested",
        checking: "Page checks running",
        in_review: "In review",
        approved: "Ready to publish",
      };
  const kinds = ru
    ? { page: "Страницы", component: "Компоненты", entity: "Сущности", asset: "Ассеты" }
    : { page: "Pages", component: "Components", entity: "Entities", asset: "Assets" };
  const scopeLost =
    list.error instanceof StudioError &&
    ["UNAUTHENTICATED", "FORBIDDEN", "PROJECT_ACCESS_REQUIRED", "SESSION_CHANGED"].includes(
      list.error.code,
    );
  useEffect(() => {
    if (scopeLost) editor.accessLost();
  }, [scopeLost, editor]);
  const items = scopeLost
    ? []
    : [...new Map((list.data?.pages.flatMap((p) => p.items) ?? []).map((i) => [i.id, i])).values()];
  async function open(id: string) {
    setOpenError(false);
    await editor.openWork(id);
    if (editor.store.getState().session?.changeset.id === id) {
      window.history.replaceState({}, "", "/editor#" + new URLSearchParams({ work: id }));
      onOpened?.();
      return true;
    } else {
      setOpenError(true);
      return false;
    }
  }
  const content = (
    <>
      <div className="catalogue-heading">
        <div>
          <p className="studio-eyebrow">Project 17</p>
          {!compact && <h1>{ru ? "Работы и согласование" : "Work and review"}</h1>}
          <p>
            {ru
              ? "Все доступные активные работы выбранного окружения."
              : "All accessible active work in this environment."}
          </p>
        </div>
        {!compact &&
          api.createWork &&
          config.data &&
          !scopeLost &&
          (!rights ||
            (rights.includes("content.read") &&
              (rights.includes("content.write") || rights.includes("asset.write")))) && (
            <NewWork
              key={[actorId, config.data.project, config.data.environment].join(":")}
              api={api}
              scope={{
                actor: actorId,
                project: config.data.project,
                environment: config.data.environment,
              }}
              ru={ru}
              disabled={busy}
              onCreated={async (id) => {
                void client.invalidateQueries({ queryKey: ["studio-work-items"] });
                return open(id);
              }}
            />
          )}
        <Button
          disabled={busy || list.isFetching}
          onClick={() => {
            void client.resetQueries({ queryKey, exact: true });
          }}
        >
          {ru ? "Обновить работы" : "Refresh work"}
        </Button>
      </div>
      {!compact && (
        <div className="work-filters">
          <label>
            {ru ? "Поиск работ" : "Search work"}
            <input
              type="search"
              value={search}
              placeholder={
                ru ? "Название работы, маршрут или автор" : "Work title, route or author"
              }
              onChange={(e) => setSearch(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  setQ(search.trim());
                }
              }}
            />
          </label>
          <label>
            {ru ? "Статус" : "Status"}
            <select value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="all">{ru ? "Все статусы" : "All states"}</option>
              {Object.entries(states).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </label>
          <label>
            {ru ? "Автор" : "Author"}
            <select value={ownership} onChange={(e) => setOwnership(e.target.value)}>
              <option value="all">{ru ? "Все авторы" : "All authors"}</option>
              <option value="mine">{ru ? "Мои работы" : "My work"}</option>
              <option value="others">{ru ? "Другие авторы" : "Other authors"}</option>
            </select>
          </label>
          <label>
            {ru ? "Тип объектов" : "Object kind"}
            <select value={kind} onChange={(e) => setKind(e.target.value)}>
              <option value="all">{ru ? "Все типы" : "All kinds"}</option>
              {Object.entries(kinds).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </label>
        </div>
      )}
      {list.isPending && !config.isError && <Notice>{ru ? "Загрузка…" : "Loading…"}</Notice>}
      {(list.isError || config.isError) && (
        <Notice error>
          {list.isFetchNextPageError
            ? ru
              ? "Следующая страница не загружена. Загруженные работы доступны."
              : "Could not load more work. Loaded work remains available."
            : ru
              ? "Не удалось загрузить работы"
              : "Could not load work"}
          <Button
            onClick={() => {
              void (config.isError
                ? config.refetch()
                : list.isFetchNextPageError
                  ? list.fetchNextPage()
                  : list.refetch());
            }}
          >
            {ru ? "Повторить" : "Retry"}
          </Button>
        </Notice>
      )}
      {!list.isPending && !list.isError && !items.length && (
        <Notice>
          {q || status !== "all" || ownership !== "all" || kind !== "all"
            ? ru
              ? "Нет совпадений"
              : "No matching work"
            : ru
              ? "Нет доступных работ"
              : "No available work"}
        </Notice>
      )}
      {!!items.length && (
        <div className="pages-table-scroll">
          <table className="pages-table work-table">
            <thead>
              <tr>
                <th>{ru ? "Работа / состав" : "Work / objects"}</th>
                <th>{ru ? "Автор" : "Author"}</th>
                <th>{ru ? "Статус" : "Status"}</th>
                <th>
                  <span className="sr-only">{ru ? "Открыть работу" : "Open work"}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {items.map((i) => (
                <tr key={i.id} data-selected={id === i.id}>
                  <td>
                    <strong>{i.title}</strong>
                    <small>
                      {Object.entries(i.countsByKind)
                        .filter(([, n]) => n > 0)
                        .map(([k, n]) => kinds[k as keyof typeof kinds] + ": " + n)
                        .join(" · ") || (ru ? "Нет изменений" : "No changes")}
                    </small>
                    <time dateTime={i.updatedAt}>
                      {new Date(i.updatedAt).toLocaleString(state.locale, {
                        timeZoneName: "short",
                      })}
                    </time>
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
                    <span className="studio-badge">{states[i.state as keyof typeof states]}</span>
                  </td>
                  <td>
                    <Button
                      data-work-id={i.id}
                      disabled={busy}
                      onClick={() => {
                        void open(i.id);
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
            ? "Не удалось открыть работу. Обновите очередь и повторите."
            : "Could not open work. Refresh the queue and retry."}
        </Notice>
      )}
      {id && state.session?.work && !items.some((i) => i.id === id) && list.isSuccess && (
        <Notice>
          {ru
            ? "Выбранная работа не входит в текущую выдачу."
            : "Selected work is outside the current results."}
        </Notice>
      )}
      <p className="catalogue-count" role="status">
        {ru ? "Загружено работ" : "Loaded work"}: {items.length}
      </p>
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
