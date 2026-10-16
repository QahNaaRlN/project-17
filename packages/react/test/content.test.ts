import { expect, it, vi } from "vitest";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import type { CmsClient, PageResponse } from "@cms/core";
import { loadPage } from "../src/server.js";
import { CmsPage } from "../src/page.js";
it("STU-210/211/212: SSR получает content до плана и отдельную подписанную image model", async () => {
  const id = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
  const page: PageResponse = {
    page: { objectId: id, versionId: id, path: "/", params: {} },
    document: {
      irVersion: "1.0",
      kind: "page",
      root: "n_root",
      nodes: {
        n_root: { id: "n_root", type: "Box", children: ["n_text", "n_image"] },
        n_text: { id: "n_text", type: "Heading", bindings: { text: "$content.title" } },
        n_image: {
          id: "n_image",
          type: "Image",
          bindings: { src: "$content.cover", alt: "$content.alt" },
        },
      },
    },
    components: {},
    data: { content: { title: "Heading", cover: { assetId: id }, alt: "Cover" } },
    dataSources: {},
    contentContext: { locale: "en", versions: { [id]: id } },
  };
  const getImage = vi.fn().mockResolvedValue({
    width: 320,
    height: 200,
    fit: "contain",
    format: "webp",
    focalPoint: { x: 0.5, y: 0.5 },
    expiresAt: "2030-01-01",
    variants: [{ width: 320, url: "https://images.test/assets/v1/staging/test/hash?sig=signed" }],
  });
  const cms = { getPage: vi.fn().mockResolvedValue(page), getImage } as unknown as CmsClient;
  const plan = (await loadPage(cms, "/", { read: { locale: "en" } }))!;
  expect(plan.diagnostics).toEqual([]);
  const html = renderToStaticMarkup(createElement(CmsPage, { plan }));
  expect(html).toContain("Heading");
  expect(html).toContain('alt="Cover"');
  expect(html).toContain("sig=signed");
  expect(getImage).toHaveBeenCalledWith(id, { locale: "en", fit: "contain" });
  expect(page.document.nodes.n_image!.bindings).toEqual({
    src: "$content.cover",
    alt: "$content.alt",
  });
  getImage.mockRejectedValue(new Error("private storage key"));
  const missing = (await loadPage(cms, "/"))!;
  expect(missing.diagnostics).toContainEqual({ code: "CMS_IMAGE_UNAVAILABLE", nodeId: "n_image" });
  expect(renderToStaticMarkup(createElement(CmsPage, { plan: missing }))).toContain(
    'aria-label="Cover"',
  );
  expect(JSON.stringify(missing)).not.toContain("private storage key");
  getImage.mockClear();
  for (const value of [undefined, null]) {
    page.data.content = { title: "Heading", cover: value, alt: "Cover" };
    const absent = (await loadPage(cms, "/"))!;
    const html = renderToStaticMarkup(createElement(CmsPage, { plan: absent }));
    expect(html).toContain("Heading");
    expect(html).toContain('aria-label="Cover"');
    expect(absent.diagnostics).toContainEqual({ code: "CMS_IMAGE_UNAVAILABLE", nodeId: "n_image" });
  }
  expect(getImage).not.toHaveBeenCalled();
});
