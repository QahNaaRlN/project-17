import { createRegistry, nativeDefinitions, t } from "@cms/component-registry";
import { createReactRegistry, defineReactComponent } from "@cms/react";
import { Storefront, Hero, Collection, Item, Empty, About } from "./sections.js";
const text = t.text().content().required().max(2000);
const string = t.string().required().max(2000);
const product = t.object({
  id: string,
  title: string,
  description: string,
  material: string,
  category: string,
  tone: t.enum(["sand", "sage", "clay"]).required(),
  priceRub: t.number().int().min(0).max(1_000_000_000),
  asset: t.asset("image"),
});
const products = t.list(product).required().max(64);
const fields = {
  brand: string,
  title: string,
  subtitle: string,
  description: string,
  ctaLabel: string,
  collectionTitle: string,
  aboutTitle: string,
  aboutText: string,
  heroAsset: t.asset("image"),
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
      props: { config: t.object(fields).responsive().required() },
      provides: { store: t.object(fields).required() },
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
      props: { product: product.required() },
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
