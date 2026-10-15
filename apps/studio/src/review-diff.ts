import {
  StudioError,
  type WorkContext,
  type DiffIndex,
  type DiffPage,
  type DiffBaseline,
  type DiffValue,
} from "./types.js";
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i;
export const snapshotFormat = /^sha256:[a-f0-9]{64}$/;
const nullableID = (id: unknown) => id === null || (typeof id === "string" && uuid.test(id));
const count = (n: unknown) => Number.isSafeInteger(n) && Number(n) >= 0;
export function validateDiffIndex(v: DiffIndex, session: WorkContext): DiffIndex {
  if (
    !v ||
    v.changesetId !== session.changeset.id ||
    v.environment !== session.config.environment ||
    !snapshotFormat.test(v.snapshot) ||
    v.complete !== true ||
    !Array.isArray(v.targets) ||
    v.targets.length !== session.changeset.targets.length ||
    !session.changeset.targets.every((t) => v.targets.includes(t)) ||
    !Array.isArray(v.objects) ||
    v.objects.length > 1000 ||
    new Set(v.objects.map((o) => o?.id)).size !== v.objects.length ||
    v.objects.some(
      (o) =>
        !o ||
        !uuid.test(o.id) ||
        !uuid.test(o.workingVersionId) ||
        !["document", "entity", "asset"].includes(o.kind) ||
        typeof o.title !== "string" ||
        ![o.beforePath, o.afterPath].every((p) => p === null || typeof p === "string") ||
        ![o.baseVersionId, o.headVersionId, o.publishedVersionId].every(nullableID) ||
        typeof o.baseStale !== "boolean" ||
        (o.workingDeleted !== undefined && typeof o.workingDeleted !== "boolean") ||
        ["base", "published"].some(
          (b) =>
            !o.summaries?.[b as DiffBaseline] ||
            !["added", "modified", "deleted", "restored", "metadata-only", "unchanged"].includes(
              o.summaries[b as DiffBaseline].effect,
            ) ||
            !count(o.summaries[b as DiffBaseline].changeCount),
        ),
    )
  )
    throw new StudioError("RESPONSE_INVALID");
  if (v.seq !== session.changeset.seq) throw new StudioError("DIFF_SNAPSHOT_STALE");
  return v;
}
export function validValue(v: DiffValue): boolean {
  if (!v || typeof v.present !== "boolean") return false;
  if (!v.present) return !("value" in v) && !("rawJson" in v) && !("redacted" in v);
  return (
    ("value" in v && !("rawJson" in v) && !("redacted" in v)) ||
    ("rawJson" in v && typeof v.rawJson === "string" && !("value" in v) && !("redacted" in v)) ||
    ("redacted" in v &&
      v.redacted === true &&
      "reason" in v &&
      v.reason === "service-field" &&
      !("value" in v) &&
      !("rawJson" in v))
  );
}
export function validateDiffPage(
  v: DiffPage,
  index: DiffIndex,
  objectId: string,
  baseline: DiffBaseline,
): DiffPage {
  const object = index.objects.find((o) => o.id === objectId);
  if (
    !v ||
    !object ||
    v.changesetId !== index.changesetId ||
    v.objectId !== objectId ||
    v.seq !== index.seq ||
    v.snapshot !== index.snapshot ||
    v.environment !== index.environment ||
    v.baseline !== baseline ||
    !count(v.totalChanges) ||
    v.totalChanges !== object.summaries[baseline].changeCount ||
    !Array.isArray(v.items) ||
    v.items.length > 100 ||
    v.items.length > v.totalChanges ||
    (v.nextCursor !== null && v.items.length === 0) ||
    new Set(v.items.map((i) => i?.key)).size !== v.items.length ||
    !(
      v.nextCursor === null ||
      (typeof v.nextCursor === "string" && v.nextCursor.length > 0 && v.nextCursor.length <= 2048)
    ) ||
    v.items.some(
      (i) =>
        !i ||
        typeof i.key !== "string" ||
        !/^\d+$/.test(i.key) ||
        !count(Number(i.key)) ||
        Number(i.key) >= v.totalChanges ||
        ![
          "structure",
          "property",
          "design",
          "binding",
          "condition",
          "behavior",
          "localContent",
          "metadata",
          "content",
          "file",
        ].includes(i.category) ||
        !["add", "remove", "replace", "move"].includes(i.kind) ||
        typeof i.pointer !== "string" ||
        i.pointer.length > 4000 ||
        !(i.pointer === "" || i.pointer.startsWith("/")) ||
        !validValue(i.before) ||
        !validValue(i.after) ||
        (i.nodeId !== undefined && typeof i.nodeId !== "string") ||
        (i.locale !== undefined && typeof i.locale !== "string"),
    )
  )
    throw new StudioError("RESPONSE_INVALID");
  return v;
}
