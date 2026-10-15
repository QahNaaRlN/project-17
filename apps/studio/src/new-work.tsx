import { useEffect, useRef, useState } from "react";
import { Button, Dialog, Notice } from "./components.js";
import { StudioError, type Api } from "./types.js";
import {
  canRetryWork,
  clearWorkAttempt,
  loadWorkAttempt,
  makeWorkAttempt,
  saveWorkAttempt,
  type WorkCreationAttempt,
  type WorkCreationScope,
} from "./work-creation.js";

export function NewWork({
  api,
  scope,
  ru,
  disabled,
  onCreated,
}: {
  api: Api;
  scope: WorkCreationScope;
  ru: boolean;
  disabled: boolean;
  onCreated: (id: string) => Promise<boolean>;
}) {
  const [initial] = useState(() => {
    try {
      return { attempt: loadWorkAttempt(scope), error: "" };
    } catch {
      return { attempt: null, error: "storage" };
    }
  });
  const [attempt, setAttempt] = useState(initial.attempt),
    [error, setError] = useState(initial.error),
    [title, setTitle] = useState(initial.attempt?.body.payload.title ?? ""),
    [shown, show] = useState(!!initial.attempt || !!initial.error),
    [busy, setBusy] = useState(false);
  const sending = useRef(false),
    alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  async function submit() {
    if (sending.current || disabled) return;
    sending.current = true;
    setBusy(true);
    setError("");
    let a: WorkCreationAttempt | null = attempt;
    let recovering = !!attempt;
    try {
      a ??= loadWorkAttempt(scope);
      recovering = !!a;
      a ??= makeWorkAttempt(scope, title);
      if (!a.result && !canRetryWork(a)) {
        setError("expired");
        return;
      }
      a = { ...a, lastSeenAt: Date.now() };
      saveWorkAttempt(a);
      setAttempt(a);
      // Keep the recovery dialog in the visible Work screen after a reload.
      window.history.replaceState({}, "", "/editor#work=");
      if (!a.result) {
        const result = await api.createWork!(a);
        if (!alive.current) return;
        a = { ...a, result };
        setAttempt(a);
        saveWorkAttempt(a);
      }
      const opened = await onCreated(a.result!.id);
      if (!alive.current) return;
      if (!opened) {
        setError("open");
        return;
      }
      clearWorkAttempt(scope);
      setAttempt(null);
      setTitle("");
      show(false);
      requestAnimationFrame(() => document.getElementById("work-card-title")?.focus());
    } catch (cause) {
      if (!alive.current) return;
      const e = cause instanceof StudioError ? cause : new StudioError("NETWORK_UNCERTAIN", true);
      if (e.code === "TITLE_INVALID") setError("title");
      else if (e.code === "WORK_STORAGE_UNAVAILABLE") setError("storage");
      else if (e.uncertain || a?.result || recovering) setError(a?.result ? "open" : "unknown");
      else {
        // An explicit rejection is distinct from an unconfirmed commit.
        try {
          clearWorkAttempt(scope);
          setAttempt(null);
          setError("rejected");
        } catch {
          setError("storage");
        }
      }
    } finally {
      sending.current = false;
      if (alive.current) setBusy(false);
    }
  }
  const expired = !!attempt && !attempt.result && !canRetryWork(attempt);
  const errors: Record<string, string> = ru
    ? {
        title: "Введите название от 1 до 200 символов.",
        storage:
          "Не удалось сохранить попытку в этой вкладке. Проверьте доступность хранилища и повторите.",
        unknown:
          "Результат создания неизвестен. Проверьте тот же запрос перед созданием другой работы.",
        rejected: "Сервер отклонил создание. Проверьте доступ и окружение, затем повторите.",
        open: "Работа создана, но не открыта. Повторите открытие той же работы.",
        expired:
          "Безопасный срок повтора истёк или время изменилось. Сверьте результат с очередью вручную; фильтры могут скрывать работу.",
      }
    : {
        title: "Enter a name of 1–200 characters.",
        storage: "Could not store the attempt in this tab. Check storage availability and retry.",
        unknown: "Creation outcome is unknown. Check the same request before creating other work.",
        rejected: "The server rejected creation. Check access and environment, then retry.",
        open: "Work was created but could not be opened. Retry opening the same work.",
        expired:
          "The safe retry window expired or the clock changed. Check the queue manually; filters may hide the work.",
      };
  return (
    <>
      <Button variant="primary" disabled={disabled || busy} onClick={() => show(true)}>
        {attempt
          ? ru
            ? "Продолжить создание"
            : "Resume creation"
          : ru
            ? "Новая работа"
            : "New work"}
      </Button>
      {shown && (
        <Dialog
          title={ru ? "Новая работа с контентом" : "New content work"}
          locked={busy}
          onDismiss={() => show(false)}
        >
          <form
            className="create-bar"
            onSubmit={(e) => {
              e.preventDefault();
              void submit();
            }}
          >
            <label>
              {ru ? "Название работы" : "Work name"}
              <input
                value={title}
                disabled={busy || !!attempt}
                aria-invalid={error === "title"}
                onChange={(e) => {
                  setTitle(e.target.value);
                  setError("");
                }}
              />
            </label>
            <p>
              {ru ? "Окружение" : "Environment"}: {scope.environment}
            </p>
            {!!attempt && !error && (
              <Notice>
                {ru
                  ? "Есть незавершённая попытка. Повтор выполняется только по вашему действию."
                  : "An unfinished attempt exists. Retry requires your action."}
              </Notice>
            )}
            {(error || expired) && <Notice error>{errors[expired ? "expired" : error]}</Notice>}
            <div className="work-card-actions">
              <Button type="submit" disabled={busy || disabled || expired}>
                {busy
                  ? ru
                    ? "Проверка…"
                    : "Checking…"
                  : attempt?.result
                    ? ru
                      ? "Открыть созданную работу"
                      : "Open created work"
                    : attempt
                      ? ru
                        ? "Проверить / повторить"
                        : "Check / retry"
                      : ru
                        ? "Создать работу"
                        : "Create work"}
              </Button>
              <Button disabled={busy} onClick={() => show(false)}>
                {ru ? "Закрыть" : "Close"}
              </Button>
              {expired && (
                <Button
                  disabled={busy}
                  onClick={() => {
                    if (
                      !window.confirm(
                        ru
                          ? "Результат прежней попытки не установлен. Другая работа может оказаться дубликатом. Создать другую?"
                          : "The previous outcome is unknown. Other work may be a duplicate. Create other work?",
                      )
                    )
                      return;
                    try {
                      clearWorkAttempt(scope);
                      setAttempt(null);
                      setTitle("");
                      setError("");
                    } catch {
                      setError("storage");
                    }
                  }}
                >
                  {ru ? "Создать другую работу" : "Create other work"}
                </Button>
              )}
            </div>
          </form>
        </Dialog>
      )}
    </>
  );
}
