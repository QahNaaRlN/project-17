import { useState } from "react";
import {
  artifactUrl,
  lighthousePages,
  lighthouseThresholds,
  visualPages,
  type VisualWidth,
} from "./review.js";

const copy = {
  ru: {
    changed: "изменено",
    unchanged: "без изменений",
    newPage: "новая страница",
    error: "снимок не получен",
    views: { before: "До", after: "После", diff: "Разница" },
    width: "Ширина",
    performance: "Производительность",
    accessibility: "Доступность",
    threshold: "порог",
    blocking: "блокирует",
    report: "Скачать отчёт Lighthouse",
    pending: "Скриншоты ещё снимаются — согласовать можно уже сейчас.",
  },
  en: {
    changed: "changed",
    unchanged: "unchanged",
    newPage: "new page",
    error: "screenshot failed",
    views: { before: "Before", after: "After", diff: "Difference" },
    width: "Width",
    performance: "Performance",
    accessibility: "Accessibility",
    threshold: "threshold",
    blocking: "blocking",
    report: "Download the Lighthouse report",
    pending: "Screenshots are still being taken — approval is already available.",
  },
};
type Locale = keyof typeof copy;
const percent = (v: number) => (v === 0 ? "0%" : v < 0.001 ? "<0.1%" : `${(v * 100).toFixed(1)}%`);

function widthLabel(w: VisualWidth, t: (typeof copy)[Locale]) {
  if (w.error) return t.error;
  if (w.baseline === "none") return t.newPage;
  return w.changedRatio ? `${t.changed} ${percent(w.changedRatio)}` : t.unchanged;
}

/** STU-584: stage 10 — before / after / difference per page and width. */
export function VisualCheck({
  locale,
  changesetId,
  checkId,
  details,
  status,
}: {
  locale: Locale;
  changesetId: string;
  checkId?: string | undefined;
  details: unknown;
  status: string;
}) {
  const t = copy[locale];
  const pages = visualPages(details);
  const [open, setOpen] = useState<{ path: string; width: number } | null>(null);
  const [view, setView] = useState<"before" | "after" | "diff">("diff");
  if (status === "queued" || status === "running") return <p className="hint">{t.pending}</p>;
  if (!checkId) return null;
  return (
    <div className="visual-check">
      {pages.map((page) => (
        <div key={page.path} className="visual-page">
          <code>{page.path}</code>{" "}
          {page.widths.map((w) => (
            <button
              key={w.width}
              type="button"
              aria-pressed={open?.path === page.path && open.width === w.width}
              onClick={() => {
                setOpen({ path: page.path, width: w.width });
                setView(w.artifacts.diff ? "diff" : "after");
              }}
            >
              {w.width}px: {widthLabel(w, t)}
            </button>
          ))}
          {open?.path === page.path &&
            (() => {
              const w = page.widths.find((x) => x.width === open.width);
              const name = w?.artifacts[view] ?? w?.artifacts.after;
              const src = name ? artifactUrl(changesetId, checkId, name) : null;
              return (
                w && (
                  <figure>
                    <div role="group" aria-label={`${t.width} ${w.width}px`}>
                      {(["before", "after", "diff"] as const)
                        .filter((kind) => w.artifacts[kind])
                        .map((kind) => (
                          <button
                            key={kind}
                            type="button"
                            aria-pressed={view === kind}
                            onClick={() => setView(kind)}
                          >
                            {t.views[kind]}
                          </button>
                        ))}
                    </div>
                    {src && (
                      <img
                        src={src}
                        alt={`${page.path} · ${w.width}px · ${t.views[view]}`}
                        loading="lazy"
                      />
                    )}
                  </figure>
                )
              );
            })()}
        </div>
      ))}
    </div>
  );
}

/** STU-584: stage 11 — scores against the thresholds and the report download. */
export function LighthouseCheck({
  locale,
  changesetId,
  checkId,
  details,
}: {
  locale: Locale;
  changesetId: string;
  checkId?: string | undefined;
  details: unknown;
}) {
  const t = copy[locale];
  const thresholds = lighthouseThresholds(details);
  return (
    <div className="lighthouse-check">
      {lighthousePages(details).map((page) => {
        const report =
          checkId && page.report ? artifactUrl(changesetId, checkId, page.report) : null;
        return (
          <div key={page.path}>
            <code>{page.path}</code>
            {page.reason && <code> {page.reason}</code>}
            {(["performance", "accessibility"] as const)
              .filter((c) => page.scores[c] !== undefined)
              .map((c) => {
                const score = page.scores[c]!;
                const limit = thresholds[c];
                return (
                  <span key={c} data-below={limit && score < limit.min ? "true" : undefined}>
                    {" "}
                    · {t[c]}: {Math.round(score * 100)}
                    {limit &&
                      ` (${t.threshold} ${Math.round(limit.min * 100)}${limit.blocking ? `, ${t.blocking}` : ""})`}
                  </span>
                );
              })}
            {report && (
              <>
                {" "}
                ·{" "}
                <a href={report} download>
                  {t.report}
                </a>
              </>
            )}
          </div>
        );
      })}
    </div>
  );
}
