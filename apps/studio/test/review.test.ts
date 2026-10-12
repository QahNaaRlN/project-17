import { expect, it } from "vitest";
import fc from "fast-check";
import {
  validateReview,
  reviewDiagnostics,
  problemDiagnostics,
  safeDiagnostics,
  diagnosticNode,
  policyViolations,
} from "../src/review.js";
import { fixture, changesetId, documentId } from "./fixture.js";
import { StudioError, type Review } from "../src/types.js";
it("STU-070: valid review and every unknown JSON response are handled safely", () => {
  expect(validateReview(fixture().review, changesetId)).toEqual(fixture().review);
  fc.assert(
    fc.property(fc.jsonValue(), (value) => {
      try {
        validateReview(value as unknown as Review, changesetId);
      } catch (error) {
        expect(error).toBeInstanceOf(StudioError);
      }
      expect(() => reviewDiagnostics(value)).not.toThrow();
      expect(() => problemDiagnostics(value)).not.toThrow();
      expect(() => safeDiagnostics(value)).not.toThrow();
    }),
    { numRuns: 100, seed: 700 },
  );
});
it.each([
  { needsAttention: null },
  { changeset: { id: changesetId, seq: -1, state: "open" } },
  { risk: "unknown" },
  { requiredApprovals: -1 },
  { approvals: 1.2 },
  { requiredRoles: [1] },
  { missingRequiredRoles: null },
  { objects: [null] },
  { objects: [{ id: "bad" }] },
  { actions: {} },
  { checks: [null] },
  { history: [null] },
  {
    history: [
      {
        approverId: changesetId,
        decision: "approve",
        comment: null,
        valid: true,
        createdAt: "bad",
      },
    ],
  },
])("STU-070: malformed review metadata is rejected", (invalid) => {
  expect(() => validateReview({ ...fixture().review, ...invalid } as Review, changesetId)).toThrow(
    "REVIEW_INVALID",
  );
});
it("STU-071: diagnostics are narrowed to codes and paths, scoped by environment and object", () => {
  const diagnostic = {
    code: "PROP_REQUIRED",
    severity: "warning",
    pointer: "/nodes/n_a~1b~0c/props",
    params: { secret: "hidden" },
    message: "hidden",
  };
  const docs = {
    documents: {
      [documentId]: [diagnostic],
      bad: [diagnostic],
      [changesetId]: [
        null,
        { code: "unsafe code" },
        { code: "IR_INVALID", severity: "error", pointer: "/" },
      ],
    },
  };
  const ds = reviewDiagnostics({ environments: { staging: docs } });
  expect(ds).toHaveLength(2);
  expect(JSON.stringify(ds)).not.toContain("hidden");
  expect(diagnosticNode(ds[0]!, documentId)).toBe("n_a/b~c");
  expect(diagnosticNode(ds[0]!, changesetId)).toBeNull();
  expect(diagnosticNode(ds[1]!, changesetId)).toBeNull();
  expect(diagnosticNode({ ...ds[0]!, nodeId: "n_explicit" }, documentId)).toBe("n_explicit");
  expect(problemDiagnostics({ checks: [{ details: docs }, null] })).toHaveLength(2);
  expect(safeDiagnostics([...ds, null, {}, { ...ds[0], pointer: "x".repeat(4001) }])).toEqual(ds);
  expect(reviewDiagnostics({ documents: { [documentId]: "bad" } })).toEqual([]);
  expect(
    policyViolations({
      violations: [
        null,
        { actorId: "bad" },
        {
          actorId: changesetId,
          operation: "node.setDesign",
          right: "design.compose",
          secret: "hidden",
        },
      ],
    }),
  ).toEqual([{ actorId: changesetId, operation: "node.setDesign", right: "design.compose" }]);
  expect(policyViolations(null)).toEqual([]);
});
