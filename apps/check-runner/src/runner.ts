import { randomUUID } from "node:crypto";
import AxeBuilder from "@axe-core/playwright";
import type { Browser, Page } from "playwright";

/** A leased stage 8–9 check (STU-485). */
export interface LeasedCheck {
  checkId: string;
  leaseId: string;
  stage: "preview" | "axe";
  changesetId: string;
  environment: string;
  appUrl: string;
  pages: { objectId: string; path: string }[];
  previewToken: string;
}
export interface RenderDiagnostic {
  code: string;
  nodeId?: string;
  severity: "error" | "warning";
}
export interface PageRender {
  objectId: string;
  path: string;
  status: number | null;
  diagnostics: RenderDiagnostic[];
  errors: string[];
}
export interface AxeViolation {
  id: string;
  impact: string | null;
  nodes: number;
  targets: string[];
}
export interface CheckResult {
  status: "passed" | "failed" | "warning";
  details: { pages: Record<string, unknown>[]; truncated: boolean };
}
export interface RunnerConfig {
  apiUrl: string;
  token: string;
  project: string;
  /** Per page; STU-490. */
  pageTimeoutMs?: number;
  fetch?: typeof fetch;
  log?: (message: string) => void;
}

const maxDetails = 256 * 1024;
const maxTargets = 20;

/** Thin Command API client of the runner; no token or page body is ever logged. */
export function createClient(config: RunnerConfig) {
  const call = async <T>(name: string, payload: unknown): Promise<T> => {
    const response = await (config.fetch ?? fetch)(
      new URL("/api/v1/commands/" + name, config.apiUrl),
      {
        method: "POST",
        headers: {
          Authorization: "Bearer " + config.token,
          "X-CMS-Project": config.project,
          "Content-Type": "application/json",
          "Idempotency-Key": randomUUID(),
        },
        body: JSON.stringify({ payload }),
        signal: AbortSignal.timeout(30_000),
      },
    );
    const body = (await response.json().catch(() => null)) as {
      result?: T;
      code?: string;
    } | null;
    if (!response.ok) throw new Error(`${name}: ${response.status} ${body?.code ?? "UNKNOWN"}`);
    return body!.result as T;
  };
  return {
    claim: async () => (await call<{ check: LeasedCheck | null }>("claim-check", {})).check,
    complete: (check: LeasedCheck, result: CheckResult) =>
      call<{ changesetState: string }>("complete-check", {
        checkId: check.checkId,
        leaseId: check.leaseId,
        status: result.status,
        details: result.details,
      }),
  };
}

function previewUrl(check: LeasedCheck, path: string): string {
  const url = new URL("/api/cms/preview", check.appUrl);
  url.searchParams.set("token", check.previewToken);
  url.searchParams.set("path", path);
  return url.toString();
}

/** STU-490: renders one page through the application's preview entry. */
export async function renderPage(
  page: Page,
  check: LeasedCheck,
  target: LeasedCheck["pages"][number],
  timeout: number,
): Promise<PageRender> {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message.slice(0, 500)));
  const out: PageRender = { ...target, status: null, diagnostics: [], errors };
  try {
    const response = await page.goto(previewUrl(check, target.path), {
      waitUntil: "load",
      timeout,
    });
    out.status = response?.status() ?? null;
    const raw = await page
      .locator("#cms-render-diagnostics")
      .first()
      .textContent({ timeout: 1000 })
      .catch(() => null);
    if (raw) {
      const parsed = JSON.parse(raw) as RenderDiagnostic[];
      out.diagnostics = Array.isArray(parsed)
        ? parsed.filter((d) => typeof d?.code === "string")
        : [];
    } else if (out.status === 200) {
      errors.push("CMS_RENDER_DIAGNOSTICS_MISSING");
    }
  } catch (error) {
    errors.push(
      error instanceof Error ? error.message.split("\n")[0]!.slice(0, 500) : "RENDER_FAILED",
    );
  }
  return out;
}

export function renderPassed(r: PageRender): boolean {
  return (
    r.status === 200 && r.errors.length === 0 && !r.diagnostics.some((d) => d.severity === "error")
  );
}

/** STU-491: axe-core with WCAG 2.1 AA tags; at most 20 selectors per rule. */
export async function axePage(page: Page): Promise<AxeViolation[]> {
  const result = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  return result.violations.map((v) => ({
    id: v.id,
    impact: v.impact ?? null,
    nodes: v.nodes.length,
    targets: v.nodes.slice(0, maxTargets).map((n) => n.target.join(" ")),
  }));
}

/** STU-491: critical/serious fail; moderate/minor only warn. */
export function axeStatus(
  current: CheckResult["status"],
  violations: AxeViolation[],
): CheckResult["status"] {
  if (
    current === "failed" ||
    violations.some((v) => v.impact === "critical" || v.impact === "serious")
  )
    return "failed";
  return violations.length > 0 ? "warning" : current;
}

/** Keeps details under the 256 KiB contract by dropping trailing pages (STU-498). */
export function bounded(pages: Record<string, unknown>[]): CheckResult["details"] {
  const details = { pages, truncated: false };
  while (JSON.stringify(details).length > maxDetails && details.pages.length > 1) {
    details.pages = details.pages.slice(0, -1);
    details.truncated = true;
  }
  return details;
}

/** Runs one check in an isolated browser context (STU-490…492). */
export async function runCheck(
  browser: Browser,
  check: LeasedCheck,
  timeout = 30_000,
): Promise<CheckResult> {
  const origin = new URL(check.appUrl).origin;
  const context = await browser.newContext();
  try {
    // Navigation stays on the application of the check; page resources load normally.
    await context.route("**/*", (route) =>
      route.request().isNavigationRequest() && new URL(route.request().url()).origin !== origin
        ? route.abort("blockedbyclient")
        : route.continue(),
    );
    const pages: Record<string, unknown>[] = [];
    let status: CheckResult["status"] = "passed";
    for (const target of check.pages) {
      const page = await context.newPage();
      const render = await renderPage(page, check, target, timeout);
      const ok = renderPassed(render);
      if (check.stage === "preview") {
        if (!ok) status = "failed";
        pages.push({ ...render });
      } else if (!ok) {
        pages.push({ objectId: target.objectId, path: target.path, reason: "PREVIEW_FAILED" });
      } else {
        const violations = await axePage(page);
        status = axeStatus(status, violations);
        pages.push({ objectId: target.objectId, path: target.path, violations });
      }
      await page.close();
    }
    return { status, details: bounded(pages) };
  } finally {
    await context.close();
  }
}

/** One claim → run → complete cycle; false when the queue was empty. */
export async function step(
  browser: Browser,
  client: ReturnType<typeof createClient>,
  config: Pick<RunnerConfig, "pageTimeoutMs" | "log">,
): Promise<boolean> {
  const check = await client.claim();
  if (!check) return false;
  const result = await runCheck(browser, check, config.pageTimeoutMs);
  const done = await client.complete(check, result);
  config.log?.(
    `check ${check.checkId} ${check.stage} ${check.environment}: ${result.status}, changeset ${done.changesetState}`,
  );
  return true;
}
