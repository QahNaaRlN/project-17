// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { LighthouseCheck, VisualCheck } from "../src/check-artifacts.js";
import { changesetId } from "./fixture.js";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const check = "0193a5d6-7a8b-7c9d-8e0f-123456789abc";
let root: Root | null = null;
afterEach(() => {
  act(() => root?.unmount());
  document.body.innerHTML = "";
});
function mount(node: React.ReactNode) {
  const host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root!.render(node));
  return host;
}
const click = (el: Element) => act(() => (el as HTMLElement).click());

it("STU-584: stage 10 shows changed pages and switches before / after / difference", () => {
  const host = mount(
    <VisualCheck
      locale="en"
      changesetId={changesetId}
      checkId={check}
      status="passed"
      details={{
        pages: [
          {
            path: "/shop",
            widths: [
              {
                width: 375,
                baseline: "published",
                changedRatio: 0.0005,
                artifacts: {
                  before: "00-shop-375-before.png",
                  after: "00-shop-375-after.png",
                  diff: "00-shop-375-diff.png",
                },
              },
              {
                width: 768,
                baseline: "published",
                changedRatio: 0,
                artifacts: { after: "00-shop-768-after.png" },
              },
              { width: 1440, error: "timeout" },
            ],
          },
          {
            path: "/new",
            widths: [
              { width: 375, baseline: "none", artifacts: { after: "01-new-375-after.png" } },
            ],
          },
        ],
      }}
    />,
  );
  const buttons = [...host.querySelectorAll("button")].map((b) => b.textContent);
  expect(buttons).toEqual([
    "375px: changed <0.1%",
    "768px: unchanged",
    "1440px: screenshot failed",
    "375px: new page",
  ]);
  click(host.querySelector("button")!);
  const img = () => host.querySelector("img")!;
  expect(img().getAttribute("src")).toBe(
    `/api/studio/api/v1/changesets/${changesetId}/checks/${check}/artifacts/00-shop-375-diff.png`,
  );
  click([...host.querySelectorAll("figure button")].find((b) => b.textContent === "Before")!);
  expect(img().getAttribute("src")).toContain("00-shop-375-before.png");
  expect(img().getAttribute("alt")).toBe("/shop · 375px · Before");
  click([...host.querySelectorAll("button")].find((b) => b.textContent === "768px: unchanged")!);
  expect(img().getAttribute("src")).toContain("00-shop-768-after.png");
  expect(host.querySelectorAll("figure button")).toHaveLength(1);
});

it("STU-571/584: a running stage 10 does not hold review; without a check id nothing links", () => {
  expect(
    mount(
      <VisualCheck
        locale="ru"
        changesetId={changesetId}
        checkId={check}
        status="running"
        details={{}}
      />,
    ).textContent,
  ).toContain("согласовать можно уже сейчас");
  act(() => root!.unmount());
  expect(
    mount(
      <VisualCheck locale="ru" changesetId={changesetId} status="passed" details={{ pages: [] }} />,
    ).innerHTML,
  ).toBe("");
});

it("STU-584: stage 11 shows scores against thresholds and links the report download", () => {
  const host = mount(
    <LighthouseCheck
      locale="en"
      changesetId={changesetId}
      checkId={check}
      details={{
        pages: [
          {
            path: "/shop",
            scores: { performance: 0.42, accessibility: 0.95 },
            report: "00-shop-lighthouse.html",
          },
          { path: "/broken", reason: "PREVIEW_FAILED" },
        ],
        thresholds: {
          performance: { min: 0.5, blocking: false },
          accessibility: { min: 0.9, blocking: true },
        },
      }}
    />,
  );
  expect(host.textContent).toContain("Performance: 42 (threshold 50)");
  expect(host.textContent).toContain("Accessibility: 95 (threshold 90, blocking)");
  expect(host.querySelector("[data-below]")!.textContent).toContain("Performance");
  const link = host.querySelector("a")!;
  expect(link.getAttribute("href")).toContain("00-shop-lighthouse.html");
  expect(link.hasAttribute("download")).toBe(true);
  expect(host.textContent).toContain("PREVIEW_FAILED");
});
