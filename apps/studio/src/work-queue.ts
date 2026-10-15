import { StudioError, type List, type WorkQueueCard, type WorkQueueItem } from "./types.js";
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i;
const states = ["open", "failed", "changes_requested", "in_review", "approved"];
const count = (n: unknown) => Number.isSafeInteger(n) && Number(n) >= 0;
export function validateQueueItem(i: WorkQueueItem): WorkQueueItem {
  if (
    !i ||
    !uuid.test(i.id) ||
    !uuid.test(i.ownerId) ||
    typeof i.title !== "string" ||
    typeof i.ownerName !== "string" ||
    typeof i.own !== "boolean" ||
    !states.includes(i.state) ||
    !count(i.seq) ||
    !Number.isFinite(Date.parse(i.createdAt)) ||
    !Number.isFinite(Date.parse(i.updatedAt)) ||
    !count(i.objectCount) ||
    !count(i.deletedCount) ||
    i.deletedCount > i.objectCount ||
    !i.countsByKind ||
    ["page", "component", "entity", "asset"].some(
      (k) => !count(i.countsByKind[k as keyof typeof i.countsByKind]),
    ) ||
    Object.values(i.countsByKind).reduce((sum, n) => sum + n, 0) !== i.objectCount
  )
    throw new StudioError("RESPONSE_INVALID");
  return i;
}
export function validateQueue(v: List<WorkQueueItem>): List<WorkQueueItem> {
  if (
    !Array.isArray(v?.items) ||
    v.items.length > 100 ||
    !(
      v.nextCursor === null ||
      (typeof v.nextCursor === "string" && /^[A-Za-z0-9_.-]{1,2048}$/.test(v.nextCursor))
    )
  )
    throw new StudioError("RESPONSE_INVALID");
  v.items.forEach(validateQueueItem);
  if (new Set(v.items.map((i) => i.id)).size !== v.items.length)
    throw new StudioError("RESPONSE_INVALID");
  return v;
}
export function validateQueueCard(v: WorkQueueCard, id: string): WorkQueueCard {
  validateQueueItem(v?.item);
  if (
    v.item.id !== id ||
    !Array.isArray(v.objects) ||
    v.objects.length !== v.item.objectCount ||
    v.objects.length > 1000 ||
    v.objects.some(
      (o) =>
        !o ||
        !uuid.test(o.id) ||
        !["document", "entity", "asset"].includes(o.kind) ||
        (o.kind === "document" && !["page", "component"].includes(o.docKind ?? "")) ||
        !(o.baseVersionId === null || uuid.test(o.baseVersionId)) ||
        !uuid.test(o.workingVersionId) ||
        typeof o.deleted !== "boolean" ||
        typeof o.title !== "string" ||
        [o.basePath, o.workingPath].some(
          (p) => p !== null && (typeof p !== "string" || !p.startsWith("/") || p.startsWith("//")),
        ),
    ) ||
    new Set(v.objects.map((o) => o.id)).size !== v.objects.length
  )
    throw new StudioError("RESPONSE_INVALID");
  return v;
}
