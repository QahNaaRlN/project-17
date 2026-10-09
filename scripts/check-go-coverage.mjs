// Проверка порога покрытия Go (docs/testing.md §4).
// Использование: node scripts/check-go-coverage.mjs <coverage.out> <минимум, %> [--exclude <regexp>]...
// Исключаются сгенерированный код и тестовая инфраструктура (пути, совпадающие с --exclude).
import { readFileSync } from "node:fs";

const args = process.argv.slice(2);
const excludes = [];
const positional = [];
for (let i = 0; i < args.length; i++) {
  if (args[i] === "--exclude") excludes.push(new RegExp(args[++i]));
  else positional.push(args[i]);
}
const [profile, min] = positional;
if (!profile || !min) {
  console.error(
    "usage: check-go-coverage.mjs <coverage.out> <min-percent> [--exclude <regexp>]...",
  );
  process.exit(2);
}

// Строка профиля: <файл>:<начало>,<конец> <число инструкций> <число выполнений>.
// С -coverpkg один блок встречается в профиле каждого тестового пакета — объединяем по максимуму.
const blocks = new Map();
for (const line of readFileSync(profile, "utf8").split("\n").slice(1)) {
  const m = /^(.+):(\d+\.\d+,\d+\.\d+) (\d+) (\d+)$/.exec(line);
  if (!m) continue;
  const [, file, range, stmts, count] = m;
  if (excludes.some((re) => re.test(file))) continue;
  const key = `${file}:${range}`;
  const prev = blocks.get(key);
  blocks.set(key, { stmts: Number(stmts), covered: (prev?.covered ?? false) || Number(count) > 0 });
}

let total = 0;
let covered = 0;
for (const b of blocks.values()) {
  total += b.stmts;
  if (b.covered) covered += b.stmts;
}
if (total === 0) {
  console.error("В профиле нет инструкций после исключений");
  process.exit(2);
}
const percent = Math.round((covered / total) * 1000) / 10;
console.log(
  `Go coverage (statements): ${percent}% (порог ${min}%, исключено: ${excludes.map(String).join(", ") || "—"})`,
);
if (percent < Number(min)) {
  console.error(`Покрытие ${percent}% ниже порога ${min}%`);
  process.exit(1);
}
