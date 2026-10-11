import type { Diagnostic } from "@cms/ir";

/** Machine-readable failure. Messages never include credentials or raw server bodies. */
export class CmsError extends Error {
  constructor(
    public readonly code: string,
    public readonly status = 0,
    public readonly diagnostics: readonly Diagnostic[] = [],
  ) {
    super(`CMS request failed (${code})`);
    this.name = "CmsError";
  }
}
