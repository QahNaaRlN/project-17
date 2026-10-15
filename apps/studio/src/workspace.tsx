import { useRef, useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import type { Editor } from "./editor.js";
import { StudioError, type Api, type Changeset } from "./types.js";
import { Button, Dialog, Notice } from "./components.js";

const copy = {
  ru: {
    workspace: "Страницы и черновики",
    page: "Страница",
    draft: "Мой черновик",
    choosePage: "Выберите страницу",
    chooseDraft: "Выберите черновик",
    open: "Открыть",
    newDraft: "Новый черновик",
    title: "Название черновика",
    create: "Создать и открыть",
    cancel: "Отмена",
    loading: "Загрузка…",
    emptyPages: "Нет доступных страниц",
    emptyDrafts: "У вас нет открытых черновиков для этой страницы",
    morePages: "Ещё страницы",
    moreDrafts: "Ещё черновики",
    refresh: "Обновить списки",
    failed: "Не удалось загрузить список",
    uncertain: "Ответ о создании не получен. Повторите запрос: второй черновик не создастся.",
    retry: "Повторить создание",
    retryOpen: "Открыть созданный черновик",
    createError: "Не удалось создать черновик",
    openError: "Не удалось открыть черновик",
    draftOnly: "Страница существует только в черновике. Продолжите работу в нём.",
    catalogue: "Страницы",
    description: "Выберите страницу и рабочий черновик, чтобы продолжить редактирование.",
    search: "Поиск страниц",
    find: "Найти",
    clear: "Сбросить поиск",
    noMatches: "Нет страниц, соответствующих поиску",
    route: "Маршрут",
    availability: "Доступность",
    head: "В каталоге",
    draftPage: "Только в черновике",
    select: "Выбрать страницу",
    loaded: "Загружено страниц",
    partialError: "Не удалось загрузить следующие страницы. Уже загруженные доступны.",
  },
  en: {
    workspace: "Pages and drafts",
    page: "Page",
    draft: "My draft",
    choosePage: "Choose a page",
    chooseDraft: "Choose a draft",
    open: "Open",
    newDraft: "New draft",
    title: "Draft name",
    create: "Create and open",
    cancel: "Cancel",
    loading: "Loading…",
    emptyPages: "No available pages",
    emptyDrafts: "You have no open drafts for this page",
    morePages: "More pages",
    moreDrafts: "More drafts",
    refresh: "Refresh lists",
    failed: "Could not load list",
    uncertain:
      "The create response was lost. Retry the request: it will not create a second draft.",
    retry: "Retry creation",
    retryOpen: "Open created draft",
    createError: "Could not create draft",
    openError: "Could not open draft",
    draftOnly: "This page exists only in a draft. Continue working in that draft.",
    catalogue: "Pages",
    description: "Choose a page and a working draft to continue editing.",
    search: "Search pages",
    find: "Search",
    clear: "Clear search",
    noMatches: "No pages match your search",
    route: "Route",
    availability: "Availability",
    head: "In catalogue",
    draftPage: "Draft only",
    select: "Select page",
    loaded: "Pages loaded",
    partialError: "Could not load more pages. Previously loaded pages remain available.",
  },
};
interface Attempt {
  documentId: string;
  title: string;
  key: string;
  createdId?: string;
}

export function Workspace({
  api,
  editor,
  locale,
  busy,
  compact = false,
  onOpened,
}: {
  api: Api;
  editor: Editor;
  locale: "ru" | "en";
  busy: boolean;
  compact?: boolean;
  onOpened?: () => void;
}) {
  const t = copy[locale];
  const [searchInput, setSearchInput] = useState(""),
    [search, setSearch] = useState("");
  const saved = new URLSearchParams(window.location.hash.slice(1));
  const [documentId, setDocumentId] = useState(saved.get("document") ?? "");
  const [changesetId, setChangesetId] = useState(saved.get("changeset") ?? "");
  const [creating, setCreating] = useState(false),
    [title, setTitle] = useState("");
  const [pending, setPending] = useState(false),
    [error, setError] = useState<"create" | "open" | null>(null);
  const [uncertain, setUncertain] = useState(false),
    [createdId, setCreatedId] = useState<string | null>(null);
  const attempt = useRef<Attempt | null>(null),
    running = useRef(false);
  const [recentDraft, setRecentDraft] = useState<{ documentId: string; draft: Changeset } | null>(
    null,
  );
  const pages = useInfiniteQuery({
    queryKey: ["studio-pages", search],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      search ? api.pages(pageParam, signal, search) : api.pages(pageParam, signal),
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    retry: false,
  });
  const drafts = useInfiniteQuery({
    queryKey: ["studio-drafts", documentId],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => api.drafts(documentId, pageParam, signal),
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    retry: false,
    enabled: !!documentId,
  });
  const pageItems = pages.data?.pages.flatMap((p) => p.items) ?? [];
  const draftItems = drafts.data?.pages.flatMap((p) => p.items) ?? [];
  // A new CS may fall beyond the first catalogue page; keep the acknowledged choice visible.
  if (
    recentDraft?.documentId === documentId &&
    !draftItems.some((d) => d.id === recentDraft.draft.id)
  )
    draftItems.push(recentDraft.draft);
  const selectedPage = pageItems.find((p) => p.id === documentId);
  const selectedDraft = draftItems.find((d) => d.id === changesetId);
  const locked = busy || pending || uncertain || !!createdId;
  function choosePage(id: string) {
    if (locked) return;
    setDocumentId(id);
    setChangesetId("");
    setCreating(false);
    setRecentDraft(null);
    setError(null);
  }

  async function open(doc: string, cs: string) {
    await editor.open(doc, cs);
    const state = editor.store.getState();
    if (
      state.status !== "ready" ||
      state.session?.record?.id !== doc ||
      state.session.changeset.id !== cs
    )
      throw new StudioError("OPEN_FAILED");
    window.location.hash = new URLSearchParams({ document: doc, changeset: cs }).toString();
    onOpened?.();
  }
  async function create() {
    if (running.current || busy) return;
    if (!attempt.current && (!selectedPage?.hasHead || !title.trim())) return;
    const current = attempt.current ?? {
      documentId,
      title: title.trim(),
      key: crypto.randomUUID(),
    };
    attempt.current = current;
    running.current = true;
    setPending(true);
    setError(null);
    try {
      if (!current.createdId) {
        const result = await api.createDraft(current.title, current.key);
        setRecentDraft({ documentId: current.documentId, draft: result });
        current.createdId = result.id;
        setCreatedId(result.id);
        setUncertain(false);
        setChangesetId(result.id);
      }
      await drafts.refetch();
      await open(current.documentId, current.createdId);
      attempt.current = null;
      setCreatedId(null);
      setCreating(false);
      setTitle("");
    } catch (cause) {
      if (current.createdId) setError("open");
      else if (cause instanceof StudioError && cause.uncertain) setUncertain(true);
      else {
        attempt.current = null;
        setUncertain(false);
        setError("create");
      }
    } finally {
      running.current = false;
      setPending(false);
    }
  }
  return (
    <section
      className={`workspace-picker ${compact ? "compact" : "page-catalogue"}`}
      aria-label={t.workspace}
    >
      {!compact && (
        <>
          <div className="catalogue-heading">
            <div>
              <p className="studio-eyebrow">Project 17</p>
              <h1>{t.catalogue}</h1>
              <p>{t.description}</p>
            </div>
          </div>
          <form
            className="page-search"
            onSubmit={(event) => {
              event.preventDefault();
              if (!locked) setSearch(searchInput.trim());
            }}
          >
            <label>
              {t.search}
              <input
                type="search"
                aria-label={t.search}
                value={searchInput}
                maxLength={400}
                disabled={locked}
                onChange={(event) => setSearchInput(event.target.value)}
              />
            </label>
            <Button type="submit" disabled={locked || [...searchInput.trim()].length > 200}>
              {t.find}
            </Button>
            {search && (
              <Button
                disabled={locked}
                onClick={() => {
                  setSearchInput("");
                  setSearch("");
                }}
              >
                {t.clear}
              </Button>
            )}
          </form>
          {pages.isPending && <Notice>{t.loading}</Notice>}
          {pages.isSuccess && pageItems.length === 0 && (
            <Notice>{search ? t.noMatches : t.emptyPages}</Notice>
          )}
          {pageItems.length > 0 && (
            <div className="pages-table-scroll">
              <table className="pages-table">
                <thead>
                  <tr>
                    <th>{t.page}</th>
                    <th>{t.route}</th>
                    <th>{t.availability}</th>
                    <th>
                      <span className="sr-only">{t.select}</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {pageItems.map((page) => (
                    <tr key={page.id} data-selected={page.id === documentId}>
                      <td>
                        <strong>{page.title || page.path}</strong>
                      </td>
                      <td>
                        <code>{page.path}</code>
                      </td>
                      <td>
                        <span className={`studio-badge ${page.hasHead ? "" : "accent"}`}>
                          {page.hasHead ? t.head : t.draftPage}
                        </span>
                      </td>
                      <td>
                        <Button
                          disabled={locked}
                          aria-pressed={page.id === documentId}
                          aria-label={`${t.select} ${page.path}`}
                          onClick={() => choosePage(page.id)}
                        >
                          {t.select}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <p className="catalogue-count" role="status">
            {t.loaded}: {pageItems.length}
            {pages.isFetching && !pages.isPending ? ` · ${t.loading}` : ""}
          </p>
        </>
      )}
      <form
        className="open-bar"
        onSubmit={(event) => {
          event.preventDefault();
          if (locked || !selectedPage || !selectedDraft) return;
          setError(null);
          void open(documentId, changesetId).catch(() => setError("open"));
        }}
      >
        <label>
          {t.page}
          <select
            aria-label={t.page}
            value={selectedPage ? documentId : ""}
            disabled={locked || pages.isPending}
            onChange={(event) => {
              choosePage(event.target.value);
            }}
          >
            <option value="">{t.choosePage}</option>
            {pageItems.map((p) => (
              <option key={p.id} value={p.id}>
                {p.path}
                {p.title ? ` · ${p.title}` : ""}
              </option>
            ))}
          </select>
        </label>
        <label>
          {t.draft}
          <select
            aria-label={t.draft}
            value={selectedDraft ? changesetId : ""}
            disabled={locked || !selectedPage || drafts.isPending}
            onChange={(event) => setChangesetId(event.target.value)}
          >
            <option value="">{t.chooseDraft}</option>
            {draftItems.map((d) => (
              <option key={d.id} value={d.id}>
                {d.title}
              </option>
            ))}
          </select>
        </label>
        <Button
          type="submit"
          variant="primary"
          disabled={locked || !selectedPage || !selectedDraft}
        >
          {t.open}
        </Button>
        <button
          type="button"
          disabled={locked || !selectedPage?.hasHead}
          onClick={() => {
            setCreating(true);
            setError(null);
          }}
        >
          {t.newDraft}
        </button>
        <button
          type="button"
          disabled={locked || pages.isFetching || (!!documentId && drafts.isFetching)}
          onClick={() => {
            void pages.refetch();
            if (documentId) void drafts.refetch();
          }}
        >
          {t.refresh}
        </button>
      </form>
      <div className="picker-status" role="status">
        {(pages.isPending || (!!documentId && drafts.isPending)) && <span>{t.loading}</span>}
        {compact && pages.isSuccess && pageItems.length === 0 && (
          <span>{search ? t.noMatches : t.emptyPages}</span>
        )}
        {selectedPage && drafts.isSuccess && draftItems.length === 0 && (
          <span>{t.emptyDrafts}</span>
        )}
        {selectedPage && !selectedPage.hasHead && <span>{t.draftOnly}</span>}
        {pages.hasNextPage && (
          <button
            disabled={locked || pages.isFetching}
            onClick={() => {
              void pages.fetchNextPage();
            }}
          >
            {t.morePages}
          </button>
        )}
        {drafts.hasNextPage && (
          <button
            disabled={locked || drafts.isFetching}
            onClick={() => {
              void drafts.fetchNextPage();
            }}
          >
            {t.moreDrafts}
          </button>
        )}
      </div>
      {(pages.isError || (!!documentId && drafts.isError)) && (
        <Notice error>{pages.isFetchNextPageError ? t.partialError : t.failed}</Notice>
      )}
      {creating && (
        <Dialog
          title={t.newDraft}
          locked={locked}
          onDismiss={() => {
            setCreating(false);
            setError(null);
          }}
        >
          <form
            className="create-bar"
            onSubmit={(event) => {
              event.preventDefault();
              void create();
            }}
          >
            <label>
              {t.title}
              <input
                aria-label={t.title}
                value={title}
                required
                maxLength={200}
                disabled={locked}
                onChange={(event) => setTitle(event.target.value)}
              />
            </label>
            <Button
              type="submit"
              variant="primary"
              disabled={busy || pending || (!title.trim() && !attempt.current)}
            >
              {pending ? t.loading : createdId ? t.retryOpen : uncertain ? t.retry : t.create}
            </Button>
            <button
              type="button"
              disabled={locked}
              onClick={() => {
                setCreating(false);
                setError(null);
              }}
            >
              {t.cancel}
            </button>
          </form>
          {uncertain && <Notice error>{t.uncertain}</Notice>}
          {error && <Notice error>{error === "open" ? t.openError : t.createError}</Notice>}
        </Dialog>
      )}
      {!creating && error && (
        <Notice error>{error === "open" ? t.openError : t.createError}</Notice>
      )}
    </section>
  );
}
