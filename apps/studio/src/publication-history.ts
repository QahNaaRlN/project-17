import { StudioError, type DiffChange, type DiffSummary } from "./types.js";
import { validValue, snapshotFormat } from "./review-diff.js";
export interface PublicationEntry {
  id: string;
  environment: string;
  kind: string;
  actorId: string;
  createdAt: string;
  changesetId: string | null;
  sourcePublicationId: string | null;
  reason: string | null;
  manifestChanged: boolean;
  previousManifestId: string | null;
  currentManifestId: string | null;
  objectCount?: number;
}
export interface PublicationObject {
  id: string;
  kind: string;
  docKind?: string;
  title: string;
  beforeVersionId: string | null;
  afterVersionId: string | null;
  beforePath: string | null;
  afterPath: string | null;
  summary: DiffSummary;
}
export interface PublicationCard {
  publication: PublicationEntry;
  objects: PublicationObject[];
  snapshot: string;
  complete: true;
  canRollback: boolean;
  blockers: string[];
}
export interface PublicationPage {
  publicationId: string;
  objectId: string;
  snapshot: string;
  items: DiffChange[];
  totalChanges: number;
  nextCursor: string | null;
}
export interface PublicationList {
  items: PublicationEntry[];
  nextCursor: string | null;
}
export interface RollbackAttempt {
  publicationId: string;
  resetHead: false;
  expectedRollbackSnapshot: string;
  reason: string;
  key: string;
  startedAt: number;
}
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i;
const nullableID = (v: unknown) => v === null || (typeof v === "string" && uuid.test(v));
const text = (v: unknown) => v === null || typeof v === "string";
const cursor = (v: unknown) => v === null || (typeof v === "string" && v.length <= 2048);
const count = (v: unknown) => Number.isSafeInteger(v) && Number(v) >= 0;
function entry(v: PublicationEntry, environment: string) {
  return (
    !!v &&
    uuid.test(v.id) &&
    uuid.test(v.actorId) &&
    v.environment === environment &&
    typeof v.kind === "string" &&
    Number.isFinite(Date.parse(v.createdAt)) &&
    [v.changesetId, v.sourcePublicationId, v.previousManifestId, v.currentManifestId].every(
      nullableID,
    ) &&
    text(v.reason) &&
    typeof v.manifestChanged === "boolean"
  );
}
export function publicationList(v: PublicationList, environment: string): PublicationList {
  if (
    !v ||
    !Array.isArray(v.items) ||
    v.items.length > 100 ||
    !cursor(v.nextCursor) ||
    new Set(v.items.map((p) => p?.id)).size !== v.items.length ||
    v.items.some((p) => !entry(p, environment) || !count(p.objectCount))
  )
    throw new StudioError("RESPONSE_INVALID");
  return v;
}
export function publicationCard(
  v: PublicationCard,
  id: string,
  environment: string,
): PublicationCard {
  if (
    !v ||
    !entry(v.publication, environment) ||
    v.publication.id !== id ||
    v.complete !== true ||
    typeof v.canRollback !== "boolean" ||
    !Array.isArray(v.blockers) ||
    v.blockers.some((b) => typeof b !== "string") ||
    !Array.isArray(v.objects) ||
    v.objects.length > 1000 ||
    new Set(v.objects.map((o) => o?.id)).size !== v.objects.length ||
    (!v.publication.manifestChanged && !snapshotFormat.test(v.snapshot)) ||
    v.objects.some(
      (o) =>
        !o ||
        !uuid.test(o.id) ||
        !["document", "entity", "asset"].includes(o.kind) ||
        typeof o.title !== "string" ||
        ![o.beforeVersionId, o.afterVersionId].every(nullableID) ||
        ![o.beforePath, o.afterPath].every(text) ||
        !o.summary ||
        !count(o.summary.changeCount) ||
        !["added", "modified", "deleted", "restored", "metadata-only", "unchanged"].includes(
          o.summary.effect,
        ),
    ) ||
    (v.canRollback && (v.publication.manifestChanged || v.blockers.length > 0))
  )
    throw new StudioError("RESPONSE_INVALID");
  return v;
}
export function publicationPage(
  v: PublicationPage,
  id: string,
  obj: string,
  snapshot: string,
): PublicationPage {
  if (
    !v ||
    v.publicationId !== id ||
    v.objectId !== obj ||
    v.snapshot !== snapshot ||
    !count(v.totalChanges) ||
    !cursor(v.nextCursor) ||
    !Array.isArray(v.items) ||
    v.items.length > 100 ||
    new Set(v.items.map((i) => i?.key)).size !== v.items.length ||
    v.items.length > v.totalChanges ||
    v.items.some(
      (i) =>
        !i ||
        typeof i.key !== "string" ||
        typeof i.pointer !== "string" ||
        typeof i.category !== "string" ||
        !["add", "remove", "replace", "move"].includes(i.kind) ||
        !validValue(i.before) ||
        !validValue(i.after),
    )
  )
    throw new StudioError("RESPONSE_INVALID");
  return v;
}
