import { useRef, useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import type { Editor } from "./editor.js";
import { StudioError, type Api, type Changeset } from "./types.js";

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
}: {
  api: Api;
  editor: Editor;
  locale: "ru" | "en";
  busy: boolean;
}) {
  const t = copy[locale];
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
    queryKey: ["studio-pages"],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => api.pages(pageParam, signal),
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

  async function open(doc: string, cs: string) {
    await editor.open(doc, cs);
    const state = editor.store.getState();
    if (
      state.status !== "ready" ||
      state.session?.record.id !== doc ||
      state.session.changeset.id !== cs
    )
      throw new StudioError("OPEN_FAILED");
    window.location.hash = new URLSearchParams({ document: doc, changeset: cs }).toString();
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
    <section className="workspace-picker" aria-label={t.workspace}>
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
              setDocumentId(event.target.value);
              setChangesetId("");
              setCreating(false);
              setRecentDraft(null);
              setError(null);
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
        <button disabled={locked || !selectedPage || !selectedDraft}>{t.open}</button>
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
        {pages.isSuccess && pageItems.length === 0 && <span>{t.emptyPages}</span>}
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
      {(pages.isError || (!!documentId && drafts.isError)) && <p role="alert">{t.failed}</p>}
      {creating && (
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
              disabled={locked}
              onChange={(event) => setTitle(event.target.value)}
            />
          </label>
          <button disabled={busy || pending || (!title.trim() && !attempt.current)}>
            {pending ? t.loading : createdId ? t.retryOpen : uncertain ? t.retry : t.create}
          </button>
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
      )}
      {uncertain && <p role="alert">{t.uncertain}</p>}
      {error && <p role="alert">{error === "open" ? t.openError : t.createError}</p>}
    </section>
  );
}
