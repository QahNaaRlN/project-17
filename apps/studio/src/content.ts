import { canonicalJson, type Type } from "@cms/manifest";
import { validateFieldValue, jsonValue } from "@cms/runtime";
import { StudioError, type ContentOperation, type ContentRecord } from "./types.js";
export type ContentFields = Record<string, Type>;
export const assetFields: ContentFields = {
  alt: { type: "text", localized: true },
  title: { type: "string" },
  tags: { type: "list", of: { type: "string" } },
  focalPoint: {
    type: "object",
    fields: {
      x: { type: "number", min: 0, max: 1, required: true },
      y: { type: "number", min: 0, max: 1, required: true },
    },
  },
};
export const equal = (a: unknown, b: unknown) => {
  if (a === undefined || b === undefined) return a === b;
  try {
    jsonValue(a);
    jsonValue(b);
    return canonicalJson(a) === canonicalJson(b);
  } catch {
    return false;
  }
};
export function contentProblems(
  data: Record<string, unknown>,
  fields: ContentFields,
  locales: string[] = [],
  draftWarnings = true,
): { path: string; warning: boolean }[] {
  const problems: { path: string; warning: boolean }[] = [];
  try {
    jsonValue(data);
    canonicalJson(data);
  } catch {
    return [{ path: "/", warning: false }];
  }
  const check = (field: Type, value: unknown, path: string, depth = 0): void => {
    if (value == null) {
      if (field.required && !("default" in field && field.default !== undefined))
        problems.push({ path, warning: draftWarnings });
      return;
    }
    try {
      if (depth > 32) throw new Error();
      if (field.localized) {
        if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error();
        for (const [locale, entry] of Object.entries(value)) {
          if (
            !/^[a-z]{2,3}(?:-[A-Z]{2})?$/.test(locale) ||
            (locales.length && !locales.includes(locale))
          )
            problems.push({ path: path + "." + locale, warning: false });
          else check({ ...field, localized: false }, entry, path + "." + locale, depth + 1);
        }
        if (field.required)
          for (const locale of locales)
            if (!Object.hasOwn(value, locale))
              problems.push({ path: path + "." + locale, warning: draftWarnings });
      } else if (field.type === "object") {
        if (
          typeof value !== "object" ||
          Array.isArray(value) ||
          Object.keys(value).some((key) => !Object.hasOwn(field.fields, key))
        )
          throw new Error();
        for (const [key, child] of Object.entries(field.fields))
          check(child, (value as Record<string, unknown>)[key], path + "." + key, depth + 1);
      } else if (field.type === "list") {
        if (
          !Array.isArray(value) ||
          value.length < (field.min ?? 0) ||
          value.length > (field.max ?? Infinity)
        )
          throw new Error();
        value.forEach((entry, i) => check(field.of, entry, path + "." + i, depth + 1));
      } else if (
        field.type === "asset" &&
        typeof value === "object" &&
        typeof (value as Record<string, unknown>).alt === "object" &&
        (value as Record<string, unknown>).alt !== null
      ) {
        const { alt, ...ref } = value as Record<string, unknown>;
        validateFieldValue(field, ref);
        check({ type: "text", localized: true }, alt, path + ".alt", depth + 1);
      } else validateFieldValue({ ...field, required: false }, value);
    } catch {
      problems.push({ path, warning: false });
    }
  };
  for (const [name, field] of Object.entries(fields)) check(field, data[name], name);
  return problems;
}
export function contentDiff(
  before: ContentRecord,
  after: Record<string, unknown>,
  fields: ContentFields,
): ContentOperation[] {
  if (contentProblems(after, fields, [], before.kind === "entity").some((p) => !p.warning))
    throw new StudioError("FIELD_INVALID");
  const set: Record<string, unknown> = {},
    unset: string[] = [];
  const diff = (path: string, a: unknown, b: unknown) => {
    if (equal(a, b)) return;
    if (b === undefined) unset.push(path);
    else set[path] = b;
  };
  for (const name of [...new Set([...Object.keys(before.data), ...Object.keys(after)])].sort()) {
    const field = fields[name];
    if (!field) {
      if (!equal(before.data[name], after[name])) throw new StudioError("CONTENT_FIELD_READ_ONLY");
      continue;
    }
    if (field.localized && before.data[name] && after[name]) {
      const a = before.data[name] as Record<string, unknown>,
        b = after[name] as Record<string, unknown>;
      for (const locale of [...new Set([...Object.keys(a), ...Object.keys(b)])].sort())
        diff(name + "." + locale, a[locale], b[locale]);
    } else diff(name, before.data[name], after[name]);
  }
  return Object.keys(set).length || unset.length
    ? [
        {
          type: before.kind === "entity" ? "entity.setFields" : "asset.updateMeta",
          target: before.id,
          expectedVersionId: before.versionId,
          payload: {
            ...(Object.keys(set).length ? { set } : {}),
            ...(unset.length ? { unset } : {}),
          },
        },
      ]
    : [];
}
export function uploadLimit(mime: string): number {
  if (
    ["image/jpeg", "image/png", "image/webp", "image/avif", "image/gif", "image/svg+xml"].includes(
      mime,
    )
  )
    return 25 * 1024 * 1024;
  if (["video/mp4", "video/webm"].includes(mime)) return 500 * 1024 * 1024;
  return mime === "application/pdf" ? 100 * 1024 * 1024 : 0;
}
export function uploadUrl(url: string, origin: string | undefined): string {
  const parsed = new URL(url);
  if (
    !origin ||
    parsed.origin !== origin ||
    parsed.username ||
    parsed.password ||
    parsed.hash ||
    !["https:", "http:"].includes(parsed.protocol)
  )
    throw new StudioError("UPLOAD_ORIGIN_INVALID");
  return parsed.href;
}
