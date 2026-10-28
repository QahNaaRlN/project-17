import { createServer } from "node:net";
import lighthouse from "lighthouse";
import pixelmatch from "pixelmatch";
import { PNG } from "pngjs";
import { chromium, type Browser, type BrowserContext } from "playwright";
import {
  bounded,
  checkContext,
  previewUrl,
  renderPage,
  renderPassed,
  type CheckIO,
  type CheckResult,
  type LeasedCheck,
  type Thresholds,
} from "./runner.js";

/** STU-576: screenshot widths of stage 10. */
export const widths = [375, 768, 1440] as const;
const maxHeight = 10_000;

/** Artifact names: `[a-z0-9._-]{1,128}`, unique per page of the check (STU-573). */
export function artifactBase(index: number, path: string): string {
  const slug =
    path
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "")
      .slice(0, 60) || "root";
  return `${String(index).padStart(2, "0")}-${slug}`;
}

export interface Comparison {
  changedRatio: number;
  heights: { before: number; after: number };
  diff: Buffer;
}

/**
 * STU-577: pixelmatch over the common area (colour threshold 0.1, anti-aliasing ignored);
 * the area only one screenshot covers counts as changed and is red in the diff.
 */
export function compare(before: Buffer, after: Buffer): Comparison {
  const a = PNG.sync.read(before);
  const b = PNG.sync.read(after);
  const width = Math.max(a.width, b.width);
  const height = Math.max(a.height, b.height);
  const common = { width: Math.min(a.width, b.width), height: Math.min(a.height, b.height) };
  const crop = (img: PNG) => {
    if (img.width === common.width && img.height === common.height) return img;
    const out = new PNG(common);
    PNG.bitblt(img, out, 0, 0, common.width, common.height, 0, 0);
    return out;
  };
  const overlap = new PNG(common);
  const changed = pixelmatch(
    crop(a).data,
    crop(b).data,
    overlap.data,
    common.width,
    common.height,
    { threshold: 0.1, includeAA: false },
  );
  const diff = new PNG({ width, height });
  for (let i = 0; i < diff.data.length; i += 4) diff.data.set([255, 0, 0, 255], i);
  PNG.bitblt(overlap, diff, 0, 0, common.width, common.height, 0, 0);
  const total = width * height;
  return {
    changedRatio: (changed + total - common.width * common.height) / total,
    heights: { before: a.height, after: b.height },
    diff: PNG.sync.write(diff),
  };
}

async function shoot(context: BrowserContext, url: string, width: number, timeout: number) {
  const page = await context.newPage();
  try {
    await page.setViewportSize({ width, height: 900 });
    const response = await page.goto(url, { waitUntil: "networkidle", timeout });
    await page.evaluate(() => document.fonts.ready);
    const height = await page.evaluate(() => document.documentElement.scrollHeight);
    const png = await page.screenshot({
      fullPage: true,
      animations: "disabled",
      clip: { x: 0, y: 0, width, height: Math.max(1, Math.min(height, maxHeight)) },
      timeout,
    });
    return { status: response?.status() ?? null, png };
  } finally {
    await page.close();
  }
}

const message = (error: unknown) =>
  error instanceof Error ? error.message.split("\n")[0]!.slice(0, 500) : String(error);

/**
 * STU-576…578: the published page without a token against the draft through the preview
 * token, at three widths. Never fails: a screenshot error is a warning.
 */
