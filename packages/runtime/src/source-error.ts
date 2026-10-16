import type { BindingScope } from "@cms/core";
const failures = new WeakMap<object, string>();
const safeCode = (code: unknown) =>
  typeof code === "string" &&
  /^CMS_SOURCE_(UNKNOWN|TIMEOUT|ABORTED|TIMEOUT_INVALID|CURSOR_INVALID|PARAMS_INVALID|RESULT_INVALID|KEY_INVALID|FAILED)$/.test(
    code,
  )
    ? code
    : "CMS_SOURCE_FAILED";
export function sourceFailure(code: string): { error: { code: string } } {
  const result = { error: { code: safeCode(code) } };
  failures.set(result, result.error.code);
  return result;
}
/** Capture failure identity before transporting data as JSON. */
export function sourceFailures(data: Record<string, unknown>): Record<string, string> {
  return Object.fromEntries(
    Object.entries(data).flatMap(([key, value]) => {
      const code = value && typeof value === "object" ? failures.get(value) : undefined;
      return code ? [[key, code]] : [];
    }),
  );
}

/** Inspect expression paths only; literal strings and ordinary error fields are data. */
export function sourceError(value: unknown, scope: BindingScope): string | undefined {
  if (typeof value === "string") {
    const match = /^\$data\.([A-Za-z][A-Za-z0-9_]*)(?:\.|$)/.exec(value);
    if (match && scope.sourceErrors !== undefined)
      return Object.hasOwn(scope.sourceErrors, match[1]!)
        ? safeCode(scope.sourceErrors[match[1]!])
        : undefined;
    const result = match
      ? (scope.data as Record<string, unknown> | undefined)?.[match[1]!]
      : undefined;
    if (
      result &&
      typeof result === "object" &&
      !Array.isArray(result) &&
      Object.keys(result).length === 1 &&
      Object.hasOwn(result, "error")
    ) {
      const code = (result as { error?: { code?: unknown } }).error?.code;
      return safeCode(code);
    }
  } else if (Array.isArray(value)) {
    for (const child of value) {
      const code = sourceError(child, scope);
      if (code) return code;
    }
  } else if (value && typeof value === "object") {
    for (const [key, child] of Object.entries(value)) {
      if (["lit", "default", "template", "format"].includes(key)) continue;
      const code = sourceError(child, scope);
      if (code) return code;
    }
  }
  return undefined;
}
