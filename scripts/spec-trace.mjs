// Проверка матрицы прослеживаемости (docs/spec-reconciliation.md, STU-610…625).
// Использование: node scripts/spec-trace.mjs [--draft]
//   без флагов — проверка docs/spec/traceability.md против глав 00–12 и тестов;
//   --draft    — печать черновика матрицы: требования глав и найденные тесты с их ID.
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const specDir = join(root, "docs/spec");
const matrixPath = join(specDir, "traceability.md");

export const STATUSES = ["выполнено", "выполнено с отступлением", "отложено", "снято"];
const NEEDS_ADR = new Set(["выполнено с отступлением", "отложено", "снято"]);
const ID = /^(FR|NFR|IR|DS|MF|CNT|CHG|PUB|API|AGT|SDK|STU|ST|SEC|OPS)-\d{3}$/;

// Требование — строка таблицы главы 00–12, первая ячейка которой — идентификатор.
export function requirements(files) {
  const out = new Map();
  for (const [name, text] of files) {
    const chapter = name.slice(0, 2);
    for (const line of text.split("\n")) {
      const cells = line.split("|").map((c) => c.trim());
      if (cells.length < 4 || !ID.test(cells[1])) continue;
      out.set(cells[1], { chapter, text: cells[2] });
    }
  }
  return out;
}

// Строки матрицы: | ID | Глава | Статус | Доказательство |.
export function parseMatrix(text) {
  const rows = new Map();
  const dup = [];
  for (const line of text.split("\n")) {
    const cells = line.split("|").map((c) => c.trim());
    if (cells.length < 6 || !ID.test(cells[1])) continue;
    if (rows.has(cells[1])) dup.push(cells[1]);
    rows.set(cells[1], { chapter: cells[2], status: cells[3], evidence: cells[4] });
  }
  return { rows, dup };
}

// Идентификаторы в тексте: «IR-060», диапазон «NFR-001…007» и перечень «FR-050/051».
export function mentionedIds(text) {
  const ids = [];
  for (const m of text.matchAll(/\b([A-Z]{2,3})-(\d{3})(?!\d)((?:\s*[…/]\s*\d{3}(?!\d))*)/g)) {
    const [, prefix, first, rest] = m;
    let prev = Number(first);
    const nums = [prev];
    for (const part of rest.matchAll(/([…/])\s*(\d{3})/g)) {
      const n = Number(part[2]);
      if (part[1] === "…") for (let i = prev + 1; i <= n && i - prev <= 50; i++) nums.push(i);
      else nums.push(n);
      prev = n;
    }
    for (const n of nums) ids.push(`${prefix}-${String(n).padStart(3, "0")}`);
  }
  return ids.filter((id) => ID.test(id));
}

// Идентификаторы, встречающиеся в тестах, с файлами.
export function testMentions(files) {
  const out = new Map();
  for (const [path, text] of files) {
    for (const id of mentionedIds(text)) {
      if (!out.has(id)) out.set(id, new Set());
      out.get(id).add(path);
    }
  }
  return out;
}

// STU-619: главы описывают продукт, а ход работ (PR, слияния) — план 14 и документы пакетов.
const PROGRESS = /PR #\d|слит[аоы]? в dev|merge `[0-9a-f]{7}/;

export function progressLines(files) {
  const out = [];
  for (const [name, text] of files)
    text.split("\n").forEach((line, i) => PROGRESS.test(line) && out.push(`${name}:${i + 1}`));
  return out;
}

export function check({ reqs, matrix, tests, linkExists, progress = [] }) {
  const errors = progress.map((at) => `${at}: статус реализации в главе (место — план 14)`);
  const { rows, dup } = matrix;
  for (const id of dup) errors.push(`${id}: повторная строка матрицы`);
  for (const [id, r] of reqs) {
    const row = rows.get(id);
    if (!row) {
      errors.push(`${id}: нет строки в матрице (глава ${r.chapter})`);
      continue;
    }
    if (row.chapter !== r.chapter)
      errors.push(`${id}: глава ${row.chapter}, ожидалась ${r.chapter}`);
  }
  for (const [id, row] of rows) {
    if (!reqs.has(id)) errors.push(`${id}: в матрице, но нет в главах 00–12`);
    if (!STATUSES.includes(row.status)) {
      errors.push(`${id}: статус «${row.status}» не из ${STATUSES.join(", ")}`);
      continue;
    }
    const links = [...row.evidence.matchAll(/\]\(([^)#]+)(#[^)]*)?\)/g)].map((m) => m[1]);
    if (NEEDS_ADR.has(row.status) && !links.some((l) => /\/adr\/\d{4}-/.test(l)))
      errors.push(`${id}: статус «${row.status}» без ссылки на ADR`);
    for (const l of links)
      if (!/^https?:/.test(l) && !linkExists(l)) errors.push(`${id}: битая ссылка ${l}`);
    if (row.status.startsWith("выполнено") && !row.evidence)
      errors.push(`${id}: нет доказательства`);
    if (/^тест:/.test(row.evidence)) {
      const named = [...row.evidence.matchAll(/`([^`]+)`/g)].map((m) => m[1]);
      if (!named.some((f) => tests.get(id)?.has(f)))
        errors.push(`${id}: ID не встречается в указанном тесте ${named.join(", ")}`);
    }
  }
  return errors;
}

// Тесты и conformance-фикстуры, которые эти тесты выполняют на Go и TS.
const TEST_FILE =
  /(_test\.go|\.test\.(ts|tsx|mjs)|\.spec\.ts)$|^perf\/|^packages\/[^/]+\/fixtures\/.*\.json$/;

function load() {
  const chapters = readdirSync(specDir)
    .filter((f) => /^(0\d|1[0-2])-.*\.md$/.test(f))
    .map((f) => [f, readFileSync(join(specDir, f), "utf8")]);
  const listed = execFileSync("git", ["ls-files"], { cwd: root, encoding: "utf8" }).split("\n");
  const tests = listed
    .filter((p) => TEST_FILE.test(p) && existsSync(join(root, p)))
    .map((p) => [p, readFileSync(join(root, p), "utf8")]);
  return {
    reqs: requirements(chapters),
    tests: testMentions(tests),
    progress: progressLines(chapters),
  };
}

function draft({ reqs, tests }) {
  const lines = ["| ID | Глава | Статус | Доказательство |", "|---|---|---|---|"];
  for (const [id, r] of reqs) {
    const files = [...(tests.get(id) ?? [])].slice(0, 2);
    const evidence = files.length ? `тест: ${files.map((f) => `\`${f}\``).join(", ")}` : "";
    lines.push(`| ${id} | ${r.chapter} | ${files.length ? "выполнено" : "?"} | ${evidence} |`);
  }
  return lines.join("\n");
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const data = load();
  if (process.argv.includes("--draft")) {
    console.log(draft(data));
  } else {
    const matrix = parseMatrix(readFileSync(matrixPath, "utf8"));
    const errors = check({
      ...data,
      matrix,
      linkExists: (l) => existsSync(resolve(specDir, decodeURI(l))),
    });
    for (const e of errors) console.error(e);
    const counts = Object.fromEntries(STATUSES.map((s) => [s, 0]));
    for (const row of matrix.rows.values()) if (row.status in counts) counts[row.status]++;
    console.log(
      `Требований: ${data.reqs.size}; ${STATUSES.map((s) => `${s}: ${counts[s]}`).join("; ")}`,
    );
    if (errors.length) process.exit(1);
  }
}
