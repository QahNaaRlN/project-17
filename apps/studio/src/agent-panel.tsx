import { useRef, useState } from "react";
import { useStore } from "zustand";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { StudioError, type Api } from "./types.js";
import type { Editor } from "./editor.js";

const stepNames: Record<string, [string, string]> = {
  started: ["Задача запущена", "Task started"],
  finished: ["Задача завершена", "Task finished"],
  get_changeset: ["Чтение черновика", "Reading draft"],
  get_page_structure: ["Чтение структуры страницы", "Reading page structure"],
  list_schemas: ["Чтение схем данных", "Reading data schemas"],
  get_capabilities: ["Проверка доступных возможностей", "Checking available capabilities"],
  insert_node: ["Добавление элемента", "Adding element"],
  move_node: ["Перенос элемента", "Moving element"],
  remove_node: ["Удаление элемента", "Removing element"],
  update_props: ["Изменение свойств", "Changing properties"],
  update_design: ["Изменение оформления", "Changing design"],
  bind_data: ["Привязка данных", "Binding data"],
  set_behavior: ["Настройка поведения", "Configuring behavior"],
  set_local_content: ["Изменение текста", "Changing text"],
};
const errorNames: Record<string, [string, string]> = {
  NETWORK_UNCERTAIN: [
    "Ответ не дошёл. Повтор запроса не создаст вторую задачу.",
    "The response was lost. Retrying will not create another task.",
  ],
  AGENT_TOKEN_LIMIT: ["Достигнут месячный лимит токенов.", "Monthly token limit reached."],
  AGENT_USAGE_UNKNOWN: [
    "Ответ модели не получен. Расход остаётся неизвестным.",
    "The model response was lost. Usage remains unknown.",
  ],
  AGENT_CONTEXT_LIMIT: [
    "Для этой задачи слишком большой контекст.",
    "The context is too large for this task.",
  ],
  AGENT_STEP_LIMIT: ["Достигнут лимит шагов задачи.", "The task reached its step limit."],
  AGENT_SCOPE_CHANGED: [
    "Черновик изменился. Обновите состояние.",
    "The draft changed. Refresh its state.",
  ],
  AGENT_SESSION_INVALID: [
    "Доступ агента отозван или истёк.",
    "Agent access was revoked or expired.",
  ],
  AGENT_INTERRUPTED: [
    "Выполнение прервалось. Проверьте сохранённые правки.",
    "Execution was interrupted. Review saved changes.",
  ],
};

