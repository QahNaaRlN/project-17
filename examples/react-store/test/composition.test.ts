import { expect, it, vi } from "vitest";
import { preparePage } from "@cms/runtime";
import { storeComposition, definitionsFor } from "../src/composition.js";
import { loadStore } from "../src/load.js";
import { cms, page } from "./fixtures.js";
import { catalog } from "../scripts/catalog.js";
import { storeContracts } from "../scripts/catalog.js";
import { validateManifest } from "@cms/manifest";
import { renderToString } from "react-dom/server";
import { createElement } from "react";
import { StoreApp } from "../src/app.js";
import { model } from "./fixtures.js";

it("STORE-001: storefront manifest accepts native contracts and declares the store context", () => {
  const manifest = {
    manifestVersion: "1.0",
    app: { id: "store-test", version: "1.0.0", framework: "react" },
    irVersions: ["1.0"],
    breakpoints: { sm: 640, md: 768, lg: 1024 },
    tokens: {},
    components: storeContracts,
  };
  expect(validateManifest(manifest).diagnostics).toEqual([]);
  expect(storeContracts.Storefront.provides.store.fields.products.type).toBe("list");
});
it("DS-030: native components apply generated classes, including the root wrapper", () => {
  const initial = model();
  const response = page(initial.catalog);
  for (const id of ["n_store", "n_product", "n_empty"])
    response.document.nodes[id]!.design = { padding: { raw: 12 } };
  const plan = preparePage(storeComposition(response.document, initial.catalog!), {
    natives: definitionsFor(initial.catalog!),
    scope: { context: { store: initial.catalog } },
  });
  expect(plan.diagnostics).toEqual([]);
  const html = renderToString(createElement(StoreApp, { initialModel: { ...initial, plan } }));
  expect(html).toMatch(/<div id="n_store" class="c-[0-9a-f]+">/);
  expect(html).toMatch(/<article class="product-card c-[0-9a-f]+">/);
});

it("SDK-013/STORE-007: drops unused root config and private metadata from browser model", async () => {
  const client = cms();
  vi.mocked(client.getPage).mockResolvedValue(page({ ...catalog, privateNote: "not-for-browser" }));
  const result = await loadStore(client, "/store");
  expect(result.status).toBe("ready");
  expect(result.plan!.roots[0]!.props).toEqual({});
  expect(JSON.stringify(result)).not.toContain("not-for-browser");
});
it("SDK-013: invalid native bindings reject the example composition with a sanitized page error", async () => {
  const client = cms();
  for (const [id, bindings] of [
    ["n_product", { product: "$content" }],
    ["n_collection", { title: "$content.collectionTitle", products: "$content.title" }],
    ["n_hero", { title: "$content.products" }],
    ["n_about", { title: "$content.products", text: "$content.aboutText" }],
  ] as const) {
    const response = page();
    response.document.nodes[id]!.bindings = bindings;
    vi.mocked(client.getPage).mockResolvedValue(response);
    expect(await loadStore(client, "/store")).toEqual({
      status: "error",
      preview: false,
      images: {},
    });
  }
});
it("STORE-001: reads the historical one-node draft without mutating it", () => {
  const response = page();
  delete response.document.nodes.n_store!.children;
  const before = JSON.stringify(response.document);
  const doc = storeComposition(response.document, catalog);
  const plan = preparePage(doc, {
    natives: definitionsFor(catalog),
    scope: { context: { store: catalog } },
  });
  expect(plan.diagnostics).toEqual([]);
  expect(plan.roots[0]!.children.map((n) => n.type)).toEqual([
    "StoreHero",
    "StoreCollection",
    "StoreAbout",
  ]);
  expect(JSON.stringify(response.document)).toBe(before);
});
