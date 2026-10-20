import { useEffect, useRef, useState } from "react";
import { useStore } from "zustand";
import type { Type } from "@cms/manifest";
import { StudioError, type Api } from "./types.js";
import type { Editor } from "./editor.js";
import type { CapabilityRequest } from "./capabilities.js";
import { Argument } from "./behavior-panel.js";
import { behaviorFields } from "./behavior.js";
import {
  anchorLabel,
  fulfillmentMessage,
  fulfillmentReason,
  type FulfillmentAnchor,
  type FulfillmentAttempt,
  type FulfillmentCommand,
  type FulfillmentView,
} from "./capability-fulfillment.js";

const retryable = ["reverted", "anchor_changed", "failed", "invalid_args", "invalid_props"];

/** Request card section «Выполнение» (STU-351/352). */
export function CapabilityFulfillmentPanel({
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
  const [view, setView] = useState<FulfillmentView | null>(null),
    [error, setError] = useState(""),
    [values, setValues] = useState<Record<string, Record<string, unknown>>>({}),
    [reason, setReason] = useState(""),
    [pending, setPending] = useState(false),
    [uncertain, setUncertain] = useState(false);
  const attempt = useRef<FulfillmentAttempt | null>(null),
    alive = useRef(true),
    sequence = useRef(0);
  async function read(signal?: AbortSignal) {
    const n = ++sequence.current;
    try {
      const v = await api.fulfillment!(request.id, signal);
      if (alive.current && !signal?.aborted && n === sequence.current) {
        setView(v);
        setError("");
      }
    } catch (e) {
      if (alive.current && !signal?.aborted && n === sequence.current)
        setError(e instanceof StudioError ? e.code : "FULFILLMENT_UNAVAILABLE");
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
  }, [api, request.id, request.revision, state.historyRevision]);
  const dirty = Object.keys(values).length > 0 || !!reason || pending || uncertain;
  useEffect(() => {
    editor.formDirty("property-capability-fulfillment", dirty);
    return () => editor.formDirty("property-capability-fulfillment", false);
  }, [editor, dirty]);
  async function send(name?: FulfillmentCommand, payload?: Record<string, unknown>) {
    if (pending || !api.fulfillmentCommand) return;
    setPending(true);
    setError("");
    try {
      if (!attempt.current) {
        if (!name || !payload) return;
        const account = await api.account();
        if (!account.authenticated || !account.user?.id) throw new StudioError("UNAUTHENTICATED");
        attempt.current = {
          name,
          payload: structuredClone(payload),
          key: crypto.randomUUID(),
          reason: reason.trim() || "Studio: capability fulfillment",
          actor: account.user.id,
        };
      }
      await api.fulfillmentCommand(attempt.current);
      attempt.current = null;
      if (!alive.current) return;
      setUncertain(false);
      setValues({});
      setReason("");
      if (editor.store.getState().session) await editor.refresh();
      await read();
    } catch (e) {
      if (!alive.current) return;
      const u = e instanceof StudioError && e.uncertain;
      setUncertain(u);
      if (!u) attempt.current = null;
      setError(e instanceof StudioError ? e.code : "FULFILLMENT_UNAVAILABLE");
    } finally {
      if (alive.current) setPending(false);
    }
  }
  const session = state.session,
    doc = state.document;
  const action = session?.manifest.actions?.[request.key]?.args as Record<string, Type> | undefined;
  const args = (action ?? request.proposedContract.args ?? {}) as Record<string, Type>;
  // STU-370: a component instance has no fulfillment arguments.
  const component = request.kind !== "action";
  const locked = pending || uncertain;
  const payload = (a: FulfillmentAnchor) => ({
    requestId: request.id,
    anchorId: a.id,
    expectedAnchorRevision: a.revision,
  });
  function anchorRow(a: FulfillmentAnchor) {
    const here =
      a.own &&
      !!session?.record &&
      session.changeset.id === a.changesetId &&
      session.record.id === a.documentId;
    const fields =
      here && doc
        ? behaviorFields(doc, session!.manifest, a.nodeId!, session!.record!.path ?? "").fields
        : [];
    const own = values[a.id] ?? {};
    return (
      <li key={a.id}>
        <p>
          {a.own
            ? component
              ? `${a.nodeId}`
              : `${a.nodeId} · ${a.event}`
            : text("Привязка другого черновика", "Another draft's anchor")}{" "}
          · {anchorLabel(a.state, ru)}
          {a.own && a.argsSet ? text(" · аргументы подтверждены", " · arguments confirmed") : ""}
        </p>
        {a.own &&
          here &&
          doc &&
          !component &&
          ["active", "awaiting_args", "invalid_args", "reverted"].includes(a.state) && (
            <fieldset disabled={locked}>
              <legend>{text("Аргументы выполнения", "Fulfillment arguments")}</legend>
              {Object.entries(args).map(([name, schema]) => (
                <Argument
                  key={name}
                  {...{ name, schema, doc, fields, ru }}
                  value={own[name]}
                  change={(v) =>
                    setValues((old) => {
                      const next = { ...(old[a.id] ?? {}) };
                      if (v === undefined) delete next[name];
                      else next[name] = v;
                      return { ...old, [a.id]: next };
                    })
                  }
                />
              ))}
              <button
                type="button"
                onClick={() =>
                  void send("set-capability-fulfillment-args", {
                    ...payload(a),
                    handlers: [{ action: request.key, args: own }],
                  })
                }
              >
                {text("Подтвердить аргументы выполнения", "Confirm fulfillment arguments")}
              </button>
            </fieldset>
          )}
        {a.own && view?.state === "fulfilled" && retryable.includes(a.state) && (
          <button
            type="button"
            disabled={locked}
            onClick={() => void send("retry-capability-fulfillment", payload(a))}
          >
            {text("Повторить замену", "Retry replacement")}
          </button>
        )}
        {a.own && here && !a.original && !["replaced", "detached"].includes(a.state) && (
          <button
            type="button"
            disabled={locked}
            onClick={() =>
              void send("detach-capability-request", {
                ...payload(a),
                expectedSeq: session!.changeset.seq,
              })
            }
          >
            {text("Отсоединить", "Detach")}
          </button>
        )}
      </li>
    );
  }
  return (
    <section aria-label={text("Выполнение", "Fulfillment")} aria-busy={pending}>
      <h4>{text("Выполнение", "Fulfillment")}</h4>
      {error && <p role="alert">{fulfillmentMessage(error, ru)}</p>}
      {view && (
        <>
          <p>
            {view.state === "fulfilled"
              ? view.mode === "automatic"
                ? text("Выполнен автоматически", "Fulfilled automatically")
                : text("Выполнен вручную", "Fulfilled manually")
              : fulfillmentReason(view.reason, ru)}
          </p>
          {view.basis?.integrationSha && (
            <p>
              {text("Собрано из слияния PR", "Built from the merge of PR")} #{view.basis.pullNumber}{" "}
              <code title={view.basis.commit}>{view.basis.commit?.slice(0, 7)}</code>
              {view.basis.comparison
                ? text(
                    ` · на ${view.basis.comparison.aheadBy} коммит(ов) новее слияния`,
                    ` · ${view.basis.comparison.aheadBy} commit(s) after the merge`,
                  )
                : ""}
            </p>
          )}
          <p>
            {text("Привязок в группе", "Anchors in the group")}: {view.anchorCount}
          </p>
          <ul>{view.anchors.map(anchorRow)}</ul>
          {view.canCheck && view.state === "waiting" && (
            <button
              type="button"
              disabled={locked}
              onClick={() => void send("check-capability-fulfillment", { requestId: request.id })}
            >
              {text("Проверить выполнение", "Check fulfillment")}
            </button>
          )}
          <label>
            {text("Причина", "Reason")}
            <input maxLength={2000} value={reason} onChange={(e) => setReason(e.target.value)} />
          </label>
        </>
      )}
      {uncertain && (
        <button type="button" disabled={pending} onClick={() => void send()}>
          {text("Повторить тот же запрос", "Retry exact request")}
        </button>
      )}
    </section>
  );
}