export async function runVisual(
  browser: Browser,
  check: LeasedCheck,
  io: CheckIO,
  timeout: number,
): Promise<CheckResult> {
  const options = { reducedMotion: "reduce" as const, deviceScaleFactor: 1 };
  const published = await checkContext(browser, check, options);
  const draft = await checkContext(browser, check, options);
  let status: CheckResult["status"] = "passed";
  const pages: Record<string, unknown>[] = [];
  try {
    for (const [index, target] of check.pages.entries()) {
      const base = artifactBase(index, target.path);
      const entries: Record<string, unknown>[] = [];
      for (const width of widths) {
        const entry: Record<string, unknown> = { width };
        const name = (kind: string) => `${base}-${width}-${kind}.png`;
        try {
          const after = await shoot(draft, previewUrl(check, target.path), width, timeout);
          if (after.status !== 200) throw new Error(`draft HTTP ${after.status}`);
          const before = await shoot(
            published,
            new URL(target.path, check.appUrl).toString(),
            width,
            timeout,
          );
          await io.upload(check, name("after"), "image/png", after.png);
          const artifacts: Record<string, string> = { after: name("after") };
          if (before.status !== 200) {
            entry.baseline = "none";
          } else {
            const c = compare(before.png, after.png);
            Object.assign(entry, {
              baseline: "published",
              changedRatio: c.changedRatio,
              heights: c.heights,
            });
            if (c.changedRatio > 0) {
              await io.upload(check, name("before"), "image/png", before.png);
              await io.upload(check, name("diff"), "image/png", c.diff);
              Object.assign(artifacts, { before: name("before"), diff: name("diff") });
            }
          }
          entry.artifacts = artifacts;
        } catch (error) {
          entry.error = message(error);
          status = "warning";
        }
        entries.push(entry);
      }
      pages.push({ objectId: target.objectId, path: target.path, widths: entries });
      await io.extend(check);
    }
  } finally {
    await published.close();
    await draft.close();
  }
  return { status, details: bounded(pages) };
}

/** Decision §11.4: used only when the check carries no thresholds. */
export const defaultThresholds: Thresholds = {
  performance: { min: 0.5, blocking: false },
  accessibility: { min: 0.9, blocking: true },
};

/** STU-581: a score below a blocking threshold fails; below another, warns. */
export function lighthouseStatus(
  current: CheckResult["status"],
  scores: Record<string, number | null>,
  thresholds: Thresholds,
): CheckResult["status"] {
  let status = current;
  for (const [category, t] of Object.entries(thresholds)) {
    const score = scores[category];
    if (score === null || score === undefined || score >= t.min) continue;
    if (t.blocking) status = "failed";
    else if (status === "passed") status = "warning";
  }
  return status;
}

async function freePort(): Promise<number> {
  const server = createServer();
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  const { port } = server.address() as { port: number };
  await new Promise((r) => server.close(r));
  return port;
}

/**
 * STU-579, 580: Lighthouse (performance, accessibility; mobile, simulated throttling) on
 * the draft pages that render. Lighthouse drives its own Chromium so that measurements of
 * concurrent checks do not share a browser.
 */
export async function runLighthouse(
  _browser: Browser,
  check: LeasedCheck,
  io: CheckIO,
  timeout: number,
): Promise<CheckResult> {
  const thresholds = check.thresholds ?? defaultThresholds;
  const blocking = thresholds.performance.blocking || thresholds.accessibility.blocking;
  const port = await freePort();
  const chrome = await chromium.launch({ args: [`--remote-debugging-port=${port}`] });
  let status: CheckResult["status"] = "passed";
  const pages: Record<string, unknown>[] = [];
  try {
    const context = await checkContext(chrome, check);
    for (const [index, target] of check.pages.entries()) {
      const entry: Record<string, unknown> = { objectId: target.objectId, path: target.path };
      pages.push(entry);
      const page = await context.newPage();
      const render = await renderPage(page, check, target, timeout);
      await page.close();
      if (!renderPassed(render)) {
        entry.reason = "PREVIEW_FAILED";
        continue;
      }
      try {
        let timer: NodeJS.Timeout | undefined;
        const run = await Promise.race([
          lighthouse(previewUrl(check, target.path), {
            port,
            output: "html",
            onlyCategories: ["performance", "accessibility"],
            logLevel: "error",
          }),
          new Promise<never>((_, reject) => {
            timer = setTimeout(() => reject(new Error(`timeout ${timeout} ms`)), timeout);
          }),
        ]).finally(() => clearTimeout(timer));
        if (!run) throw new Error("no Lighthouse result");
        const scores = {
          performance: run.lhr.categories.performance?.score ?? null,
          accessibility: run.lhr.categories.accessibility?.score ?? null,
        };
        const report = `${artifactBase(index, target.path)}-lighthouse.html`;
        const html = Array.isArray(run.report) ? run.report[0]! : run.report;
        await io.upload(check, report, "text/html", new TextEncoder().encode(html));
        Object.assign(entry, { scores, report });
        status = lighthouseStatus(status, scores, thresholds);
      } catch (error) {
        entry.error = message(error);
        status = blocking ? "failed" : status === "failed" ? status : "warning";
      }
      await io.extend(check);
    }
    await context.close();
  } finally {
    await chrome.close();
  }
  return { status, details: { ...bounded(pages), thresholds } };
}
