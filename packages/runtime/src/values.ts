import { compilePattern, type Fields, type Type } from "@cms/manifest";
import { linkHref } from "./plan.js";
import { richValue } from "./rich-value.js";
export type ReadonlyContract<T> = T extends object
  ? { readonly [K in keyof T]: ReadonlyContract<T[K]> }
  : T;

export function object(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
/** JSON transport rejects accessors, prototypes, cycles and oversized values before use. */
export function jsonValue(value: unknown): unknown {
  let count = 0;
  const seen = new Set<object>();
  function copy(item: unknown, depth: number): unknown {
    if (++count > 100_000 || depth > 32) throw new Error("CMS_VALUE_INVALID");
    if (item === null || typeof item === "boolean" || typeof item === "string") return item;
    if (typeof item === "number" && Number.isFinite(item)) return item;
    if (typeof item !== "object" || seen.has(item)) throw new Error("CMS_VALUE_INVALID");
    if (
      Array.isArray(item)
        ? Object.getPrototypeOf(item) !== Array.prototype
        : ![Object.prototype, null].includes(Object.getPrototypeOf(item))
    )
      throw new Error("CMS_VALUE_INVALID");
    const descriptors = Object.getOwnPropertyDescriptors(item);
    if (
      Reflect.ownKeys(item).some(
        (key) => typeof key !== "string" || !Object.hasOwn(descriptors[key]!, "value"),
      )
    )
      throw new Error("CMS_VALUE_INVALID");
    seen.add(item);
    const result = Array.isArray(item)
      ? Array.from({ length: item.length }, (_entry, index) =>
          copy(descriptors[index]?.value, depth + 1),
        )
      : Object.fromEntries(
          Object.entries(item).map(([key, entry]) => {
            if (["__proto__", "prototype", "constructor"].includes(key))
              throw new Error("CMS_VALUE_INVALID");
            return [key, copy(entry, depth + 1)];
          }),
        );
    seen.delete(item);
    return result;
  }
  const result = copy(value, 0);
  if (new TextEncoder().encode(JSON.stringify(result)).length > 2 * 1024 * 1024)
    throw new Error("CMS_VALUE_INVALID");
  return result;
}
function fail(): never {
  throw new Error("CMS_VALUE_INVALID");
}
export function fieldValue(schema: ReadonlyContract<Type>, value: unknown): unknown {
  if (value == null) {
    if (value === undefined && "default" in schema && schema.default !== undefined)
      return fieldValue(schema, schema.default);
    if (schema.required) fail();
    return value;
  }
  switch (schema.type) {
    case "string":
    case "text":
    case "color":
    case "nodeRef":
    case "enum":
    case "date":
    case "datetime":
    case "url": {
      if (typeof value !== "string" || value.includes("\0")) fail();
      if (schema.type === "string" || schema.type === "text") {
        const length = Array.from(value).length;
        if (
          length < (schema.type === "string" ? (schema.minLength ?? 0) : 0) ||
          length > (schema.maxLength ?? Infinity)
        )
          fail();
        if (
          schema.type === "string" &&
          schema.pattern !== undefined &&
          !compilePattern(schema.pattern)?.matcher(value).find()
        )
          fail();
      }
      if (schema.type === "enum" && !schema.values.includes(value)) fail();
      if (schema.type === "nodeRef" && !/^[A-Za-z0-9_-]{4,32}$/.test(value)) fail();
      if (schema.type === "date" || schema.type === "datetime") {
        if (
          !(
            schema.type === "date"
              ? /^\d{4}-\d{2}-\d{2}$/
              : /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/
          ).test(value)
        )
          fail();
        if (
          !Number.isFinite(Date.parse(value)) ||
          new Date(value.slice(0, 10)).toISOString().slice(0, 10) !== value.slice(0, 10)
        )
          fail();
      }
      if (schema.type === "url") {
        if (!linkHref({ kind: "url", url: value })) fail();
        const scheme = /^([a-z]+):/i.exec(value)?.[1]?.toLowerCase();
        if (scheme && schema.schemes && !schema.schemes.includes(scheme)) fail();
      }
      return value;
    }
    case "number":
      if (
        typeof value !== "number" ||
        !Number.isFinite(value) ||
        (schema.integer && !Number.isInteger(value)) ||
        value < (schema.min ?? -Infinity) ||
        value > (schema.max ?? Infinity)
      )
        fail();
      return value;
    case "boolean":
      if (typeof value !== "boolean") fail();
      return value;
    case "list":
      if (
        !Array.isArray(value) ||
        value.length < (schema.min ?? 0) ||
        value.length > (schema.max ?? Infinity)
      )
        fail();
      return value.map((item) => fieldValue(schema.of, item));
    case "object":
      if (!object(value)) fail();
      return fieldValues(schema.fields, value);
    case "asset":
    case "reference": {
      const key = schema.type === "asset" ? "assetId" : "entityId";
      if (
        !object(value) ||
        typeof value[key] !== "string" ||
        !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
          value[key] as string,
        )
      )
        fail();
      if (
        Object.keys(value).some(
          (name) =>
            name !== key &&
            !(schema.type === "asset" && name === "alt" && typeof value.alt === "string"),
        )
      )
        fail();
      return value;
    }
    case "link":
      if (!object(value)) fail();
      if (value.kind === "page") {
        if (
          typeof value.page !== "string" ||
          !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value.page) ||
          (value.params !== undefined && !object(value.params)) ||
          Object.keys(value).some((key) => !["kind", "page", "params"].includes(key))
        )
          fail();
      } else if (!linkHref(value)) fail();
      else if (
        Object.keys(value).some(
          (key) => !["kind", value.kind === "anchor" ? "node" : "url"].includes(key),
        )
      )
        fail();
      return value;
    case "richText":
      if (!object(value) || value.type !== "doc" || !Array.isArray(value.content)) fail();
      richValue(value, schema);
      return value;
  }
}
export function fieldValues(
  fields: ReadonlyContract<Fields>,
  input: Record<string, unknown>,
): Record<string, unknown> {
  if (Object.keys(input).some((key) => !Object.hasOwn(fields, key))) fail();
  const result: Record<string, unknown> = Object.create(null);
  for (const [key, schema] of Object.entries(fields)) {
    const value = fieldValue(schema, Object.hasOwn(input, key) ? input[key] : undefined);
    if (value !== undefined) result[key] = value;
  }
  return result;
}
