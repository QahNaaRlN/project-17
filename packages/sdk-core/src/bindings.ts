import type { Binding, Format, Predicate } from "@cms/ir";

export interface BindingScope {
  content?: unknown;
  local?: unknown;
  props?: unknown;
  item?: unknown;
  index?: number;
  each?: unknown;
  data?: unknown;
  context?: { locale?: string; [key: string]: unknown };
}
const pathPattern =
  /^\$(content|local|props|item|index|each|data|context)(\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$/;
const forbidden = new Set(["__proto__", "prototype", "constructor"]);
export function readPath(path: string, scope: BindingScope): unknown {
  if (!pathPattern.test(path)) return undefined;
  let value: unknown = scope;
  for (const part of path.slice(1).split(".")) {
    if (
      forbidden.has(part) ||
      value === null ||
      typeof value !== "object" ||
      !Object.hasOwn(value, part)
    )
      return undefined;
    value = (value as Record<string, unknown>)[part];
  }
  return value;
}
function integer(value: unknown, fallback: number, max: number): number {
  if (value === undefined) return fallback;
  if (!Number.isInteger(value) || (value as number) < 0 || (value as number) > max)
    throw new Error("CMS_FORMAT_INVALID");
  return value as number;
}
export function formatValue(value: unknown, format: Format, locale = "en"): unknown {
  if (value === null || value === undefined) return value;
  const args = (format.args ?? {}) as Record<string, unknown>;
  switch (format.fn) {
    case "uppercase":
    case "lowercase":
      if (typeof value !== "string") throw new Error("CMS_FORMAT_INVALID");
      return format.fn === "uppercase"
        ? value.toLocaleUpperCase(locale)
        : value.toLocaleLowerCase(locale);
    case "truncate": {
      if (typeof value !== "string") throw new Error("CMS_FORMAT_INVALID");
      const length = integer(args.length, 100, 100_000);
      const chars = Array.from(value);
      return chars.length <= length ? value : chars.slice(0, length).join("");
    }
    case "number":
    case "currency": {
      if (typeof value !== "number" || !Number.isFinite(value))
        throw new Error("CMS_FORMAT_INVALID");
      const style = format.fn === "currency" ? "currency" : (args.style ?? "decimal");
      if (style !== "decimal" && style !== "percent" && style !== "currency")
        throw new Error("CMS_FORMAT_INVALID");
      if (format.fn === "number" && style === "currency") throw new Error("CMS_FORMAT_INVALID");
      if (style === "currency" && (typeof args.code !== "string" || !/^[A-Z]{3}$/.test(args.code)))
        throw new Error("CMS_FORMAT_INVALID");
      return new Intl.NumberFormat(locale, {
        style,
        ...(style === "currency" ? { currency: args.code as string } : {}),
        ...(style === "currency"
          ? {}
          : {
              minimumFractionDigits: integer(args.minFraction, 0, 20),
              maximumFractionDigits: integer(args.maxFraction, 3, 20),
            }),
      }).format(value);
    }
    case "date": {
      // Never parse locale-dependent strings or depend on the server time zone.
      if (
        typeof value !== "string" ||
        !/^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2}))?$/.test(value)
      )
        throw new Error("CMS_FORMAT_INVALID");
      const date = new Date(value);
      if (!Number.isFinite(date.getTime())) throw new Error("CMS_FORMAT_INVALID");
      const calendar = new Date(value.slice(0, 10));
      if (calendar.toISOString().slice(0, 10) !== value.slice(0, 10))
        throw new Error("CMS_FORMAT_INVALID");
      const style = args.style ?? "medium";
      if (style === "iso") return date.toISOString();
      if (style !== "short" && style !== "medium" && style !== "long")
        throw new Error("CMS_FORMAT_INVALID");
      return new Intl.DateTimeFormat(locale, { dateStyle: style, timeZone: "UTC" }).format(date);
    }
    default:
      throw new Error("CMS_FORMAT_UNKNOWN");
  }
}
export function evaluateBinding(binding: Binding, scope: BindingScope): unknown {
  if (typeof binding === "string") return readPath(binding, scope);
  if ("template" in binding) {
    return binding.template.replace(/\{([A-Za-z][A-Za-z0-9_]*)\}/g, (_match, name: string) => {
      if (!Object.hasOwn(binding.vars, name)) throw new Error("CMS_TEMPLATE_INVALID");
      const value = evaluateBinding(binding.vars[name]!, scope);
      if (value === null || value === undefined) return "";
      if (typeof value !== "string" && typeof value !== "number" && typeof value !== "boolean")
        throw new Error("CMS_TEMPLATE_INVALID");
      return String(value);
    });
  }
  const value = readPath(binding.expr, scope) ?? binding.default;
  return binding.format ? formatValue(value, binding.format, scope.context?.locale) : value;
}
function operand(value: unknown, scope: BindingScope): unknown {
  if (typeof value === "string") return readPath(value, scope);
  if (value !== null && typeof value === "object" && Object.hasOwn(value, "lit"))
    return (value as { lit: unknown }).lit;
  return undefined;
}
export function evaluatePredicate(predicate: Predicate, scope: BindingScope, depth = 0): boolean {
  if (depth > 8) throw new Error("CMS_PREDICATE_LIMIT");
  switch (predicate.op) {
    case "exists":
      return readPath(predicate.arg, scope) != null;
    case "empty": {
      const value = readPath(predicate.arg, scope);
      return value == null || value === "" || (Array.isArray(value) && value.length === 0);
    }
    case "not":
      return !evaluatePredicate(predicate.arg, scope, depth + 1);
    case "and":
    case "or": {
      if (predicate.args.length < 1 || predicate.args.length > 16)
        throw new Error("CMS_PREDICATE_LIMIT");
      const check = (p: Predicate) => evaluatePredicate(p, scope, depth + 1);
      return predicate.op === "and" ? predicate.args.every(check) : predicate.args.some(check);
    }
    case "in": {
      const value = operand(predicate.args[0], scope);
      const list = operand(predicate.args[1], scope);
      return value != null && Array.isArray(list) && list.includes(value);
    }
    case "eq":
    case "neq":
    case "gt":
    case "gte":
    case "lt":
    case "lte": {
      const a = operand(predicate.args[0], scope);
      const b = operand(predicate.args[1], scope);
      if (predicate.op === "eq") return a === b;
      if (predicate.op === "neq") return a !== b;
      if (
        typeof a !== "number" ||
        typeof b !== "number" ||
        !Number.isFinite(a) ||
        !Number.isFinite(b)
      )
        return false;
      if (predicate.op === "gt") return a > b;
      if (predicate.op === "gte") return a >= b;
      if (predicate.op === "lt") return a < b;
      return a <= b;
    }
    default:
      throw new Error("CMS_PREDICATE_UNKNOWN");
  }
}
