import { StudioError, type Changeset } from "./types.js";

export interface WorkCreationScope {
  actor: string;
  project: string;
  environment: string;
}
export interface WorkCreationAttempt extends WorkCreationScope {
  key: string;
  firstSentAt: number;
  lastSeenAt: number;
  body: { payload: { title: string; targets: [string] }; reason: string };
  result?: Changeset;
}
export const workCreationKey = "studio-content-work-attempt-v1";
export const workCreationWindow = 23 * 60 * 60 * 1000;
export function workStorageKey(scope: WorkCreationScope) {
  return workCreationKey + ":" + JSON.stringify([scope.actor, scope.project, scope.environment]);
}
export function sameWorkScope(a: WorkCreationScope, b: WorkCreationScope) {
  return a.actor === b.actor && a.project === b.project && a.environment === b.environment;
}
export function workTitle(value: string) {
  const title = value.trim();
  if ([...title].length < 1 || [...title].length > 200) throw new StudioError("TITLE_INVALID");
  return title;
}
export function makeWorkAttempt(
  scope: WorkCreationScope,
  title: string,
  now = Date.now(),
): WorkCreationAttempt {
  return {
    ...scope,
    key: crypto.randomUUID(),
    firstSentAt: now,
    lastSeenAt: now,
    body: {
      payload: { title: workTitle(title), targets: [scope.environment] },
      reason: "Studio: create content work",
    },
  };
}
export function checkCreatedWork(value: Changeset, environment: string): Changeset {
  if (
    !value ||
    !/^[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i.test(value.id) ||
    value.kind !== "standard" ||
    value.state !== "open" ||
    value.seq !== 0 ||
    typeof value.title !== "string" ||
    !Array.isArray(value.targets) ||
    value.targets.length !== 1 ||
    value.targets[0] !== environment
  )
    throw new StudioError("RESPONSE_INVALID", true);
  return value;
}
export function canRetryWork(a: WorkCreationAttempt, now = Date.now()) {
  return (
    Number.isSafeInteger(a.firstSentAt) &&
    Number.isSafeInteger(a.lastSeenAt) &&
    a.firstSentAt > 0 &&
    a.lastSeenAt >= a.firstSentAt &&
    now >= a.lastSeenAt &&
    now - a.firstSentAt < workCreationWindow
  );
}
export function saveWorkAttempt(a: WorkCreationAttempt, storage: Storage = sessionStorage) {
  try {
    storage.setItem(workStorageKey(a), JSON.stringify(a));
  } catch {
    throw new StudioError("WORK_STORAGE_UNAVAILABLE");
  }
}
export function clearWorkAttempt(scope?: WorkCreationScope, storage: Storage = sessionStorage) {
  try {
    if (scope) storage.removeItem(workStorageKey(scope));
    else
      for (const key of Object.keys(storage))
        if (key.startsWith(workCreationKey)) storage.removeItem(key);
  } catch {
    throw new StudioError("WORK_STORAGE_UNAVAILABLE");
  }
}
export function loadWorkAttempt(
  scope: WorkCreationScope,
  storage: Storage = sessionStorage,
): WorkCreationAttempt | null {
  try {
    const raw = storage.getItem(workStorageKey(scope));
    if (!raw) return null;
    const a = JSON.parse(raw) as WorkCreationAttempt;
    if (
      !a ||
      typeof a.actor !== "string" ||
      typeof a.project !== "string" ||
      typeof a.environment !== "string"
    )
      throw new Error("invalid scope");
    if (!sameWorkScope(a, scope)) return null;
    if (
      typeof a.key !== "string" ||
      !/^[a-f0-9-]{36}$/i.test(a.key) ||
      a.body?.reason !== "Studio: create content work" ||
      typeof a.body.payload?.title !== "string" ||
      workTitle(a.body.payload.title) !== a.body.payload.title ||
      a.body.payload.targets?.length !== 1 ||
      a.body.payload.targets[0] !== scope.environment
    )
      throw new Error("invalid attempt");
    if (a.result) checkCreatedWork(a.result, scope.environment);
    return a;
  } catch {
    throw new StudioError("WORK_STORAGE_UNAVAILABLE");
  }
}
