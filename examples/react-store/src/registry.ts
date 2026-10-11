import { createRegistry, nativeDefinitions } from "@cms/component-registry";
import { createReactRegistry, defineReactComponent } from "@cms/react";
import { Storefront, Hero, Collection, Item, Empty, About } from "./sections.js";
const text = { type: "text", content: true, required: true, maxLength: 2000 } as const;
const string = { type: "string", required: true, maxLength: 2000 } as const;
const product = {
  type: "object",
  fields: {
    id: string,
    title: string,
    description: string,
    material: string,
    category: string,
    tone: { type: "enum", values: ["sand", "sage", "clay"], required: true },
    priceRub: { type: "number", integer: true, min: 0, max: 1_000_000_000 },
    asset: { type: "asset", assetKind: "image" },
  },
} as const;
// satisfies keeps literal inference while checking the existing manifest schema types.
const products = { type: "list", required: true, max: 64, of: product } as const;
const fields = {
  brand: string,
  title: string,
  subtitle: string,
  description: string,
  ctaLabel: string,
  collectionTitle: string,
  aboutTitle: string,
  aboutText: string,
  heroAsset: { type: "asset", assetKind: "image" },
  products,
} as const;
const strings = (values: Record<string, unknown>) =>
  Object.values(values).every((value) => typeof value === "string" && value.length <= 2000);
export const registry = createRegistry({
  breakpoints: { sm: 640, md: 768, lg: 1024 },
  tokens: {},
  components: [
    defineReactComponent({
      name: "Storefront",
      origin: "native",
      container: true,
      component: Storefront,
      props: { config: { type: "object", responsive: true, required: true, fields } },
      provides: { store: { type: "object", required: true, fields } },
    }),
    defineReactComponent({
      name: "StoreHero",
      origin: "native",
      container: false,
      component: Hero,
      props: { title: text, subtitle: text, description: text, ctaLabel: text },
      validate: (p) =>
        ["title", "subtitle", "description", "ctaLabel"].every(
          (key) => typeof p[key] === "string",
        ) && strings(p),
    }),
    defineReactComponent({
      name: "StoreCollection",
      origin: "native",
      container: true,
      component: Collection,
      props: { title: text, products },
    }),
    defineReactComponent({
      name: "StoreProduct",
      origin: "native",
      container: false,
      component: Item,
      props: { product: { ...product, required: true } },
    }),
    defineReactComponent({
      name: "StoreEmpty",
      origin: "native",
      container: false,
      component: Empty,
      props: {},
    }),
    defineReactComponent({
      name: "StoreAbout",
      origin: "native",
      container: false,
      component: About,
      props: { title: text, text },
      validate: (p) => typeof p.title === "string" && typeof p.text === "string" && strings(p),
    }),
  ],
});
export const storeDefinitions = nativeDefinitions(registry);
export const storeRegistry = createReactRegistry(registry);
