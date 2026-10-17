import { useEffect, useRef, useState } from "react";
import { useStore } from "zustand";
import { manifestHash } from "@cms/manifest";
import { Dialog } from "./components.js";
import { StudioError, type Api } from "./types.js";
import type { Editor } from "./editor.js";
import type { CapabilityRequest } from "./capabilities.js";
import {
  deliveryActive,
  deliveryMessage,
  deliveryStatus,
  sendPayload,
  type Delivery,
  type DeliveryAttempt,
  type DeliveryCommand,
  type DeliveryEndpoint,
  type DeliveryPreview,
  type DeliveryTarget,
} from "./capability-delivery.js";

function useDeliveryCommand(api: Api, editor: Editor, done: () => Promise<void>) {
  const [pending, setPending] = useState(false),
    [uncertain, setUncertain] = useState(false),
    [error, setError] = useState("");
  const attempt = useRef<DeliveryAttempt | null>(null),
    running = useRef(false),
    alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  async function send(name?: DeliveryCommand, payload?: Record<string, unknown>) {
    if (running.current || !api.deliveryCommand) return;
    running.current = true;
    setPending(true);
    setError("");
    editor.store.setState({ status: "saving" });
    try {
      if (!attempt.current) {
        if (!name || !payload) return;
        const account = await api.account();
        if (!account.authenticated || !account.user?.id) throw new StudioError("UNAUTHENTICATED");
        attempt.current = {
          name,
          payload: structuredClone(payload),
          key: crypto.randomUUID(),
          reason: "Studio: explicit context pack delivery",
          actor: account.user.id,
        };
      }
      await api.deliveryCommand(attempt.current);
      attempt.current = null;
      if (!alive.current) return;
      setUncertain(false);
      editor.store.setState({ status: editor.store.getState().session ? "ready" : "idle" });
      await done();
    } catch (cause) {
      if (!alive.current) return;
      const retry = !!attempt.current && (!(cause instanceof StudioError) || cause.uncertain);
      if (!retry) attempt.current = null;
      setUncertain(retry);
      setError(cause instanceof StudioError ? cause.code : "NETWORK_UNCERTAIN");
      editor.store.setState({
        status: retry ? "uncertain" : editor.store.getState().session ? "ready" : "idle",
      });
    } finally {
      running.current = false;
      if (alive.current) setPending(false);
    }
  }
  return {
    send,
    pending,
    uncertain,
    error,
    clearError: () => {
      if (!uncertain) setError("");
    },
  };
}

