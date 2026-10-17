import { useEffect, useRef, useState } from "react";
import { useStore } from "zustand";
import { StudioError, type Api } from "./types.js";
import type { Editor } from "./editor.js";
import type { CapabilityRequest } from "./capabilities.js";
import {
  githubLabel,
  githubMessage,
  type GithubAttempt,
  type GithubCommand,
  type GithubConfig,
  type GithubObservation,
  type GithubView,
} from "./capability-github.js";

function useGithubAction(api: Api, editor: Editor, done: () => Promise<void>) {
  const [pending, setPending] = useState(false),
    [uncertain, setUncertain] = useState(false),
    [error, setError] = useState("");
  const attempt = useRef<GithubAttempt | null>(null),
    running = useRef(false),
    alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  async function send(name?: GithubCommand, payload?: Record<string, unknown>, reason?: string) {
    if (running.current || !api.githubCommand) return;
    running.current = true;
    setPending(true);
    setError("");
    editor.store.setState({ status: "saving" });
    try {
      if (!attempt.current) {
        if (!name || !payload || !reason?.trim()) return;
        const account = await api.account();
        if (!account.authenticated || !account.user?.id) throw new StudioError("UNAUTHENTICATED");
        attempt.current = {
          name,
          payload: structuredClone(payload),
          key: crypto.randomUUID(),
          reason,
          actor: account.user.id,
        };
      }
      await api.githubCommand(attempt.current);
      attempt.current = null;
      if (!alive.current) return;
      setUncertain(false);
      await done();
    } catch (e) {
      if (!alive.current) return;
      const u = e instanceof StudioError && e.uncertain;
      setUncertain(u);
      if (!u) attempt.current = null;
      setError(e instanceof StudioError ? e.code : "CAPABILITY_GITHUB_UNAVAILABLE");
    } finally {
      running.current = false;
      if (alive.current) {
        setPending(false);
        editor.store.setState({ status: editor.store.getState().session ? "ready" : "idle" });
      }
    }
  }
  return { send, pending, uncertain, error };
}
function Observation({ value, ru }: { value: GithubObservation; ru: boolean }) {
  const f = value.facts;
  return (
    <div>
      <p>
        {ru ? "Наблюдение" : "Observation"}: PR #{f.number} · {ru ? "репозиторий" : "repository"}{" "}
        {f.repositoryId}
      </p>
      <p>
        {githubLabel(f.state, ru)}
        {f.draft ? " · Draft" : ""} · CI: {githubLabel(f.ci, ru)}
      </p>
      <p>
        {ru ? "Проверено" : "Observed"}: <time dateTime={value.observedAt}>{value.observedAt}</time>
      </p>
      <p>
        {githubLabel("head", ru)}: <code style={{ overflowWrap: "anywhere" }}>{f.headSha}</code>
      </p>
      <p>
        {githubLabel(f.integrationType, ru)}:{" "}
        <code style={{ overflowWrap: "anywhere" }}>{f.integrationSha ?? "—"}</code>
      </p>
      <ul>
        {f.results.map((r, i) => (
          <li key={i}>
            {r.name} · {githubLabel(r.target, ru)} · {githubLabel(r.outcome, ru)}{" "}
            <code style={{ overflowWrap: "anywhere" }}>{r.exactSha}</code>
          </li>
        ))}
      </ul>
    </div>
  );
}
export function CapabilityGithubPanel({
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
  const [view, setView] = useState<GithubView | null>(null),
    [number, setNumber] = useState(""),
    [reason, setReason] = useState(""),
    [replace, setReplace] = useState(false),
    [error, setError] = useState(""),
    [history, setHistory] = useState<GithubObservation[]>([]),
    [cursor, setCursor] = useState<string | null>(null);
  const alive = useRef(true);
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 30000);
    return () => clearInterval(id);
  }, []);
  const current = view?.current && Date.parse(view.current.freshUntil) > now ? view.current : null;
  async function read(signal?: AbortSignal) {
    try {
      const v = await api.github!(request.id, signal);
      if (alive.current && !signal?.aborted) {
        setView(v);
        setError("");
      }
    } catch (e) {
      if (alive.current && !signal?.aborted)
        setError(e instanceof StudioError ? e.code : "CAPABILITY_GITHUB_UNAVAILABLE");
    }
  }
  const command = useGithubAction(api, editor, async () => {
    setNumber("");
    setReason("");
    setReplace(false);
    await read();
  });
  const locked = command.pending || command.uncertain,
    dirty = !!number || !!reason || replace || locked;
  useEffect(() => {
    alive.current = true;
    const c = new AbortController();
    void read(c.signal);
    return () => {
      alive.current = false;
      c.abort();
    };
  }, [api, request.id, request.revision]);
  useEffect(() => {
    editor.formDirty("property-capability-github", dirty);
    return () => editor.formDirty("property-capability-github", false);
  }, [editor, dirty]);
  const payload = {
    requestId: request.id,
    expectedRequestRevision: request.revision,
    expectedLinkRevision: view?.link?.revision ?? 0,
    expectedConfigRevision: view?.configRevision ?? 0,
  };
  async function historyPage(more = false) {
    try {
      const v = await api.githubHistory!(request.id, more ? (cursor ?? undefined) : undefined);
      if (alive.current) {
        setHistory((old) => (more ? [...old, ...v.items] : v.items));
        setCursor(v.nextCursor ?? null);
      }
    } catch (e) {
      if (alive.current)
        setError(e instanceof StudioError ? e.code : "CAPABILITY_GITHUB_UNAVAILABLE");
    }
  }
  return (
    <section aria-label="GitHub" aria-busy={command.pending}>
      <h4>GitHub</h4>
      <p>
        {text(
          "Факты GitHub не подтверждают развёртывание или выполнение запроса.",
          "GitHub facts do not confirm deployment or capability fulfillment.",
        )}
      </p>
      {(error || command.error) && <p role="alert">{githubMessage(error || command.error, ru)}</p>}
      {view && (
        <>
          <p>
            {view.repository?.owner}/{view.repository?.name} · {view.repository?.baseBranch} ·{" "}
            {githubLabel(view.current && !current ? "stale" : view.syncState, ru)}
          </p>
          {view.url && (
            <a href={view.url} target="_blank" rel="noopener noreferrer">
              PR #{view.link?.pullNumber}
            </a>
          )}
          {view.link?.lastError && (
            <p>
              {text(
                "Последнее обновление не подтверждено.",
                "The last refresh could not be verified.",
              )}
            </p>
          )}
          {current ? (
            <Observation value={current} ru={ru} />
          ) : view.lastKnown ? (
            <>
              <p>
                {text(
                  "Последние известные данные — не актуальное подтверждение.",
                  "Last-known information — not current verification.",
                )}
              </p>
              <Observation value={view.lastKnown} ru={ru} />
            </>
          ) : (
            <p>{text("Проверенных данных пока нет.", "No verified information yet.")}</p>
          )}
          {view.canManage && (
            <form
              onSubmit={(e) => {
                e.preventDefault();
                if (Number.isSafeInteger(Number(number)) && Number(number) > 0)
                  void command.send(
                    "link-capability-github-pr",
                    { ...payload, pullNumber: Number(number), confirmReplace: replace },
                    reason,
                  );
              }}
            >
              <fieldset disabled={locked || !view.configRevision || view.syncState === "disabled"}>
                <legend>{text("Связь с PR", "PR association")}</legend>
                <label>
                  {text("Номер PR", "PR number")}
                  <input
                    type="number"
                    min="1"
                    step="1"
                    required
                    value={number}
                    onChange={(e) => setNumber(e.target.value)}
                  />
                </label>
                <label>
                  {text("Причина", "Reason")}
                  <input
                    maxLength={2000}
                    required
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                  />
                </label>
                {view.link?.active && (
                  <label>
                    <input
                      type="checkbox"
                      checked={replace}
                      onChange={(e) => setReplace(e.target.checked)}
                    />
                    {text(
                      `Подтверждаю замену PR #${view.link.pullNumber} на #${number || "…"}`,
                      `Replace PR #${view.link.pullNumber} with #${number || "…"}`,
                    )}
                  </label>
                )}
                <button>{text("Привязать PR", "Link PR")}</button>
                <button
                  type="button"
                  disabled={!view.link?.active || !reason.trim()}
                  onClick={() => void command.send("refresh-capability-github-pr", payload, reason)}
                >
                  {text("Проверить GitHub", "Refresh GitHub")}
                </button>
                <button
                  type="button"
                  disabled={!view.link?.active || !reason.trim()}
                  onClick={() => {
                    const { expectedConfigRevision: _, ...p } = payload;
                    void command.send("unlink-capability-github-pr", p, reason);
                  }}
                >
                  {text("Отвязать PR", "Unlink PR")}
                </button>
              </fieldset>
            </form>
          )}
          {view.link && (
            <p>
              {text("Следующее ручное обновление не раньше", "Next manual refresh after")}:{" "}
              {view.link.nextAllowedAt}
            </p>
          )}
          <button type="button" disabled={locked} onClick={() => void read()}>
            {text("Обновить отображение", "Reload view")}
          </button>
          <button type="button" disabled={locked} onClick={() => void historyPage()}>
            {text("История наблюдений", "Observation history")}
          </button>
          {history.map((v) => (
            <details key={v.id}>
              <summary>{v.observedAt}</summary>
              <Observation value={v} ru={ru} />
            </details>
          ))}
          {cursor && (
            <button type="button" onClick={() => void historyPage(true)}>
              {text("Ещё наблюдения", "More observations")}
            </button>
          )}
        </>
      )}
      {command.uncertain && (
        <button type="button" disabled={command.pending} onClick={() => void command.send()}>
          {text("Повторить тот же запрос", "Retry exact request")}
        </button>
      )}
      {dirty && !locked && (
        <button
          type="button"
          onClick={() => {
            setNumber("");
            setReason("");
            setReplace(false);
          }}
        >
          {text("Сбросить ввод GitHub", "Discard GitHub input")}
        </button>
      )}
    </section>
  );
}
export function GithubConfigPanel({ api, editor }: { api: Api; editor: Editor }) {
  const state = useStore(editor.store),
    ru = state.locale === "ru",
    text = (r: string, e: string) => (ru ? r : e);
  const [config, setConfig] = useState<GithubConfig | null>(null),
    [selected, setSelected] = useState(""),
    [enabled, setEnabled] = useState(false),
    [consent, setConsent] = useState(false),
    [reason, setReason] = useState(""),
    [error, setError] = useState("");
  const alive = useRef(true);
  async function read(signal?: AbortSignal) {
    try {
      const c = await api.githubConfig!(signal);
      if (alive.current && !signal?.aborted) {
        setConfig(c);
        setSelected(c.configId ?? "");
        setEnabled(c.enabled ?? false);
        setError("");
      }
    } catch (e) {
      if (alive.current && !signal?.aborted)
        setError(e instanceof StudioError ? e.code : "CAPABILITY_GITHUB_UNAVAILABLE");
    }
  }
  const command = useGithubAction(api, editor, async () => {
    setReason("");
    setConsent(false);
    await read();
  });
  const locked = command.pending || command.uncertain;
  const dirty =
    selected !== (config?.configId ?? "") ||
    enabled !== (config?.enabled ?? false) ||
    !!reason ||
    consent ||
    locked;
  useEffect(() => {
    alive.current = true;
    const c = new AbortController();
    void read(c.signal);
    return () => {
      alive.current = false;
      c.abort();
    };
  }, [api]);
  useEffect(() => {
    editor.formDirty("property-github-config", dirty);
    return () => editor.formDirty("property-github-config", false);
  }, [editor, dirty]);
  return (
    <section className="capability-queue">
      <h1>{text("Интеграция GitHub", "GitHub integration")}</h1>
      {config && !config.choices?.length && (
        <p>
          {text(
            "Каталог репозиториев пуст. Обратитесь к администратору сервера для подключения GitHub.",
            "The repository catalogue is empty. Contact the server administrator to connect GitHub.",
          )}
        </p>
      )}
      <p>
        {text(
          "Включение разрешает участникам проекта с доступом к запросу видеть ограниченные метаданные настроенного репозитория, включая приватные PR. Код и журналы CI не передаются.",
          "Enabling allows project members with access to a request to view limited metadata of the configured repository, including private PRs. Code and CI logs are not shared.",
        )}
      </p>
      {(error || command.error) && <p role="alert">{githubMessage(error || command.error, ru)}</p>}
      {config && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void api
              .config()
              .then((c) =>
                command.send(
                  "set-capability-github-config",
                  {
                    environment: c.environment,
                    configId: selected,
                    enabled,
                    expectedConfigRevision: config.revision,
                  },
                  reason,
                ),
              )
              .catch(() => setError("CAPABILITY_GITHUB_UNAVAILABLE"));
          }}
        >
          <fieldset disabled={locked}>
            <legend>{text("Настройки", "Settings")}</legend>
            <label>
              {text("Репозиторий", "Repository")}
              <select required value={selected} onChange={(e) => setSelected(e.target.value)}>
                <option value="">—</option>
                {config.choices?.map((c) => (
                  <option key={c.configId} value={c.configId}>
                    {c.owner}/{c.name} · {c.baseBranch}
                  </option>
                ))}
              </select>
            </label>
            <label>
              <input
                type="checkbox"
                checked={enabled}
                onChange={(e) => setEnabled(e.target.checked)}
              />
              {text("Включено", "Enabled")}
            </label>
            {enabled && (
              <label>
                <input
                  type="checkbox"
                  required
                  checked={consent}
                  onChange={(e) => setConsent(e.target.checked)}
                />
                {text(
                  "Подтверждаю доступ участников к метаданным",
                  "I confirm member access to metadata",
                )}
              </label>
            )}
            <label>
              {text("Причина", "Reason")}
              <input
                required
                maxLength={2000}
                value={reason}
                onChange={(e) => setReason(e.target.value)}
              />
            </label>
            <button disabled={!selected || (enabled && !consent)}>
              {text("Сохранить настройки", "Save settings")}
            </button>
          </fieldset>
        </form>
      )}
      {command.uncertain && (
        <button disabled={command.pending} onClick={() => void command.send()}>
          {text("Повторить тот же запрос", "Retry exact request")}
        </button>
      )}
      {dirty && !locked && (
        <button
          onClick={() => {
            setSelected(config?.configId ?? "");
            setEnabled(config?.enabled ?? false);
            setReason("");
            setConsent(false);
          }}
        >
          {text("Сбросить ввод", "Discard input")}
        </button>
      )}
    </section>
  );
}
