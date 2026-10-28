import { StudioError, workflowActions, type Review, type ReviewDiagnostic } from "./types.js";
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i;
const code = /^[A-Z0-9_]{1,80}$/;
function object(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}
export function validateReview(value: Review, id: string): Review {
  if (
    !value ||
    typeof value.canEdit !== "boolean" ||
    [value.canEditBehavior, value.canEditCondition].some(
      (flag) => flag !== undefined && typeof flag !== "boolean",
    ) ||
    typeof value.needsAttention !== "boolean" ||
    value.changeset?.id !== id ||
    !Number.isSafeInteger(value.changeset.seq) ||
    value.changeset.seq < 0 ||
    ![
      "open",
      "checking",
      "in_review",
      "approved",
      "failed",
      "changes_requested",
      "merged",
      "abandoned",
    ].includes(value.changeset.state) ||
    ![null, "low", "medium", "high"].includes(value.risk) ||
    !Number.isSafeInteger(value.requiredApprovals) ||
    value.requiredApprovals < 0 ||
    !Number.isSafeInteger(value.approvals) ||
    value.approvals < 0 ||
    [value.requiredRoles, value.missingRequiredRoles].some(
      (v) => v !== undefined && (!Array.isArray(v) || v.some((r) => typeof r !== "string")),
    ) ||
    !Array.isArray(value.objects) ||
    value.objects.some(
      (o) =>
        !o ||
        !uuid.test(o.id) ||
        typeof o.kind !== "string" ||
        !(o.path === null || typeof o.path === "string") ||
        typeof o.deleted !== "boolean",
    ) ||
    !value.actions ||
    workflowActions.some(
      (a) =>
        !(
          value.actions[a] === null ||
          (typeof value.actions[a] === "string" && code.test(value.actions[a]))
        ),
    ) ||
    !Array.isArray(value.checks) ||
    value.checks.some(
      (c) =>
        !c ||
        (c.id !== undefined && !uuid.test(c.id)) ||
        typeof c.stage !== "string" ||
        typeof c.status !== "string" ||
        typeof c.blocking !== "boolean" ||
        (c.environment !== undefined && typeof c.environment !== "string") ||
        (c.stale !== undefined && typeof c.stale !== "boolean"),
    ) ||
    !Array.isArray(value.history) ||
    value.history.some(
      (h) =>
        !h ||
        !uuid.test(h.approverId) ||
        !["approve", "request_changes"].includes(h.decision) ||
        !(h.comment === null || typeof h.comment === "string") ||
        typeof h.valid !== "boolean" ||
        !Number.isFinite(Date.parse(h.createdAt)),
    )
  )
    throw new StudioError("REVIEW_INVALID");
  return value;
}
export interface RenderCheckPage {
  path: string;
  problems: string[];
}
/**
 * STU-494: pages of a stage 8–9 check with their problems — codes, axe rule IDs with
 * impact and node counts, HTTP status. Text only; no markup or URLs from the runner.
 */
export function renderCheckPages(details: unknown): RenderCheckPage[] {
  const pages = object(details).pages;
  if (!Array.isArray(pages)) return [];
  return pages.slice(0, 50).flatMap((raw) => {
    const page = object(raw);
    if (typeof page.path !== "string" || page.path.length > 512) return [];
    const problems: string[] = [];
    if (typeof page.status === "number" && page.status !== 200)
      problems.push("HTTP " + page.status);
    if (typeof page.reason === "string" && code.test(page.reason)) problems.push(page.reason);
    for (const d of Array.isArray(page.diagnostics) ? page.diagnostics : []) {
      const item = object(d);
      if (typeof item.code === "string" && code.test(item.code) && item.severity === "error")
        problems.push(item.code);
    }
    if (Array.isArray(page.errors) && page.errors.length) problems.push("SCRIPT_ERROR");
    for (const v of Array.isArray(page.violations) ? page.violations : []) {
      const item = object(v);
      if (typeof item.id === "string" && /^[a-z0-9-]{1,80}$/.test(item.id))
        problems.push(
          `${item.id} (${typeof item.impact === "string" ? item.impact : "?"}, ${Number(item.nodes) || 0})`,
        );
    }
    return problems.length ? [{ path: page.path, problems: problems.slice(0, 20) }] : [];
  });
}
const artifactName = /^[a-z0-9._-]{1,128}$/;
const ratio = (v: unknown) => (typeof v === "number" && v >= 0 && v <= 1 ? v : null);
export interface VisualWidth {
  width: number;
  baseline: "published" | "none";
  changedRatio: number | null;
  error: boolean;
  artifacts: Partial<Record<"before" | "after" | "diff", string>>;
}
export interface VisualPage {
  path: string;
  widths: VisualWidth[];
  /** The largest share of changed pixels over the widths. */
  changed: number;
}
/**
 * STU-584: stage 10 pages, changed first by the share of changed pixels. Artifact names
 * are checked; nothing else from the runner reaches markup.
 */
