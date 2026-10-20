import { StudioError } from "./types.js";

/** Commands of requiredBy groups and automatic fulfillment (STU-336…346). */
export const fulfillmentCommands = [
  "attach-capability-request",
  "detach-capability-request",
  "set-capability-fulfillment-args",
  "retry-capability-fulfillment",
  "check-capability-fulfillment",
] as const;
export type FulfillmentCommand = (typeof fulfillmentCommands)[number];
export type FulfillmentAttempt = {
  name: FulfillmentCommand;
  payload: Record<string, unknown>;
  key: string;
  reason: string;
  actor?: string;
};
export const anchorStates = [
  "active",
  "queued",
  "replaced",
  "awaiting_args",
  "invalid_args",
  "invalid_props",
  "anchor_changed",
  "changeset_in_review",
  "changeset_closed",
  "reverted",
  "detached",
  "failed",
] as const;
export type AnchorState = (typeof anchorStates)[number];
export type FulfillmentAnchor = {
  id: string;
  state: AnchorState;
  revision: number;
  original: boolean;
  own: boolean;
  changesetId?: string;
  documentId?: string;
  nodeId?: string;
  event?: string;
  argsSet?: boolean;
  lastError?: string;
};
export type FulfillmentView = {
  requestId: string;
  status: string;
  state: "waiting" | "fulfilled";
  reason: string;
  mode: "manual" | "automatic" | null;
  commit: string | null;
  fulfilledAt: string | null;
  basis: {
    pullNumber?: number;
    integrationSha?: string;
    commit?: string;
    observedAt?: string;
    comparison?: { status: string; aheadBy: number };
  } | null;
  anchorCount: number;
  anchors: FulfillmentAnchor[];
  canCheck: boolean;
};
export type OpenRequest = {
  id: string;
  key: string;
  title: string;
  status: string;
  revision: number;
};

const id = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i;
const sha = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;

export function fulfillmentView(v: FulfillmentView): FulfillmentView {
  if (
    !v ||
    !id.test(v.requestId) ||
    !["waiting", "fulfilled"].includes(v.state) ||
    typeof v.reason !== "string" ||
    !Number.isSafeInteger(v.anchorCount) ||
    !Array.isArray(v.anchors) ||
    v.anchors.length > 50 ||
    typeof v.canCheck !== "boolean" ||
    (v.commit !== null && !sha.test(v.commit)) ||
    (v.mode !== null && !["manual", "automatic"].includes(v.mode))
  )
    throw new StudioError("RESPONSE_INVALID");
  for (const a of v.anchors)
    if (
      !a ||
      !id.test(a.id) ||
      !anchorStates.includes(a.state) ||
      !Number.isSafeInteger(a.revision) ||
      typeof a.own !== "boolean" ||
      (a.own && (!id.test(a.changesetId ?? "") || !id.test(a.documentId ?? "")))
    )
      throw new StudioError("RESPONSE_INVALID");
  return v;
}

export function openRequest(v: OpenRequest): OpenRequest {
  if (!v || !id.test(v.id) || typeof v.key !== "string" || !Number.isSafeInteger(v.revision))
    throw new StudioError("RESPONSE_INVALID");
  return v;
}

const reasons: Record<string, [string, string]> = {
  key_missing: ["В активном manifest нет действия", "The active manifest has no such action"],
  contract_incompatible: [
    "Контракт действия не совпадает с запросом",
    "The action contract does not match the request",
  ],
  no_github_link: [
    "Нет проверенной связи с PR — доступно только ручное применение",
    "No verified PR link — only manual apply is available",
  ],
  pr_not_merged: ["PR ещё не слит", "The PR is not merged yet"],
  observation_stale: [
    "Сведения GitHub устарели — запрошено обновление",
    "GitHub facts are stale — a refresh was requested",
  ],
  ci_not_successful: [
    "Проверки на коммите слияния не успешны",
    "Checks on the merge commit are not successful",
  ],
  no_code_index: [
    "У активного manifest нет индекса кода с коммитом",
    "The active manifest has no code index commit",
  ],
  commit_unrelated: [
    "Сборка не содержит коммит слияния PR",
    "The build does not contain the PR merge commit",
  ],
  comparison_unavailable: [
    "Сравнение коммитов в GitHub недоступно",
    "The GitHub commit comparison is unavailable",
  ],
};
export function fulfillmentReason(reason: string, ru: boolean) {
  if (!reason) return ru ? "Ожидает активации manifest" : "Waiting for a manifest activation";
  const r = reasons[reason];
  return r ? (ru ? r[0] : r[1]) : ru ? "Ожидает" : "Waiting";
}

const states: Record<string, [string, string]> = {
  active: ["Ждёт выполнения запроса", "Waiting for fulfillment"],
  queued: ["Замена в очереди", "Replacement queued"],
  replaced: ["Заменено системой", "Replaced by the system"],
  awaiting_args: ["Нужны аргументы выполнения", "Fulfillment arguments needed"],
  invalid_args: ["Аргументы не прошли проверку", "Arguments failed validation"],
  invalid_props: [
    "Свойства не подходят к компоненту — исправьте и повторите",
    "Props do not fit the component — fix them and retry",
  ],
  anchor_changed: ["Узел изменён — заглушки нет", "The node changed — no placeholder"],
  changeset_in_review: [
    "Черновик на проверке — замена после возврата в работу",
    "Draft in review — replaced once back in work",
  ],
  changeset_closed: ["Черновик закрыт", "Draft closed"],
  reverted: ["Замена отменена — можно повторить", "Replacement undone — can be repeated"],
  detached: ["Отсоединено", "Detached"],
  failed: ["Замена не выполнена", "Replacement failed"],
};
export function anchorLabel(state: string, ru: boolean) {
  const s = states[state];
  return s ? (ru ? s[0] : s[1]) : ru ? "Недоступно" : "Unavailable";
}

export function fulfillmentMessage(code: string, ru: boolean) {
  if (code === "CAPABILITY_ANCHOR_LIMIT")
    return ru ? "Достигнут лимит привязок запроса." : "The request anchor limit is reached.";
  if (code === "CAPABILITY_FULFILLMENT_ARGS_INVALID")
    return ru ? "Аргументы не подходят к действию." : "The arguments do not fit the action.";
  if (code.includes("CONFLICT") || code.includes("STALE"))
    return ru
      ? "Контекст изменился. Обновите сведения и повторите действие."
      : "Context changed. Reload and try again.";
  if (code.includes("STATE_INVALID"))
    return ru ? "Действие сейчас недоступно." : "This action is not available now.";
  if (code.includes("FORBIDDEN") || code === "NOT_FOUND")
    return ru ? "Недостаточно прав." : "Insufficient permissions.";
  return ru ? "Сведения о выполнении недоступны." : "Fulfillment information is unavailable.";
}
