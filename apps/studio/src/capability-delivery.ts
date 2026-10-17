import { StudioError } from "./types.js";

export const deliveryCommands = [
  "set-capability-delivery-target",
  "send-capability-context",
  "retry-capability-delivery",
  "cancel-capability-delivery",
] as const;
export type DeliveryCommand = (typeof deliveryCommands)[number];
export interface DeliveryAttempt {
  name: DeliveryCommand;
  payload: Record<string, unknown>;
  key: string;
  reason: string;
  actor?: string;
}
export interface DeliveryEndpoint {
  endpointId: string;
  url: string;
  fingerprint: string;
  ready: boolean;
}
export interface DeliveryTarget {
  configured: boolean;
  revision: number;
  ready: boolean;
  id?: string;
  environment?: string;
  endpointId?: string;
  url?: string;
  fingerprint?: string;
  enabled?: boolean;
}
export interface DeliveryPreview {
  requestId: string;
  requestRevision: number;
  packRevision: number;
  packHash: string;
  seq: number;
  manifestHash: string;
  contextPack: Record<string, unknown>;
  target: DeliveryTarget;
  canSend: boolean;
}
export interface Delivery {
  id: string;
  requestId: string;
  requestRevision: number;
  packRevision: number;
  packHash: string;
  targetId: string;
  targetRevision: number;
  targetFingerprint: string;
  destinationUrl: string;
  bodyHash: string;
  initiatedBy: string;
  createdAt: string;
  revision: number;
  state: "queued" | "sending" | "retry_wait" | "delivered" | "failed" | "cancelled";
  cycle: number;
  totalAttempts: number;
  attemptsInCycle: number;
  nextAttemptAt: string;
  deliveredAt?: string | null;
  cancelledAt?: string | null;
  lastError: string;
  lastHttpStatus?: number | null;
  retryableExhaustion: boolean;
  canSend?: boolean;
  canRetry?: boolean;
  canCancel?: boolean;
  retryReason?: string;
  attempts?: {
    id: string;
    ordinal: number;
    cycle: number;
    startedAt: string;
    finishedAt?: string | null;
    httpStatus?: number | null;
    outcome: string;
    retryable: boolean;
    receiptDeliveryId?: string | null;
    receiptBodyHash?: string | null;
    nextAttemptAt?: string | null;
  }[];
  audit?: {
    id: number;
    event: string;
    source: string;
    actorId?: string | null;
    initiatedBy?: string | null;
    reason: string;
    revision: number;
    createdAt: string;
  }[];
}
export interface DeliveryResult {
  delivery: Delivery;
  deduplicated: boolean;
}
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i;
const hash = /^sha256:[a-f0-9]{64}$/;
const integer = (value: number, min = 1) => Number.isSafeInteger(value) && value >= min;
const date = (value: string) => typeof value === "string" && Number.isFinite(Date.parse(value));
export function deliveryTarget(value: DeliveryTarget, uncertain = false): DeliveryTarget {
  if (
    !value ||
    typeof value.configured !== "boolean" ||
    typeof value.ready !== "boolean" ||
    !integer(value.revision, 0) ||
    (value.configured &&
      (!uuid.test(value.id ?? "") ||
        !value.endpointId ||
        typeof value.url !== "string" ||
        !hash.test(value.fingerprint ?? "") ||
        typeof value.enabled !== "boolean" ||
        value.revision < 1))
  )
    throw new StudioError("RESPONSE_INVALID", uncertain);
  return value;
}
export function delivery(value: Delivery, uncertain = false): Delivery {
  if (
    !value ||
    !uuid.test(value.id) ||
    !uuid.test(value.requestId) ||
    !uuid.test(value.targetId) ||
    !uuid.test(value.initiatedBy) ||
    !hash.test(value.packHash) ||
    !hash.test(value.bodyHash) ||
    !hash.test(value.targetFingerprint) ||
    !integer(value.revision) ||
    !integer(value.requestRevision) ||
    !integer(value.packRevision) ||
    !integer(value.targetRevision) ||
    !integer(value.cycle) ||
    value.cycle > 3 ||
    !integer(value.totalAttempts, 0) ||
    value.totalAttempts > 18 ||
    !integer(value.attemptsInCycle, 0) ||
    value.attemptsInCycle > 6 ||
    !["queued", "sending", "retry_wait", "delivered", "failed", "cancelled"].includes(
      value.state,
    ) ||
    !date(value.createdAt) ||
    !date(value.nextAttemptAt) ||
    typeof value.destinationUrl !== "string" ||
    typeof value.lastError !== "string" ||
    typeof value.retryableExhaustion !== "boolean"
  )
    throw new StudioError("RESPONSE_INVALID", uncertain);
  return value;
}
export function deliveryPreview(value: DeliveryPreview): DeliveryPreview {
  if (
    !value ||
    !uuid.test(value.requestId) ||
    !integer(value.requestRevision) ||
    !integer(value.packRevision) ||
    !integer(value.seq, 0) ||
    !hash.test(value.packHash) ||
    !hash.test(value.manifestHash) ||
    !value.contextPack ||
    typeof value.contextPack !== "object" ||
    Array.isArray(value.contextPack) ||
    value.canSend !== true
  )
    throw new StudioError("RESPONSE_INVALID");
  deliveryTarget(value.target);
  if (!value.target.configured || !value.target.ready) throw new StudioError("RESPONSE_INVALID");
  return value;
}
export function sendPayload(p: DeliveryPreview): Record<string, unknown> {
  return {
    requestId: p.requestId,
    expectedRequestRevision: p.requestRevision,
    expectedPackRevision: p.packRevision,
    expectedPackHash: p.packHash,
    expectedSeq: p.seq,
    expectedManifestHash: p.manifestHash,
    expectedTargetRevision: p.target.revision,
    expectedTargetFingerprint: p.target.fingerprint,
    confirmExternalShare: true,
  };
}
export const deliveryActive = (d: Delivery) =>
  ["queued", "sending", "retry_wait"].includes(d.state);
