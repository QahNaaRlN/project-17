import { useEffect, useRef, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useStore } from "zustand";
import { Button, Dialog, Notice } from "./components.js";
import { StudioError, type Api, type DiffValue } from "./types.js";
import type { Editor } from "./editor.js";
import type { PromoteAttempt, PublicationCard, PublicationEntry } from "./publication-history.js";

function diffText(v: DiffValue, ru: boolean): string {
  if (!v.present) return ru ? "Отсутствует" : "Absent";
  if ("redacted" in v) return ru ? "Служебное поле" : "Service field";
  if ("rawJson" in v) return v.rawJson;
  return JSON.stringify(v.value, null, 2);
}
const blockerText: Record<string, [string, string]> = {
  PROMOTE_OUTDATED: [
    "В целевом окружении опубликована более новая версия; вернуть её можно только откатом в этом окружении",
    "The target has a newer version; only a rollback in the target can return it",
  ],
  PROMOTE_NOTHING: [
    "В целевом окружении уже опубликованы версии этой публикации",
    "The target already has this publication's versions",
  ],
  PROMOTE_NOT_SUPPORTED: [
    "Переносятся только публикации и переносы, не откаты",
    "Only publications and promotions can be promoted, not rollbacks",
  ],
  PROMOTE_SCHEMA_NOT_SUPPORTED: [
    "Публикация меняла manifest: зарегистрируйте manifest в целевом окружении",
    "The publication changed the manifest: register it in the target",
  ],
  PROMOTE_SAME_ENVIRONMENT: ["Публикация уже в этом окружении", "Already in this environment"],
  PROMOTE_FORBIDDEN: [
    "Нужно право content.publish в целевом окружении",
    "content.publish in the target is required",
  ],
  ENVIRONMENT_NOT_PUBLISHABLE: [
    "В целевое окружение нельзя публиковать",
    "The target is not publishable",
  ],
  VALIDATION_FAILED: [
    "Будущее состояние целевого окружения не проходит проверку",
    "The target's future state fails validation",
  ],
  PATH_TAKEN: ["Маршрут страницы занят в целевом окружении", "A page route is taken in the target"],
  APPROVAL_ROLES_MISSING: [
    "Политика целевого окружения требует больше согласований",
    "The target's policy requires more approvals",
  ],
  POLICY_DENIED: [
    "Действующая политика агентов запрещает операции этой работы",
    "The current agent policy denies this work's operations",
  ],
};

/**
 * STU-469…473: promotion of one publication into a configured target environment.
 * The preview is read first; the command carries its snapshot and a reason.
 */
