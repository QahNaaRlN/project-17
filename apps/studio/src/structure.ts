import {
  generateNodeId,
  normalize,
  toNested,
  type IrDocument,
  type Node,
  type NestedNode,
  type Policy,
} from "@cms/ir";
import { builtinCatalogue } from "@cms/manifest/builtins";
import type { Component, Manifest, Type } from "@cms/manifest";
import { fieldOperations } from "./fields.js";
import { StudioError, type Operation } from "./types.js";

export interface Place {
  parentId: string;
  slot: string;
  index: number;
}
export function contract(type: string, manifest: Manifest): Component | undefined {
  return (
    (builtinCatalogue as unknown as Record<string, Component>)[type] ??
    manifest.primitives?.[type] ??
    manifest.components?.[type]
  );
}
export function palette(manifest: Manifest) {
  return Object.keys({ ...builtinCatalogue, ...manifest.primitives, ...manifest.components })
    .sort()
    .map((type) => {
      const definition = contract(type, manifest)!;
      const required = Object.entries(definition.props ?? {}).filter(
        ([, t]) => t.required && !("default" in t),
      );
      const supported =
        !["Repeat", "Slot", "Composed"].includes(type) &&
        !Object.values(definition.slots ?? {}).some((s) => (s.min ?? 0) > 0) &&
        required.every(
          ([, t]) =>
            ["string", "text", "number", "boolean", "enum"].includes(t.type) &&
            (!t.content || t.type === "text"),
        );
      return { type, definition, required, supported };
    });
}
export function location(doc: IrDocument, id: string): Place | null {
  for (const n of Object.values(doc.nodes)) {
    for (const [slot, list] of [["", n.children ?? []], ...Object.entries(n.slots ?? {})] as [
      string,
      string[],
    ][]) {
      const index = list.indexOf(id);
      if (index >= 0) return { parentId: n.id, slot, index };
    }
  }
  return null;
}
export function childList(doc: IrDocument, place: Place): string[] {
  const node = doc.nodes[place.parentId];
  if (!node) throw new StudioError("NODE_NOT_FOUND");
  return place.slot ? ((node.slots ??= {})[place.slot] ??= []) : (node.children ??= []);
}
export function subtree(doc: IrDocument, id: string): NestedNode {
  if (!doc.nodes[id]) throw new StudioError("NODE_NOT_FOUND");
  return structuredClone(toNested({ ...doc, root: id }).root);
}
export function structuralApply(doc: IrDocument, operation: Operation): void {
  const p = operation.payload;
  if (operation.type === "node.insert") {
    const flat = normalize({ irVersion: "1.0", kind: doc.kind, root: p.subtree as NestedNode });
    if (Object.keys(flat.nodes).some((id) => Object.hasOwn(doc.nodes, id)))
      throw new StudioError("NODE_ID_DUPLICATE");
    Object.assign(doc.nodes, flat.nodes);
    childList(doc, p as unknown as Place).splice(Number(p.index), 0, flat.root);
  } else {
    const id = String(p.nodeId),
      from = location(doc, id);
    if (!from) throw new StudioError("ROOT_IMMUTABLE");
    childList(doc, from).splice(from.index, 1);
    if (operation.type === "node.move")
      childList(doc, p as unknown as Place).splice(Number(p.index), 0, id);
    else {
      const flat = normalize({ irVersion: "1.0", kind: doc.kind, root: subtree(doc, id) });
      for (const nodeId of Object.keys(flat.nodes)) delete doc.nodes[nodeId];
    }
  }
}
function ancestors(doc: IrDocument, id: string): Node[] {
  const out: Node[] = [],
    seen = new Set<string>();
  while (doc.nodes[id] && !seen.has(id)) {
    seen.add(id);
    out.unshift(doc.nodes[id]!);
    const parent = location(doc, id);
    if (!parent) break;
    id = parent.parentId;
  }
  return out;
}
function policyAllows(
  type: string,
  ref: string | undefined,
  levels: Policy[],
  manifest: Manifest,
): boolean {
  let mode = "SYSTEM";
  for (const p of levels) {
    mode = p.mode ?? mode;
    if (p.allowedTypes && !p.allowedTypes.includes(type) && !(ref && p.allowedTypes.includes(ref)))
      return false;
    if (p.deniedTypes?.includes(type) || (ref && p.deniedTypes?.includes(ref))) return false;
  }
  // Composed mode/certification needs server resolution; do not advertise unresolved targets.
  return (
    type !== "Composed" && (mode !== "STRICT" || Object.hasOwn(manifest.components ?? {}, type))
  );
}
function treeAllowed(node: NestedNode, levels: Policy[], manifest: Manifest): boolean {
  if (node.locked || node.zone) return false;
  if (!policyAllows(node.type, node.ref?.component, levels, manifest)) return false;
  return [...(node.children ?? []), ...Object.values(node.slots ?? {}).flat()].every((n) =>
    treeAllowed(n, levels, manifest),
  );
}
function protectedPaths(doc: IrDocument): Map<string, string> {
  const paths = new Map<string, string>(),
    seen = new Set<string>();
  function walk(id: string, path: string, locked: boolean) {
    const n = doc.nodes[id];
    if (!n || seen.has(id)) return;
    seen.add(id);
    locked ||= !!n.locked || !!n.zone?.locked;
    if (locked) paths.set(id, path);
    n.children?.forEach((child, index) => walk(child, path + "/children/" + index, locked));
    Object.entries(n.slots ?? {}).forEach(([slot, list]) =>
      list.forEach((child, index) => walk(child, path + "/slots/" + slot + "/" + index, locked)),
    );
  }
  walk(doc.root, doc.root, false);
  return paths;
}
function preservesLocks(before: IrDocument, after: IrDocument): boolean {
  const a = protectedPaths(before),
    b = protectedPaths(after);
  return [...a].every(
    ([id, path]) =>
      b.get(id) === path && JSON.stringify(before.nodes[id]) === JSON.stringify(after.nodes[id]),
  );
}
function sourceAllowed(doc: IrDocument, id: string, manifest: Manifest, target?: Place): boolean {
  const from = location(doc, id);
  if (!from) return false;
  if (ancestors(doc, id).some((n) => n.locked || n.zone?.locked)) return false;
  const tree = subtree(doc, id);
  const flat = normalize({ irVersion: "1.0", kind: doc.kind, root: tree });
  if (Object.values(flat.nodes).some((n) => n.locked || n.zone)) return false;
  if (target?.parentId === from.parentId && target.slot === from.slot) return true;
  const min = contract(doc.nodes[from.parentId]!.type, manifest)?.slots?.[from.slot]?.min ?? 0;
  return childList(structuredClone(doc), from).length - 1 >= min;
}
export function canRemove(doc: IrDocument, id: string, manifest: Manifest): boolean {
  if (!sourceAllowed(doc, id, manifest)) return false;
  const next = structuredClone(doc);
  structuralApply(next, { type: "node.remove", target: "", payload: { nodeId: id } });
  return preservesLocks(doc, next);
}
export function destinations(
  doc: IrDocument,
  manifest: Manifest,
  tree: NestedNode,
  moving?: string,
): Place[] {
  const result: Place[] = [];
  const hasLocks = protectedPaths(doc).size > 0;
  const excluded = moving
    ? new Set(
        Object.keys(
          normalize({ irVersion: "1.0", kind: doc.kind, root: subtree(doc, moving) }).nodes,
        ),
      )
    : new Set<string>();
  for (const parent of Object.values(doc.nodes)) {
    if (excluded.has(parent.id)) continue;
    const chain = ancestors(doc, parent.id);
    if (chain.some((n) => n.locked || n.zone?.locked)) continue;
    const levels: Policy[] = [doc.policy ?? {}, ...chain.flatMap((n) => (n.zone ? [n.zone] : []))];
    if (!treeAllowed(tree, levels, manifest)) continue;
    const def = contract(parent.type, manifest);
    if (!def) continue;
    for (const slot of [...(def.container ? [""] : []), ...Object.keys(def.slots ?? {})]) {
      const rule = def.slots?.[slot];
      if (rule?.allowedTypes && !rule.allowedTypes.includes(tree.type)) continue;
      const list = slot ? (parent.slots?.[slot] ?? []) : (parent.children ?? []);
      const count = list.length - (moving && list.includes(moving) ? 1 : 0);
      if (rule?.max !== undefined && count + 1 > rule.max) continue;
      for (let index = 0; index <= count; index++) {
        const place = { parentId: parent.id, slot, index };
        if (
          moving &&
          (!sourceAllowed(doc, moving, manifest, place) ||
            JSON.stringify(location(doc, moving)) === JSON.stringify(place))
        )
          continue;
        if (hasLocks) {
          const next = structuredClone(doc);
          structuralApply(next, {
            type: moving ? "node.move" : "node.insert",
            target: "",
            payload: moving ? { ...place, nodeId: moving } : { ...place, subtree: tree },
          });
          if (!preservesLocks(doc, next)) continue;
        }
        result.push(place);
      }
    }
  }
  return result;
}
export function insertion(documentId: string, place: Place, tree: NestedNode): Operation {
  return { type: "node.insert", target: documentId, payload: { ...place, subtree: tree } };
}
export function createBlock(
  type: string,
  manifest: Manifest,
  documentId: string,
  locale: string,
  values: Record<string, string>,
  taken: ReadonlySet<string>,
  generate = generateNodeId,
): { tree: NestedNode; operations: Operation[] } {
  const item = palette(manifest).find((p) => p.type === type);
  if (!item?.supported) throw new StudioError("STRUCTURE_SETUP_REQUIRED");
  const tree: NestedNode = { id: generate(taken), type },
    operations: Operation[] = [];
  const node: Node = { id: tree.id!, type };
  for (const [name, schema] of Object.entries(item.definition.props ?? {})) {
    if (schema.content && schema.type === "text") {
      const key = "t_" + crypto.randomUUID().replace(/-/g, "");
      const value = values[name] ?? ("default" in schema ? String(schema.default) : type);
      const ops = fieldOperations(
        documentId,
        node,
        { name, schema, editable: true, value: undefined, binding: undefined, translations: {} },
        value,
        key,
        locale,
      );
      operations.push(ops[0]!);
      (tree.bindings ??= {})[name] = "$local." + key;
    } else if (values[name] !== undefined || (schema.required && !("default" in schema))) {
      const op = fieldOperations(
        documentId,
        node,
        { name, schema, editable: true, value: undefined, binding: undefined, translations: {} },
        values[name] ?? "",
        "",
        locale,
      )[0]!;
      Object.assign((tree.props ??= {}), op.payload.set);
    } else if ("default" in schema && !schema.content)
      Object.assign((tree.props ??= {}), { [name]: structuredClone(schema.default) });
  }
  return { tree, operations };
}
function remap(value: unknown, schema: Type | undefined, ids: Map<string, string>): unknown {
  if (schema?.responsive && value && typeof value === "object" && "base" in value) {
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [
        key,
        remap(item, { ...schema, responsive: false }, ids),
      ]),
    );
  }
  if (schema?.type === "nodeRef" && typeof value === "string") return ids.get(value) ?? value;
  if (
    schema?.type === "link" &&
    value &&
    typeof value === "object" &&
    "kind" in value &&
    value.kind === "anchor" &&
    "node" in value &&
    typeof value.node === "string"
  )
    return { ...value, node: ids.get(value.node) ?? value.node };
  if (schema?.type === "list" && Array.isArray(value))
    return value.map((v) => remap(v, schema.of, ids));
  if (schema?.type === "object" && value && typeof value === "object")
    return Object.fromEntries(
      Object.entries(value).map(([k, v]) => [k, remap(v, schema.fields[k], ids)]),
    );
  return value;
}
export function duplicate(
  doc: IrDocument,
  id: string,
  manifest: Manifest,
  generate = generateNodeId,
): NestedNode {
  const tree = subtree(doc, id),
    taken = new Set(Object.keys(doc.nodes)),
    ids = new Map<string, string>();
  function walk(n: NestedNode, fn: (n: NestedNode) => void) {
    fn(n);
    n.children?.forEach((c) => walk(c, fn));
    Object.values(n.slots ?? {})
      .flat()
      .forEach((c) => walk(c, fn));
  }
  walk(tree, (n) => {
    const next = generate(taken);
    taken.add(next);
    ids.set(n.id!, next);
  });
  walk(tree, (n) => {
    n.id = ids.get(n.id!)!;
    const def = contract(n.type, manifest);
    if (n.props)
      n.props = Object.fromEntries(
        Object.entries(n.props).map(([k, v]) => [k, remap(v, def?.props?.[k], ids)]),
      );
    for (const [key, binding] of Object.entries(n.bindings ?? {})) {
      if (binding && typeof binding === "object" && "default" in binding) {
        binding.default = remap(binding.default, def?.props?.[key], ids);
      }
    }
    for (const event of Object.values(n.on ?? {}))
      for (const action of Array.isArray(event) ? event : [event]) {
        for (const [key, value] of Object.entries(action.args ?? {})) {
          let schema = manifest.actions?.[action.action]?.args?.[key];
          if (["openModal", "closeModal", "scrollTo"].includes(action.action) && key === "target")
            schema = { type: "nodeRef" };
          if (action.action === "navigate" && key === "to") schema = { type: "link" };
          if (value && typeof value === "object" && "lit" in value)
            action.args![key] = { lit: remap(value.lit, schema, ids) };
          else if (schema?.type === "link" && typeof value === "object")
            action.args![key] = remap(value, schema, ids) as typeof value;
        }
      }
  });
  return tree;
}
