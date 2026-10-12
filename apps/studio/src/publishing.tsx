import { useEffect, useRef, useState } from "react";
import { useStore } from "zustand";
import type { Editor } from "./editor.js";
import { diagnosticNode, reviewDiagnostics } from "./review.js";
import type { WorkflowAction } from "./types.js";
const copy = {
  ru: {
    panel: "Проверки и публикация",
    targets: "Окружения проверки",
    objects: "Изменённые объекты",
    all: "Действия относятся ко всему черновику, включая другие страницы и объекты.",
    risk: "Риск",
    approvals: "Согласования",
    missing: "Необходимые роли",
    checks: "Проверки",
    none: "Проверки ещё не запускались. Отправьте черновик на проверку.",
    history: "Решения согласующих",
    emptyHistory: "Решений пока нет",
    stale: "Сброшено или относится к прежним изменениям",
    comment: "Что нужно исправить",
    cancel: "Отмена",
    confirm: "Подтвердить публикацию",
    question: "Опубликовать весь черновик",
    reopenHint:
      "Возврат в работу сбросит полученные согласования. После правок потребуется повторная проверка.",
    reopenConfirm: "Вернуть в работу и сбросить согласования",
    diagnostics: "Замечания проверки",
    goto: "Выбрать блок",
    retry: "Повторить тот же запрос",
    uncertain:
      "Ответ о действии не подтверждён. Повторите запрос с прежними параметрами; новое действие не будет создано.",
    published: "Публикация подтверждена",
    visit: "Открыть страницу приложения",
    snapshot:
      "Показаны результаты последней подачи. При согласовании и публикации сервер повторит необходимые проверки.",
    states: {
      open: "В работе",
      in_review: "На согласовании",
      approved: "Готов к публикации",
      failed: "Проверки не пройдены",
      changes_requested: "Нужны правки",
      merged: "Опубликован",
      abandoned: "Закрыт",
    },
    actions: {
      submit: "Отправить на проверку",
      reopen: "Вернуть в работу",
      approve: "Согласовать",
      "request-changes": "Запросить правки",
      publish: "Опубликовать",
    },
    risks: { low: "Низкий", medium: "Средний", high: "Высокий" },
    status: {
      passed: "Пройдена",
      failed: "Не пройдена",
      pending: "Ожидает",
      running: "Выполняется",
    },
    stages: { ir: "Страница и контракт окружения", policy: "Права и правила проекта" },
    reasons: {
      FORBIDDEN: "У текущей учётной записи нет права на это действие.",
      CHANGESET_NOT_OWNER: "Подавать и возвращать в работу может владелец черновика.",
      APPROVAL_FORBIDDEN_ACTOR:
        "Согласование требует входа от имени человека. Локальная сервисная учётная запись не может согласовывать.",
      APPROVAL_SELF: "Автор правок не может согласовать свои изменения.",
      CHANGESET_EMPTY: "В черновике ещё нет изменений.",
      CHANGESET_HAS_CONFLICTS: "Сначала разрешите конфликты черновика.",
      REBASE_REQUIRED:
        "Исходные версии изменились. Нужны обновление основы черновика и повторная проверка.",
      CHANGESET_SEQ_CONFLICT:
        "Черновик изменился. Перечитайте его и проверьте изменения перед новым решением.",
      VALIDATION_FAILED:
        "Контракт окружения отклонил изменения. Исправьте замечания и повторите проверку.",
      CHANGESET_CONTENT_CHANGED:
        "Содержимое или требования согласования изменились. Верните черновик в работу и повторите проверку.",
      APPROVAL_ROLES_MISSING: "Не хватает действующих согласований с нужными ролями.",
      PATH_TAKEN: "Маршрут страницы уже занят. Исправьте маршрут перед публикацией.",
      REVIEW_READ_FAILED:
        "Действие выполнено, но новое состояние не удалось загрузить. Перечитайте черновик.",
      REVIEW_INVALID: "Ответ проверки некорректен. Перечитайте черновик.",
    },
  },
  en: {
    panel: "Checks and publishing",
    targets: "Check environments",
    objects: "Changed objects",
    all: "Actions apply to the entire draft, including other pages and objects.",
    risk: "Risk",
    approvals: "Approvals",
    missing: "Required roles",
    checks: "Checks",
    none: "Checks have not run yet. Submit the draft for review.",
    history: "Review decisions",
    emptyHistory: "No decisions yet",
    stale: "Invalidated or for earlier changes",
    comment: "What needs to change",
    cancel: "Cancel",
    confirm: "Confirm publication",
    question: "Publish the entire draft",
    reopenHint:
      "Reopening invalidates existing approvals. The edited draft will need another review.",
    reopenConfirm: "Reopen and invalidate approvals",
    diagnostics: "Check diagnostics",
    goto: "Select block",
    retry: "Retry the same request",
    uncertain:
      "The action response is unconfirmed. Retry with the original parameters; it will not create another action.",
    published: "Publication confirmed",
    visit: "Open application page",
    snapshot:
      "These are the results of the last submission. The server repeats required checks on approval and publication.",
    states: {
      open: "In progress",
      in_review: "In review",
      approved: "Ready to publish",
      failed: "Checks failed",
      changes_requested: "Changes requested",
      merged: "Published",
      abandoned: "Closed",
    },
    actions: {
      submit: "Submit for review",
      reopen: "Reopen draft",
      approve: "Approve",
      "request-changes": "Request changes",
      publish: "Publish",
    },
    risks: { low: "Low", medium: "Medium", high: "High" },
    status: { passed: "Passed", failed: "Failed", pending: "Pending", running: "Running" },
    stages: { ir: "Page and environment contract", policy: "Permissions and project rules" },
    reasons: {
      FORBIDDEN: "The current account cannot perform this action.",
      CHANGESET_NOT_OWNER: "Only the draft owner can submit or reopen it.",
      APPROVAL_FORBIDDEN_ACTOR:
        "Approval requires a human identity. The local service account cannot approve changes.",
      APPROVAL_SELF: "Authors cannot approve their own changes.",
      CHANGESET_EMPTY: "This draft has no changes yet.",
      CHANGESET_HAS_CONFLICTS: "Resolve the draft conflicts first.",
      REBASE_REQUIRED: "The base versions changed. Rebase the draft and submit it again.",
      CHANGESET_SEQ_CONFLICT:
        "The draft changed. Reload it and check the changes before deciding again.",
      VALIDATION_FAILED:
        "The environment contract rejected the changes. Fix the diagnostics and submit again.",
      CHANGESET_CONTENT_CHANGED:
        "Content or approval requirements changed. Reopen the draft and submit again.",
      APPROVAL_ROLES_MISSING: "Valid approvals from the required roles are missing.",
      PATH_TAKEN: "The page route is already in use. Correct it before publishing.",
      REVIEW_READ_FAILED:
        "The action completed, but the new state could not be loaded. Reload the draft.",
      REVIEW_INVALID: "The review response is invalid. Reload the draft.",
    },
  },
};
export function PublishingPanel({ editor }: { editor: Editor }) {
  const state = useStore(editor.store),
    session = state.session,
    t = copy[state.locale];
  const [choice, setChoice] = useState<"publish" | "reopen" | "request-changes" | null>(null),
    [comment, setComment] = useState("");
  const confirmation = useRef<HTMLDivElement>(null);
  useEffect(() => {
    setChoice(null);
    setComment("");
  }, [session?.changeset.id, session?.changeset.seq, session?.changeset.state]);
  useEffect(() => {
    if (choice) confirmation.current?.focus();
  }, [choice]);
  if (!session) return null;
  const review = session.review,
    busy = state.status !== "ready" || Object.values(state.dirtyFields).some(Boolean);
  const reasons = t.reasons as Record<string, string>;
  const diagnostics = [
    ...review.checks.flatMap((c) => reviewDiagnostics(c.details)),
    ...state.workflowDiagnostics,
  ];
  const actions: WorkflowAction[] =
    session.changeset.state === "open"
      ? ["submit"]
      : session.changeset.state === "in_review"
        ? ["approve", "request-changes", "reopen"]
        : session.changeset.state === "approved"
          ? ["publish", "reopen"]
          : ["failed", "changes_requested"].includes(session.changeset.state)
            ? ["reopen"]
            : [];
  function act(action: WorkflowAction) {
    if (action === "publish" || action === "reopen" || action === "request-changes")
      setChoice(action);
    else void editor.workflow(action);
  }
  const objectList = (
    <ul>
      {review.objects.map((o) => (
        <li key={o.id}>
          <strong>{o.path ?? o.kind}</strong> <code>{o.id}</code>
          {o.deleted && <span> · {state.locale === "ru" ? "Удаление" : "Deletion"}</span>}
        </li>
      ))}
    </ul>
  );
  return (
    <section className="publishing-panel" aria-label={t.panel}>
      <div className="publishing-heading">
        <h2>{t.panel}</h2>
        <strong role="status">
          {t.states[session.changeset.state as keyof typeof t.states] ?? session.changeset.state}
        </strong>
      </div>
      {Object.values(state.dirtyFields).some(Boolean) && (
        <p role="status">
          {state.locale === "ru"
            ? "Сохраните введённые значения перед отправкой черновика."
            : "Save the entered values before submitting the draft."}
        </p>
      )}
      {review.needsAttention && (
        <p role="alert">
          {state.locale === "ru"
            ? "Есть ошибки или устаревшие проверки окружения. Проверьте замечания; при необходимости верните черновик в работу и отправьте повторно."
            : "There are errors or outdated environment checks. Review the diagnostics; reopen and resubmit if needed."}
        </p>
      )}
      <p>
        {t.targets}: <strong>{session.changeset.targets.join(", ")}</strong> · {t.risk}:{" "}
        {review.risk ? t.risks[review.risk] : "—"} · {t.approvals}: {review.approvals} /{" "}
        {review.requiredApprovals}
      </p>
      {!!review.missingRequiredRoles?.length && (
        <p>
          {t.missing}: {review.missingRequiredRoles.join(", ")}
        </p>
      )}
      {!!review.requiredRoles?.length && (
        <p>
          {state.locale === "ru" ? "Роли согласования" : "Approval roles"}:{" "}
          {review.requiredRoles.join(", ")}
        </p>
      )}
      <details>
        <summary>
          {t.objects} ({review.objects.length})
        </summary>
        <p>{t.all}</p>
        {objectList}
      </details>
      <div className="publishing-actions">
        {actions.map((action) => (
          <div key={action}>
            <button disabled={busy || review.actions[action] !== null} onClick={() => act(action)}>
              {t.actions[action]}
            </button>
            {review.actions[action] && (
              <p className="hint">{reasons[review.actions[action]] ?? review.actions[action]}</p>
            )}
          </div>
        ))}
      </div>
      {choice && (
        <div
          ref={confirmation}
          tabIndex={-1}
          className="publication-confirmation"
          onKeyDown={(e) => {
            if (e.key === "Escape" && !busy) setChoice(null);
          }}
        >
          {choice === "publish" ? (
            <>
              <h3>
                {t.question}: «{session.changeset.title}» → {session.config.environment}
              </h3>
              <p>{t.all}</p>
              {objectList}
            </>
          ) : choice === "reopen" ? (
            <p>{t.reopenHint}</p>
          ) : (
            <label>
              {t.comment}
              <textarea
                aria-label={t.comment}
                value={comment}
                maxLength={2000}
                disabled={busy}
                onChange={(e) => setComment(e.target.value)}
              />
            </label>
          )}
          <button
            disabled={busy || (choice === "request-changes" && !comment.trim())}
            onClick={() => {
              void editor.workflow(choice, comment);
              setChoice(null);
            }}
          >
            {choice === "publish"
              ? t.confirm
              : choice === "reopen"
                ? t.reopenConfirm
                : t.actions[choice]}
          </button>
          <button disabled={busy} onClick={() => setChoice(null)}>
            {t.cancel}
          </button>
        </div>
      )}
      {state.workflowRetry && (
        <p role="alert">
          {t.uncertain}{" "}
          <button
            disabled={state.status === "saving" || state.status === "loading"}
            onClick={() => {
              void editor.retryWorkflow();
            }}
          >
            {t.retry}
          </button>
        </p>
      )}
      {state.error && reasons[state.error] && <p role="alert">{reasons[state.error]}</p>}
      {state.publication && (
        <p role="status">
          {t.published}: {state.publication.environment} ·{" "}
          <time dateTime={state.publication.createdAt}>
            {new Date(state.publication.createdAt).toLocaleString(state.locale)}
          </time>{" "}
          · <code>{state.publication.id}</code>{" "}
          <a
            href={new URL(session.record.path!, session.config.appOrigin).href}
            target="_blank"
            rel="noreferrer"
          >
            {t.visit}
          </a>
        </p>
      )}
      <h3>{t.checks}</h3>
      {review.checks.length ? (
        <ul>
          {review.checks.map((c, i) => (
            <li key={i}>
              {(t.stages as Record<string, string>)[c.stage] ?? c.stage}:{" "}
              {(t.status as Record<string, string>)[c.status] ?? c.status}
            </li>
          ))}
        </ul>
      ) : (
        <p>{t.none}</p>
      )}
      {review.checks.length > 0 && <p className="hint">{t.snapshot}</p>}
      {diagnostics.length > 0 && (
        <div aria-label={t.diagnostics}>
          <h3>{t.diagnostics}</h3>
          <ul>
            {diagnostics.map((d, i) => {
              const node = diagnosticNode(d, session.record.id),
                available = node && !!state.document?.nodes[node];
              return (
                <li key={i}>
                  <strong>{d.code}</strong> ·{" "}
                  {d.severity === "error"
                    ? state.locale === "ru"
                      ? "Ошибка"
                      : "Error"
                    : state.locale === "ru"
                      ? "Предупреждение"
                      : "Warning"}{" "}
                  · {d.environment || session.config.environment} · <code>{d.objectId}</code> ·{" "}
                  <code>{d.pointer}</code>
                  {available && (
                    <button
                      onClick={() => {
                        editor.select(node);
                        [...document.querySelectorAll<HTMLElement>('[role="treeitem"]')]
                          .find((el) => el.title === node)
                          ?.focus();
                      }}
                    >
                      {t.goto}
                    </button>
                  )}
                </li>
              );
            })}
          </ul>
        </div>
      )}
      <details>
        <summary>
          {t.history} ({review.history.length})
        </summary>
        {review.history.length ? (
          <ul>
            {review.history.map((h, i) => (
              <li key={i}>
                <code>{h.approverId}</code> ·{" "}
                {h.decision === "approve" ? t.actions.approve : t.actions["request-changes"]} ·{" "}
                <time dateTime={h.createdAt}>
                  {new Date(h.createdAt).toLocaleString(state.locale)}
                </time>
                {h.comment && <p>{h.comment}</p>}
                {!h.valid && <small>{t.stale}</small>}
              </li>
            ))}
          </ul>
        ) : (
          <p>{t.emptyHistory}</p>
        )}
      </details>
    </section>
  );
}
