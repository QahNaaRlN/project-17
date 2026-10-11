import type { IrDocument } from "@cms/ir";
import type { BindingScope } from "@cms/core";
import { preparePage, type PlanOptions, type RenderPlan } from "./plan.js";
import { loadSources, type RuntimeSource, type SourceOptions } from "./sources.js";

export interface DataPlanOptions extends PlanOptions {
  sources?: readonly RuntimeSource[];
  sourceOptions?: SourceOptions;
}
/** Discover Composed instances using the same scopes/keys as the final renderer. */
export async function preparePageWithData(
  document: IrDocument,
  options: DataPlanOptions = {},
): Promise<RenderPlan> {
  const data = new Map<string, Record<string, unknown>>();
  let sourceCount = 0;
  while (true) {
    const pending = new Map<string, { doc: IrDocument; scope: BindingScope }>();
    const plan = preparePage(document, {
      ...options,
      documentScope(doc, scope, instance) {
        const appScope = options.documentScope?.(doc, scope, instance) ?? scope;
        if (doc.dataSources && Object.keys(doc.dataSources).length && !data.has(instance))
          pending.set(instance, { doc, scope: appScope });
        return {
          ...appScope,
          data: { ...(appScope.data as Record<string, unknown>), ...data.get(instance) },
        };
      },
    });
    if (!pending.size) return plan;
    sourceCount += [...pending.values()].reduce(
      (count, { doc }) => count + Object.keys(doc.dataSources!).length,
      0,
    );
    if (sourceCount > 64)
      return {
        roots: [],
        css: "",
        diagnostics: [{ code: "CMS_SOURCE_LIMIT", nodeId: document.root }],
      };
    await Promise.all(
      [...pending].map(async ([instance, { doc, scope }]) => {
        data.set(
          instance,
          await loadSources(doc.dataSources!, options.sources ?? [], scope, options.sourceOptions),
        );
      }),
    );
  }
}
