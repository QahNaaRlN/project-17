import { expect, it } from "vitest";
import fc from "fast-check";
import {
  validateReview,
  reviewDiagnostics,
  problemDiagnostics,
  safeDiagnostics,
  diagnosticNode,
  policyViolations,
  renderCheckPages,
  renderCheckReason,
  visualPages,
  lighthousePages,
  lighthouseThresholds,
  artifactUrl,
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
it("STU-494: render check details become safe text per page", () => {
  const details = {
    pages: [
      { objectId: documentId, path: "/ok", status: 200, diagnostics: [], errors: [] },
      {
        path: "/broken",
        status: 500,
        diagnostics: [
          { code: "CMS_COMPONENT_MISSING", severity: "error" },
          { code: "CMS_IMAGE_UNAVAILABLE", severity: "warning" },
          { code: "<b>", severity: "error" },
        ],
        errors: ["TypeError: <script>"],
      },
      {
        path: "/a11y",
        violations: [{ id: "button-name", impact: "serious", nodes: 2 }, { id: "<x>" }],
      },
      { path: "/later", reason: "PREVIEW_FAILED" },
      { path: "x".repeat(600), reason: "PREVIEW_FAILED" },
      null,
    ],
  };
  expect(renderCheckPages(details)).toEqual([
    { path: "/broken", problems: ["HTTP 500", "CMS_COMPONENT_MISSING", "SCRIPT_ERROR"] },
    { path: "/a11y", problems: ["button-name (serious, 2)"] },
    { path: "/later", problems: ["PREVIEW_FAILED"] },
  ]);
  expect(renderCheckPages({ pages: "x" })).toEqual([]);
  expect(renderCheckPages(null)).toEqual([]);
  expect(renderCheckReason({ reason: "CHECK_APP_URL_MISSING" })).toBe("CHECK_APP_URL_MISSING");
  expect(renderCheckReason({ reason: "<b>" })).toBeNull();
  const review = fixture().review;
  expect(
    validateReview(
      {
        ...review,
        changeset: { ...review.changeset, state: "checking" },
        checks: [
          {
            stage: "preview",
            status: "queued",
            blocking: true,
            details: {},
            environment: "staging",
            stale: true,
          },
        ],
      },
      changesetId,
    ).changeset.state,
  ).toBe("checking");
  for (const check of [{ environment: 1 }, { stale: "yes" }])
    expect(() =>
      validateReview(
        {
          ...review,
          checks: [
            { stage: "axe", status: "queued", blocking: true, details: {}, ...check } as never,
          ],
        },
        changesetId,
      ),
    ).toThrow(StudioError);
});
it("STU-584: stage 10–11 details are parsed into safe pages, changed pages first", () => {
  const details = {
    pages: [
      {
        path: "/same",
        widths: [
          {
            width: 375,
            baseline: "published",
            changedRatio: 0,
            artifacts: { after: "00-same-375-after.png" },
          },
        ],
      },
      {
        path: "/shop",
        widths: [
          {
            width: 375,
            baseline: "published",
            changedRatio: 0.25,
            artifacts: {
              before: "01-shop-375-before.png",
              after: "01-shop-375-after.png",
              diff: "../x",
            },
          },
          { width: 768, error: "timeout" },
          { width: 999, baseline: "published" },
        ],
      },
      {
        path: "/new",
        widths: [{ width: 1440, baseline: "none", artifacts: { after: "02-new-1440-after.png" } }],
      },
      { path: 1, widths: [] },
    ],
  };
  const pages = visualPages(details);
  expect(pages.map((p) => p.path)).toEqual(["/shop", "/same", "/new"]);
  expect(pages[0]!.widths).toEqual([
    {
      width: 375,
      baseline: "published",
      changedRatio: 0.25,
      error: false,
      artifacts: { before: "01-shop-375-before.png", after: "01-shop-375-after.png" },
    },
    { width: 768, baseline: "none", changedRatio: null, error: true, artifacts: {} },
  ]);
  expect(visualPages(null)).toEqual([]);
  const lighthouse = {
    pages: [
      {
        path: "/shop",
        scores: { performance: 0.42, accessibility: 2 },
        report: "00-shop-lighthouse.html",
      },
      { path: "/broken", reason: "PREVIEW_FAILED", report: "<x>" },
      { path: "/slow", error: "timeout" },
    ],
    thresholds: {
      performance: { min: 0.5, blocking: false },
      accessibility: { min: 0.9, blocking: true },
    },
  };
  expect(lighthousePages(lighthouse)).toEqual([
    {
      path: "/shop",
      scores: { performance: 0.42 },
      report: "00-shop-lighthouse.html",
      reason: null,
      error: false,
    },
    { path: "/broken", scores: {}, report: null, reason: "PREVIEW_FAILED", error: false },
    { path: "/slow", scores: {}, report: null, reason: null, error: true },
  ]);
  expect(lighthousePages({})).toEqual([]);
  expect(lighthouseThresholds(lighthouse)).toEqual(lighthouse.thresholds);
  expect(lighthouseThresholds({ thresholds: { performance: { min: 5 } } })).toEqual({});
  const check = "0193a5d6-7a8b-7c9d-8e0f-123456789abc";
  expect(artifactUrl(changesetId, check, "a.png")).toBe(
    `/api/studio/api/v1/changesets/${changesetId}/checks/${check}/artifacts/a.png`,
  );
  expect(artifactUrl(changesetId, check, "../a.png")).toBeNull();
  expect(artifactUrl("bad", check, "a.png")).toBeNull();
  const review = fixture().review;
  expect(() =>
    validateReview(
      {
        ...review,
        checks: [{ id: "bad", stage: "visual", status: "queued", blocking: false, details: {} }],
      },
      changesetId,
    ),
  ).toThrow(StudioError);
});
