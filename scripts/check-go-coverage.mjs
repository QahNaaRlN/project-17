// Проверка порога покрытия Go (docs/testing.md §4).
// Использование: node scripts/check-go-coverage.mjs <coverage.out> <минимум, %>
import { execFileSync } from "node:child_process";

const [profile, min] = process.argv.slice(2);
if (!profile || !min) {
  console.error("usage: check-go-coverage.mjs <coverage.out> <min-percent>");
  process.exit(2);
}

const report = execFileSync("go", ["tool", "cover", `-func=${profile}`], { encoding: "utf8" });
const total = /^total:\s+\(statements\)\s+([\d.]+)%$/m.exec(report);
if (!total) {
  console.error("Не найдена строка total в отчёте go tool cover");
  process.exit(2);
}
const percent = Number(total[1]);
console.log(`Go coverage (statements): ${percent}% (порог ${min}%)`);
if (percent < Number(min)) {
  console.error(`Покрытие ${percent}% ниже порога ${min}%`);
  process.exit(1);
}
