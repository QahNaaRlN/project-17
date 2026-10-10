import { Ajv2020, type ErrorObject, type ValidateFunction } from "ajv/dist/2020.js";
import localizeRuModule from "ajv-i18n/localize/ru/index.js";
import {
  BUILTIN_ACTIONS,
  BUILTIN_PRIMITIVES,
  CONTENT_TYPES,
  RESERVED_FIELDS,
  SUPPORTED_IR_VERSIONS,
  UNIQUE_TYPES,
} from "./builtins.js";
import { DiagnosticCode, pointer, type Diagnostic } from "./diagnostics.js";
import { hasInvalidUnicode } from "./unicode.js";
import { manifestSchema } from "./generated/schema.js";
import type { Component, Fields, Manifest, Type } from "./generated/types.js";

export interface ValidationResult {
  valid: boolean;
  diagnostics: Diagnostic[];
}

// ajv-i18n — CommonJS с `module.exports = fn`, а его .d.ts объявляет `export default`.
const localizeRu = ((localizeRuModule as unknown as { default?: unknown }).default ??
  localizeRuModule) as (errors?: ErrorObject[] | null) => void;

let compiled: ValidateFunction | undefined;

function schemaValidator(): ValidateFunction {
  compiled ??= new Ajv2020({
    allErrors: true,
    strict: true,
    strictTypes: false,
    strictRequired: false,
    discriminator: true,
  }).compile(manifestSchema);
  return compiled;
}

/**
 * Валидация manifest: JSON Schema, затем — для структурно корректного manifest — семантические
 * проверки (имена, ссылки между разделами, значения по умолчанию, диапазоны, CNT-001).
 * Диагностики отсортированы по указателю и коду — так же, как в Go-реализации.
 */
export function validateManifest(manifest: unknown): ValidationResult {
  if (hasInvalidUnicode(manifest)) {
    return {
      valid: false,
      diagnostics: [
        {
          code: DiagnosticCode.SchemaViolation,
          severity: "error",
          pointer: "",
          message: "manifest содержит некорректный Unicode",
          params: { keyword: "unicode" },
        },
      ],
    };
  }
  const validate = schemaValidator();
  let diagnostics: Diagnostic[];
  if (validate(manifest)) {
    diagnostics = new Checker(manifest as unknown as Manifest).run();
  } else {
    localizeRu(validate.errors);
    diagnostics = schemaDiagnostics(validate.errors ?? []);
  }
  diagnostics.sort((a, b) =>
    a.pointer === b.pointer
      ? a.code < b.code
        ? -1
        : a.code > b.code
          ? 1
          : 0
      : a.pointer < b.pointer
        ? -1
        : 1,
  );
  return { valid: !diagnostics.some((d) => d.severity === "error"), diagnostics };
}

// --- JSON Schema ------------------------------------------------------------

const VAGUE_KEYWORDS = new Set(["oneOf", "anyOf", "allOf", "if", "not", "type"]);
const BRANCH_KEYWORDS = new Set([
  "additionalProperties",
  "unevaluatedProperties",
  "required",
  "const",
]);

function rank(error: ErrorObject): number {
  if (VAGUE_KEYWORDS.has(error.keyword)) return 2;
  if (BRANCH_KEYWORDS.has(error.keyword)) return 1;
  return 0;
}

/** Одна, самая конкретная ошибка на место в документе (см. @cms/ir). */
function schemaDiagnostics(errors: ErrorObject[]): Diagnostic[] {
  const byPointer = new Map<string, ErrorObject>();
  for (const error of errors) {
    if ((error as ErrorObject & { propertyName?: string }).propertyName !== undefined) continue;
    const ptr = errorPointer(error);
    const current = byPointer.get(ptr);
    if (!current || rank(error) < rank(current)) byPointer.set(ptr, error);
  }
  const pointers = [...byPointer.keys()];
  const hasDescendant = (ptr: string) => pointers.some((p) => p !== ptr && p.startsWith(ptr + "/"));
  const out: Diagnostic[] = [];
  for (const [ptr, error] of byPointer) {
    if (VAGUE_KEYWORDS.has(error.keyword) && hasDescendant(ptr)) continue;
    out.push({
      code: DiagnosticCode.SchemaViolation,
      severity: "error",
      pointer: ptr,
      message: error.message ?? error.keyword,
      params: { keyword: error.keyword, ...error.params },
    });
  }
  return out;
}

function errorPointer(error: ErrorObject): string {
  if (error.keyword === "additionalProperties") {
    return error.instancePath + pointer(String(error.params["additionalProperty"]));
  }
  if (error.keyword === "unevaluatedProperties") {
    return error.instancePath + pointer(String(error.params["unevaluatedProperty"]));
  }
  if (error.keyword === "propertyNames") {
    return error.instancePath + pointer(String(error.params["propertyName"]));
  }
  // Неизвестное значение дискриминатора (type, op) — ошибка самого поля, как и в Go-реализации.
  if (error.keyword === "discriminator") {
    return error.instancePath + pointer(String(error.params["tag"]));
  }
  return error.instancePath;
}

