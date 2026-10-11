import { createElement, type ReactNode } from "react";
import { renderToString } from "react-dom/server";
import { expect, it } from "vitest";
import { preparePage, type PlanNode, type RenderPlan } from "@cms/runtime";
import type { IrDocument } from "@cms/ir";
import { CmsPage, type NativeProps } from "../src/index.js";

function plan(
  type: string,
  props: Record<string, unknown> = {},
  children: PlanNode[] = [],
): RenderPlan {
  return {
    roots: [{ id: "node", key: "instance/node", type, props, className: "c-123", children }],
    css: ".c-123{color:#abc}",
    diagnostics: [],
  };
}
it("renders all supported primitive semantics with escaped text and generated CSS", () => {
  for (const [type, props, tag] of [
    ["Heading", { text: "<script>x</script>", level: 1 }, "h1"],
    ["Heading", { text: "default" }, "h2"],
    ["Text", { text: "text" }, "p"],
    ["Text", { text: "span", as: "span" }, "span"],
    ["Label", { text: "label" }, "span"],
    ["Box", { as: "section" }, "section"],
    ["Box", {}, "div"],
    ["Stack", {}, "div"],
    ["Flex", {}, "div"],
    ["Grid", {}, "div"],
    ["Divider", {}, "hr"],
  ] as const) {
    const html = renderToString(createElement(CmsPage, { plan: plan(type, props) }));
    expect(html).toContain(`<${tag} id="instance/node" class="c-123"`);
    expect(html).toContain("<style>.c-123{color:#abc}</style>");
    expect(html).not.toContain("<script>");
    if (type === "Heading" && props.text.startsWith("<")) expect(html).toContain("&lt;script&gt;");
  }
});
it("renders links and native registry components without arbitrary DOM prop spreading", () => {
  const html = renderToString(
    createElement(CmsPage, {
      plan: plan("Link", {
        href: "/safe",
        newTab: true,
        label: "Go",
        onClick: "javascript",
        style: "bad",
      }),
    }),
  );
  expect(html).toContain('href="/safe"');
  expect(html).toContain('target="_blank" rel="noopener noreferrer"');
  expect(html).not.toContain("javascript");
  expect(
    renderToString(createElement(CmsPage, { plan: plan("Link", { href: "/safe" }) })),
  ).not.toContain("target=");
  const child = plan("Text", { text: "Child" }).roots[0]!;
  const Widget = ({ values, children, nodeId, instanceId, className }: NativeProps): ReactNode =>
    createElement(
      "article",
      { className, "data-owned": `${nodeId}/${instanceId}` },
      String(values.title),
      children,
    );
  const native = renderToString(
    createElement(CmsPage, {
      plan: plan("Widget", { title: "Native" }, [child]),
      registry: { Widget },
    }),
  );
  expect(native).toContain('<article class="c-123" data-owned="node/instance/node">Native');
  expect(native).toContain("Child");
  expect(renderToString(createElement(CmsPage, { plan: plan("Unknown") }))).not.toContain(
    "Unknown",
  );
  const empty = plan("constructor");
  empty.css = "";
  empty.roots[0]!.className = "";
  expect(renderToString(createElement(CmsPage, { plan: empty }))).toBe("");
});
it("assigns unique element IDs in Repeat and resolves in-instance anchor links", () => {
  const doc: IrDocument = {
    irVersion: "1.0",
    kind: "page",
    root: "root",
    nodes: {
      root: { id: "root", type: "Repeat", props: { items: [1, 2] }, children: ["link", "text"] },
      link: {
        id: "link",
        type: "Link",
        props: { to: { kind: "anchor", node: "text" }, label: "Jump" },
      },
      text: { id: "text", type: "Text", props: { text: "Target" } },
    },
  };
  const prepared = preparePage(doc);
  expect(prepared.diagnostics).toEqual([]);
  const html = renderToString(createElement(CmsPage, { plan: prepared }));
  for (const target of prepared.roots.filter((n) => n.type === "Text"))
    expect(html).toContain(`id="${target.key}"`);
  expect(new Set([...html.matchAll(/ id="([^"]+)"/g)].map((m) => m[1])).size).toBe(4);
  expect(html).toContain('href="#root%3Anumber%253A0%2Ftext"');
});
it("leaves native SSR exceptions to the application error response", () => {
  const Widget = () => {
    throw new Error("private-native-detail");
  };
  expect(() =>
    renderToString(createElement(CmsPage, { plan: plan("Widget"), registry: { Widget } })),
  ).toThrow("private-native-detail");
});
