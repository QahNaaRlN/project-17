import type { CmsClient, ReadOptions } from "@cms/core";
import { Image, preparePage, loadSources, documentHash } from "@cms/runtime";
import { registry } from "./registry.js";
import { parseCatalog, type StoreModel } from "./model.js";
import { storeComposition, definitionsFor } from "./composition.js";

export async function loadStore(
  cms: CmsClient,
  path: string,
  options: ReadOptions & { locale?: string } = {},
  bridge?: { studioOrigin: string; manifestHash: string },
): Promise<StoreModel> {
  const preview = !!options.preview;
  const { locale = "ru", ...read } = options;
  try {
    const page = await cms.getPage(path, read);
    if (!page) return { status: "missing", preview, images: {} };
    const catalog = parseCatalog(page);
    const model: StoreModel = { status: "ready", preview, catalog, images: {}, locale };
    const scope = { context: { locale, store: catalog } };
    const composition = storeComposition(page.document, catalog);
    const data = await loadSources(composition.dataSources ?? {}, registry.dataSources, scope, {
      app: { catalog },
      ...(options.signal ? { signal: options.signal } : {}),
    });
    model.plan = preparePage(composition, {
      natives: definitionsFor(catalog),
      scope: { ...scope, data },
      breakpoints: registry.breakpoints,
      actions: registry.actions,
    });
    if (preview) {
      model.plan.preview = true;
      model.plan.interactionMode = "design";
    }
    if (preview && bridge) {
      const root = page.document.nodes[page.document.root]!;
      const document = {
        ...page.document,
        nodes: {
          ...page.document.nodes,
          [page.document.root]: { ...root, props: { ...root.props, config: { base: catalog } } },
        },
      };
      model.bridge = {
        ...bridge,
        documentId: page.page.objectId,
        path,
        document,
        hash: await documentHash(document),
        data,
        ...(options.preview?.changesetId ? { changesetId: options.preview.changesetId } : {}),
      };
    }
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
            ...read,
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
