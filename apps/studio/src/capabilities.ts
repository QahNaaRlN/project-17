import type { Fields } from "@cms/manifest";
import { StudioError } from "./types.js";

export const capabilityCommands = [
  "create-capability-request",
  "update-capability-request",
  "submit-capability-request",
  "refresh-capability-context",
  "resolve-capability-request",
] as const;
export type CapabilityCommand = (typeof capabilityCommands)[number];
export interface CapabilityRequest {
  id: string;
  kind: "action" | "component" | "dataSource" | "token";
  environment: string;
  key: string;
  title: string;
  description: string;
  proposedContract: {
    args?: Fields;
    props?: Fields;
    params?: Fields;
    result?: Fields[string];
    paginated?: boolean;
    category?: string;
    value?: string;
    container?: boolean;
    slots?: Record<string, { max?: number }>;
  };
  /** Component requests: where the placeholder was inserted (STU-359). */
  placement?: {
    parentId?: string;
    slot?: string;
    index?: number;
    dataSourceKey?: string;
    nodeId?: string;
    property?: string;
    breakpoint?: string;
  };
  initialProps?: Record<string, unknown>;
  acceptance: string[];
  status: string;
  revision: number;
  changesetId: string;
  documentId: string;
  nodeId: string;
  event: string;
  ownerId: string;
  externalRefs: string[];
  transitions?: string[];
  stale?: boolean;
  anchorChanged?: boolean;
  fulfillmentReverted?: boolean;
  seq?: number;
  packs?: { revision: number; hash: string; changesetSeq: number }[];
  audit?: { revision: number; event: string; reason: string; createdAt: string }[];
}
export interface CapabilityResult {
  request: CapabilityRequest;
  change?: { changeset: { seq: number } };
  pack?: { hash: string };
}
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i;
export function capabilityURL(value: string): boolean {
  try {
    const url = new URL(value);
    return (
      value.length <= 2048 &&
      url.protocol === "https:" &&
      !!url.hostname &&
      !url.username &&
      !url.password &&
      !/[\r\n\t]/.test(value)
    );
  } catch {
    return false;
  }
}
export function capability(value: CapabilityRequest, uncertain = false): CapabilityRequest {
  if (
    !value ||
    !uuid.test(value.id) ||
    !["action", "component", "dataSource", "token"].includes(value.kind) ||
    typeof value.title !== "string" ||
    typeof value.description !== "string" ||
    typeof value.key !== "string" ||
    !Number.isSafeInteger(value.revision) ||
    value.revision < 1 ||
    !uuid.test(value.changesetId) ||
    !uuid.test(value.documentId) ||
    typeof value.nodeId !== "string" ||
    typeof value.event !== "string" ||
    ![
      "draft",
      "submitted",
      "accepted",
      "in_progress",
      "pr_open",
      "fulfilled",
      "rejected",
      "cancelled",
    ].includes(value.status) ||
    !(value.kind === "component"
      ? value.proposedContract?.props
      : value.kind === "dataSource"
        ? value.proposedContract?.result
        : value.kind === "token"
          ? value.proposedContract?.category
          : value.proposedContract?.args) ||
    !Array.isArray(value.acceptance) ||
    value.acceptance.some((x) => typeof x !== "string") ||
    !Array.isArray(value.externalRefs) ||
    value.externalRefs.some((x) => typeof x !== "string" || !capabilityURL(x))
  )
    throw new StudioError("RESPONSE_INVALID", uncertain);
  return value;
}
export interface CapabilityAttempt {
  name: CapabilityCommand;
  payload: Record<string, unknown>;
  key: string;
  reason: string;
}

export function capabilityMessage(code: string, ru: boolean): string {
  const messages: Record<string, [string, string]> = {
    CAPABILITY_KEY_TAKEN: [
      "Для этого ключа уже есть открытый запрос. Найдите его в очереди.",
      "An open request already uses this key. Find it in the request queue.",
    ],
    CAPABILITY_ALREADY_AVAILABLE: [
      "Действие уже доступно. Обновите каталог действий.",
      "The action is already available. Refresh the action catalogue.",
    ],
    CAPABILITY_REVISION_CONFLICT: [
      "Запрос изменился. Обновите карточку и проверьте изменения.",
      "The request changed. Reload its card and review the changes.",
    ],
    CAPABILITY_CONTEXT_STALE: [
      "Страница или manifest изменились. Обновите черновик и заново проверьте контекст.",
      "The page or manifest changed. Reload the draft and review the context again.",
    ],
    CAPABILITY_INDEX_UNAVAILABLE: [
      "В manifest нет подтверждённого индекса кода. Нужна новая сборка и загрузка через CLI.",
      "The manifest has no verified code index. Build and upload a new manifest with the CLI.",
    ],
    CAPABILITY_CONTRACT_MISMATCH: [
      "Контракт действия не совместим с запросом.",
      "The action contract does not match the request.",
    ],
    CAPABILITY_ANCHOR_CHANGED: [
      "Узел, событие или заглушка изменились. Проверьте поведение страницы.",
      "The node, event or placeholder changed. Review page behavior.",
    ],
    CAPABILITY_STATE_INVALID: [
      "Действие недоступно в текущем статусе запроса или черновика.",
      "The action is unavailable in the current request or draft state.",
    ],
    CONTEXT_PACK_LIMIT_EXCEEDED: [
      "Контекст превышает размер пакета. Сократите поддерево или контракты.",
      "Context exceeds the size limit. Reduce the subtree or contracts.",
    ],
    FORBIDDEN: [
      "Права изменились. Ввод сохранён; форму можно закрыть без записи.",
      "Permissions changed. Your input is retained; you can close without saving.",
    ],
    CAPABILITY_CODE_REQUIRED: [
      "Запрос доступен только в CODE-зоне.",
      "Requests are available only in a CODE zone.",
    ],
  };
  return (
    messages[code]?.[ru ? 0 : 1] ??
    (ru
      ? "Не удалось выполнить действие. Ввод сохранён: "
      : "Could not complete the action. Your input is retained: ") + code
  );
}
