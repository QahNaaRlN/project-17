import {
  createRegistry,
  nativeDefinitions,
  t,
  defineAction,
  defineDataSource,
} from "@cms/component-registry";
import { createReactRegistry, defineReactComponent } from "@cms/react";
import { Storefront, Hero, Collection, Item, Card, Empty, About, Promo } from "./sections.js";
import { productField as product, productsField as products } from "./contracts.js";
import {
  favoriteProduct,
  loadProducts,
  loadCollectionProducts,
  loadRecommendations,
} from "./operations.js";
const text = t.text().content().required().max(2000);
const string = t.string().required().max(2000);
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
export const storeTokens = {
  spacing: { sm: "8px", md: "16px", lg: "24px", xl: "32px", huge: "48px" },
};
export const registry = createRegistry({
  breakpoints: { sm: 640, md: 768, lg: 1024 },
  tokens: storeTokens,
  // The §2 scenario binds a page to a Collection entity (STU-447).
  schemas: {
    Collection: {
      version: 1,
      title: "Collection",
      fields: {
        title: { type: "text", localized: true, required: true },
        handle: { type: "string", required: true, maxLength: 64 },
      },
    },
  },
  actions: [
    defineAction({
      name: "store.favorite",
      args: { id: t.string().required().max(64), title: t.string().required().max(200) },
      handler: favoriteProduct,
    }),
  ],
  dataSources: [
    defineDataSource({
      name: "store.collectionProducts",
      params: {
        collection: t.string().required().max(64),
        limit: t.number().int().min(1).max(64).default(12),
        locale: t.string(),
      },
      result: t.object({ items: products }),
      loader: loadCollectionProducts,
    }),
    defineDataSource({
      name: "store.recommendations",
      params: { limit: t.number().int().min(1).max(64).required() },
      result: t.list(t.object({ title: t.string().required().max(2000) })).max(64),
      loader: loadRecommendations,
    }),
    defineDataSource({
      name: "store.products",
      params: { limit: t.number().int().min(1).max(64).default(64) },
      result: t.object({ items: products }),
      loader: loadProducts,
    }),
  ],
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
      props: { title: text, products: t.list(product).max(64) },
    }),
    defineReactComponent({
      name: "StoreProduct",
      origin: "native",
      container: false,
      component: Item,
      props: { product: product.required() },
      events: { favorite: {} },
    }),
    defineReactComponent({
      name: "ProductCard",
      origin: "native",
      container: false,
      component: Card,
      props: {
        product: product.required(),
        variant: t.enum(["default", "compact"]).default("default"),
      },
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
    defineReactComponent({
      name: "PromoBanner",
      origin: "native",
      container: true,
      component: Promo,
      props: {
        title: t.string().required().max(200),
        tone: t.enum(["plain", "accent"]).default("plain"),
      },
      validate: (p) => typeof p.title === "string" && strings({ title: p.title }),
    }),
  ],
});
export const storeDefinitions = nativeDefinitions(registry);
export const storeRegistry = createReactRegistry(registry);
