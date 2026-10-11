import { expect, it, vi } from "vitest";
import { preparePageWithData } from "@cms/runtime";
import { storeComposition, definitionsFor } from "../src/composition.js";
import { loadStore } from "../src/load.js";
import { cms, page } from "./fixtures.js";
import { catalog } from "../scripts/catalog.js";
import { buildManifest } from "@cms/component-registry/manifest";
import { registry, storeDefinitions, storeRegistry } from "../src/registry.js";
import { validateManifest } from "@cms/manifest";
import { renderToString } from "react-dom/server";
import { createElement } from "react";
import { StoreApp } from "../src/app.js";
import { model } from "./fixtures.js";

it("MF-010/STORE-001: storefront manifest, plan and React consume one registry", () => {
  const manifest = buildManifest(registry, {
    app: { id: "store-test", version: "1.0.0", framework: "react" },
  });
  const storeContracts = manifest.components!;
  expect(validateManifest(manifest).diagnostics).toEqual([]);
  expect(Object.keys(storeDefinitions)).toEqual(Object.keys(storeRegistry));
  expect(Object.keys(storeDefinitions).sort()).toEqual(Object.keys(storeContracts).sort());
  for (const definition of registry.components) {
    expect(storeRegistry[definition.name]).toBe(definition.component);
    expect(Object.keys(storeDefinitions[definition.name]!.props).sort()).toEqual(
      Object.keys(definition.props).sort(),
    );
    expect(storeContracts[definition.name]!.props).toEqual(definition.props);
  }
  expect(storeContracts.Storefront!.provides!.store).toMatchObject({
    type: "object",
    fields: { products: { type: "list" } },
  });
});
it("DS-030: native components apply generated classes, including the root wrapper", async () => {
  const initial = model();
  const response = page(initial.catalog);
  for (const id of ["n_store", "n_product", "n_empty"])
    response.document.nodes[id]!.design = { padding: { raw: 12 } };
  const plan = await preparePageWithData(storeComposition(response.document, initial.catalog!), {
    natives: definitionsFor(initial.catalog!),
    scope: { context: { store: initial.catalog } },
    actions: registry.actions,
    sources: registry.dataSources,
    sourceOptions: { app: { catalog: initial.catalog } },
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
it("STORE-001: reads the historical one-node draft without mutating it", async () => {
  const response = page();
  delete response.document.nodes.n_store!.children;
  const before = JSON.stringify(response.document);
  const doc = storeComposition(response.document, catalog);
  const plan = await preparePageWithData(doc, {
    natives: definitionsFor(catalog),
    scope: { context: { store: catalog } },
    actions: registry.actions,
    sources: registry.dataSources,
    sourceOptions: { app: { catalog } },
  });
  expect(plan.diagnostics).toEqual([]);
  expect(plan.roots[0]!.children.map((n) => n.type)).toEqual([
    "StoreHero",
    "StoreCollection",
    "StoreAbout",
  ]);
  expect(JSON.stringify(response.document)).toBe(before);
});
