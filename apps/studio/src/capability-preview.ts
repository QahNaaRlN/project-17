import { StudioError } from "./types.js";

/** Mapping states of a request to a PR preview environment (STU-319). */
export const previewStates = [
  "ready",
  "ready_last_known",
  "commit_behind",
  "commit_unverified",
  "key_missing",
  "contract_incompatible",
  "not_registered",
  "no_github_link",
  "environment_expired",
] as const;
export type PreviewState = (typeof previewStates)[number];
export type PreviewItem = {
  environment: string;
  pullNumber: number;
  state: PreviewState;
  expiresAt: string | null;
  archivedAt: string | null;
  archiveReason: string | null;
  keyAvailable: boolean;
  registration: {
    seq: number;
    commit: string;
    registeredAt: string;
    appUrl: string | null;
    manifestHash: string;
  } | null;
  observation: { headSha: string; observedAt: string; freshUntil: string } | null;
  actionArgs?: Record<string, unknown>;
  args: { status: "set" | "stale" | "missing" | "not_required"; revision: number };
};
export type PreviewList = { items: PreviewItem[]; canOpen: boolean; canSetArgs: boolean };
export type PreviewArgsAttempt = {
  payload: {
    requestId: string;
    expectedRequestRevision: number;
    environment: string;
    manifestHash: string;
    handlers: { action: string; args?: Record<string, unknown> }[];
    expectedArgsRevision: number;
  };
  key: string;
  reason: string;
  actor?: string;
};

const commit = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;
const environment = /^preview\/pr-[1-9][0-9]{0,18}$/;
const hash = /^sha256:[0-9a-f]{64}$/;
const date = (v: unknown) => typeof v === "string" && Number.isFinite(Date.parse(v));

/** Whether the URL belongs to an allowed exact origin (STU-328). */
export function previewOrigin(url: string | null | undefined, allowed: string[] | undefined) {
  if (!url || !allowed?.length) return null;
  try {
    const u = new URL(url);
    if (u.username || u.password || u.search || u.hash) return null;
    return allowed.includes(u.origin) ? u.origin : null;
  } catch {
    return null;
  }
}

export function previewList(v: PreviewList): PreviewList {
  if (
    !v ||
    !Array.isArray(v.items) ||
    v.items.length > 10 ||
    typeof v.canOpen !== "boolean" ||
    typeof v.canSetArgs !== "boolean"
  )
    throw new StudioError("RESPONSE_INVALID");
  for (const i of v.items)
    if (
      !i ||
      !environment.test(i.environment) ||
      !Number.isSafeInteger(i.pullNumber) ||
      !previewStates.includes(i.state) ||
      typeof i.keyAvailable !== "boolean" ||
      !i.args ||
      !["set", "stale", "missing", "not_required"].includes(i.args.status) ||
      !Number.isSafeInteger(i.args.revision) ||
      (i.registration &&
        (!commit.test(i.registration.commit) ||
          !Number.isSafeInteger(i.registration.seq) ||
          i.registration.seq < 1 ||
          !date(i.registration.registeredAt) ||
          !hash.test(i.registration.manifestHash) ||
          (i.registration.appUrl !== null && typeof i.registration.appUrl !== "string"))) ||
      (i.observation &&
        (!commit.test(i.observation.headSha) ||
          !date(i.observation.observedAt) ||
          !date(i.observation.freshUntil)))
    )
      throw new StudioError("RESPONSE_INVALID");
  return v;
}

/** Human age of an observation, e.g. "2 ч" / "2 h" (decision §11.2). */
export function previewAge(at: string, now: number, ru: boolean) {
  const minutes = Math.max(0, Math.floor((now - Date.parse(at)) / 60000));
  if (minutes < 60) return ru ? `${minutes} мин` : `${minutes} min`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return ru ? `${hours} ч` : `${hours} h`;
  const days = Math.floor(hours / 24);
  return ru ? `${days} дн` : `${days} d`;
}

export function previewLabel(state: string, ru: boolean): string {
  const labels: Record<string, [string, string]> = {
    ready: ["Готов к просмотру", "Ready to view"],
    ready_last_known: [
      "Готов по последнему известному наблюдению",
      "Ready by the last-known observation",
    ],
    commit_behind: [
      "Устарел: GitHub видит более новый коммит PR",
      "Outdated: GitHub sees a newer PR commit",
    ],
    commit_unverified: [
      "Сборка новее наблюдения GitHub — обновите GitHub",
      "Build is newer than the GitHub observation — refresh GitHub",
    ],
    key_missing: ["В сборке нет запрошенного действия", "The build has no requested action"],
    contract_incompatible: [
      "Контракт действия в сборке не совпадает с запросом",
      "The build's action contract does not match the request",
    ],
    not_registered: ["Сборка ещё не зарегистрирована", "No build registered yet"],
    no_github_link: ["Нет проверенной связи с PR", "No verified PR link"],
    environment_expired: ["Окружение архивировано", "Environment archived"],
  };
  const l = labels[state];
  return l ? (ru ? l[0] : l[1]) : ru ? "Недоступно" : "Unavailable";
}

export function previewArchiveLabel(reason: string | null, ru: boolean) {
  const labels: Record<string, [string, string]> = {
    deleted: ["удалено CI или администратором", "deleted by CI or an administrator"],
    ttl: ["14 дней без регистраций", "14 days without registrations"],
    pr_closed: ["PR закрыт", "PR closed"],
    pr_merged: ["PR слит", "PR merged"],
  };
  const l = reason ? labels[reason] : undefined;
  return l ? (ru ? l[0] : l[1]) : "";
}

export function previewMessage(code: string, ru: boolean): string {
  if (code === "PREVIEW_READ_ONLY")
    return ru
      ? "Режим предпросмотра PR: только просмотр. Вернитесь в рабочее окружение для правки."
      : "PR preview mode is view-only. Return to the working environment to edit.";
  if (code === "PREVIEW_OUTDATED" || code === "PREVIEW_ARGS_STALE")
    return ru
      ? "Сборка preview изменилась. Перезапустите предпросмотр."
      : "The preview build changed. Restart the preview.";
  if (code === "PREVIEW_ARGS_INVALID")
    return ru
      ? "Аргументы не проходят проверку контракта сборки."
      : "Arguments fail the build's contract check.";
  if (code === "PREVIEW_NOT_READY")
    return ru ? "Предпросмотр сейчас недоступен." : "The preview is not available now.";
  if (code.includes("CONFLICT"))
    return ru
      ? "Контекст изменился. Обновите сведения и повторите действие."
      : "Context changed. Reload and try again.";
  if (code.includes("FORBIDDEN")) return ru ? "Недостаточно прав." : "Insufficient permissions.";
  return ru ? "Сведения предпросмотра недоступны." : "Preview information is unavailable.";
}