export function PromotePanel({
  api,
  editor,
  card,
  target,
  disabled,
  onOpenPublication,
  onOpenEnvironment,
  onDone,
}: {
  api: Api;
  editor: Editor;
  card: PublicationCard;
  target: string;
  disabled: boolean;
  onOpenPublication: (id: string) => void;
  onOpenEnvironment: (environment: string) => void;
  onDone: (publication: PublicationEntry) => Promise<void>;
}) {
  const state = useStore(editor.store),
    ru = state.locale === "ru";
  const [open, setOpen] = useState(false),
    [object, setObject] = useState(""),
    [dialog, setDialog] = useState(false),
    [reason, setReason] = useState(""),
    [stage, setStage] = useState<"idle" | "sending" | "uncertain">("idle"),
    [error, setError] = useState("");
  const attempt = useRef<PromoteAttempt | null>(null),
    sending = useRef(false),
    generation = useRef(0);
  const field = "promotion:" + target;
  const ownDirty = stage !== "idle" || reason.length > 0;
  const locked =
    disabled ||
    state.status === "saving" ||
    state.status === "loading" ||
    state.status === "uncertain" ||
    Object.entries(state.dirtyFields).some(([k, v]) => k !== field && v);
  const id = card.publication.id,
    source = card.publication.environment;
  const preview = useQuery({
    queryKey: ["promote-card", id, source, target, state.historyRevision, state.remotePending],
    queryFn: ({ signal }) => api.promotePreview!(id, source, target, signal),
    enabled: open && stage === "idle",
    retry: false,
    refetchInterval: dialog ? false : 30000,
    refetchOnWindowFocus: !dialog,
  });
  const visible = preview.isSuccess && !preview.isError ? preview.data : undefined;
  const selected = visible?.objects.find((o) => o.id === object) ?? visible?.objects[0];
  const page = useInfiniteQuery({
    queryKey: ["promote-diff", id, target, visible?.snapshot, selected?.id],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ signal, pageParam }) => api.promoteDiff!(visible!, selected!.id, pageParam, signal),
    getNextPageParam: (p) => p.nextCursor ?? undefined,
    enabled: !!visible && !!selected && !preview.isFetching && stage === "idle",
    retry: false,
  });
  const items = page.data?.pages.flatMap((p) => p.items) ?? [];
  const full =
    page.isSuccess &&
    !page.isError &&
    !page.isFetching &&
    !page.hasNextPage &&
    items.length === (page.data?.pages[0]?.totalChanges ?? 0);
  const ready =
    !!visible &&
    visible.canPromote &&
    !preview.isFetching &&
    !locked &&
    stage === "idle" &&
    (!selected || full);
  useEffect(() => {
    editor.formDirty(field, ownDirty);
    return () => editor.formDirty(field, false);
  }, [editor, field, ownDirty]);
  // STU-472: a new preview response, SSE or an error closes the confirmation.
  useEffect(() => {
    if (stage === "idle" && (preview.isFetching || preview.isError || page.isError)) {
      setDialog(false);
      setReason("");
    }
  }, [preview.isFetching, preview.isError, page.isError, visible?.snapshot, stage]);
  useEffect(
    () => () => {
      generation.current++;
    },
    [],
  );
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
    editor.formDirty(field, true);
    try {
      if (!retry) {
        const fresh = await api.promotePreview!(id, source, target);
        if (fresh.snapshot !== visible!.snapshot || !fresh.canPromote) {
          setDialog(false);
          setReason("");
          setStage("idle");
          setError("PROMOTE_SNAPSHOT_STALE");
          await preview.refetch();
          return;
        }
        attempt.current = {
          publicationId: id,
          toEnvironment: target,
          expectedPromoteSnapshot: fresh.snapshot,
          reason: reason.trim(),
          key: crypto.randomUUID(),
          startedAt: Date.now(),
        };
      }
      const result = await api.promote!(attempt.current!);
      if (token !== generation.current) return;
      attempt.current = null;
      setStage("idle");
      setReason("");
      setDialog(false);
      setOpen(false);
      editor.formDirty(field, false);
      await onDone(result);
    } catch (cause) {
      if (token !== generation.current) return;
      const uncertain = attempt.current && (!(cause instanceof StudioError) || cause.uncertain);
      setStage(uncertain ? "uncertain" : "idle");
      setDialog(false);
      setError(cause instanceof StudioError ? cause.code : "PROMOTE_FAILED");
      if (!uncertain) {
        attempt.current = null;
        setReason("");
        editor.formDirty(field, false);
        await preview.refetch();
      }
    } finally {
      sending.current = false;
    }
  }
  const moving = visible?.objects.filter((o) => !o.skipped).length ?? 0;
  const skipped = (visible?.objects.length ?? 0) - moving;
  return (
    <div className="promote-panel" aria-label={(ru ? "Перенос в " : "Promote to ") + target}>
      <Button
        disabled={locked || ownDirty || open}
        onClick={() => {
          setOpen(true);
          setError("");
        }}
      >
        {ru ? `Перенести в ${target}…` : `Promote to ${target}…`}
      </Button>
      {open && preview.isLoading && (
        <p role="status">{ru ? "Загрузка последствий переноса…" : "Loading promotion preview…"}</p>
      )}
      {open && preview.isError && (
        <Notice error>
          {ru ? "Предпросмотр переноса недоступен" : "Promotion preview unavailable"}{" "}
          <Button onClick={() => void preview.refetch()}>{ru ? "Повторить" : "Retry"}</Button>
        </Notice>
      )}
      {open && visible && (
        <>
          <h3>
            {ru ? "Что изменится в " : "Changes in "}
            {target}
          </h3>
          <p>
            {ru ? "Будет перенесено" : "To promote"}: {moving} ·{" "}
            {ru ? `уже в ${target}` : `already in ${target}`}: {skipped}
          </p>
          {!visible.canPromote && (
            <Notice>
              {ru ? "Перенос недоступен" : "Promotion unavailable"}
              <ul>
                {visible.blockers.map((b) => {
                  const [code, objectId] = b.split(":");
                  const text = blockerText[code!];
                  return (
                    <li key={b}>
                      {text ? text[ru ? 0 : 1] : code}
                      {objectId && <code> {objectId}</code>}
                      {code === "PROMOTE_OUTDATED" && (
                        <Button disabled={ownDirty} onClick={() => onOpenEnvironment(target)}>
                          {ru ? `Открыть историю ${target}` : `Open ${target} history`}
                        </Button>
                      )}
                    </li>
                  );
                })}
              </ul>
              {Object.entries(visible.diagnostics).flatMap(([objectId, ds]) =>
                ds.map((d, i) => (
                  <p key={objectId + i}>
                    <code>{d.code}</code> {d.message} <code>{d.pointer}</code>
                  </p>
                )),
              )}
              {visible.dependencies.map((d) => (
                <p key={d.objectId}>
                  {ru ? "Сначала перенесите объект" : "Promote this object first"}{" "}
                  <code>{d.objectId}</code>
                  {d.publicationId && (
                    <Button disabled={ownDirty} onClick={() => onOpenPublication(d.publicationId!)}>
                      {ru ? "Открыть публикацию-зависимость" : "Open the publication it needs"}
                    </Button>
                  )}
                </p>
              ))}
            </Notice>
          )}
          {visible.objects.map((o) => (
            <Button
              key={o.id}
              disabled={stage !== "idle" || dialog}
              aria-pressed={o.id === selected?.id}
              onClick={() => setObject(o.id)}
            >
              {o.title} ·{" "}
              {o.skipped ? (ru ? "уже в окружении" : "already there") : o.summary.changeCount} ·{" "}
              <code>
                {o.beforePath ?? "∅"} → {o.afterPath ?? "∅"}
              </code>
            </Button>
          ))}
          {page.isError && (
            <Notice error>
              {ru ? "Не удалось загрузить полное сравнение" : "Could not load complete comparison"}{" "}
              <Button
                onClick={() =>
                  void (page.isFetchNextPageError ? page.fetchNextPage() : page.refetch())
                }
              >
                {ru ? "Повторить" : "Retry"}
              </Button>
            </Notice>
          )}
          {!preview.isFetching &&
            !page.isError &&
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
            <Button disabled={page.isFetching} onClick={() => void page.fetchNextPage()}>
              {ru ? "Ещё изменения" : "More changes"}
            </Button>
          )}
          <div className="work-card-actions">
            <Button
              disabled={stage !== "idle"}
              onClick={() => {
                setOpen(false);
                setDialog(false);
                setReason("");
              }}
            >
              {ru ? "Закрыть перенос" : "Close promotion"}
            </Button>
            <Button variant="primary" disabled={!ready} onClick={() => setDialog(true)}>
              {ru ? `Перенести в ${target}` : `Promote to ${target}`}
            </Button>
          </div>
        </>
      )}
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
            {ru ? "Повторить запрос переноса" : "Retry promotion request"}
          </Button>
          <a
            href={"/editor#" + new URLSearchParams({ publication: "", environment: target })}
            target="_blank"
            rel="noreferrer"
          >
            {ru ? `Проверить историю ${target}` : `Check ${target} history`}
          </a>
          <p className="hint">
            {ru
              ? "Пока результат неизвестен, новые переносы, откаты и смена окружения заблокированы. Повтор отправляет тот же запрос с тем же ключом."
              : "While the result is unknown, new promotions, rollbacks and environment changes are blocked. Retry resends the same request with the same key."}
          </p>
        </div>
      )}
      {stage === "sending" && <p role="status">{ru ? "Выполняется перенос…" : "Promoting…"}</p>}
      {dialog && visible && (
        <Dialog
          title={ru ? "Подтверждение переноса" : "Confirm promotion"}
          locked={stage !== "idle"}
          onDismiss={() => {
            if (stage === "idle") {
              setDialog(false);
              setReason("");
            }
          }}
        >
          <p>
            {source} → {target}
          </p>
          <p>
            {card.publication.createdAt} · {card.publication.actorId}
            {card.publication.changesetId && <> · CS {card.publication.changesetId}</>}
          </p>
          <p>
            {ru ? "Будет перенесено" : "To promote"}: {moving} · {ru ? "пропущено" : "skipped"}:{" "}
            {skipped}
          </p>
          <p>
            {ru
              ? "Меняется только опубликованное состояние целевого окружения. Черновики и основная версия сохранятся."
              : "Only the target's published state changes. Drafts and head remain unchanged."}
          </p>
          {visible.objects
            .filter((o) => !o.skipped)
            .map((o) => (
              <p key={o.id}>
                {o.title}: {o.beforePath ?? "∅"} → {o.afterPath ?? "∅"}
              </p>
            ))}
          <label>
            {ru ? "Причина переноса" : "Promotion reason"}
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
            {ru ? "Подтвердить перенос" : "Confirm promotion"}
          </Button>
        </Dialog>
      )}
    </div>
  );
}
