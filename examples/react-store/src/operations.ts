import type { ActionContext, LoaderContext } from "@cms/component-registry";
import type { Catalog, Product } from "./model.js";
export interface FavoriteServices {
  favorite: (id: string, title: string) => void;
}
export function favoriteProduct(
  args: { id: string; title: string },
  context: ActionContext<FavoriteServices>,
): void {
  context.app.favorite(args.id, args.title);
}
/** Server-owned catalogue service; the browser never invokes this loader. */
export function loadProducts(
  params: { limit?: number | null },
  context: LoaderContext<{ catalog: Catalog }>,
): { items: Product[] } {
  if (context.signal.aborted) throw new Error("STORE_SOURCE_CANCELED");
  return { items: context.app.catalog.products.slice(0, params.limit ?? 64) };
}