export function AgentPanel({ api, editor }: { api: Api; editor: Editor }) {
  const state = useStore(editor.store),
    client = useQueryClient(),
    session = state.session;
  const [prompt, setPrompt] = useState(""),
    [pending, setPending] = useState(false),
    [error, setError] = useState<string | null>(null);
  const attempt = useRef<{
    name: "start-agent-run" | "cancel-agent-run";
    payload: Record<string, unknown>;
    key: string;
  } | null>(null);
  const config = useQuery({
    queryKey: ["agent-config"],
    queryFn: ({ signal }) => api.agentConfig!(signal),
    enabled: !!api.agentConfig,
    retry: false,
  });
  const runs = useQuery({
    queryKey: ["agent-runs", session?.record?.id, session?.changeset.id],
    queryFn: ({ signal }) => api.agentRuns!({ ...session!, record: session!.record! }, signal),
    enabled: !!session?.record && !!api.agentRuns,
    retry: false,
    refetchInterval: 5000,
  });
  const ru = state.locale === "ru",
    active = runs.data?.some((r) => r.state === "queued" || r.state === "running");
  const dirty = Object.values(state.dirtyFields).some(Boolean),
    busy = state.status !== "ready";
  const canStart =
    !!session &&
    !!config.data?.configured &&
    runs.isSuccess &&
    config.data.canDelegate &&
    session.review.canEdit &&
    session.changeset.state === "open" &&
    !active &&
    !dirty &&
    !busy &&
    !pending &&
    !attempt.current;
  async function send(
    name: "start-agent-run" | "cancel-agent-run",
    payload: Record<string, unknown>,
  ) {
    if (pending || !api.agentCommand) return;
    const current = attempt.current ?? { name, payload, key: crypto.randomUUID() };
    attempt.current = current;
    setPending(true);
    setError(null);
    try {
      await api.agentCommand(current.name, current.payload, current.key);
      attempt.current = null;
      if (current.name === "start-agent-run") setPrompt("");
      await client.invalidateQueries({ queryKey: ["agent-runs"] });
    } catch (cause) {
      const uncertain = cause instanceof StudioError && cause.uncertain;
      if (!uncertain) attempt.current = null;
      setError(
        uncertain
          ? "NETWORK_UNCERTAIN"
          : cause instanceof StudioError
            ? cause.code
            : "AGENT_REQUEST_FAILED",
      );
    } finally {
      setPending(false);
    }
  }
  if (!session) return null;
  return (
    <section className="agent-panel" aria-label="Agent">
      <h2>Agent</h2>
      <p>
        {ru
          ? "Задача меняет только текущий черновик. Проверьте правки в истории перед отправкой на согласование."
          : "The task edits the current draft. Review changes in history before submitting for approval."}
      </p>
      {config.isError || runs.isError ? (
        <p role="alert">
          {ru ? "Не удалось загрузить состояние агента" : "Could not load Agent state"}
        </p>
      ) : null}
      {config.data && !config.data.configured ? (
        <p role="status">
          {ru
            ? "OpenRouter не настроен: администратору нужно задать серверный ключ и модель."
            : "OpenRouter is not configured: an administrator must set the server key and model."}
        </p>
      ) : null}
      {config.data?.configured ? <p>OpenRouter · {config.data.model}</p> : null}
      {config.data && !config.data.canDelegate ? (
        <p>
          {ru
            ? "Для запуска нужен вход пользователя и право делегирования."
            : "Sign in with a user account that can delegate."}
        </p>
      ) : null}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (canStart && prompt.trim())
            void send("start-agent-run", {
              documentId: session.record!.id,
              changesetId: session.changeset.id,
              environment: session.config.environment,
              expectedSeq: session.changeset.seq,
              prompt: prompt.trim(),
            });
        }}
      >
        <label>
          {ru ? "Задача для агента" : "Agent task"}
          <textarea
            maxLength={4000}
            value={prompt}
            disabled={!canStart}
            onChange={(e) => setPrompt(e.target.value)}
          />
        </label>
        <button disabled={!canStart || !prompt.trim()}>
          {pending ? (ru ? "Отправка…" : "Sending…") : ru ? "Запустить задачу" : "Start task"}
        </button>
      </form>
      {error ? (
        <p role="alert">
          {errorNames[error]?.[ru ? 0 : 1] ??
            (ru ? "Не удалось выполнить запрос." : "The request could not be completed.")}
          {attempt.current ? (
            <button
              disabled={pending}
              onClick={() => {
                const current = attempt.current;
                if (current) void send(current.name, current.payload);
              }}
            >
              {ru ? "Повторить запрос" : "Retry request"}
            </button>
          ) : null}
        </p>
      ) : null}
      <button
        disabled={pending}
        onClick={() => {
          void runs.refetch();
          void config.refetch();
        }}
      >
        {ru ? "Обновить состояние" : "Refresh state"}
      </button>
      {runs.data?.map((run) => (
        <article key={run.id}>
          <h3>{run.prompt}</h3>
          <p role="status">
            {
              {
                queued: ru ? "В очереди" : "Queued",
                running: ru ? "Выполняется" : "Running",
                completed: ru ? "Завершено" : "Completed",
                failed: ru ? "Не выполнено" : "Failed",
                cancelled: ru ? "Отменено" : "Cancelled",
                interrupted: ru ? "Прервано" : "Interrupted",
              }[run.state]
            }{" "}
            {run.errorCode
              ? (errorNames[run.errorCode]?.[ru ? 0 : 1] ??
                (ru
                  ? "Проверьте шаги и сохранённые правки."
                  : "Review the steps and saved changes."))
              : ""}
          </p>
          <ol>
            {run.steps.map((step) => (
              <li key={step.seq}>
                {step.data.text ??
                  stepNames[step.data.name ?? step.kind]?.[ru ? 0 : 1] ??
                  (ru ? "Действие агента" : "Agent action")}
                {step.data.isError ? " · " + (ru ? "ошибка" : "error") : ""}
              </li>
            ))}
          </ol>
          <p>
            {ru ? "Подтверждённые токены" : "Confirmed tokens"}: {run.tokens}
            {!run.usageKnown
              ? " · " +
                (ru ? "часть расхода неизвестна; резерв" : "some usage unknown; reserved") +
                ": " +
                run.reserved
              : ""}
          </p>
          {run.state === "queued" || run.state === "running" ? (
            <button
              disabled={pending || !!attempt.current}
              onClick={() => void send("cancel-agent-run", { runId: run.id })}
            >
              {ru ? "Отменить задачу" : "Cancel task"}
            </button>
          ) : null}
        </article>
      ))}
      <button
        disabled={!session.history.canUndo || dirty || busy || pending || !!active}
        onClick={() => void editor.changeHistory("undo")}
      >
        {ru ? "Отменить последнюю группу правок" : "Undo last change group"}
      </button>
    </section>
  );
}
