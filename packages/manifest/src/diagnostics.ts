/** Коды диагностики manifest (04-manifest.md, 05-content.md §1). */
export const DiagnosticCode = {
  SchemaViolation: "MANIFEST_SCHEMA_VIOLATION",
  IrVersionUnsupported: "MANIFEST_IR_VERSION_UNSUPPORTED",
  NameReserved: "MANIFEST_NAME_RESERVED",
  NameDuplicate: "MANIFEST_NAME_DUPLICATE",
  UnknownType: "MANIFEST_UNKNOWN_TYPE",
  UnknownSchema: "MANIFEST_UNKNOWN_SCHEMA",
  UnknownCapability: "MANIFEST_UNKNOWN_CAPABILITY",
  UnknownBreakpoint: "MANIFEST_UNKNOWN_BREAKPOINT",
  UnknownField: "MANIFEST_UNKNOWN_FIELD",
  DefaultInvalid: "MANIFEST_DEFAULT_INVALID",
  RangeInvalid: "MANIFEST_RANGE_INVALID",
  ModifierInvalid: "MANIFEST_MODIFIER_INVALID",
  FieldReserved: "MANIFEST_FIELD_RESERVED",
  MigrationInvalid: "MANIFEST_MIGRATION_INVALID",
} as const;

export type DiagnosticCode = (typeof DiagnosticCode)[keyof typeof DiagnosticCode];

export interface Diagnostic {
  code: DiagnosticCode;
  severity: "error" | "warning";
  /** JSON Pointer (RFC 6901) на место нарушения в manifest. */
  pointer: string;
  message: string;
  params?: Record<string, unknown>;
}

/** JSON Pointer из сегментов. */
export function pointer(...segments: (string | number)[]): string {
  return segments.map((s) => "/" + String(s).replaceAll("~", "~0").replaceAll("/", "~1")).join("");
}
