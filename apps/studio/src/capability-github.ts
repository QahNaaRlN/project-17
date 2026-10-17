import { StudioError } from "./types.js";
export const githubCommands = [
  "set-capability-github-config",
  "link-capability-github-pr",
  "unlink-capability-github-pr",
  "refresh-capability-github-pr",
] as const;
export type GithubCommand = (typeof githubCommands)[number];
export type GithubAttempt = {
  name: GithubCommand;
  payload: Record<string, unknown>;
  key: string;
  reason: string;
  actor?: string;
};
export type GithubRule = {
  source: "check_run" | "commit_status";
  name: string;
  target: "head" | "integration";
  appId?: string;
  creatorId?: string;
};
export type GithubConfig = {
  configured: boolean;
  revision: number;
  ready: boolean;
  enabled?: boolean;
  configId?: string;
  owner?: string;
  name?: string;
  baseBranch?: string;
  requiredChecks?: GithubRule[];
  choices?: {
    configId: string;
    owner: string;
    name: string;
    baseBranch: string;
    requiredChecks: GithubRule[];
  }[];
};
export type GithubLink = {
  id: string;
  requestId: string;
  revision: number;
  generation: number;
  active: boolean;
  pullNumber: number;
  configRevision: number;
  syncId: string;
  syncState: string;
  lastError: string;
  nextAllowedAt: string;
  nextAttemptAt: string;
  pollsLeft: number;
  attempts: number;
};
export type GithubObservation = {
  id: string;
  generation: number;
  configRevision: number;
  observedAt: string;
  freshUntil: string;
  digest: string;
  facts: {
    repositoryId: string;
    prId: string;
    number: number;
    state: "open" | "closed" | "merged";
    draft: boolean;
    headSha: string;
    baseSha: string;
    integrationSha?: string;
    integrationType: string;
    ci: string;
    results: {
      source: string;
      name: string;
      target: string;
      exactSha?: string;
      outcome: string;
      status?: string;
      conclusion?: string;
    }[];
  };
};
export type GithubView = {
  link: GithubLink | null;
  current: GithubObservation | null;
  lastKnown: GithubObservation | null;
  syncState: string;
  canManage: boolean;
  configRevision?: number;
  repository?: { owner: string; name: string; baseBranch: string };
  url?: string;
};
const sha = /^[0-9a-f]{40}$/;
const id = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i;
const date = (value: unknown) => typeof value === "string" && Number.isFinite(Date.parse(value));
const positive = (value: unknown) => typeof value === "string" && /^[1-9][0-9]{0,19}$/.test(value);
export function githubConfig(v: GithubConfig, uncertain = false): GithubConfig {
  if (
    !v ||
    typeof v.configured !== "boolean" ||
    typeof v.ready !== "boolean" ||
    !Number.isSafeInteger(v.revision) ||
    v.revision < 0 ||
    (v.configured && (!v.configId || typeof v.enabled !== "boolean"))
  )
    throw new StudioError("RESPONSE_INVALID", uncertain);
  if (
    v.choices !== undefined &&
    (!Array.isArray(v.choices) ||
      v.choices.some(
        (c) =>
          !c ||
          typeof c.configId !== "string" ||
          typeof c.owner !== "string" ||
          typeof c.name !== "string" ||
          typeof c.baseBranch !== "string" ||
          !Array.isArray(c.requiredChecks),
      ))
  )
    throw new StudioError("RESPONSE_INVALID", uncertain);
  return v;
}
export function githubView(v: GithubView): GithubView {
  if (
    !v ||
    typeof v.canManage !== "boolean" ||
    typeof v.syncState !== "string" ||
    !Object.hasOwn(v, "link") ||
    !Object.hasOwn(v, "current") ||
    !Object.hasOwn(v, "lastKnown")
  )
    throw new StudioError("RESPONSE_INVALID");
  if (
    v.link &&
    (!Number.isSafeInteger(v.link.revision) ||
      v.link.revision < 1 ||
      !Number.isSafeInteger(v.link.pullNumber) ||
      v.link.pullNumber < 1 ||
      !id.test(v.link.id) ||
      !id.test(v.link.requestId) ||
      typeof v.link.active !== "boolean" ||
      !date(v.link.nextAllowedAt) ||
      !date(v.link.nextAttemptAt) ||
      !["queued", "sending", "retry_wait", "idle", "stopped"].includes(v.link.syncState))
  )
    throw new StudioError("RESPONSE_INVALID");
  for (const o of [v.current, v.lastKnown])
    if (
      o &&
      (!o.facts ||
        !["test_merge", "merged_commit", "unavailable"].includes(o.facts.integrationType) ||
        !Array.isArray(o.facts.results) ||
        !Number.isFinite(Date.parse(o.observedAt)) ||
        !Number.isFinite(Date.parse(o.freshUntil)) ||
        !id.test(o.id) ||
        !positive(o.facts.repositoryId) ||
        !positive(o.facts.prId) ||
        !sha.test(o.facts.headSha) ||
        !sha.test(o.facts.baseSha) ||
        (o.facts.integrationSha !== undefined && !sha.test(o.facts.integrationSha)) ||
        !["open", "closed", "merged"].includes(o.facts.state) ||
        !["success", "failure", "cancelled", "pending", "unknown"].includes(o.facts.ci) ||
        typeof o.facts.draft !== "boolean" ||
        o.facts.results.length > 32 ||
        o.facts.results.some(
          (r) =>
            !r ||
            typeof r.name !== "string" ||
            r.name.length > 200 ||
            !["check_run", "commit_status"].includes(r.source) ||
            !["head", "integration"].includes(r.target) ||
            !["success", "failure", "cancelled", "pending", "unknown"].includes(r.outcome) ||
            (r.exactSha !== undefined && !sha.test(r.exactSha)),
        ))
    )
      throw new StudioError("RESPONSE_INVALID");
  if (
    v.url &&
    !/^https:\/\/github\.com\/[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+\/pull\/[1-9][0-9]*$/.test(v.url)
  )
    throw new StudioError("RESPONSE_INVALID");
  return v;
}
export function githubMessage(code: string, ru: boolean): string {
  if (
    code.includes("AUTH_REVOKED") ||
    code.includes("PERMISSION_DENIED") ||
    code.includes("CREDENTIAL_UNAVAILABLE")
  )
    return ru
      ? "Доступ GitHub отозван или недоступен. Обратитесь к администратору."
      : "GitHub access was revoked or is unavailable. Contact the administrator.";
  if (code.includes("REPOSITORY_MISMATCH"))
    return ru
      ? "Репозиторий или базовая ветка не соответствуют настройке."
      : "Repository or base branch does not match the configuration.";
  if (code.includes("NOT_FOUND_OR_INACCESSIBLE"))
    return ru
      ? "PR не найден или недоступен; это не подтверждение удаления."
      : "PR not found or inaccessible; this does not confirm deletion.";
  if (code.includes("INCOMPLETE") || code.includes("INVALID_RESPONSE"))
    return ru
      ? "Данные GitHub неполны. Успех CI не подтверждён."
      : "GitHub data is incomplete. CI success is not verified.";
  if (code.includes("EXHAUSTED"))
    return ru
      ? "Автоматические обновления завершены. Запустите новую проверку явно."
      : "Automatic updates have ended. Start a new refresh explicitly.";
  if (code === "RESPONSE_INVALID")
    return ru ? "Некорректный ответ сервера." : "Invalid server response.";
  if (code.includes("CONFLICT") || code.includes("CHANGED"))
    return ru
      ? "Контекст изменился. Обновите сведения и повторите действие."
      : "Context changed. Reload and try again.";
  if (code.includes("COOLDOWN") || code.includes("RATE_LIMITED"))
    return ru ? "Лимит обновлений. Повторите позже." : "Refresh limit reached. Try later.";
  if (code.includes("FORBIDDEN")) return ru ? "Недостаточно прав." : "Insufficient permissions.";
  return ru
    ? "Сведения GitHub недоступны. Проверьте подключение или повторите позже."
    : "GitHub information is unavailable. Check the connection or try later.";
}
export function githubLabel(value: string, ru: boolean): string {
  const labels: Record<string, string> = {
    success: "Успешно",
    failure: "Ошибка проверки",
    pending: "Ожидается",
    cancelled: "Отменено",
    unknown: "Неизвестно",
    open: "Открыт",
    closed: "Закрыт",
    merged: "Слит",
    queued: "В очереди",
    sending: "Обновление",
    retry_wait: "Ожидание повтора",
    idle: "Обновлено",
    stopped: "Остановлено",
    stale: "Устарело",
    disabled: "Не настроено",
    unlinked: "PR не привязан",
    test_merge: "Пробное слияние",
    merged_commit: "Коммит после слияния",
    unavailable: "Недоступно",
    head: "Коммит ветки",
    integration: "Коммит интеграции",
  };
  return ru ? (labels[value] ?? "Недоступно") : value.replaceAll("_", " ");
}