// --- Семантика ----------------------------------------------------------------

// Порядок обхода не важен: диагностики сортируются в конце validateManifest.
function entries<T>(record: Record<string, T> | undefined): [string, T][] {
  return Object.entries(record ?? {});
}

class Checker {
  private readonly out: Diagnostic[] = [];
  private readonly types: Set<string>;
  private readonly schemas: Set<string>;
  private readonly capabilities: Set<string>;
  private readonly colors: Set<string>;

  constructor(private readonly m: Manifest) {
    this.types = new Set<string>([
      ...BUILTIN_PRIMITIVES,
      ...Object.keys(m.primitives ?? {}),
      ...Object.keys(m.components ?? {}),
    ]);
    this.schemas = new Set(Object.keys(m.schemas ?? {}));
    this.capabilities = new Set(Object.keys(m.capabilities ?? {}));
    this.colors = new Set(Object.keys(m.tokens.colors ?? {}));
  }

  private add(
    code: Diagnostic["code"],
    ptr: string,
    message: string,
    params?: Record<string, unknown>,
  ) {
    this.out.push({ code, severity: "error", pointer: ptr, message, ...(params && { params }) });
  }

  run(): Diagnostic[] {
    const m = this.m;
    if (!m.irVersions.some((v) => (SUPPORTED_IR_VERSIONS as readonly string[]).includes(v))) {
      this.add(
        DiagnosticCode.IrVersionUnsupported,
        "/irVersions",
        `manifest не поддерживает ни одну версию IR сервера (${SUPPORTED_IR_VERSIONS.join(", ")})`,
      );
    }
    for (const [name, typo] of entries(m.tokens.typography)) {
      for (const [bp] of entries(typo.fontSize)) {
        if (bp !== "base" && !(bp in m.breakpoints)) {
          this.add(
            DiagnosticCode.UnknownBreakpoint,
            pointer("tokens", "typography", name, "fontSize", bp),
            `breakpoint ${bp} не объявлен в breakpoints`,
            { breakpoint: bp },
          );
        }
      }
    }
    for (const [section, record] of [
      ["primitives", m.primitives],
      ["components", m.components],
    ] as const) {
      for (const [name, component] of entries(record)) {
        const ptr = pointer(section, name);
        if ((BUILTIN_PRIMITIVES as readonly string[]).includes(name)) {
          this.add(DiagnosticCode.NameReserved, ptr, `имя ${name} занято встроенным примитивом`, {
            name,
          });
        }
        if (section === "components" && m.primitives && name in m.primitives) {
          this.add(
            DiagnosticCode.NameDuplicate,
            ptr,
            `${name} объявлен и как примитив, и как компонент`,
            { name },
          );
        }
        this.component(ptr, component);
      }
    }
    for (const [name, action] of entries(m.actions)) {
      const ptr = pointer("actions", name);
      if ((BUILTIN_ACTIONS as readonly string[]).includes(name)) {
        this.add(DiagnosticCode.NameReserved, ptr, `имя ${name} занято встроенным действием`, {
          name,
        });
      }
      this.fields(ptr + "/args", action.args, false);
      this.capabilityRefs(ptr, action.capabilities);
    }
    for (const [name, source] of entries(m.dataSources)) {
      const ptr = pointer("dataSources", name);
      this.fields(ptr + "/params", source.params, false);
      this.type(ptr + "/result", source.result, false);
      this.capabilityRefs(ptr, source.capabilities);
    }
    for (const [name, formatter] of entries(m.formatters)) {
      this.fields(pointer("formatters", name) + "/args", formatter.args, false);
    }
    for (const [name, schema] of entries(m.schemas)) {
      const ptr = pointer("schemas", name);
      for (const [field] of entries(schema.fields)) {
        if ((RESERVED_FIELDS as readonly string[]).includes(field)) {
          this.add(
            DiagnosticCode.FieldReserved,
            `${ptr}/fields${pointer(field)}`,
            `имя поля ${field} зарезервировано (CNT-001)`,
            { field },
          );
        }
      }
      this.fields(ptr + "/fields", schema.fields, true);
      for (const key of ["titleField", "previewField"] as const) {
        const field = schema.display?.[key];
        if (field !== undefined && !(field in schema.fields)) {
          this.add(
            DiagnosticCode.UnknownField,
            `${ptr}/display/${key}`,
            `поля ${field} нет в схеме`,
            { field },
          );
        }
      }
      for (const [from] of entries(schema.migrateFrom)) {
        if (Number(from) >= schema.version) {
          this.add(
            DiagnosticCode.MigrationInvalid,
            `${ptr}/migrateFrom${pointer(from)}`,
            `миграция с версии ${from} должна быть с версии меньше текущей ${schema.version}`,
            { from: Number(from), version: schema.version },
          );
        }
      }
    }
    return this.out;
  }

