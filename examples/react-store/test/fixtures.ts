import type { PageResponse, CmsClient, ImageModel } from "@cms/core";
import { catalog } from "../scripts/catalog.js";
import type { StoreModel } from "../src/model.js";
import { vi } from "vitest";
import { preparePage } from "@cms/runtime";
import { storeDocument, storeComposition, definitionsFor } from "../src/composition.js";
import { registry } from "../src/registry.js";

export const id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11";
export const now = Date.parse("2030-01-01T00:00:00Z");
export function page(config: unknown = structuredClone(catalog)): PageResponse {
  return {
    page: { objectId: id, versionId: id, path: "/store", params: {} },
    document: storeDocument(config),
    components: {},
    data: {},
    dataSources: {},
  };
}
export function model(): StoreModel {
  return {
    status: "ready",
    preview: true,
    catalog: structuredClone(catalog),
    images: {},
    get plan() {
      return preparePage(storeComposition(storeDocument(this.catalog), this.catalog!), {
        natives: definitionsFor(this.catalog!),
        scope: {
          context: { store: this.catalog },
          data: { featured: { items: this.catalog!.products } },
        },
        actions: registry.actions,
      });
    },
  };
}
export function image(): ImageModel {
  return {
    assetId: id,
    versionId: id,
    width: 800,
    height: 600,
    fit: "contain",
    format: "webp",
    focalPoint: { x: 0.5, y: 0.5 },
    expiresAt: new Date(now + 300_000).toISOString(),
    variants: [
      {
        width: 320,
        url: `https://images.example/assets/v1/staging/${id}/${"a".repeat(64)}?sig=example`,
      },
    ],
  };
}
export function cms(): CmsClient {
  return {
    getPage: vi.fn().mockResolvedValue(page()),
    getDocument: vi.fn(),
    getEntity: vi.fn(),
    getAsset: vi.fn(),
    getRoutes: vi.fn(),
    getImage: vi.fn().mockResolvedValue(image()),
    clearCache: vi.fn(),
  };
}
