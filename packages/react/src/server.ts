import type { CmsClient, ReadOptions } from "@cms/core";
import type { IrDocument } from "@cms/ir";
import { preparePage, type PlanOptions, type RenderPlan } from "@cms/runtime";
export interface LoadPageOptions extends PlanOptions {
  read?: ReadOptions;
}

/** Server entrypoint: client credentials and preview options never enter the returned plan. */
export async function loadPage(
  cms: CmsClient,
  path: string,
  options: LoadPageOptions = {},
): Promise<RenderPlan | null> {
  const page = await cms.getPage(path, options.read);
  if (!page) return null;
  const components: Record<string, IrDocument> = Object.create(null);
  for (const [id, item] of Object.entries(page.components)) components[id] = item.document;
  Object.assign(components, options.components);
  const seen = new Set<string>();
  const queue = [page.document];
  while (queue.length) {
    const doc = queue.shift()!;
    for (const node of Object.values(doc.nodes)) {
      if (
        node.type !== "Composed" ||
        !node.ref ||
        (node.ref.version !== undefined && node.ref.version !== "live")
      )
        continue;
      const id = node.ref.component;
      if (seen.has(id)) continue;
      seen.add(id);
      if (seen.size > 64)
        return {
          roots: [],
          css: "",
          diagnostics: [{ code: "CMS_COMPONENT_LOAD_LIMIT", nodeId: page.document.root }],
        };
      if (!Object.hasOwn(components, id)) {
        try {
          const result = await cms.getDocument(id, options.read);
          if (result) components[id] = result.document;
        } catch (error) {
          if (options.read?.signal?.aborted) throw error;
          /* A missing component produces a sanitized diagnostic during preparation. */
        }
      }
      if (components[id]) queue.push(components[id]);
    }
  }
  return preparePage(page.document, {
    ...options,
    components,
    scope: { data: page.data, ...options.scope },
  });
}