  private component(ptr: string, c: Component) {
    this.fields(ptr + "/props", c.props, false);
    for (const [name, slot] of entries(c.slots)) {
      const slotPtr = `${ptr}/slots${pointer(name)}`;
      slot.allowedTypes?.forEach((t, i) => {
        if (!this.types.has(t)) {
          this.add(
            DiagnosticCode.UnknownType,
            `${slotPtr}/allowedTypes/${i}`,
            `тип узла ${t} не объявлен`,
            { type: t },
          );
        }
      });
      this.range(slotPtr, slot.min, slot.max);
    }
    for (const [name, event] of entries(c.events)) {
      this.fields(`${ptr}/events${pointer(name)}/payload`, event.payload, false);
    }
    this.fields(ptr + "/provides", c.provides, false);
    this.capabilityRefs(ptr, c.capabilities);
  }

  private capabilityRefs(ptr: string, refs: string[] | undefined) {
    refs?.forEach((cap, i) => {
      if (!this.capabilities.has(cap)) {
        this.add(
          DiagnosticCode.UnknownCapability,
          `${ptr}/capabilities/${i}`,
          `capability ${cap} не объявлена в capabilities`,
          { capability: cap },
        );
      }
    });
  }

  private fields(ptr: string, fields: Fields | undefined, schemaField: boolean) {
    for (const [name, type] of entries(fields)) {
      this.type(ptr + pointer(name), type, schemaField);
    }
  }

  private range(ptr: string, min: number | undefined, max: number | undefined) {
    if (min !== undefined && max !== undefined && min > max) {
      this.add(DiagnosticCode.RangeInvalid, ptr, `минимум ${min} больше максимума ${max}`, {
        min,
        max,
      });
    }
  }

  private defaultInvalid(ptr: string, message: string) {
    this.add(DiagnosticCode.DefaultInvalid, ptr + "/default", message);
  }

  /** schemaField — поле схемы контента верхнего уровня: только там допустимы localized и unique. */
  private type(ptr: string, t: Type, schemaField: boolean) {
    if (t.content && !(CONTENT_TYPES as readonly string[]).includes(t.type)) {
      this.add(
        DiagnosticCode.ModifierInvalid,
        ptr + "/content",
        `content неприменим к типу ${t.type}`,
      );
    }
    if (t.localized && !schemaField) {
      this.add(
        DiagnosticCode.ModifierInvalid,
        ptr + "/localized",
        "localized допустим только у полей схем контента",
      );
    }
    if (t.unique && (!schemaField || !(UNIQUE_TYPES as readonly string[]).includes(t.type))) {
      this.add(
        DiagnosticCode.ModifierInvalid,
        ptr + "/unique",
        "unique допустим только у полей схем контента типов string, text, number",
      );
    }
    switch (t.type) {
      case "string":
        this.range(ptr, t.minLength, t.maxLength);
        if (t.default !== undefined && !lengthFits(t.default, t.minLength, t.maxLength)) {
          this.defaultInvalid(ptr, "значение по умолчанию не укладывается в ограничения длины");
        }
        break;
      case "text":
        if (t.default !== undefined && !lengthFits(t.default, undefined, t.maxLength)) {
          this.defaultInvalid(ptr, "значение по умолчанию длиннее maxLength");
        }
        break;
      case "number":
        this.range(ptr, t.min, t.max);
        if (
          t.default !== undefined &&
          ((t.min !== undefined && t.default < t.min) ||
            (t.max !== undefined && t.default > t.max) ||
            (t.integer === true && !Number.isInteger(t.default)))
        ) {
          this.defaultInvalid(ptr, "значение по умолчанию вне допустимого диапазона");
        }
        break;
      case "enum":
        if (t.default !== undefined && !t.values.includes(t.default)) {
          this.defaultInvalid(ptr, `значение по умолчанию ${t.default} не входит в values`);
        }
        break;
      case "reference":
        if (!this.schemas.has(t.schema)) {
          this.add(
            DiagnosticCode.UnknownSchema,
            ptr + "/schema",
            `схема ${t.schema} не объявлена в schemas`,
            { schema: t.schema },
          );
        }
        break;
      case "list":
        this.range(ptr, t.min, t.max);
        this.type(ptr + "/of", t.of, false);
        break;
      case "object":
        this.fields(ptr + "/fields", t.fields, false);
        break;
      case "color":
        if (t.default !== undefined && !this.colors.has(t.default)) {
          this.defaultInvalid(ptr, `цветового токена ${t.default} нет в tokens.colors`);
        }
        break;
      case "nodeRef":
        if (t.nodeType !== undefined && !this.types.has(t.nodeType)) {
          this.add(
            DiagnosticCode.UnknownType,
            ptr + "/nodeType",
            `тип узла ${t.nodeType} не объявлен`,
            { type: t.nodeType },
          );
        }
        break;
    }
  }
}

/** Длина в кодовых точках Unicode — так же считает Go (utf8.RuneCountInString). */
function lengthFits(value: string, min: number | undefined, max: number | undefined): boolean {
  const n = [...value].length;
  return (min === undefined || n >= min) && (max === undefined || n <= max);
}
