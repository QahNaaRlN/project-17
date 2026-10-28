import type { CmsClient, ReadOptions } from "@cms/core";
import type { IrDocument } from "@cms/ir";
import {
  Image,
  preparePageWithData,
  type DataPlanOptions,
  type RenderPlan,
  type PlanNode,
} from "@cms/runtime";
import { nativeDefinitions, type Registry } from "@cms/component-registry";
export { createPreviewHandler, getPreviewState, previewFrameAncestors } from "./preview-server.js";
export type { PreviewHandlerOptions } from "./preview-server.js";
export interface LoadPageOptions extends DataPlanOptions {
  read?: ReadOptions;
  registry?: Registry;
  mode?: "design" | "interact";
}

/** Server entrypoint: client credentials and preview options never enter the returned plan. */
export async function loadPage(
  cms: CmsClient,
  path: string,
  options: LoadPageOptions = {},
): Promise<RenderPlan | null> {
  const page = await cms.getPage(path, options.read);
  if (!page) return null;
  const components = await loadComponents(cms, page, options.read, options.components);
  if (!components)
    return {
      roots: [],
      css: "",
      diagnostics: [{ code: "CMS_COMPONENT_LOAD_LIMIT", nodeId: page.document.root }],
    };
  const plan = await preparePageWithData(page.document, {
    ...(options.registry
      ? {
          natives: nativeDefinitions(options.registry),
          actions: options.registry.actions,
          sources: options.registry.dataSources,
          breakpoints: options.registry.breakpoints,
        }
      : {}),
    ...options,
    components,
    scope: {
      data: page.data,
      ...options.scope,
      ...(page.data.content ? { content: page.data.content as Record<string, unknown> } : {}),
      context: {
        ...options.scope?.context,
        ...(page.contentContext ? { locale: page.contentContext.locale } : {}),
      },
    },
    sourceOptions: {
      ...options.sourceOptions,
      ...(options.read?.signal ? { signal: options.read.signal } : {}),
    },
  });
  if (options.read?.preview) {
    plan.interactionMode = options.mode ?? "design";
    plan.preview = true;
  }
  if (page.diagnostics)
    plan.diagnostics.push(
      ...page.diagnostics.map((d) => ({
        code: d.code,
        nodeId: d.nodeId ?? page.document.root,
        severity: d.severity,
      })),
    );
  await loadPlanImages(cms, plan, options.read);
  // OPS-023: published renders report their diagnostics; preview drafts do not.
  if (!options.read?.preview)
    cms.report?.(
      plan.diagnostics.map((d) => ({
        code: d.code,
        source: d.code.startsWith("CMS_SOURCE_")
          ? ("source" as const)
          : d.code.startsWith("CMS_ACTION_")
            ? ("action" as const)
            : ("render" as const),
      })),
    );
  return plan;
}

/**
 * Live Composed components of a page, transitively (at most 64). Each is read with the
 * page's read options, so a preview resolves the working version of its own Change Set.
 * Returns null when the limit is exceeded; a missing component stays a plan diagnostic.
 */
export async function loadComponents(
  cms: CmsClient,
  page: { document: IrDocument; components?: Record<string, { document: IrDocument }> },
  read?: ReadOptions,
  known?: Record<string, IrDocument>,
): Promise<Record<string, IrDocument> | null> {
  const components: Record<string, IrDocument> = Object.create(null);
  for (const [id, item] of Object.entries(page.components ?? {})) components[id] = item.document;
  Object.assign(components, known);
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
      if (seen.size > 64) return null;
      if (!Object.hasOwn(components, id)) {
        try {
          const result = await cms.getDocument(id, read);
          if (result) components[id] = result.document;
        } catch (error) {
          if (read?.signal?.aborted) throw error;
          /* A missing component produces a sanitized diagnostic during preparation. */
        }
      }
      if (components[id]) queue.push(components[id]);
    }
  }
  return components;
}

/** Load signed models in the same scope and return their earliest refresh time. */
export async function loadPlanImages(
  cms: CmsClient,
  plan: RenderPlan,
  read: ReadOptions = {},
): Promise<number | undefined> {
  const images: NonNullable<RenderPlan["images"]> = Object.create(null);
  const requests = new Map<string, ReturnType<CmsClient["getImage"]>>();
  let refreshAt: number | undefined;
  async function visit(node: PlanNode): Promise<void> {
    if (node.type === "Image") {
      try {
        const ref = node.props.src as { assetId: string };
        if (!ref) throw new Error("CMS_IMAGE_UNAVAILABLE");
        const fit = node.props.fit === "cover" ? "cover" : "contain";
        const key = ref.assetId + ":" + fit;
        if (!requests.has(key)) requests.set(key, cms.getImage(ref.assetId, { ...read, fit }));
        const model = await requests.get(key);
        if (!model) throw new Error("CMS_IMAGE_UNAVAILABLE");
        images[node.key] = Image(model, {
          alt: node.props.decorative === true ? "" : String(node.props.alt ?? ""),
          loading: node.props.loading === "eager" ? "eager" : "lazy",
          width: { base: "100vw" },
        }).props;
        const next = Date.parse(model.expiresAt) - 30_000;
        refreshAt = Math.min(refreshAt ?? next, next);
      } catch (error) {
        if (read.signal?.aborted) throw error;
        plan.diagnostics.push({ code: "CMS_IMAGE_UNAVAILABLE", nodeId: node.id });
      }
    }
    await Promise.all(node.children.map(visit));
  }
  await Promise.all(plan.roots.map(visit));
  if (Object.keys(images).length) plan.images = images;
  return refreshAt;
}
