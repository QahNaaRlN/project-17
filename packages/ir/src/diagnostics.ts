/** Коды диагностики уровня L1 (структура и инварианты IR), см. 02-ir.md §11. */
export const DiagnosticCode = {
  IrVersionUnsupported: "IR_VERSION_UNSUPPORTED",
  SchemaViolation: "IR_SCHEMA_VIOLATION",
  NodeNotFound: "NODE_NOT_FOUND",
  NodeIdMismatch: "NODE_ID_MISMATCH",
  NodeIdDuplicate: "NODE_ID_DUPLICATE",
  NodeOrphan: "NODE_ORPHAN",
  NodeMultipleParents: "NODE_MULTIPLE_PARENTS",
  NodeCycle: "NODE_CYCLE",
  LimitExceeded: "LIMIT_EXCEEDED",
  PropBothStaticAndBound: "PROP_BOTH_STATIC_AND_BOUND",
} as const;

export type DiagnosticCode = (typeof DiagnosticCode)[keyof typeof DiagnosticCode];

export type Severity = "error" | "warning";

export interface Diagnostic {
  code: DiagnosticCode;
  severity: Severity;
  /** JSON Pointer (RFC 6901) на место нарушения в документе. */
  pointer: string;
  nodeId?: string;
  message: string;
  params?: Record<string, unknown>;
}

/** Экранирование сегмента JSON Pointer. */
export function pointerSegment(segment: string | number): string {
  return String(segment).replaceAll("~", "~0").replaceAll("/", "~1");
}

export function pointer(...segments: (string | number)[]): string {
  return segments.map((s) => "/" + pointerSegment(s)).join("");
}

/** ID узла, к которому относится указатель вида /nodes/<id>/…, если он есть. */
export function nodeIdFromPointer(ptr: string): string | undefined {
  const match = /^\/nodes\/([^/]+)/.exec(ptr);
  return match?.[1]?.replaceAll("~1", "/").replaceAll("~0", "~");
}
