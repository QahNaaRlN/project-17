export type * from "./generated/types.js";
export { irSchema } from "./generated/schema.js";
export {
  DiagnosticCode,
  nodeIdFromPointer,
  pointer,
  pointerSegment,
  type Diagnostic,
  type Severity,
} from "./diagnostics.js";
export { generateNodeId, isNodeId } from "./ids.js";
export { IR_LIMITS, SUPPORTED_IR_VERSIONS } from "./limits.js";
export {
  normalize,
  toNested,
  NormalizeError,
  type NestedDocument,
  type NestedNode,
  type NormalizeOptions,
} from "./normalize.js";
export { validateDocument, type ValidationResult } from "./validate.js";