export function DeliveryTargetPanel({ api, editor }: { api: Api; editor: Editor }) {
  const state = useStore(editor.store),
    ru = state.locale === "ru",
    text = (r: string, e: string) => (ru ? r : e);
  const [target, setTarget] = useState<DeliveryTarget | null>(null),
    [endpoints, setEndpoints] = useState<DeliveryEndpoint[]>([]),
    [environment, setEnvironment] = useState(""),
    [endpoint, setEndpoint] = useState(""),
    [enabled, setEnabled] = useState(false),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(false);
  const alive = useRef(true);
  async function read(signal?: AbortSignal) {
    command.clearError();
    setLoading(true);
    setError("");
    try {
      const [next, list, config] = await Promise.all([
        api.deliveryTarget!(signal),
        api.deliveryEndpoints!(signal),
        api.config(signal),
      ]);
      if (!alive.current || signal?.aborted) return;
      setTarget(next);
      setEndpoints(list.items);
      setEnvironment(config.environment);
      setEndpoint(next.endpointId ?? "");
      setEnabled(next.enabled ?? false);
    } catch (cause) {
      if (alive.current && !signal?.aborted)
        setError(cause instanceof StudioError ? cause.code : "CAPABILITY_DELIVERY_UNAVAILABLE");
    } finally {
      if (alive.current && !signal?.aborted) setLoading(false);
    }
  }
  useEffect(() => {
    alive.current = true;
    const controller = new AbortController();
    void read(controller.signal);
    return () => {
      alive.current = false;
      controller.abort();
    };
  }, [api]);
  const command = useDeliveryCommand(api, editor, async () => {
      await read();
    }),
    locked = command.pending || command.uncertain;
  const dirty =
    endpoint !== (target?.endpointId ?? "") || enabled !== (target?.enabled ?? false) || locked;
  useEffect(() => {
    editor.formDirty("property-delivery-target", dirty);
    return () => editor.formDirty("property-delivery-target", false);
  }, [editor, dirty]);
  const chosen = endpoints.find((e) => e.endpointId === endpoint);
  return (
    <section className="capability-queue" aria-busy={loading || command.pending}>
      <h1>{text("Получатель запросов", "Request recipient")}</h1>
      <p>
        {text(
          "Получатель выбирается из настроенного сервером каталога для текущего окружения. Включение доставки само по себе ничего не отправляет.",
          "Choose a recipient from the server catalogue for this environment. Enabling delivery does not send any packs.",
        )}
      </p>
      {(error || command.error) && (
        <p role="alert">{deliveryMessage(error || command.error, ru)}</p>
      )}
      {!loading && !endpoints.length && (
        <p>
          {text(
            "Каталог получателей пуст. Обратитесь к администратору сервера. Ручная выгрузка пакетов доступна.",
            "The recipient catalogue is empty. Contact the server administrator. Manual pack downloads remain available.",
          )}
        </p>
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (target && chosen)
            void command.send("set-capability-delivery-target", {
              environment,
              endpointId: endpoint,
              enabled,
              expectedRevision: target.revision,
            });
        }}
      >
        <fieldset disabled={locked || loading}>
          <legend>{text("Настройки получателя", "Recipient settings")}</legend>
          <label>
            {text("Получатель", "Recipient")}
            <select
              aria-label={text("Получатель", "Recipient")}
              required
              value={endpoint}
              onChange={(e) => setEndpoint(e.target.value)}
            >
              <option value="">—</option>
              {endpoints.map((e) => (
                <option key={e.endpointId} value={e.endpointId}>
                  {e.endpointId} · {e.url}
                </option>
              ))}
            </select>
          </label>
          {chosen && (
            <p>
              {chosen.url}
              <br />
              <code>{chosen.fingerprint}</code>
            </p>
          )}
          <label>
            <input
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
            />
            {text(
              "Разрешить передачу после отдельного подтверждения автора",
              "Enable delivery after the author's separate consent",
            )}
          </label>
          <p>
            {text("Ревизия", "Revision")}: {target?.revision ?? 0} ·{" "}
            {target?.ready ? text("Готов", "Ready") : text("Не готов", "Not ready")}
          </p>
          <button disabled={!chosen || !target || !!command.error}>
            {text("Сохранить получателя", "Save recipient")}
          </button>
        </fieldset>
      </form>
      {command.uncertain && (
        <button disabled={command.pending} onClick={() => void command.send()}>
          {text("Повторить тот же запрос", "Retry exact request")}
        </button>
      )}
      <button disabled={locked || loading} onClick={() => void read()}>
        {text("Перечитать настройки", "Reload settings")}
      </button>
      <button
        disabled={locked || loading}
        onClick={() => {
          setEndpoint(target?.endpointId ?? "");
          setEnabled(target?.enabled ?? false);
        }}
      >
        {text("Сбросить ввод", "Discard input")}
      </button>
    </section>
  );
}