export function deliveryMessage(code: string, ru: boolean): string {
  const key = code.replace(/^CAPABILITY_DELIVERY_/, "");
  const messages: Record<string, [string, string]> = {
    REVISION_CONFLICT: [
      "Данные изменились. Обновите сведения и подтвердите передачу заново.",
      "Details changed. Reload and confirm sharing again.",
    ],
    CONTEXT_STALE: [
      "Контекст устарел. Сначала проверьте и обновите context pack.",
      "Context is stale. Review and refresh the context pack first.",
    ],
    ANCHOR_CHANGED: [
      "Страница, заглушка или manifest изменились. Нужна новая проверка контекста.",
      "The page, placeholder or manifest changed. Review the context again.",
    ],
    TARGET_CHANGED: [
      "Получатель изменился. Проверьте адрес и подтвердите передачу заново.",
      "The recipient changed. Review the address and confirm sharing again.",
    ],
    TARGET_DISABLED: ["Получатель отключён.", "The recipient is disabled."],
    KEY_UNAVAILABLE: [
      "Ключ подписи недоступен. Обратитесь к администратору.",
      "The signing key is unavailable. Contact an administrator.",
    ],
    UNAVAILABLE: [
      "Доставка не настроена или получатель недоступен. Пакет можно скачать вручную.",
      "Delivery is not configured or the recipient is unavailable. You can download the pack manually.",
    ],
    FORBIDDEN: [
      "Недостаточно прав для передачи. Ввод сохранён.",
      "You lack permission to share. Your input is retained.",
    ],
    AUTH_REVOKED: [
      "Права отправителя отозваны. Передача остановлена.",
      "The sender's permission was revoked. Delivery stopped.",
    ],
    REQUEST_TERMINAL: [
      "Запрос завершён. Передача остановлена.",
      "The request is closed. Delivery stopped.",
    ],
    STATE_INVALID: [
      "Действие недоступно в текущем состоянии передачи.",
      "This action is unavailable in the current delivery state.",
    ],
    RETRY_EXHAUSTED: [
      "Исчерпаны три цикла передачи. Повторы больше недоступны.",
      "All three delivery cycles are exhausted. No further retries are available.",
    ],
    ADDRESS_BLOCKED: [
      "Адрес получателя отклонён сервером.",
      "The server rejected the recipient address.",
    ],
    ACK_INVALID: [
      "Получатель не подтвердил сохранение пакета.",
      "The recipient did not confirm durable storage.",
    ],
    NETWORK_UNCERTAIN: [
      "Подтверждение не получено. Получатель мог сохранить пакет; повтор использует тот же идентификатор.",
      "No confirmation received. The recipient may have stored the pack; retries use the same identifier.",
    ],
    HTTP_RETRYABLE: [
      "Получатель временно недоступен. Повтор запланирован.",
      "The recipient is temporarily unavailable. A retry is scheduled.",
    ],
    HTTP_PERMANENT: ["Получатель отклонил передачу.", "The recipient rejected the delivery."],
    LIMIT_EXCEEDED: ["Превышен размер пакета.", "The pack exceeds the size limit."],
    CANCELLED_BY_HUMAN: [
      "Дальнейшие попытки отменены. Уже отправленный пакет мог быть сохранён.",
      "Further attempts were cancelled. An in-flight pack may already have been stored.",
    ],
  };
  return (
    messages[key]?.[ru ? 0 : 1] ??
    (ru
      ? "Не удалось выполнить действие. Ввод сохранён: "
      : "Could not complete the action. Your input is retained: ") + code
  );
}
export function deliveryStatus(state: Delivery["state"], ru: boolean): string {
  const values: Record<Delivery["state"], [string, string]> = {
    queued: ["В очереди", "Queued"],
    sending: ["Отправляется", "Sending"],
    retry_wait: ["Ожидает повтора", "Waiting for retry"],
    delivered: ["Пакет сохранён получателем", "Stored by recipient"],
    failed: ["Передача не завершена", "Delivery failed"],
    cancelled: ["Попытки остановлены", "Attempts stopped"],
  };
  return values[state][ru ? 0 : 1];
}
