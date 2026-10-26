import { useLayoutEffect, useState } from "react";
import { useQuery, useInfiniteQuery } from "@tanstack/react-query";
import { useStore } from "zustand";
import type { Editor } from "./editor.js";
import type { Api, DiffBaseline, DiffValue } from "./types.js";
import { StudioError } from "./types.js";
import { Button, Notice } from "./components.js";
export function ReviewDiff({
  api,
  editor,
  onSnapshot,
  onOpen,
  onContent,
}: {
  api: Api;
  editor: Editor;
  onSnapshot: (value: string | null) => void;
  onOpen?: ((node: string) => void) | undefined;
  onContent?: ((id: string, kind: "entity" | "asset") => void) | undefined;
}) {
  const state = useStore(editor.store),
    session = state.session!,
    ru = state.locale === "ru";
  const [baseline, setBaseline] = useState<DiffBaseline>("base"),
    [selected, setSelected] = useState("");
  const allowed = ["open", "failed", "changes_requested", "in_review", "approved"].includes(
    session.changeset.state,
  );
  const locked = state.status !== "ready" || Object.values(state.dirtyFields).some(Boolean);
  const index = useQuery({
    queryKey: [
      "studio-diff",
      session.config.project,
      session.config.environment,
      session.changeset.id,
      session.changeset.seq,
      session.changeset.state,
      state.historyRevision,
    ],
    queryFn: ({ signal }) => api.diffIndex!(session, signal),
    retry: false,
    enabled: allowed,
    refetchInterval: 30000,
  });
  const object =
    index.data?.objects.find((o) => o.id === selected) ??
    index.data?.objects.find((o) => o.id === session.record?.id) ??
    index.data?.objects[0];
  const page = useInfiniteQuery({
    queryKey: [
      "studio-diff-page",
      session.config.project,
      session.changeset.id,
      index.data?.snapshot,
      session.config.environment,
      object?.id,
      baseline,
    ],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ signal, pageParam }) =>
      api.diffPage!(session, index.data!, object!.id, baseline, pageParam, signal),
    getNextPageParam: (p) => p.nextCursor ?? undefined,
    retry: false,
    enabled: allowed && !!object && index.isSuccess && !index.isFetching,
  });
  const items = page.data?.pages.flatMap((p) => p.items) ?? [];
  const invalid =
    !!page.data &&
    (new Set(items.map((i) => i.key)).size !== items.length ||
      items.length > page.data.pages[0]!.totalChanges);
  const ready =
    allowed &&
    !locked &&
    index.isSuccess &&
    !index.isFetching &&
    !page.isFetching &&
    !page.isError &&
    !invalid &&
    (!object || page.isSuccess) &&
    !state.remotePending;
  useLayoutEffect(() => {
    onSnapshot(ready ? index.data!.snapshot : null);
    return () => onSnapshot(null);
  }, [ready, index.data?.snapshot, onSnapshot]);
  const effects: Record<string, string> = ru
    ? {
        added: "Добавление",
        modified: "Изменение",
        deleted: "Удаление",
        restored: "Восстановление",
        "metadata-only": "Изменены сведения",
        unchanged: "Итоговых изменений нет",
      }
    : {
        added: "Added",
        modified: "Modified",
        deleted: "Deleted",
        restored: "Restored",
        "metadata-only": "Metadata changed",
        unchanged: "No final changes",
      };
  const categories: Record<string, string> = ru
    ? {
        structure: "Структура",
        property: "Текст и свойства",
        design: "Оформление",
        binding: "Привязки",
        condition: "Условие",
        behavior: "Поведение",
        localContent: "Локальный контент",
        metadata: "Сведения",
        content: "Контент",
        file: "Файл",
      }
    : {
        structure: "Structure",
        property: "Text and properties",
        design: "Design",
        binding: "Bindings",
        condition: "Condition",
        behavior: "Behavior",
        localContent: "Local content",
        metadata: "Metadata",
        content: "Content",
        file: "File",
      };
  const display = (v: DiffValue) =>
    !v.present
      ? ru
        ? "Отсутствует"
        : "Absent"
      : "redacted" in v
        ? ru
          ? "Служебное значение скрыто"
          : "Service value hidden"
        : "rawJson" in v
          ? v.rawJson
          : JSON.stringify(v.value, null, 2);
  const errorText = (error: unknown) =>
    error instanceof StudioError && error.code === "DIFF_SNAPSHOT_STALE"
      ? ru
        ? "Работа или окружение изменились — перечитайте сравнение."
        : "Work or environment changed. Reload the comparison."
      : error instanceof StudioError &&
          ["DIFF_LIMIT_EXCEEDED", "DIFF_PROJECTION_UNSUPPORTED"].includes(error.code)
        ? ru
          ? "Полное сравнение недоступно: превышен лимит или формат пока не поддерживается."
          : "Complete comparison is unavailable: limit or unsupported format."
        : ru
          ? "Не удалось загрузить сравнение."
          : "Could not load comparison.";
  async function go(node?: string) {
    if (locked || !object || object.workingDeleted) return;
    if (object.kind !== "document") {
      onContent?.(object.id, object.kind);
      return;
    }
    if (object.docKind !== "page") return;
    if (object.id !== session.record?.id) {
      await editor.open(object.id, session.changeset.id);
      const s = editor.store.getState();
      if (s.status !== "ready" || s.session?.record?.id !== object.id) return;
      window.location.hash = new URLSearchParams({
        document: object.id,
        changeset: session.changeset.id,
      }).toString();
    }
    const s = editor.store.getState();
    const chosen = node ?? s.document?.root;
    if (chosen && s.document?.nodes[chosen]) {
      editor.select(chosen);
      onOpen?.(chosen);
    }
  }
  return (
    <section className="review-diff" aria-label={ru ? "Изменения работы" : "Work changes"}>
      <div className="review-diff-heading">
        <h2>{ru ? "Изменения" : "Changes"}</h2>
        <Button
          disabled={locked || index.isFetching}
          onClick={() => {
            void editor.refresh();
          }}
        >
          {ru ? "Перечитать сравнение" : "Reload comparison"}
        </Button>
      </div>
      {!allowed ? (
        <Notice>
          {session.changeset.state === "checking"
            ? ru
              ? "Сравнение будет доступно после проверок страниц."
              : "The comparison is available after page checks."
            : ru
              ? "Работа завершена. Текущее сравнение недоступно."
              : "This work is closed; current comparison is unavailable."}
        </Notice>
      ) : (
        <>
          <div
            className="diff-baselines"
            role="group"
            aria-label={ru ? "Основание сравнения" : "Comparison baseline"}
          >
            <Button aria-pressed={baseline === "base"} onClick={() => setBaseline("base")}>
              {ru ? "Изменения работы" : "Work changes"}
            </Button>
            <Button
              aria-pressed={baseline === "published"}
              onClick={() => setBaseline("published")}
            >
              {ru ? "Изменения в окружении" : "Environment changes"} · {session.config.environment}
            </Button>
          </div>
          <p className="hint">
            {baseline === "base"
              ? ru
                ? "База черновика → рабочая версия"
                : "Draft base → working version"
              : ru
                ? "Опубликовано в окружении → рабочая версия"
                : "Published in environment → working version"}
          </p>
          {index.isPending && <Notice>{ru ? "Загружаем изменения…" : "Loading changes…"}</Notice>}
          {index.isError && <Notice error>{errorText(index.error)}</Notice>}
          {index.isSuccess && index.data && (
            <>
              <p className="catalogue-count" role="status">
                {ru ? "Объектов в работе" : "Objects in work"}: {index.data.objects.length} · #
                {index.data.seq}
                {index.isFetching
                  ? ru
                    ? " · Обновляем сравнение…"
                    : " · Updating comparison…"
                  : ""}
              </p>
              {!index.data.objects.length && (
                <Notice>{ru ? "Итоговых изменений нет" : "No final changes"}</Notice>
              )}
              <div className="diff-layout">
                <div
                  className="diff-object-list"
                  role="group"
                  aria-label={ru ? "Изменённые объекты" : "Changed objects"}
                >
                  {index.data.objects.map((o) => (
                    <Button
                      key={o.id}
                      aria-pressed={o.id === object?.id}
                      onClick={() => setSelected(o.id)}
                    >
                      <strong>{o.title}</strong>
                      <span>
                        {o.afterPath ??
                          o.beforePath ??
                          (ru
                            ? { document: "Документ", entity: "Сущность", asset: "Ассет" }[o.kind]
                            : o.kind)}
                      </span>
                      <small>
                        {effects[o.summaries[baseline].effect]} ·{" "}
                        {o.summaries[baseline].changeCount}
                      </small>
                    </Button>
                  ))}
                </div>
                {object && (
                  <div className="diff-details">
                    <div className="review-diff-heading">
                      <h3>{object.title}</h3>
                      {(object.docKind === "page" || object.kind !== "document") &&
                        !object.workingDeleted &&
                        object.summaries[baseline].effect !== "deleted" && (
                          <Button
                            disabled={locked}
                            onClick={() => {
                              void go();
                            }}
                          >
                            {ru ? "Открыть объект" : "Open object"}
                          </Button>
                        )}
                    </div>
                    {object.baseStale && (
                      <Notice error>
                        {ru
                          ? "База работы изменилась. Перед решением потребуется rebase."
                          : "The work base changed. Rebase is required before a decision."}
                      </Notice>
                    )}
                    {page.isPending && (
                      <Notice>{ru ? "Загружаем изменения…" : "Loading changes…"}</Notice>
                    )}
                    {(page.isError || invalid) && <Notice error>{errorText(page.error)}</Notice>}
                    {page.isSuccess && !items.length && (
                      <Notice>{ru ? "Итоговых изменений нет" : "No final changes"}</Notice>
                    )}
                    {page.isSuccess &&
                      !invalid &&
                      items.map((change) => (
                        <article className="diff-change" key={change.key}>
                          <div className="diff-change-heading">
                            <strong>{categories[change.category]}</strong>
                            <code>{change.pointer || "/"}</code>
                            {change.locale && <span>{change.locale}</span>}
                            {change.nodeId &&
                              object.docKind === "page" &&
                              change.kind !== "remove" &&
                              !object.workingDeleted &&
                              object.summaries[baseline].effect !== "deleted" &&
                              (object.id !== session.record?.id ||
                                !!state.document?.nodes[change.nodeId]) && (
                                <Button
                                  disabled={locked}
                                  onClick={() => {
                                    void go(change.nodeId);
                                  }}
                                >
                                  {ru ? "Выбрать блок" : "Select block"}
                                </Button>
                              )}
                          </div>
                          <div className="diff-values">
                            <div>
                              <small>{ru ? "До" : "Before"}</small>
                              <pre>{display(change.before)}</pre>
                            </div>
                            <div>
                              <small>{ru ? "После" : "After"}</small>
                              <pre>{display(change.after)}</pre>
                            </div>
                          </div>
                        </article>
                      ))}
                    {page.isSuccess && page.data && (
                      <p className="hint">
                        {ru ? "Загружено изменений" : "Changes loaded"}: {items.length} /{" "}
                        {page.data.pages[0]!.totalChanges}
                      </p>
                    )}
                    {page.hasNextPage && (
                      <Button
                        disabled={page.isFetching}
                        onClick={() => {
                          void page.fetchNextPage();
                        }}
                      >
                        {ru ? "Ещё изменения" : "More changes"}
                      </Button>
                    )}
                  </div>
                )}
              </div>
            </>
          )}
          {!ready && (
            <p className="hint" role="status">
              {ru
                ? "Решение доступно после загрузки актуального сравнения и сохранения введённых значений."
                : "Load the current comparison and save input before deciding."}
            </p>
          )}
        </>
      )}
    </section>
  );
}
