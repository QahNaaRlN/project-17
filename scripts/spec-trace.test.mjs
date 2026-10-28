import assert from "node:assert/strict";
import { test } from "node:test";

import {
  check,
  mentionedIds,
  parseMatrix,
  progressLines,
  requirements,
  testMentions,
} from "./spec-trace.mjs";

const chapters = [
  [
    "01-requirements.md",
    "| ID | Требование |\n|---|---|\n| FR-001 | Command Bus. |\n| NFR-008 | Доступность | 99,9 % |\nтекст FR-002 вне таблицы",
  ],
  ["02-ir.md", "| IR-010 | `root` в `nodes`. |"],
];

function matrix(rows) {
  return parseMatrix(
    ["| ID | Глава | Статус | Доказательство |", "|---|---|---|---|", ...rows].join("\n"),
  );
}

const ok = [
  "| FR-001 | 01 | выполнено | тест: `a_test.go` |",
  "| NFR-008 | 01 | отложено | [ADR-0001](../adr/0001-production-ops.md) |",
  "| IR-010 | 02 | выполнено | По построению: проверка L1 |",
];
const tests = testMentions([["a_test.go", "// FR-001: all writes go through the bus."]]);

test("STU-610: requirements are the table rows of chapters 00–12", () => {
  assert.deepEqual([...requirements(chapters).keys()], ["FR-001", "NFR-008", "IR-010"]);
  assert.equal(requirements(chapters).get("IR-010").chapter, "02");
});

test("STU-612: ranges and lists of IDs in tests are expanded", () => {
  assert.deepEqual(mentionedIds("NFR-001…003, FR-050/051, IR-060 … 061; STU-9999"), [
    "NFR-001",
    "NFR-002",
    "NFR-003",
    "FR-050",
    "FR-051",
    "IR-060",
    "IR-061",
  ]);
});

test("STU-623: a complete matrix passes", () => {
  const errors = check({
    reqs: requirements(chapters),
    matrix: matrix(ok),
    tests,
    linkExists: () => true,
  });
  assert.deepEqual(errors, []);
});

test("STU-623/625: missing, unknown, duplicate and misplaced rows fail", () => {
  const errors = check({
    reqs: requirements(chapters),
    matrix: matrix([
      ok[0],
      ok[0],
      "| IR-010 | 03 | выполнено | x |",
      "| SEC-099 | 12 | выполнено | x |",
    ]),
    tests,
    linkExists: () => true,
  });
  assert.deepEqual(errors, [
    "FR-001: повторная строка матрицы",
    "NFR-008: нет строки в матрице (глава 01)",
    "IR-010: глава 03, ожидалась 02",
    "SEC-099: в матрице, но нет в главах 00–12",
  ]);
});

test("STU-611/614: statuses, ADR links and test evidence are checked", () => {
  const errors = check({
    reqs: requirements(chapters),
    matrix: matrix([
      "| FR-001 | 01 | выполнено | тест: `b_test.go` |",
      "| NFR-008 | 01 | отложено | [план](../spec/14-mvp-plan.md) |",
      "| IR-010 | 02 | готово | x |",
    ]),
    tests,
    linkExists: (l) => !l.includes("14-mvp"),
  });
  assert.deepEqual(errors, [
    "FR-001: ID не встречается в указанном тесте b_test.go",
    "NFR-008: статус «отложено» без ссылки на ADR",
    "NFR-008: битая ссылка ../spec/14-mvp-plan.md",
    "IR-010: статус «готово» не из выполнено, выполнено с отступлением, отложено, снято",
  ]);
  const empty = check({
    reqs: requirements(chapters),
    matrix: matrix([ok[0], ok[1], "| IR-010 | 02 | выполнено |  |"]),
    tests,
    linkExists: () => true,
  });
  assert.deepEqual(empty, ["IR-010: нет доказательства"]);
});

test("STU-619: progress notes in chapters fail", () => {
  const progress = progressLines([
    ["08-api.md", "Описание.\nСлит в dev в [PR #57](x).\nmerge `35ccee4`"],
  ]);
  assert.deepEqual(progress, ["08-api.md:2", "08-api.md:3"]);
  const errors = check({
    reqs: new Map(),
    matrix: matrix([]),
    tests,
    linkExists: () => true,
    progress,
  });
  assert.equal(errors[0], "08-api.md:2: статус реализации в главе (место — план 14)");
});
