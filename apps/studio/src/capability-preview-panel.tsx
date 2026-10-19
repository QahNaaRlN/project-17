import { useEffect, useRef, useState } from "react";
import { useStore } from "zustand";
import type { Type } from "@cms/manifest";
import { StudioError, type Api } from "./types.js";
import type { Editor } from "./editor.js";
import type { CapabilityRequest } from "./capabilities.js";
import { Argument } from "./behavior-panel.js";
import { behaviorFields } from "./behavior.js";
import {
  previewAge,
  previewArchiveLabel,
  previewLabel,
  previewMessage,
  previewOrigin,
  type PreviewArgsAttempt,
  type PreviewItem,
  type PreviewList,
} from "./capability-preview.js";

const ready = (s: string) => s === "ready" || s === "ready_last_known";

/** Request card section «Предпросмотр» (STU-319…321, STU-330). */
export function CapabilityPreviewPanel({
  api,
  editor,
  request,
}: {
  api: Api;
  editor: Editor;
  request: CapabilityRequest;
}) {
  const state = useStore(editor.store),
    ru = state.locale === "ru",
    text = (r: string, e: string) => (ru ? r : e);
  const [view, setView] = useState<PreviewList | null>(null),
    [error, setError] = useState(""),
    [values, setValues] = useState<Record<string, Record<string, unknown>>>({}),
    [reason, setReason] = useState(""),
    [pending, setPending] = useState(false),
    [uncertain, setUncertain] = useState(false),
    [now, setNow] = useState(Date.now());
  const attempt = useRef<PreviewArgsAttempt | null>(null),
    alive = useRef(true),
    sequence = useRef(0);
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 30000);
    return () => clearInterval(id);
  }, []);
  async function read(signal?: AbortSignal) {
    const n = ++sequence.current;
    try {
      const v = await api.capabilityPreview!(request.id, signal);
      if (alive.current && !signal?.aborted && n === sequence.current) {
        setView(v);
        setError("");
      }
    } catch (e) {
      if (alive.current && !signal?.aborted && n === sequence.current)
        setError(e instanceof StudioError ? e.code : "PREVIEW_UNAVAILABLE");
    }
  }
  useEffect(() => {
    alive.current = true;
    const c = new AbortController();
    void read(c.signal);
    return () => {
      alive.current = false;
      sequence.current++;
      c.abort();
    };
    // The project revision changes on every registration/archive (realtime).
  }, [api, request.id, request.revision, state.historyRevision]);
  const dirty = Object.keys(values).length > 0 || !!reason || pending || uncertain;
  useEffect(() => {
    editor.formDirty("property-capability-preview", dirty);
    return () => editor.formDirty("property-capability-preview", false);
  }, [editor, dirty]);
  async function send(next?: PreviewArgsAttempt) {
    if (pending || !api.previewArgs) return;
    setPending(true);
    setError("");
    try {
      if (!attempt.current) {
        if (!next) return;
        const account = await api.account();
        if (!account.authenticated || !account.user?.id) throw new StudioError("UNAUTHENTICATED");
        attempt.current = { ...structuredClone(next), actor: account.user.id };
      }
      await api.previewArgs(attempt.current);
      attempt.current = null;
      if (!alive.current) return;
      setUncertain(false);
      setValues({});
      setReason("");
      await read();
    } catch (e) {
      if (!alive.current) return;
      const u = e instanceof StudioError && e.uncertain;
      setUncertain(u);
      if (!u) attempt.current = null;
      setError(e instanceof StudioError ? e.code : "PREVIEW_UNAVAILABLE");
    } finally {
      if (alive.current) setPending(false);
    }
  }
  const session = state.session,
    doc = state.document,
    matches =
      !!session?.record &&
      session.changeset.id === request.changesetId &&
      session.record.id === request.documentId;
  const fields =
    matches && doc
      ? behaviorFields(doc, session.manifest, request.nodeId, session.record!.path ?? "").fields
      : [];
  const locked = pending || uncertain;
  function open(item: PreviewItem, origin: string) {
    void editor.viewEnvironment({
      environment: item.environment,
      origin,
      pullNumber: item.pullNumber,
      registrationSeq: item.registration!.seq,
      commit: item.registration!.commit,
    });
  }
  return (
    <section aria-label={text("Предпросмотр", "Preview")} aria-busy={pending}>
      <h4>{text("Предпросмотр", "Preview")}</h4>
      <p>
        {text(
          "Сборка заявлена CI для коммита; совпадение с PR подтверждает GitHub. Предпросмотр не означает слияния, развёртывания или выполнения запроса.",
          "CI declares the build for a commit; GitHub confirms it matches the PR. A preview does not mean merge, deployment or fulfillment.",
        )}
      </p>
      {error && <p role="alert">{previewMessage(error, ru)}</p>}
      {view && view.items.length === 0 && (
        <p>{text("Preview-окружений этого PR нет.", "No preview environments for this PR.")}</p>
      )}
      {view?.items.map((item) => {
        const origin = previewOrigin(item.registration?.appUrl, session?.config.previewAppOrigins);
        const args = item.actionArgs as Record<string, Type> | undefined;
        const own = values[item.environment] ?? {};
        const needsArgs = item.args.status !== "not_required";
        const openable =
          view.canOpen &&
          ready(item.state) &&
          (item.args.status === "set" || item.args.status === "not_required") &&
          matches &&
          !!origin;
        const viewing = state.viewTarget?.environment === item.environment;
        return (
          <div key={item.environment}>
            <p>
              <strong>{item.environment}</strong> · PR #{item.pullNumber} ·{" "}
              {previewLabel(item.state, ru)}
              {item.state === "ready_last_known" && item.observation && (
                <>
                  {" "}
                  · {text("проверено GitHub", "verified by GitHub")}{" "}
                  {previewAge(item.observation.observedAt, now, ru)} {text("назад", "ago")}
                </>
              )}
            </p>
            {item.archivedAt && <p>{previewArchiveLabel(item.archiveReason, ru)}</p>}
            {item.registration && (
              <details>
                <summary>
                  {text("Коммит", "Commit")} <code>{item.registration.commit.slice(0, 7)}</code> ·{" "}
                  <time dateTime={item.registration.registeredAt}>
                    {item.registration.registeredAt}
                  </time>
                </summary>
                <code style={{ overflowWrap: "anywhere" }}>{item.registration.commit}</code>
                {item.expiresAt && (
                  <p>
                    {text("Архивируется после", "Archived after")}:{" "}
                    <time dateTime={item.expiresAt}>{item.expiresAt}</time>
                  </p>
                )}
              </details>
            )}
            {item.registration?.appUrl && !origin && (
              <p>
                {text(
                  "Адрес сборки вне разрешённых origin Studio.",
                  "The build address is outside Studio's allowed origins.",
                )}
              </p>
            )}
            {view.canSetArgs && ready(item.state) && needsArgs && args && matches && doc && (
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  void send({
                    payload: {
                      requestId: request.id,
                      expectedRequestRevision: request.revision,
                      environment: item.environment,
                      manifestHash: item.registration!.manifestHash,
                      handlers: [{ action: request.key, args: own }],
                      expectedArgsRevision: item.args.revision,
                    },
                    key: crypto.randomUUID(),
                    reason,
                  });
                }}
              >
                <fieldset disabled={locked}>
                  <legend>
                    {item.args.status === "set"
                      ? text("Аргументы предпросмотра сохранены", "Preview arguments saved")
                      : item.args.status === "stale"
                        ? text(
                            "Аргументы относятся к прежней сборке — задайте заново",
                            "Arguments belong to a previous build — set them again",
                          )
                        : text("Аргументы для предпросмотра", "Preview arguments")}
                  </legend>
                  {Object.entries(args).map(([name, schema]) => (
                    <Argument
                      key={name}
                      {...{ name, schema, doc, fields, ru }}
                      value={own[name]}
                      change={(v) =>
                        setValues((old) => {
                          const next = { ...(old[item.environment] ?? {}) };
                          if (v === undefined) delete next[name];
                          else next[name] = v;
                          return { ...old, [item.environment]: next };
                        })
                      }
                    />
                  ))}
                  <label>
                    {text("Причина", "Reason")}
                    <input
                      required
                      maxLength={2000}
                      value={reason}
                      onChange={(e) => setReason(e.target.value)}
                    />
                  </label>
                  <button disabled={!reason.trim()}>
                    {text("Сохранить аргументы предпросмотра", "Save preview arguments")}
                  </button>
                </fieldset>
              </form>
            )}
            {viewing ? (
              <button type="button" onClick={() => void editor.viewEnvironment(null)}>
                {text("Вернуться в рабочее окружение", "Back to the working environment")}
              </button>
            ) : (
              <button
                type="button"
                disabled={!openable || locked}
                onClick={() => open(item, origin!)}
              >
                {text("Открыть в предпросмотре", "Open in preview")}
              </button>
            )}
          </div>
        );
      })}
      <button type="button" disabled={locked} onClick={() => void read()}>
        {text("Обновить предпросмотр", "Reload preview status")}
      </button>
      {uncertain && (
        <button type="button" disabled={pending} onClick={() => void send()}>
          {text("Повторить тот же запрос", "Retry exact request")}
        </button>
      )}
      {dirty && !locked && (
        <button
          type="button"
          onClick={() => {
            setValues({});
            setReason("");
          }}
        >
          {text("Сбросить ввод предпросмотра", "Discard preview input")}
        </button>
      )}
    </section>
  );
}
