import type { PageResponse } from "@cms/core";
import type { NativeDefinition } from "@cms/runtime";
import type { Catalog, Product } from "./model.js";
type IrDocument = PageResponse["document"];

import { storeDefinitions } from "./registry.js";
function sameProduct(input: unknown, expected: Product): boolean {
  if (!input || typeof input !== "object" || Array.isArray(input)) return false;
  const value = input as Record<string, unknown>;
  const keys = Object.keys(expected) as (keyof Product)[];
  return (
    Object.keys(value).length === keys.length &&
    keys.every((key) => {
      if (!Object.hasOwn(value, key)) return false;
      if (key !== "asset") return value[key] === expected[key];
      const asset = value.asset;
      return (
        !!asset &&
        typeof asset === "object" &&
        !Array.isArray(asset) &&
        Object.keys(asset).length === 1 &&
        Object.hasOwn(asset, "assetId") &&
        (asset as { assetId: unknown }).assetId === expected.asset?.assetId
      );
    })
  );
}
export function definitionsFor(catalog: Catalog): Record<string, NativeDefinition> {
  return {
    ...storeDefinitions,
    StoreProduct: {
      ...storeDefinitions.StoreProduct!,
      validate: (p) => catalog.products.some((product) => sameProduct(p.product, product)),
    },
    StoreCollection: {
      ...storeDefinitions.StoreCollection!,
      validate: (p) =>
        typeof p.title === "string" &&
        p.title.length <= 2000 &&
        (p.products === undefined ||
          (Array.isArray(p.products) &&
            p.products.every((product) =>
              catalog.products.some((item) => sameProduct(product, item)),
            ))),
    },
  };
}
/** Default composition used when creating a new CMS draft. */
export function storeDocument(config: unknown): IrDocument {
  return {
    irVersion: "1.0",
    kind: "page",
    root: "n_store",
    dataSources: { featured: { source: "store.products", params: { limit: { lit: 64 } } } },
    nodes: {
      n_store: {
        id: "n_store",
        type: "Storefront",
        props: { config: { base: config } },
        children: ["n_hero", "n_collection", "n_about"],
      },
      n_hero: {
        id: "n_hero",
        type: "StoreHero",
        bindings: {
          title: "$context.store.title",
          subtitle: "$context.store.subtitle",
          description: "$context.store.description",
          ctaLabel: "$context.store.ctaLabel",
        },
      },
      n_collection: {
        id: "n_collection",
        type: "StoreCollection",
        bindings: { title: "$context.store.collectionTitle", products: "$data.featured.items" },
        children: ["n_products"],
      },
      n_products: {
        id: "n_products",
        type: "Repeat",
        props: { key: "$item.id", limit: 64 },
        bindings: { items: "$data.featured.items" },
        children: ["n_product"],
        slots: { empty: ["n_empty"] },
      },
      n_product: {
        id: "n_product",
        type: "StoreProduct",
        bindings: { product: "$item" },
        on: {
          favorite: { action: "store.favorite", args: { id: "$item.id", title: "$item.title" } },
        },
      },
      n_empty: { id: "n_empty", type: "StoreEmpty" },
      n_about: {
        id: "n_about",
        type: "StoreAbout",
        bindings: { title: "$context.store.aboutTitle", text: "$context.store.aboutText" },
      },
    },
  };
}
/** Keep the previous single-node document readable until its draft is replaced. */
export function storeComposition(document: IrDocument, catalog: Catalog): IrDocument {
  const source =
    document.nodes[document.root]?.children === undefined ? storeDocument(catalog) : document;
  const root = source.nodes[source.root]!;
  const { config: _config, ...props } = root.props as Record<string, unknown>;
  return { ...source, nodes: { ...source.nodes, [source.root]: { ...root, props } } };
}
