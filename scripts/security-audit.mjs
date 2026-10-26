// STU-500/501: `pnpm audit` gate of the Security job. High and critical findings
// always fail; lower ones fail unless security/exceptions.json accepts them until
// an expiry within 90 days. Expired or malformed exceptions fail as well.
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";

const repo = fileURLToPath(new URL("..", import.meta.url));
const day = 24 * 60 * 60 * 1000;

/** Returns the problems of an audit report against the exceptions file. */
export function evaluate(report, exceptions, now = Date.now()) {
  const problems = [];
  const accepted = new Map();
  for (const e of exceptions.exceptions ?? []) {
    const expires = Date.parse(e?.expires);
    if (
      typeof e?.id !== "string" ||
      typeof e.package !== "string" ||
      typeof e.reason !== "string" ||
      !e.reason.trim() ||
      typeof e.owner !== "string" ||
      !Number.isFinite(expires)
    )
      problems.push(
        `exception ${JSON.stringify(e?.id)}: id, package, reason, owner and expires are required`,
      );
    else if (expires < now) problems.push(`exception ${e.id} expired on ${e.expires}`);
    else if (expires - now > 90 * day) problems.push(`exception ${e.id} lasts longer than 90 days`);
    else accepted.set(`${e.id}|${e.package}`, e);
  }
  for (const a of Object.values(report.advisories ?? {})) {
    const id = String(a.github_advisory_id ?? a.id);
    const key = `${id}|${a.module_name}`;
    const finding = `${a.severity} ${a.module_name} ${a.vulnerable_versions}: ${a.title} (${id})`;
    if (a.severity === "high" || a.severity === "critical") problems.push(finding);
    else if (!accepted.has(key))
      problems.push(finding + " — not accepted in security/exceptions.json");
  }
  for (const [key, e] of accepted)
    if (
      !Object.values(report.advisories ?? {}).some(
        (a) => `${String(a.github_advisory_id ?? a.id)}|${a.module_name}` === key,
      )
    )
      problems.push(`exception ${e.id} for ${e.package} no longer matches a finding; remove it`);
  return problems;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  let raw;
  try {
    raw = execFileSync("pnpm", ["audit", "--json"], {
      cwd: repo,
      encoding: "utf8",
      shell: process.platform === "win32",
      maxBuffer: 64 * 1024 * 1024,
    });
  } catch (error) {
    // pnpm audit exits non-zero when it finds vulnerabilities; the report is on stdout.
    raw = error.stdout;
  }
  const exceptions = JSON.parse(
    readFileSync(new URL("../security/exceptions.json", import.meta.url), "utf8"),
  );
  const problems = evaluate(JSON.parse(raw), exceptions);
  for (const p of problems) console.error(p);
  console.log(problems.length ? `pnpm audit: ${problems.length} problem(s)` : "pnpm audit: clean");
  process.exit(problems.length ? 1 : 0);
}
