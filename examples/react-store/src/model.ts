import type { IrDocument } from "@cms/ir";
import type { PageResponse } from "@cms/core";
import type { Image } from "@cms/runtime";
import type { RenderPlan } from "@cms/runtime";
import type { PreviewSnapshot } from "@cms/react/preview";

export interface AssetRef {
  assetId: string;
}
export interface Product {
  id: string;
  title: string;
  description: string;
  material: string;
  category: string;
  tone: "sand" | "sage" | "clay";
  priceRub?: number | null;
  asset?: AssetRef;
}
export interface Catalog {
  brand: string;
  title: string;
  subtitle: string;
  description: string;
  ctaLabel: string;
  collectionTitle: string;
  aboutTitle: string;
  aboutText: string;
  heroAsset?: AssetRef;
  products: Product[];
}
export type ImageProps = ReturnType<typeof Image>["props"];
export interface StoreModel {
  locale?: string;
  status: "ready" | "missing" | "error";
  preview: boolean;
  catalog?: Catalog;
  images: Record<string, ImageProps | null>;
  refreshAt?: number;
  plan?: RenderPlan;
  bridge?: PreviewSnapshot & {
    studioOrigin: string;
    manifestHash: string;
    data: Record<string, unknown>;
    components?: Record<string, IrDocument>;
    sourceErrors?: Record<string, string>;
    changesetId?: string;
  };
}

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("STORE_CONFIG_INVALID");
  return value as Record<string, unknown>;
}
function text(value: unknown, limit: number): string {
  if (typeof value !== "string" || !value.trim() || value.length > limit)
    throw new Error("STORE_CONFIG_INVALID");
  return value;
}
function asset(value: unknown): AssetRef {
  const id = object(value).assetId;
  if (typeof id !== "string" || !uuid.test(id)) throw new Error("STORE_CONFIG_INVALID");
  return { assetId: id };
}
export function parseCatalog(page: PageResponse): Catalog {
  const node = page.document.nodes[page.document.root];
  if (!node || node.type !== "Storefront") throw new Error("STORE_CONFIG_INVALID");
  const wrapper = object(object(node.props).config);
  if (Object.keys(wrapper).some((key) => key !== "base")) throw new Error("STORE_CONFIG_INVALID");
  const config = object(wrapper.base);
  if (!Array.isArray(config.products) || config.products.length > 64)
    throw new Error("STORE_CONFIG_INVALID");
  const identifiers = new Set<string>();
  const products = config.products.map((raw): Product => {
    const p = object(raw);
    const id = text(p.id, 64);
    if (!/^[a-zA-Z0-9_-]+$/.test(id) || identifiers.has(id))
      throw new Error("STORE_CONFIG_INVALID");
    identifiers.add(id);
    if (p.tone !== "sand" && p.tone !== "sage" && p.tone !== "clay")
      throw new Error("STORE_CONFIG_INVALID");
    if (
      p.priceRub !== undefined &&
      p.priceRub !== null &&
      (!Number.isSafeInteger(p.priceRub) ||
        (p.priceRub as number) < 0 ||
        (p.priceRub as number) > 1_000_000_000)
    )
      throw new Error("STORE_CONFIG_INVALID");
    return {
      id,
      title: text(p.title, 200),
      description: text(p.description, 2000),
      material: text(p.material, 100),
      category: text(p.category, 80),
      tone: p.tone,
      ...(p.priceRub !== undefined ? { priceRub: p.priceRub as number | null } : {}),
      ...(p.asset !== undefined ? { asset: asset(p.asset) } : {}),
    };
  });
  return {
    brand: text(config.brand, 80),
    title: text(config.title, 200),
    subtitle: text(config.subtitle, 200),
    description: text(config.description, 2000),
    ctaLabel: text(config.ctaLabel, 100),
    collectionTitle: text(config.collectionTitle, 200),
    aboutTitle: text(config.aboutTitle, 200),
    aboutText: text(config.aboutText, 2000),
    products,
    ...(config.heroAsset !== undefined ? { heroAsset: asset(config.heroAsset) } : {}),
  };
}
export function priceLabel(value?: number | null): string {
  return value === undefined || value === null
    ? "Цена не указана"
    : new Intl.NumberFormat("ru-RU", {
        style: "currency",
        currency: "RUB",
        maximumFractionDigits: 0,
      }).format(value);
}
export function serializeModel(model: StoreModel): string {
  return JSON.stringify(model)
    .replace(/</g, "\\u003c")
    .replace(/\u2028/g, "\\u2028")
    .replace(/\u2029/g, "\\u2029");
}
