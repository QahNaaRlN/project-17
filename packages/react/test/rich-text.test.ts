import { createElement } from "react";
import { renderToString } from "react-dom/server";
import { expect, it } from "vitest";
import { preparePage, type ImageModel } from "@cms/runtime";
import { CmsPage, RichText } from "../src/index.js";
it("STU-102/CNT-030: RichText из CMS рендерит блоки/marks и экранирует текст", () => {
  const value = {
    type: "doc",
    content: [
      { type: "heading", attrs: { level: 2 }, content: [{ type: "text", text: "Rich title" }] },
      {
        type: "paragraph",
        content: [
          {
            type: "text",
            text: "<script>x</script>",
            marks: [{ type: "bold" }, { type: "italic" }],
          },
        ],
      },
    ],
  };
  const plan = preparePage({
    irVersion: "1.0",
    kind: "page",
    root: "n_rich",
    nodes: { n_rich: { id: "n_rich", type: "RichText", props: { value } } },
  });
  expect(plan.diagnostics).toEqual([]);
  const html = renderToString(createElement(CmsPage, { plan }));
  expect(html).toContain("<h2>Rich title</h2>");
  expect(html).toContain("&lt;script&gt;");
  expect(html).toContain("<strong>");
  expect(html).toContain("<em>");
});
it("STU-102: списки/quote/break/divider, все marks, безопасные ссылки и изображения", () => {
  const id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c66";
  const text = {
    type: "text",
    text: "marked",
    marks: [
      { type: "underline" },
      { type: "strike" },
      { type: "code" },
      { type: "link", attrs: { to: { kind: "url", url: "https://example.test" } } },
    ],
  };
  const p = { type: "paragraph", content: [text, { type: "hardBreak" }] };
  const value = {
    type: "doc",
    content: [
      { type: "bulletList", content: [{ type: "listItem", content: [p] }] },
      { type: "orderedList", content: [{ type: "listItem", content: [p] }] },
      { type: "blockquote", content: [p] },
      { type: "divider" },
      { type: "image", attrs: { assetId: id, alt: "picture" } },
    ],
  };
  let html = renderToString(createElement(RichText, { value }));
  for (const tag of ["ul", "ol", "li", "blockquote", "hr", "br", "u", "s", "code", "a"])
    expect(html).toContain("<" + tag);
  expect(html).toContain('href="https://example.test"');
  expect(html).toContain('role="img" aria-label="picture"');
  const image: ImageModel = {
    width: 640,
    height: 400,
    fit: "cover",
    format: "png",
    focalPoint: { x: 0.5, y: 0.5 },
    expiresAt: new Date(Date.now() + 60000).toISOString(),
    variants: [
      {
        width: 640,
        url: "https://images.test/assets/v1/staging/" + id + "/" + "a".repeat(64) + "?sig=test",
      },
    ],
  };
  html = renderToString(createElement(RichText, { value, images: { [id]: image } }));
  expect(html).toContain('src="https://images.test/assets/v1/');
  expect(html).toContain('alt="picture"');
  expect(
    renderToString(
      createElement(RichText, { value, images: { [id]: { ...image, variants: [] } } }),
    ),
  ).toContain('role="img"');
});
// SDK-016: rich text renders without dangerouslySetInnerHTML.
it("STU-102: malformed/HTML отклоняются; page resolver проверяется; anchor учитывает instance", () => {
  for (const value of [
    "<p>html</p>",
    null,
    { type: "doc", content: [{ type: "text", text: "bad" }] },
    { type: "doc", content: [{ type: "image", attrs: { assetId: "bad" } }] },
  ])
    expect(renderToString(createElement(RichText, { value }))).toBe("");
  const mark = (to: unknown) => ({
    type: "doc",
    content: [
      {
        type: "paragraph",
        content: [{ type: "text", text: "link", marks: [{ type: "link", attrs: { to } }] }],
      },
    ],
  });
  const page = mark({ kind: "page", page: "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c66" });
  expect(renderToString(createElement(RichText, { value: page }))).not.toContain("<a");
  expect(
    renderToString(createElement(RichText, { value: page, resolvePage: () => "/article" })),
  ).toContain('href="/article"');
  expect(
    renderToString(
      createElement(RichText, { value: page, resolvePage: () => "javascript:alert(1)" }),
    ),
  ).not.toContain("<a");
  const html = renderToString(
    createElement(CmsPage, {
      plan: {
        css: "",
        diagnostics: [],
        roots: [
          {
            id: "n_rich",
            key: "instance/n_rich",
            type: "RichText",
            props: { value: mark({ kind: "anchor", node: "n_target" }) },
            children: [],
            className: "",
          },
        ],
      },
    }),
  );
  expect(html).toContain('href="#instance%2Fn_target"');
});
