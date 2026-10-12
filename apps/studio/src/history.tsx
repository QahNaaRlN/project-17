import { useInfiniteQuery } from "@tanstack/react-query";
import type { Api, Session, HistoryAction } from "./types.js";
import type { Editor } from "./editor.js";

const copy = {
  ru: {
    title: "История черновика",
    scope: "Отмена действует на весь черновик, включая другие страницы.",
    undo: "Отменить",
    redo: "Повторить",
    retry: "Проверить отмену или повтор",
    empty: "Пока нет изменений",
    more: "Более ранние действия",
    error: "Не удалось загрузить историю. Перечитайте черновик.",
    loading: "Загрузка…",
    edit: "Изменение",
    text: "Изменение текста",
    design: "Изменение оформления",
    props: "Изменение свойства",
    undoEvent: "Отмена",
    redoEvent: "Повтор",
    other: "Есть изменения другого объекта",
    blocked: "Действие недоступно",
    dropped: "Исключено при переносе",
    conflict: "Конфликт",
    refresh: "Перечитать черновик",
  },
  en: {
    title: "Draft history",
    scope: "Undo affects the entire draft, including other pages.",
    undo: "Undo",
    redo: "Redo",
    retry: "Check undo or redo",
    empty: "No changes yet",
    more: "Earlier actions",
    error: "Could not load history. Reload the draft.",
    loading: "Loading…",
    edit: "Change",
    text: "Text change",
    design: "Design change",
    props: "Property change",
    undoEvent: "Undo",
    redoEvent: "Redo",
    other: "Includes another object",
    blocked: "Action unavailable",
    dropped: "Dropped during rebase",
    conflict: "Conflict",
    refresh: "Reload draft",
  },
};
const blockedCopy = {
  ru: {
    FORBIDDEN: "Нет права на отмену или повтор.",
    CHANGESET_NOT_OWNER: "Черновик принадлежит другому пользователю.",
    CHANGESET_STATE_INVALID: "Этот черновик закрыт для правок.",
    CHANGESET_HAS_CONFLICTS: "Сначала разрешите конфликты черновика.",
    UNDO_NOT_SUPPORTED: "Это действие не поддерживает отмену.",
    REDO_NOT_SUPPORTED: "Это действие не поддерживает повтор.",
  },
  en: {
    FORBIDDEN: "You cannot undo or redo this action.",
    CHANGESET_NOT_OWNER: "This draft belongs to another user.",
    CHANGESET_STATE_INVALID: "This draft is closed for edits.",
    CHANGESET_HAS_CONFLICTS: "Resolve the draft conflicts first.",
    UNDO_NOT_SUPPORTED: "This action does not support undo.",
    REDO_NOT_SUPPORTED: "This action does not support redo.",
  },
};
function description(action: HistoryAction, t: typeof copy.ru) {
  if (action.kind === "undo") return t.undoEvent;
  if (action.kind === "redo") return t.redoEvent;
  if (action.operations.some((o) => o.type.startsWith("localContent."))) return t.text;
  if (action.operations.some((o) => o.type === "node.setDesign")) return t.design;
  if (action.operations.some((o) => o.type === "node.setProps")) return t.props;
  return t.edit;
}
export function HistoryPanel({
  session,
  api,
  editor,
  locale,
  revision,
  busy,
  retry,
}: {
  session: Session;
  api: Api;
  editor: Editor;
  locale: "ru" | "en";
  revision: number;
  busy: boolean;
  retry: boolean;
}) {
  const t = copy[locale];
  const query = useInfiniteQuery({
    queryKey: ["studio-history", session.changeset.id, revision],
    initialPageParam: undefined as number | undefined,
    initialData: { pages: [session.history], pageParams: [undefined] },
    staleTime: Infinity,
    retry: false,
    queryFn: ({ pageParam, signal }) => api.history(session, pageParam, signal),
    getNextPageParam: (last) => last.nextBeforeSeq ?? undefined,
  });
  const items = query.data.pages.flatMap((page) => page.items);
  return (
    <section className="history-panel" aria-label={t.title}>
      <div className="history-heading">
        <h2>{t.title}</h2>
        <button
          disabled={busy || !session.history.canUndo}
          title={session.history.undoReason ? t.blocked : "Ctrl/Cmd+Z"}
          onClick={() => {
            void editor.changeHistory("undo");
          }}
        >
          {t.undo}
        </button>
        <button
          disabled={busy || !session.history.canRedo}
          title={session.history.redoReason ? t.blocked : "Ctrl/Cmd+Shift+Z"}
          onClick={() => {
            void editor.changeHistory("redo");
          }}
        >
          {t.redo}
        </button>
        {retry && (
          <button
            disabled={!busy || editor.store.getState().status === "saving"}
            onClick={() => {
              void editor.retryHistory();
            }}
          >
            {t.retry}
          </button>
        )}
      </div>
      <p className="hint">{t.scope}</p>
      {session.history.undoReason && !session.history.undoReason.startsWith("NOTHING_TO_") && (
        <p>
          {blockedCopy[locale][session.history.undoReason as keyof typeof blockedCopy.ru] ??
            t.blocked}
        </p>
      )}
      {items.length === 0 && <p>{t.empty}</p>}
      <ol>
        {items.map((action) => (
          <li key={action.id}>
            <strong>{description(action, t)}</strong>
            <span>
              {" "}
              · {action.actorName} · #{action.firstSeq}
              {action.firstSeq !== action.lastSeq ? `–${action.lastSeq}` : ""}
            </span>
            <time dateTime={action.createdAt}>
              {" "}
              · {new Date(action.createdAt).toLocaleString(locale)}
            </time>
            {action.operations.some((o) => o.target !== session.record.id) && (
              <small>{t.other}</small>
            )}
            {action.operations.some((o) => o.status === "conflict") && <small>{t.conflict}</small>}
            {action.operations.every((o) => o.status === "dropped") && <small>{t.dropped}</small>}
          </li>
        ))}
      </ol>
      {query.isError && (
        <p role="alert">
          {t.error}
          <button
            disabled={busy}
            onClick={() => {
              void editor.refresh();
            }}
          >
            {t.refresh}
          </button>
        </p>
      )}
      {query.hasNextPage && (
        <button
          disabled={busy || query.isFetching}
          onClick={() => {
            void query.fetchNextPage();
          }}
        >
          {query.isFetching ? t.loading : t.more}
        </button>
      )}
    </section>
  );
}