export function CapabilityDeliveryPanel({
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
  const [items, setItems] = useState<Delivery[]>([]),
    [cursor, setCursor] = useState<string | null>(null),
    [selected, setSelected] = useState<Delivery | null>(null),
    [preview, setPreview] = useState<DeliveryPreview | null>(null),
    [retry, setRetry] = useState(false),
    [consent, setConsent] = useState(false),
    [reviewed, setReviewed] = useState(false),
    [account, setAccount] = useState<{ id: string; rights: string[] } | null>(null),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(false);
  const alive = useRef(true),
    selectedID = useRef<string | undefined>(undefined),
    readVersion = useRef(0);
  const canSend =
    !!account &&
    account.id === request.ownerId &&
    ["capability.request", "design.read", "capability.deliver"].every((r) =>
      account.rights.includes(r),
    ) &&
    !["draft", "fulfilled", "rejected", "cancelled"].includes(request.status);
  async function read(signal?: AbortSignal, more = false) {
    const version = ++readVersion.current;
    try {
      const list = await api.deliveries!(request.id, more && cursor ? cursor : undefined, signal);
      if (!alive.current || signal?.aborted || version !== readVersion.current) return false;
      setItems((old) => (more ? [...old, ...list.items] : list.items));
      setCursor(list.nextCursor ?? null);
      const id = selectedID.current ?? list.items[0]?.id;
      let detail: Delivery | undefined;
      if (id) {
        detail = await api.delivery!(id, signal);
        if (alive.current && !signal?.aborted && version === readVersion.current) {
          setSelected(detail);
          selectedID.current = id;
        }
      }
      return list.items.some(deliveryActive) || (!!detail && deliveryActive(detail));
    } catch (cause) {
      if (alive.current && !signal?.aborted && version === readVersion.current)
        setError(cause instanceof StudioError ? cause.code : "CAPABILITY_DELIVERY_UNAVAILABLE");
      return false;
    }
  }
  const [generation, setGeneration] = useState(0);
  useEffect(() => {
    alive.current = true;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    void api
      .account(controller.signal)
      .then((a) => {
        if (alive.current && !controller.signal.aborted)
          setAccount(a.authenticated && a.user ? { id: a.user.id, rights: a.rights } : null);
      })
      .catch(() => {});
    const poll = async () => {
      const active = await read(controller.signal);
      if (active && alive.current && !controller.signal.aborted)
        timer = setTimeout(() => void poll(), document.visibilityState === "hidden" ? 15000 : 2000);
    };
    void poll();
    return () => {
      alive.current = false;
      controller.abort();
      clearTimeout(timer);
    };
  }, [api, request.id, generation]);
  const command = useDeliveryCommand(api, editor, async () => {
    setPreview(null);
    setConsent(false);
    setReviewed(false);
    setError("");
    setGeneration((v) => v + 1);
  });
  const locked = command.pending || command.uncertain;
  const dirty = !!preview || locked || loading;
  useEffect(() => {
    editor.formDirty("property-capability-delivery", dirty);
    return () => editor.formDirty("property-capability-delivery", false);
  }, [editor, dirty]);
  useEffect(() => {
    if (command.error && !command.uncertain) {
      setConsent(false);
      setReviewed(false);
    }
  }, [command.error, command.uncertain]);
  async function review(forRetry = false) {
    if (locked) return;
    setLoading(true);
    setError("");
    command.clearError();
    setConsent(false);
    setReviewed(false);
    try {
      const value = await api.deliveryPreview!(request.id);
      if ((await manifestHash(value.contextPack)) !== value.packHash)
        throw new StudioError("RESPONSE_INVALID");
      if (!alive.current) return;
      setPreview(value);
      setRetry(forRetry);
      setConsent(false);
      setReviewed(true);
    } catch (cause) {
      if (alive.current)
        setError(cause instanceof StudioError ? cause.code : "CAPABILITY_DELIVERY_UNAVAILABLE");
    } finally {
      if (alive.current) setLoading(false);
    }
  }
  function send() {
    if (!preview || !consent || !reviewed) return;
    if (retry && selected)
      void command.send("retry-capability-delivery", {
        deliveryId: selected.id,
        expectedDeliveryRevision: selected.revision,
        expectedRequestRevision: preview.requestRevision,
        expectedSeq: preview.seq,
        expectedManifestHash: preview.manifestHash,
        expectedTargetRevision: preview.target.revision,
        confirmExternalShare: true,
      });
    else void command.send("send-capability-context", sendPayload(preview));
  }
  return (
    <section className="capability-delivery" aria-busy={loading || command.pending}>
      <h4>{text("Передача разработчику", "Developer handoff")}</h4>
      <p>
        {text(
          "Подтверждение означает только сохранение пакета получателем. Инженерный статус запроса меняется отдельно.",
          "Confirmation only means the recipient stored the pack. Engineering status changes separately.",
        )}
      </p>
      {(error || command.error) && (
        <p role="alert">{deliveryMessage(error || command.error, ru)}</p>
      )}
      {canSend && (
        <button disabled={locked || loading || !!preview} onClick={() => void review()}>
          {text("Передать context pack", "Deliver context pack")}
        </button>
      )}
      <button
        disabled={locked || loading || !!preview}
        onClick={() => {
          setError("");
          setGeneration((v) => v + 1);
        }}
      >
        {text("Обновить журнал передачи", "Refresh delivery journal")}
      </button>
      {!items.length && (
        <p>
          {text(
            "Пакеты ещё не передавались автоматически. Доступна ручная выгрузка.",
            "No packs have been delivered automatically. Manual download is available.",
          )}
        </p>
      )}
      <ul>
        {items.map((d) => (
          <li key={d.id}>
            <button
              disabled={locked || !!preview}
              onClick={() => {
                selectedID.current = d.id;
                setGeneration((v) => v + 1);
              }}
            >
              {deliveryStatus(d.state, ru)} · v{d.packRevision} · {d.id}
            </button>
          </li>
        ))}
      </ul>
      {cursor && (
        <button disabled={locked || !!preview} onClick={() => void read(undefined, true)}>
          {text("Ещё передачи", "More deliveries")}
        </button>
      )}
      {selected && (
        <div role="region" aria-label={text("Журнал передачи", "Delivery journal")}>
          <p role="status">
            {deliveryStatus(selected.state, ru)} · {selected.totalAttempts}/18
          </p>
          <p>{selected.destinationUrl}</p>
          <code>{selected.bodyHash}</code>
          {selected.lastError && <p>{deliveryMessage(selected.lastError, ru)}</p>}
          {selected.state === "retry_wait" && (
            <p>
              {text("Следующая попытка", "Next attempt")}:{" "}
              {new Date(selected.nextAttemptAt).toLocaleString(ru ? "ru-RU" : "en-GB")}
            </p>
          )}
          <ol>
            {selected.attempts?.map((a) => (
              <li key={a.id}>
                {a.ordinal} · {a.outcome} · {a.httpStatus ?? "—"}
                {a.receiptDeliveryId && (
                  <p>
                    {text("Квитанция сохранения", "Storage receipt")}: {a.receiptDeliveryId}
                    <br />
                    <code>{a.receiptBodyHash}</code>
                  </p>
                )}
              </li>
            ))}
          </ol>
          {selected.canRetry && (
            <button disabled={locked || loading || !!preview} onClick={() => void review(true)}>
              {text("Повторить передачу", "Retry delivery")}
            </button>
          )}
          {selected.canCancel && (
            <button
              disabled={locked || !!preview}
              onClick={() =>
                void command.send("cancel-capability-delivery", {
                  deliveryId: selected.id,
                  expectedDeliveryRevision: selected.revision,
                })
              }
            >
              {text("Остановить попытки", "Stop attempts")}
            </button>
          )}
          <details>
            <summary>{text("История передачи", "Delivery audit")}</summary>
            {selected.audit?.map((a) => (
              <p key={a.id}>
                {a.event} · {a.source} · {a.reason}
              </p>
            ))}
          </details>
        </div>
      )}
      {preview && (
        <Dialog
          title={text("Подтверждение внешней передачи", "Confirm external sharing")}
          locked={locked}
          onDismiss={() => {
            setPreview(null);
            setConsent(false);
          }}
        >
          <p>
            {text("Точный адрес получателя", "Exact recipient address")}:{" "}
            <strong>{preview.target.url}</strong>
          </p>
          <p>
            {text("Ревизия получателя", "Recipient revision")}: {preview.target.revision}
          </p>
          <p>
            Context pack v{preview.packRevision}: <code>{preview.packHash}</code>
          </p>
          <pre className="capability-context">{JSON.stringify(preview.contextPack, null, 2)}</pre>
          <label>
            <input
              type="checkbox"
              checked={consent}
              disabled={locked}
              onChange={(e) => setConsent(e.target.checked)}
            />
            {text(
              "Разрешаю передать этот context pack на указанный внешний адрес",
              "I consent to sharing this context pack with this external address",
            )}
          </label>
          <button disabled={locked || !consent || !reviewed} onClick={send}>
            {text("Подтвердить передачу", "Confirm delivery")}
          </button>
          <button disabled={locked || loading} onClick={() => void review(retry)}>
            {text("Перечитать сведения", "Reload details")}
          </button>
          <button
            disabled={locked}
            onClick={() => {
              setPreview(null);
              setConsent(false);
            }}
          >
            {text("Закрыть без передачи", "Close without sharing")}
          </button>
        </Dialog>
      )}
      {command.uncertain && (
        <button disabled={command.pending} onClick={() => void command.send()}>
          {text("Повторить тот же запрос", "Retry exact request")}
        </button>
      )}
    </section>
  );
}
