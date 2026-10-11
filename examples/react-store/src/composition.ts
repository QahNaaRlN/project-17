import type { PageResponse } from "@cms/core";
import type { NativeDefinition } from "@cms/runtime";
import type { Catalog, Product } from "./model.js";
type IrDocument = PageResponse["document"];

export const storeDefinitions: Record<string, NativeDefinition> = {
  Storefront: { container: true, props: {} },
  StoreHero: {
    props: { title: {}, subtitle: {}, description: {}, ctaLabel: {} },
    validate: (p) =>
      [p.title, p.subtitle, p.description, p.ctaLabel].every(
        (v) => typeof v === "string" && v.length <= 2000,
      ),
  },
  StoreCollection: { container: true, props: { title: {}, products: {} } },
  StoreProduct: { props: { product: {} } },
  StoreEmpty: { props: {} },
  StoreAbout: {
    props: { title: {}, text: {} },
    validate: (p) => [p.title, p.text].every((v) => typeof v === "string" && v.length <= 2000),
  },
};
export function definitionsFor(catalog: Catalog): Record<string, NativeDefinition> {
  return {
    ...storeDefinitions,
    StoreProduct: {
      ...storeDefinitions.StoreProduct!,
      validate: (p) => catalog.products.includes(p.product as Product),
    },
    StoreCollection: {
      ...storeDefinitions.StoreCollection!,
      validate: (p) =>
        typeof p.title === "string" && p.title.length <= 2000 && p.products === catalog.products,
    },
  };
}
/** Default composition used when creating a new CMS draft. */
export function storeDocument(config: unknown): IrDocument {
  return {
    irVersion: "1.0",
    kind: "page",
    root: "n_store",
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
        bindings: { title: "$context.store.collectionTitle", products: "$context.store.products" },
        children: ["n_products"],
      },
      n_products: {
        id: "n_products",
        type: "Repeat",
        props: { key: "$item.id", limit: 64 },
        bindings: { items: "$context.store.products" },
        children: ["n_product"],
        slots: { empty: ["n_empty"] },
      },
      n_product: { id: "n_product", type: "StoreProduct", bindings: { product: "$item" } },
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
