/** Reject non-JSON metadata rather than silently dropping executable values. */
export function snapshot<T>(value: T, seen = new Set<object>()): T {
  if (value === null || typeof value === "string" || typeof value === "boolean") return value;
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value !== "object" || seen.has(value))
    throw new Error("CMS_REGISTRY_METADATA_INVALID");
  if (
    !Array.isArray(value) &&
    Object.getPrototypeOf(value) !== Object.prototype &&
    Object.getPrototypeOf(value) !== null
  )
    throw new Error("CMS_REGISTRY_METADATA_INVALID");
  if (
    Reflect.ownKeys(value).some(
      (key) =>
        typeof key !== "string" ||
        !Object.hasOwn(Object.getOwnPropertyDescriptor(value, key)!, "value"),
    )
  )
    throw new Error("CMS_REGISTRY_METADATA_INVALID");
  seen.add(value);
  const result = Array.isArray(value)
    ? Array.from(value, (item) => snapshot(item, seen))
    : Object.fromEntries(Object.entries(value).map(([key, item]) => [key, snapshot(item, seen)]));
  seen.delete(value);
  return Object.freeze(result) as T;
}