export function visualPages(details: unknown): VisualPage[] {
  const pages = object(details).pages;
  if (!Array.isArray(pages)) return [];
  return pages
    .slice(0, 50)
    .flatMap((raw) => {
      const page = object(raw);
      if (typeof page.path !== "string" || page.path.length > 512 || !Array.isArray(page.widths))
        return [];
      const widths = page.widths.slice(0, 3).flatMap((w): VisualWidth[] => {
        const entry = object(w);
        if (![375, 768, 1440].includes(entry.width as number)) return [];
        const files = object(entry.artifacts);
        const artifacts: VisualWidth["artifacts"] = {};
        for (const kind of ["before", "after", "diff"] as const)
          if (typeof files[kind] === "string" && artifactName.test(files[kind]))
            artifacts[kind] = files[kind];
        return [
          {
            width: entry.width as number,
            baseline: entry.baseline === "published" ? "published" : "none",
            changedRatio: ratio(entry.changedRatio),
            error: typeof entry.error === "string",
            artifacts,
          },
        ];
      });
      return [
        {
          path: page.path,
          widths,
          changed: Math.max(0, ...widths.map((w) => w.changedRatio ?? 0)),
        },
      ];
    })
    .sort((a, b) => b.changed - a.changed);
}
export interface LighthousePage {
  path: string;
  scores: Partial<Record<"performance" | "accessibility", number>>;
  report: string | null;
  reason: string | null;
  error: boolean;
}
/** STU-584: stage 11 scores and report names per page. */
export function lighthousePages(details: unknown): LighthousePage[] {
  const pages = object(details).pages;
  if (!Array.isArray(pages)) return [];
  return pages.slice(0, 10).flatMap((raw) => {
    const page = object(raw);
    if (typeof page.path !== "string" || page.path.length > 512) return [];
    const scores: LighthousePage["scores"] = {};
    for (const category of ["performance", "accessibility"] as const) {
      const score = ratio(object(page.scores)[category]);
      if (score !== null) scores[category] = score;
    }
    return [
      {
        path: page.path,
        scores,
        report:
          typeof page.report === "string" && artifactName.test(page.report) ? page.report : null,
        reason: typeof page.reason === "string" && code.test(page.reason) ? page.reason : null,
        error: typeof page.error === "string",
      },
    ];
  });
}
/** STU-581: thresholds recorded with the check. */
export function lighthouseThresholds(
  details: unknown,
): Partial<Record<"performance" | "accessibility", { min: number; blocking: boolean }>> {
  const out: ReturnType<typeof lighthouseThresholds> = {};
  const thresholds = object(object(details).thresholds);
  for (const category of ["performance", "accessibility"] as const) {
    const t = object(thresholds[category]);
    const min = ratio(t.min);
    if (min !== null) out[category] = { min, blocking: t.blocking === true };
  }
  return out;
}
/** STU-575: artifacts are read through the Studio server, never from storage directly. */
export function artifactUrl(changesetId: string, checkId: string, name: string): string | null {
  return uuid.test(changesetId) && uuid.test(checkId) && artifactName.test(name)
    ? `/api/studio/api/v1/changesets/${changesetId}/checks/${checkId}/artifacts/${name}`
    : null;
}
/** The reason a stage 8–9 check has no page result. */
export function renderCheckReason(details: unknown): string | null {
  const reason = object(details).reason;
  return typeof reason === "string" && code.test(reason) ? reason : null;
}
// Never render error detail, raw values, arbitrary params or a server-provided URL.
export function reviewDiagnostics(details: unknown, environment = ""): ReviewDiagnostic[] {
  const source = object(details),
    environments = object(source.environments),
    result: ReviewDiagnostic[] = [];
  if (Object.keys(environments).length) {
    for (const [name, entry] of Object.entries(environments))
      result.push(...reviewDiagnostics(entry, name));
  } else {
    for (const [objectId, entries] of Object.entries(object(source.documents))) {
      if (!uuid.test(objectId) || !Array.isArray(entries)) continue;
      for (const entry of entries) {
        const d = object(entry);
        if (
          typeof d.code !== "string" ||
          !code.test(d.code) ||
          !["error", "warning"].includes(String(d.severity)) ||
          typeof d.pointer !== "string" ||
          d.pointer.length > 4000
        )
          continue;
        result.push({
          objectId,
          environment,
          code: d.code,
          severity: d.severity as "error" | "warning",
          pointer: d.pointer,
          ...(typeof d.nodeId === "string" && d.nodeId.length <= 200 ? { nodeId: d.nodeId } : {}),
        });
      }
    }
  }
  return result;
}
export function problemDiagnostics(params: unknown): ReviewDiagnostic[] {
  const p = object(params);
  return Array.isArray(p.checks)
    ? p.checks.flatMap((c) => reviewDiagnostics(object(c).details))
    : reviewDiagnostics(p);
}
export function policyViolations(
  details: unknown,
): { actorId: string; operation: string; right: string }[] {
  const entries = object(details).violations;
  return Array.isArray(entries)
    ? entries.flatMap((entry) => {
        const v = object(entry),
          name = /^[a-z][a-zA-Z0-9.]{0,79}$/;
        return typeof v.actorId === "string" &&
          uuid.test(v.actorId) &&
          typeof v.operation === "string" &&
          name.test(v.operation) &&
          typeof v.right === "string" &&
          name.test(v.right)
          ? [{ actorId: v.actorId, operation: v.operation, right: v.right }]
          : [];
      })
    : [];
}
/** Agent operations the current policy refuses at review (STU-430). */
export function agentPolicyViolations(
  details: unknown,
): { operation: string; right: string; ruleId: string; condition: string }[] {
  const entries = object(object(details).agentPolicy).violations;
  const text = (v: unknown, max: number) => (typeof v === "string" && v.length <= max ? v : "");
  return Array.isArray(entries)
    ? entries.flatMap((entry) => {
        const v = object(entry);
        const operation = text(v.operation, 80),
          right = text(v.right, 80);
        return operation && right
          ? [{ operation, right, ruleId: text(v.ruleId, 64), condition: text(v.condition, 500) }]
          : [];
      })
    : [];
}
export function safeDiagnostics(value: unknown): ReviewDiagnostic[] {
  return Array.isArray(value)
    ? value.flatMap((entry) => {
        const d = object(entry);
        return typeof d.objectId === "string"
          ? reviewDiagnostics(
              { documents: { [d.objectId]: [d] } },
              typeof d.environment === "string" ? d.environment : "",
            )
          : [];
      })
    : [];
}
export function diagnosticNode(d: ReviewDiagnostic, documentId: string): string | null {
  if (d.objectId !== documentId) return null;
  const part = d.pointer.match(/^\/nodes\/([^/]+)(?:\/|$)/)?.[1];
  return d.nodeId ?? (part ? part.replace(/~1/g, "/").replace(/~0/g, "~") : null);
}
