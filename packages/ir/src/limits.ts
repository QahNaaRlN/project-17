/** Ограничения документа IR (NFR-011…013, 02-ir.md §6.4). */
export const IR_LIMITS = {
  maxNodes: 5000,
  maxDepth: 32,
  maxBodyBytes: 2 * 1024 * 1024,
  maxPredicateDepth: 8,
} as const;

export const SUPPORTED_IR_VERSIONS = ["1.0"] as const;
