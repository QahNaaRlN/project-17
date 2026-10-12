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
    typeof value.needsAttention !== "boolean" ||
    value.changeset?.id !== id ||
    !Number.isSafeInteger(value.changeset.seq) ||
    value.changeset.seq < 0 ||
    ![
      "open",
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
        typeof c.stage !== "string" ||
        typeof c.status !== "string" ||
        typeof c.blocking !== "boolean",
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
