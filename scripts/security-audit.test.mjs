import { test } from "node:test";
import assert from "node:assert/strict";
import { evaluate } from "./security-audit.mjs";

const now = Date.parse("2026-10-27T00:00:00Z");
const advisory = (severity, id = "GHSA-1") => ({
  github_advisory_id: id,
  module_name: "pkg",
  severity,
  vulnerable_versions: "<1.0.0",
  title: "Issue",
});
const exception = (over = {}) => ({
  id: "GHSA-1",
  package: "pkg",
  reason: "dev only",
  owner: "security",
  expires: "2026-12-01",
  ...over,
});

test("STU-500: high and critical always fail, even when excepted", () => {
  for (const severity of ["high", "critical"])
    assert.equal(
      evaluate({ advisories: { 1: advisory(severity) } }, { exceptions: [exception()] }, now)
        .length >= 1,
      true,
    );
});
test("STU-501: lower findings need a current exception", () => {
  assert.equal(
    evaluate({ advisories: { 1: advisory("moderate") } }, { exceptions: [] }, now).length,
    1,
  );
  assert.deepEqual(
    evaluate({ advisories: { 1: advisory("moderate") } }, { exceptions: [exception()] }, now),
    [],
  );
  assert.deepEqual(evaluate({}, { exceptions: [] }, now), []);
});
test("STU-501: expired, too long, malformed and stale exceptions fail", () => {
  const report = { advisories: { 1: advisory("low") } };
  assert.match(
    evaluate(report, { exceptions: [exception({ expires: "2026-10-01" })] }, now).join(),
    /expired/,
  );
  assert.match(
    evaluate(report, { exceptions: [exception({ expires: "2027-06-01" })] }, now).join(),
    /90 days/,
  );
  assert.match(
    evaluate(report, { exceptions: [exception({ reason: " " })] }, now).join(),
    /required/,
  );
  assert.match(evaluate({}, { exceptions: [exception()] }, now).join(), /no longer matches/);
});
