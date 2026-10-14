import { useEffect, useRef, useState } from "react";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { useStore } from "zustand";
import { Button, Dialog, Notice } from "./components.js";
import { StudioError, type Api, type DiffValue } from "./types.js";
import type { Editor } from "./editor.js";
import type { RollbackAttempt } from "./publication-history.js";

function diffText(v: DiffValue, ru: boolean): string {
  if (!v.present) return ru ? "Отсутствует" : "Absent";
  if ("redacted" in v) return ru ? "Служебное поле" : "Service field";
  if ("rawJson" in v) return v.rawJson;
  return JSON.stringify(v.value, null, 2);
}
export function PublicationHistory({ api, editor }: { api: Api; editor: Editor }) {
  const state = useStore(editor.store),
    ru = state.locale === "ru",
    client = useQueryClient();
  const [id, setId] = useState(
    () => new URLSearchParams(window.location.hash.slice(1)).get("publication") ?? "",
  );
  const [preview, setPreview] = useState(false),
    [object, setObject] = useState("");
  const [dialog, setDialog] = useState(false),
    [reason, setReason] = useState("");
  const [stage, setStage] = useState<"idle" | "sending" | "uncertain">("idle"),
    [error, setError] = useState("");
  const [receipt, setReceipt] = useState("");
  const config = useQuery({
    queryKey: ["studio-config"],
    queryFn: ({ signal }) => api.config(signal),
    retry: false,
  });
  const attempt = useRef<RollbackAttempt | null>(null),
    generation = useRef(0);
  const sending = useRef(false);
  const cardElement = useRef<HTMLDivElement>(null),
    listElement = useRef<HTMLDivElement>(null);
  const focusPublication = useRef<string | null>(id || null);
  const ownDirty = stage !== "idle" || reason.length > 0;
  const locked =
    state.status === "saving" ||
    state.status === "loading" ||
    state.status === "uncertain" ||
    Object.entries(state.dirtyFields).some(([k, v]) => k !== "publication" && v);
  const list = useInfiniteQuery({
    queryKey: ["publication-history"],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ signal, pageParam }) => api.publications!(pageParam, signal),
    getNextPageParam: (p) => p.nextCursor ?? undefined,
    retry: false,
  });
  const card = useQuery({
    queryKey: ["publication-card", id, preview, state.historyRevision, state.remotePending],
    queryFn: ({ signal }) => api.publication!(id, preview, signal),
    enabled: !!id && stage === "idle",
    retry: false,
    refetchInterval: 30000,
  });
  const visible = card.isSuccess && !card.isError ? card.data : undefined;
  useEffect(() => {
    if (visible && focusPublication.current === id) {
      cardElement.current?.focus();
      focusPublication.current = null;
    }
  }, [visible, id]);
  const selected = visible?.objects.find((o) => o.id === object) ?? visible?.objects[0];
  const page = useInfiniteQuery({
    queryKey: ["publication-diff", id, preview, visible?.snapshot, selected?.id],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ signal, pageParam }) =>
      api.publicationDiff!(visible!, selected!.id, preview, pageParam, signal),
    getNextPageParam: (p) => p.nextCursor ?? undefined,
    enabled: !!visible && !!selected && !card.isFetching && stage === "idle",
    retry: false,
  });
  const items = page.data?.pages.flatMap((p) => p.items) ?? [];
  const full =
    page.isSuccess &&
    !page.isError &&
    !page.isFetching &&
    !page.hasNextPage &&
    items.length === (page.data?.pages[0]?.totalChanges ?? 0) &&
    new Set(items.map((i) => i.key)).size === items.length;
  const ready =
    !!visible &&
    visible.complete &&
    visible.canRollback &&
    preview &&
    !card.isFetching &&
    !locked &&
    stage === "idle" &&
    (!selected || full);
  useEffect(() => {
    editor.formDirty("publication", ownDirty);
    return () => editor.formDirty("publication", false);
  }, [editor, ownDirty]);
  useEffect(() => {
    if (stage === "idle" && (card.isFetching || card.isError || page.isError)) {
      setDialog(false);
      setReason("");
    }
  }, [card.isFetching, card.isError, page.isError, visible?.snapshot, stage]);
  useEffect(() => {
    const prevent = (e: BeforeUnloadEvent) => {
      if (ownDirty) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", prevent);
    return () => {
      window.removeEventListener("beforeunload", prevent);
    };
  }, [ownDirty]);
  useEffect(
    () => () => {
      generation.current++;
    },
    [],
  );
  function choose(next: string) {
    if (ownDirty || locked) return;
    generation.current++;
    focusPublication.current = next;
    setId(next);
    setPreview(false);
    setObject("");
    setDialog(false);
    setError("");
    const hash = new URLSearchParams(window.location.hash.slice(1));
    hash.set("publication", next);
    window.history.replaceState({}, "", "/editor#" + hash);
  }
  async function send(retry = false) {
    if (sending.current) return;
    if (!retry && (!ready || !reason.trim() || [...reason.trim()].length > 2000)) return;
    if (
      retry &&
      (!attempt.current || Date.now() - attempt.current.startedAt >= 24 * 60 * 60 * 1000)
    ) {
      setError(
        ru
          ? "Сначала установите результат по истории; срок безопасного повтора истёк."
          : "Check the history first; the safe retry period has expired.",
      );
      return;
    }
    const token = generation.current;
    sending.current = true;
    setStage("sending");
    setError("");
    editor.formDirty("publication", true);
    try {
      if (!retry) {
        const fresh = await api.publication!(id, true);
        if (fresh.snapshot !== visible!.snapshot || !fresh.canRollback) {
          setDialog(false);
          setReason("");
          setStage("idle");
          setError("ROLLBACK_SNAPSHOT_STALE");
          await card.refetch();
          return;
        }
        attempt.current = {
          publicationId: id,
          resetHead: false,
          expectedRollbackSnapshot: fresh.snapshot,
          reason: reason.trim(),
          key: crypto.randomUUID(),
          startedAt: Date.now(),
        };
      }
      const result = await api.rollback!(attempt.current!);
      if (token !== generation.current) return;
      attempt.current = null;
      setReceipt(result.id);
      focusPublication.current = result.id;
      setStage("idle");
      setReason("");
      setDialog(false);
      editor.formDirty("publication", false);
      setPreview(false);
      setId(result.id);
      setObject("");
      const hash = new URLSearchParams(window.location.hash.slice(1));
      hash.set("publication", result.id);
      window.history.replaceState({}, "", "/editor#" + hash);
      await client.invalidateQueries({
        predicate: (q) =>
          ["publication-history", "publication-card", "studio-diff", "studio-review"].includes(
            String(q.queryKey[0]),
          ),
      });
      editor.remoteChanged();
    } catch (cause) {
      if (token !== generation.current) return;
      const uncertain = attempt.current && (!(cause instanceof StudioError) || cause.uncertain);
      setStage(uncertain ? "uncertain" : "idle");
      setDialog(false);
      setError(cause instanceof StudioError ? cause.code : "ROLLBACK_FAILED");
      if (!uncertain) {
        attempt.current = null;
        setReason("");
        editor.formDirty("publication", false);
        await card.refetch();
      }
    } finally {
      sending.current = false;
    }
  }
  const entries =
    list.isError && !list.isFetchNextPageError
      ? []
      : (list.data?.pages.flatMap((p) => p.items) ?? []);
  const kinds: Record<string, string> = ru
    ? { publish: "Публикация", promote: "Перенос", rollback: "Откат" }
    : { publish: "Publication", promote: "Promotion", rollback: "Rollback" };
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
        deleted: "Removed",
        restored: "Restored",
        "metadata-only": "Metadata changed",
        unchanged: "No final changes",
      };
  return (
    <section
      className="publication-history"
      aria-label={ru ? "История публикаций" : "Publication history"}
    >
      <div className="work-heading">
        <div>
          <h1>{ru ? "Публикации" : "Publications"}</h1>
          <p className="hint">
            {ru
              ? "История выбранного окружения и отмена публикаций"
              : "Environment history and publication rollback"}
          </p>
        </div>
        <Button
          disabled={ownDirty || locked}
          onClick={() => {
            void list.refetch();
            if (id) void card.refetch();
          }}
        >
          {ru ? "Обновить историю" : "Refresh history"}
        </Button>
      </div>
      {list.isLoading && <p role="status">{ru ? "Загрузка…" : "Loading…"}</p>}
      {list.isError && (
        <Notice error>
          {ru ? "Не удалось загрузить историю" : "Could not load history"}{" "}
          <Button
            disabled={stage !== "idle"}
            onClick={() => void (list.isFetchNextPageError ? list.fetchNextPage() : list.refetch())}
          >
            {ru ? "Повторить" : "Retry"}
          </Button>
        </Notice>
      )}
      {list.isSuccess && !entries.length && (
        <p>{ru ? "Публикаций пока нет" : "No publications yet"}</p>
      )}
      <div className="publication-layout">
        <div className="publication-list" ref={listElement} tabIndex={-1}>
          {entries.map((p) => (
            <Button
              key={p.id}
              disabled={ownDirty || locked}
              aria-pressed={p.id === id}
              onClick={() => choose(p.id)}
            >
              <strong>
                {kinds[p.kind] ?? p.kind} · {p.environment}
              </strong>
              <span>
                {new Date(p.createdAt).toLocaleString(ru ? "ru-RU" : "en-GB", {
                  timeZoneName: "short",
                })}
              </span>
              <small>
                {p.actorId} · {ru ? "Объектов" : "Objects"}: {p.objectCount}
              </small>
            </Button>
          ))}
          {list.hasNextPage && (
            <Button
              disabled={list.isFetching || ownDirty}
              onClick={() => void list.fetchNextPage()}
            >
              {ru ? "Ещё публикации" : "More publications"}
            </Button>
          )}
        </div>
        <div className="publication-card" ref={cardElement} tabIndex={-1}>
          {id && (
            <Button disabled={ownDirty || locked} onClick={() => listElement.current?.focus()}>
              {ru ? "К списку публикаций" : "Back to publication list"}
            </Button>
          )}
          {receipt === id && (
            <Notice>
              {ru
                ? "Публикация отменена. Черновики сохранены."
                : "Publication rolled back. Drafts preserved."}
            </Notice>
          )}
          {receipt === id &&
            config.data &&
            visible?.objects
              .filter(
                (o) =>
                  o.docKind === "page" &&
                  o.afterPath?.startsWith("/") &&
                  !o.afterPath.startsWith("//"),
              )
              .map((o) => (
                <a
                  key={o.id}
                  href={config.data!.appOrigin + o.afterPath}
                  target="_blank"
                  rel="noreferrer"
                >
                  {ru ? "Открыть страницу в приложении" : "Open page in application"}: {o.title}
                </a>
              ))}
          {id && card.isLoading && (
            <p role="status">{ru ? "Загрузка публикации…" : "Loading publication…"}</p>
          )}
          {card.isError && (
            <Notice error>
              {ru
                ? "Публикация недоступна. Перечитайте историю."
                : "Publication unavailable. Refresh history."}
            </Notice>
          )}
          {visible && (
            <>
              <h2>
                {kinds[visible.publication.kind] ?? visible.publication.kind} ·{" "}
                {visible.publication.environment}
              </h2>
              <p>
                {new Date(visible.publication.createdAt).toLocaleString(ru ? "ru-RU" : "en-GB", {
                  timeZoneName: "short",
                })}
              </p>
              <p>
                {ru ? "Автор" : "Author"}: <code>{visible.publication.actorId}</code>
              </p>
              <p>{visible.publication.reason}</p>
              {visible.publication.changesetId && (
                <p>
                  CS: <code>{visible.publication.changesetId}</code>
                </p>
              )}
              {visible.publication.sourcePublicationId && (
                <Button
                  disabled={ownDirty}
                  onClick={() => choose(visible.publication.sourcePublicationId!)}
                >
                  {ru ? "Исходная публикация" : "Source publication"}
                </Button>
              )}
              {visible.publication.manifestChanged ? (
                <Notice>
                  {ru
                    ? "Публикация меняла manifest. Откат схем из Studio пока недоступен."
                    : "This publication changed the manifest. Schema rollback is unavailable in Studio."}{" "}
                  <code>
                    {visible.publication.previousManifestId ?? "∅"} →{" "}
                    {visible.publication.currentManifestId ?? "∅"}
                  </code>
                </Notice>
              ) : (
                <>
                  <div className="work-card-actions">
                    <Button
                      disabled={ownDirty || locked || !preview}
                      onClick={() => {
                        setPreview(false);
                        setDialog(false);
                      }}
                    >
                      {ru ? "Что изменилось при публикации" : "Publication changes"}
                    </Button>
                    <Button
                      disabled={ownDirty || locked || preview}
                      onClick={() => {
                        setPreview(true);
                        setDialog(false);
                      }}
                    >
                      {ru ? "Посмотреть последствия отката" : "Preview rollback"}
                    </Button>
                  </div>
                  <h3>
                    {preview
                      ? ru
                        ? "Что изменится при откате"
                        : "Rollback consequences"
                      : ru
                        ? "Что изменилось при публикации"
                        : "Publication changes"}
                  </h3>
                  {preview && !visible.canRollback && (
                    <Notice>
                      {ru ? "Откат недоступен" : "Rollback unavailable"}:{" "}
                      {visible.blockers.join(", ")}
                    </Notice>
                  )}
                  {visible.objects.map((o) => (
                    <Button
                      key={o.id}
                      disabled={stage !== "idle" || dialog}
                      aria-pressed={o.id === selected?.id}
                      onClick={() => setObject(o.id)}
                    >
                      {o.title} · {effects[o.summary.effect]} · {o.summary.changeCount} ·{" "}
                      <code>
                        {o.beforePath ?? "∅"} → {o.afterPath ?? "∅"}
                      </code>
                    </Button>
                  ))}
                  {page.isLoading && selected && (
                    <p role="status">{ru ? "Загрузка сравнения…" : "Loading comparison…"}</p>
                  )}
                  {page.isError && (
                    <Notice error>
                      {ru
                        ? "Не удалось загрузить полное сравнение"
                        : "Could not load complete comparison"}{" "}
                      <Button
                        onClick={() =>
                          void (page.isFetchNextPageError ? page.fetchNextPage() : page.refetch())
                        }
                      >
                        {ru ? "Повторить" : "Retry"}
                      </Button>
                    </Notice>
                  )}
                  {!card.isFetching &&
                    (!page.isError || page.isFetchNextPageError) &&
                    items.map((i) => (
                      <article className="diff-change" key={i.key}>
                        <code>{i.pointer}</code>
                        <div className="diff-values">
                          <pre>{diffText(i.before, ru)}</pre>
                          <pre>{diffText(i.after, ru)}</pre>
                        </div>
                      </article>
                    ))}
                  {page.hasNextPage && (
                    <Button
                      disabled={page.isFetching || ownDirty}
                      onClick={() => void page.fetchNextPage()}
                    >
                      {ru ? "Ещё изменения" : "More changes"}
                    </Button>
                  )}
                  {preview && (
                    <Button variant="primary" disabled={!ready} onClick={() => setDialog(true)}>
                      {ru ? "Отменить публикацию…" : "Roll back publication…"}
                    </Button>
                  )}
                </>
              )}
            </>
          )}
        </div>
      </div>
      {error && (
        <Notice error>
          {stage === "uncertain"
            ? ru
              ? "Результат пока неизвестен. Повторите тот же запрос."
              : "Result unknown. Retry the same request."
            : error}
        </Notice>
      )}
      {stage === "uncertain" && (
        <div>
          <Button onClick={() => void send(true)}>
            {ru ? "Повторить запрос отката" : "Retry rollback request"}
          </Button>
          <Button onClick={() => void list.refetch()}>
            {ru ? "Проверить результат по истории" : "Check result in history"}
          </Button>
          <p className="hint">
            {ru
              ? "Проверка истории не отправляет команду повторно. При неоднозначном результате сначала установите, какая публикация была выполнена."
              : "Checking history does not resend the command. Resolve an ambiguous result before continuing."}
          </p>
          {entries
            .filter(
              (p) =>
                p.kind === "rollback" && p.sourcePublicationId === attempt.current?.publicationId,
            )
            .map((p) => (
              <p key={p.id}>
                <a href={"/editor#publication=" + p.id} target="_blank" rel="noreferrer">
                  {ru ? "Открыть запись отката" : "Open rollback record"}: {p.createdAt}
                </a>
              </p>
            ))}
        </div>
      )}
      {stage === "sending" && <p role="status">{ru ? "Выполняется откат…" : "Rolling back…"}</p>}
      {dialog && visible && (
        <Dialog
          title={ru ? "Подтверждение отката" : "Confirm rollback"}
          locked={stage !== "idle"}
          onDismiss={() => {
            if (stage === "idle") {
              setDialog(false);
              setReason("");
            }
          }}
        >
          <p>
            {visible.publication.environment} · {visible.publication.createdAt} ·{" "}
            {visible.publication.actorId}
          </p>
          <p>
            {ru ? "Объектов" : "Objects"}: {visible.objects.length}
          </p>
          <p>
            {ru
              ? "Будет изменена только публикация в этом окружении. Черновики и основная версия сохранятся."
              : "Only this environment's publication changes. Drafts and head remain unchanged."}
          </p>
          {visible.objects.map((o) => (
            <p key={o.id}>
              {o.title}: {o.beforePath ?? "∅"} → {o.afterPath ?? "∅"}
            </p>
          ))}
          <label>
            {ru ? "Причина отката" : "Rollback reason"}
            <textarea
              value={reason}
              disabled={stage !== "idle"}
              maxLength={2000}
              onChange={(e) => setReason(e.target.value)}
            />
          </label>
          <Button
            disabled={stage !== "idle"}
            onClick={() => {
              setDialog(false);
              setReason("");
            }}
          >
            {ru ? "Отмена" : "Cancel"}
          </Button>
          <Button variant="primary" disabled={!ready || !reason.trim()} onClick={() => void send()}>
            {ru ? "Подтвердить откат" : "Confirm rollback"}
          </Button>
        </Dialog>
      )}
    </section>
  );
}
