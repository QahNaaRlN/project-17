import type { CmsClient, ReadOptions } from "@cms/core";
import { Image, preparePage } from "@cms/runtime";
import { registry } from "./registry.js";
import { parseCatalog, type StoreModel } from "./model.js";
import { storeComposition, definitionsFor } from "./composition.js";

export async function loadStore(
  cms: CmsClient,
  path: string,
  options: ReadOptions = {},
): Promise<StoreModel> {
  const preview = !!options.preview;
  try {
    const page = await cms.getPage(path, options);
    if (!page) return { status: "missing", preview, images: {} };
    const catalog = parseCatalog(page);
    const model: StoreModel = { status: "ready", preview, catalog, images: {} };
    model.plan = preparePage(storeComposition(page.document, catalog), {
      natives: definitionsFor(catalog),
      scope: { context: { locale: "ru", store: catalog } },
      breakpoints: registry.breakpoints,
    });
    if (model.plan.diagnostics.length) throw new Error("STORE_COMPOSITION_INVALID");
    const refs = [
      ...(catalog.heroAsset
        ? [{ key: "hero", asset: catalog.heroAsset, alt: "Предметы коллекции", hero: true }]
        : []),
      ...catalog.products
        .filter((p) => p.asset)
        .map((p) => ({ key: `product:${p.id}`, asset: p.asset!, alt: p.title, hero: false })),
    ];
    await Promise.all(
      refs.map(async (ref) => {
        try {
          const image = await cms.getImage(ref.asset.assetId, {
            ...options,
            fmt: "webp",
            fit: "contain",
          });
          if (!image) {
            model.images[ref.key] = null;
            return;
          }
          model.images[ref.key] = Image(image, {
            alt: ref.alt,
            loading: ref.hero ? "eager" : "lazy",
            preferredWidth: 640,
            width: {
              base: ref.hero ? "100vw" : 112,
              breakpoints: [
                { minWidth: 641, width: "33vw" },
                { minWidth: 1024, width: ref.hero ? 420 : 304 },
              ],
            },
          }).props;
          const refreshAt = Date.parse(image.expiresAt) - 30_000;
          model.refreshAt = Math.min(model.refreshAt ?? refreshAt, refreshAt);
        } catch {
          model.images[ref.key] = null;
        }
      }),
    );
    return model;
  } catch {
    return { status: "error", preview, images: {} };
  }
}
