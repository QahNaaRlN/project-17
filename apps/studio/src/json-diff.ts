import { validateDocument, type IrDocument, type Node, type NestedNode } from "@cms/ir";
import { canonicalJson } from "@cms/manifest";
import { applyOperations } from "./operations.js";
import { childList, location, structuralApply } from "./structure.js";
import { layers } from "./fields.js";
import type { Operation } from "./types.js";
export class JsonProblem extends Error {
  constructor(
    public code: string,
    public pointer: string = "",
  ) {
    super(code);
  }
}
const escape = (key: string) => key.replace(/~/g, "~0").replace(/\//g, "~1");
const same = (a: unknown, b: unknown) => canonicalJson(a) === canonicalJson(b);
const entries = (value: unknown) => Object.entries((value ?? {}) as Record<string, unknown>);
const keys = (a: unknown, b: unknown) =>
  [...new Set([...entries(a), ...entries(b)].map(([key]) => key))].sort();
// Empty optional operation containers have the same meaning as absence in CMS.
export function semanticDocument(source: IrDocument): IrDocument {
  const doc = structuredClone(source);
  for (const n of Object.values(doc.nodes)) {
    const map = n as unknown as Record<string, unknown>;
    if (n.slots) {
      for (const [slot, ids] of Object.entries(n.slots)) if (!ids.length) delete n.slots[slot];
    }
    for (const key of ["props", "design", "bindings", "on", "slots", "children"]) {
      const value = map[key];
      if (value && typeof value === "object" && !Object.keys(value).length) delete map[key];
    }
  }
  if (doc.localContent && !Object.keys(doc.localContent).length) delete doc.localContent;
  if (doc.dataSources && !Object.keys(doc.dataSources).length) delete doc.dataSources;
  return doc;
}
// Unique IDs turn LCS into LIS: O(n log n), bounded memory even for 5,000 siblings.
export function retainedOrder(before: string[], after: string[]): Set<string> {
  const positions = new Map(before.map((id, i) => [id, i])),
    values = after.filter((id) => positions.has(id));
  const tails: number[] = [],
    previous: number[] = [];
  for (let i = 0; i < values.length; i++) {
    const value = positions.get(values[i]!)!;
    let low = 0,
      high = tails.length;
    while (low < high) {
      const mid = (low + high) >>> 1;
      if (positions.get(values[tails[mid]!]!)! < value) low = mid + 1;
      else high = mid;
    }
    previous[i] = low ? tails[low - 1]! : -1;
    tails[low] = i;
  }
  const keep = new Set<string>();
  let at = tails.at(-1) ?? -1;
  while (at >= 0) {
    keep.add(values[at]!);
    at = previous[at]!;
  }
  return keep;
}
function lockedPath(doc: IrDocument, id: string): unknown[] {
  const path: unknown[] = [];
  let at = id;
  for (let p = location(doc, at); p; p = location(doc, at)) {
    path.push(p);
    at = p.parentId;
  }
  return path;
}
function newTree(after: IrDocument, id: string, before: IrDocument): NestedNode {
  const node = after.nodes[id]!;
  const { children, slots, ...data } = structuredClone(node);
  return {
    ...data,
    ...(children
      ? {
          children: children
            .filter((child) => !before.nodes[child])
            .map((child) => newTree(after, child, before)),
        }
      : {}),
    ...(slots
      ? {
          slots: Object.fromEntries(
            Object.entries(slots).map(([slot, ids]) => [
              slot,
              ids
                .filter((child) => !before.nodes[child])
                .map((child) => newTree(after, child, before)),
            ]),
          ),
        }
      : {}),
  };
}
export function documentDiff(
  source: IrDocument,
  candidate: IrDocument,
  target: string,
): Operation[] {
  const validated = validateDocument(candidate);
  if (!validated.valid) {
    const d = validated.diagnostics[0]!;
    throw new JsonProblem(d.code, d.pointer);
  }
  const before = semanticDocument(source),
    after = semanticDocument(candidate);
  for (const key of keys(before, after))
    if (
      !["nodes", "localContent", "content", "dataSources"].includes(key) &&
      !same(
        (before as unknown as Record<string, unknown>)[key],
        (after as unknown as Record<string, unknown>)[key],
      )
    )
      throw new JsonProblem("JSON_FIELD_IMMUTABLE", "/" + escape(key));
  for (const [id, node] of Object.entries(after.nodes)) {
    const old = before.nodes[id];
    for (const key of ["id", "type", "ref", "zone", "locked"]) {
      if (
        (old &&
          !same(
            (old as unknown as Record<string, unknown>)[key],
            (node as unknown as Record<string, unknown>)[key],
          )) ||
        (!old && ["zone", "locked"].includes(key) && Object.hasOwn(node, key))
      )
        throw new JsonProblem("JSON_FIELD_IMMUTABLE", `/nodes/${escape(id)}/${key}`);
    }
  }
  for (const item of layers(before).filter((n) => n.locked)) {
    if (
      !same(before.nodes[item.id], after.nodes[item.id]) ||
      !same(lockedPath(before, item.id), lockedPath(after, item.id))
    )
      throw new JsonProblem("JSON_NODE_READ_ONLY", `/nodes/${escape(item.id)}`);
    const localKeys = new Set<string>();
    const uses = (v: unknown): void => {
      if (typeof v === "string") {
        const match = /^\$local\.([A-Za-z0-9_]+)(?:\.|$)/.exec(v);
        if (match) localKeys.add(match[1]!);
      } else if (v && typeof v === "object") Object.values(v).forEach(uses);
    };
    uses(before.nodes[item.id]);
    for (const key of localKeys)
      if (!same(before.localContent?.[key], after.localContent?.[key]))
        throw new JsonProblem("JSON_NODE_READ_ONLY", "/localContent/" + escape(key));
  }
  // A one-for-one key replacement at the same place is ambiguous with renaming an ID.
  for (const id of Object.keys(before.nodes).filter((id) => !after.nodes[id])) {
    const place = location(before, id);
    if (!place) continue;
    const parent = after.nodes[place.parentId],
      replacement = place.slot
        ? parent?.slots?.[place.slot]?.[place.index]
        : parent?.children?.[place.index];
    if (replacement && !before.nodes[replacement])
      throw new JsonProblem("JSON_NODE_ID_IMMUTABLE", `/nodes/${escape(replacement)}/id`);
  }
  const operations: Operation[] = [],
    current = structuredClone(before);
  const emit = (type: Operation["type"], payload: Record<string, unknown>) => {
    const op = { type, target, payload };
    operations.push(op);
    if (operations.length > 200) throw new JsonProblem("JSON_BATCH_LIMIT");
    structuralApply(current, op);
  };
  function order(parentId: string) {
    const parent = after.nodes[parentId]!;
    for (const [slot, desired] of [
      ["", parent.children ?? []],
      ...Object.entries(parent.slots ?? {}).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)),
    ] as [string, string[]][]) {
      const place = { parentId, slot, index: 0 },
        existing = childList(current, place);
      const keep = retainedOrder(existing, desired);
      let next: string | undefined;
      for (const id of [...desired].reverse()) {
        if (!keep.has(id)) {
          const from = location(current, id),
            list = childList(current, place);
          let index = next ? list.indexOf(next) : list.length;
          if (from?.parentId === parentId && from.slot === slot && from.index < index) index--;
          if (current.nodes[id]) emit("node.move", { nodeId: id, parentId, slot, index });
          else emit("node.insert", { parentId, slot, index, subtree: newTree(after, id, before) });
        }
        next = id;
      }
    }
    for (const id of [...(parent.children ?? []), ...Object.values(parent.slots ?? {}).flat()])
      order(id);
  }
  order(after.root);
  const obsolete = Object.keys(current.nodes)
    .sort()
    .filter((id) => !after.nodes[id] && after.nodes[location(current, id)!.parentId]);
  for (const id of obsolete) emit("node.remove", { nodeId: id });
  const add = (type: Operation["type"], payload: Record<string, unknown>) => {
    operations.push({ type, target, payload });
    if (operations.length > 200) throw new JsonProblem("JSON_BATCH_LIMIT");
  };
  for (const id of Object.keys(after.nodes).sort()) {
    const old = before.nodes[id],
      node = after.nodes[id];
    if (!old) continue;
    const a = old as unknown as Record<string, unknown>,
      b = node as unknown as Record<string, unknown>;
    for (const field of ["props", "design"]) {
      const set: Record<string, unknown> = {},
        unset: string[] = [];
      const am = (a[field] ?? {}) as Record<string, unknown>,
        bm = (b[field] ?? {}) as Record<string, unknown>;
      for (const key of keys(am, bm))
        if (!same(am[key], bm[key])) {
          if (Object.hasOwn(bm, key)) set[key] = bm[key];
          else unset.push(key);
        }
      if (Object.keys(set).length || unset.length)
        add(field === "props" ? "node.setProps" : "node.setDesign", {
          nodeId: id,
          ...(Object.keys(set).length ? { set } : {}),
          ...(unset.length ? { unset } : {}),
        });
    }
    for (const field of ["bindings", "on"]) {
      const am = (a[field] ?? {}) as Record<string, unknown>,
        bm = (b[field] ?? {}) as Record<string, unknown>;
      for (const key of keys(am, bm))
        if (!same(am[key], bm[key]))
          add(
            field === "bindings" ? "node.setBinding" : "node.setBehavior",
            field === "bindings"
              ? { nodeId: id, prop: key, binding: bm[key] ?? null }
              : { nodeId: id, event: key, handlers: bm[key] ?? null },
          );
    }
    for (const field of ["name", "when"])
      if (!same(a[field], b[field]))
        add(field === "name" ? "node.rename" : "node.setCondition", {
          nodeId: id,
          [field]: b[field] ?? null,
        });
  }
  if (!same(before.content, after.content))
    add("document.setContent", { content: after.content ?? null });
  for (const key of keys(before.dataSources, after.dataSources)) {
    if (!same(before.dataSources?.[key], after.dataSources?.[key]))
      add("document.setDataSource", { key, use: after.dataSources?.[key] ?? null });
  }
  for (const key of keys(before.localContent, after.localContent)) {
    const a = before.localContent?.[key],
      b = after.localContent?.[key];
    if (same(a, b)) continue;
    if (
      !/^t_[a-z0-9_]{2,40}$/.test(key) ||
      [a, b].some(
        (v) =>
          v &&
          (v.type !== "text" || !v.value || typeof v.value !== "object" || Array.isArray(v.value)),
      )
    )
      throw new JsonProblem("JSON_LOCAL_CONTENT_UNSUPPORTED", "/localContent/" + escape(key));
    if (b) {
      const values = Object.entries(b.value as Record<string, unknown>);
      if (
        !values.length ||
        values.some(
          ([locale, v]) => !/^[a-z]{2,3}(-[A-Z]{2})?$/.test(locale) || typeof v !== "string",
        )
      )
        throw new JsonProblem("JSON_LOCAL_CONTENT_UNSUPPORTED", "/localContent/" + escape(key));
      add("localContent.set", { key, type: "text", value: b.value });
    } else add("localContent.remove", { key });
  }
  if (
    operations.length > 200 ||
    new TextEncoder().encode(
      JSON.stringify({
        reason: "Studio: edit page draft",
        payload: { changesetId: target, expectedSeq: 2147483647, group: true, operations },
      }),
    ).length >
      1024 * 1024
  )
    throw new JsonProblem("JSON_BATCH_LIMIT");
  const result = semanticDocument(applyOperations(before, operations));
  if (!same(result, after)) throw new JsonProblem("JSON_DIFF_UNSUPPORTED");
  return operations;
}
export function editedDocument(base: IrDocument, textValue: unknown, nodeId?: string): IrDocument {
  if (!nodeId) return textValue as IrDocument;
  if (!textValue || typeof textValue !== "object" || (textValue as Node).id !== nodeId)
    throw new JsonProblem("JSON_NODE_ID_IMMUTABLE", `/nodes/${escape(nodeId)}/id`);
  const doc = structuredClone(base);
  doc.nodes[nodeId] = textValue as Node;
  return doc;
}
