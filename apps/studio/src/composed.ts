import type { IrDocument, Node } from "@cms/ir";
import { builtinCatalogue } from "@cms/manifest/builtins";
import type { Component, Manifest } from "@cms/manifest";

export function contract(type: string, manifest: Manifest): Component | undefined {
  return (
    (builtinCatalogue as unknown as Record<string, Component>)[type] ??
    manifest.primitives?.[type] ??
    manifest.components?.[type]
  );
}
// Components resolved for the open draft (working version of the CS or head).
const composed = new Map<string, IrDocument>();
export function registerComponents(docs: Record<string, IrDocument>) {
  composed.clear();
  for (const [id, doc] of Object.entries(docs)) if (doc.kind === "component") composed.set(id, doc);
}
export function componentOf(node: Node | undefined): IrDocument | undefined {
  return node?.type === "Composed" && node.ref ? composed.get(node.ref.component) : undefined;
}
export function componentTitle(node: Node | undefined): string | undefined {
  const title = componentOf(node)?.meta?.title;
  return typeof title === "string" ? title : undefined;
}
// Proposed contracts of component placeholders in the open draft (STU-372).
const placeholders = new Map<string, Component>();
export function registerPlaceholders(contracts: Record<string, Component>) {
  placeholders.clear();
  for (const [id, c] of Object.entries(contracts)) placeholders.set(id, c);
}
/** Request ids of component placeholders in stable node order. */
export function placeholderRefs(doc: IrDocument): string[] {
  const ids = Object.keys(doc.nodes)
    .sort()
    .flatMap((id) => (typeof doc.nodes[id]!.pending === "string" ? [doc.nodes[id]!.pending!] : []));
  return [...new Set(ids)].slice(0, 64);
}
/** Contract of a node: a Composed instance exposes its component's inputs and slots;
 * a placeholder its request's proposed contract, without events. */
export function nodeContract(node: Node | undefined, manifest: Manifest): Component | undefined {
  if (!node) return undefined;
  if (typeof node.pending === "string") return placeholders.get(node.pending);
  const doc = componentOf(node);
  if (doc)
    return {
      props: (doc.inputs ?? {}) as Component["props"],
      slots: (doc.slotDefs ?? {}) as Component["slots"],
      events: {},
    } as Component;
  return contract(node.type, manifest);
}
/** Live components referenced by Composed instances (runtime loads at most 64). */
export function componentRefs(doc: IrDocument): string[] {
  const refs = Object.values(doc.nodes).flatMap((n) =>
    n.type === "Composed" && n.ref && (n.ref.version === undefined || n.ref.version === "live")
      ? [n.ref.component]
      : [],
  );
  return [...new Set(refs)].slice(0, 64);
}
