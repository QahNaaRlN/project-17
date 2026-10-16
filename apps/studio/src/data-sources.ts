import {
  generateNodeId,
  type IrDocument,
  type Node,
  type NestedNode,
  type DataSourceUse,
} from "@cms/ir";
import type { Manifest, Type } from "@cms/manifest";
import { contentCompatible, contentFields } from "./content-bindings.js";
import { contract, location, destinations, insertion, type Place } from "./structure.js";
import { applyOperations } from "./operations.js";
import { layers } from "./fields.js";
import { StudioError, type Operation } from "./types.js";

export const safeKey = (key: string) =>
  /^[A-Za-z][A-Za-z0-9_]*$/.test(key) &&
  !["constructor", "prototype", "__proto__", "content"].includes(key);
export interface DataField {
  path: string;
  schema: Type;
}
export function registeredSource(manifest: Manifest, name: string) {
  return Object.hasOwn(manifest.dataSources ?? {}, name) ? manifest.dataSources![name] : undefined;
}
export function compatible(from: Type, to: Type, depth = 0): boolean {
  if (depth > 8 || (to.required && !("default" in to) && !from.required && !("default" in from)))
    return false;
  if (to.type === "object" && from.type === "object")
    return (
      Object.keys(from.fields ?? {}).every((key) => Object.hasOwn(to.fields ?? {}, key)) &&
      Object.entries(to.fields ?? {}).every(([key, type]) => {
        const source = from.fields?.[key];
        return source ? compatible(source, type, depth + 1) : !type.required || "default" in type;
      })
    );
  if (to.type === "list" && from.type === "list")
    return (
      (to.min === undefined || (from.min !== undefined && from.min >= to.min)) &&
      (to.max === undefined || (from.max !== undefined && from.max <= to.max)) &&
      (!to.of || (!!from.of && compatible(from.of, to.of, depth + 1)))
    );
  if (to.type === "reference" && from.type === "reference") return to.schema === from.schema;
  if (to.type === "nodeRef" && from.type === "nodeRef")
    return !to.nodeType || to.nodeType === from.nodeType;
  if (to.type === "url" && from.type === "url")
    return !to.schemes || !!from.schemes?.every((scheme) => to.schemes!.includes(scheme));
  if (to.type === "richText" && from.type === "richText")
    return ["marks", "blocks"].every((key) => {
      const field = key as "marks" | "blocks",
        allowed = to[field],
        provided = from[field];
      return (
        !allowed || !!provided?.every((value) => (allowed as readonly string[]).includes(value))
      );
    });
  if (to.type === from.type && ["link", "color"].includes(to.type)) return true;
  return contentCompatible(from, to);
}
export function walkFields(path: string, schema: Type): { fields: DataField[]; limited: boolean } {
  const fields: DataField[] = [];
  let limited = false;
  function visit(path: string, schema: Type, depth: number, optional: boolean) {
    if (depth > 8 || fields.length >= 200) {
      limited = true;
      return;
    }
    const type = { ...schema, ...(optional ? { required: false } : {}) } as Type;
    fields.push({ path, schema: type });
    if (schema.type === "object")
      for (const [key, child] of Object.entries(schema.fields ?? {})) {
        if (/^[A-Za-z][A-Za-z0-9_]*$/.test(key) && !["constructor", "prototype"].includes(key))
          visit(path + "." + key, child, depth + 1, optional || !schema.required);
      }
  }
  visit(path, schema, 0, false);
  return { fields, limited };
}
export function resultFields(doc: IrDocument, manifest: Manifest) {
  const fields: DataField[] = [];
  let limited = false;
  for (const [key, use] of Object.entries(doc.dataSources ?? {})) {
    const source = registeredSource(manifest, use.source);
    if (!safeKey(key) || !source) continue;
    const root = { ...source.result, required: true } as Type;
    const result = walkFields(
      "$data." + key,
      source.paginated
        ? {
            type: "object",
            required: true,
            fields: { items: root, hasMore: { type: "boolean", required: true } },
          }
        : root,
    );
    limited ||= result.limited || fields.length + result.fields.length > 200;
    fields.push(...result.fields.slice(0, Math.max(0, 200 - fields.length)));
  }
  return { fields, limited };
}
// Empty slots deliberately retain only the outer item's scope.
export function itemScope(
  doc: IrDocument,
  manifest: Manifest,
  id: string,
): { repeat: string; schema: Type } | null {
  let current = id;
  const seen = new Set<string>();
  while (!seen.has(current)) {
    seen.add(current);
    const place = location(doc, current);
    if (!place) return null;
    const parent = doc.nodes[place.parentId]!;
    if (parent.type === "Repeat" && place.slot !== "empty") {
      const binding = parent.bindings?.items;
      const field = resultFields(doc, manifest).fields.find((f) => f.path === binding);
      if (field?.schema.type === "list" && field.schema.of)
        return { repeat: parent.id, schema: { ...field.schema.of, required: true } as Type };
      return null;
    }
    current = parent.id;
  }
  return null;
}
export function bindingFields(doc: IrDocument, manifest: Manifest, id: string) {
  const result = resultFields(doc, manifest),
    item = itemScope(doc, manifest, id);
  const scoped = item ? walkFields("$item", item.schema) : { fields: [], limited: false };
  return {
    fields: [...result.fields, ...scoped.fields],
    limited: result.limited || scoped.limited,
  };
}
export const directPath =
  /^\$(?:data\.[A-Za-z][A-Za-z0-9_]*|item)(?:\.[A-Za-z][A-Za-z0-9_]*){0,8}$/;
export function dataBinding(
  target: string,
  node: Node,
  prop: string,
  path: string | null,
): Operation[] {
  if (
    path !== null &&
    (!directPath.test(path) ||
      path.split(".").some((s) => ["constructor", "prototype", "__proto__"].includes(s)))
  )
    throw new StudioError("FIELD_INVALID");
  return [
    ...(path !== null && Object.hasOwn(node.props ?? {}, prop)
      ? [{ type: "node.setProps" as const, target, payload: { nodeId: node.id, unset: [prop] } }]
      : []),
    { type: "node.setBinding", target, payload: { nodeId: node.id, prop, binding: path } },
  ];
}
export function parameterFields(doc: IrDocument, manifest: Manifest): DataField[] {
  return [
    ...(doc.content ? contentFields(manifest, doc.content.schema).fields : []),
    ...Object.entries(doc.localContent ?? {})
      .filter(([, item]) => item.type === "text")
      .map(([key]) => ({
        path: "$local." + key,
        schema: { type: "text", required: true } as Type,
      })),
    { path: "$context.locale", schema: { type: "string", required: true } as Type },
  ];
}
export function literal(schema: Type, raw: string): unknown {
  if (schema.type === "number") {
    const value = raw.trim() ? Number(raw) : NaN;
    if (
      !Number.isFinite(value) ||
      (schema.integer && !Number.isInteger(value)) ||
      (schema.min !== undefined && value < schema.min) ||
      (schema.max !== undefined && value > schema.max)
    )
      throw new StudioError("FIELD_INVALID");
    return value;
  }
  if (schema.type === "boolean") {
    if (!["true", "false"].includes(raw)) throw new StudioError("FIELD_INVALID");
    return raw === "true";
  }
  if (schema.type === "enum") {
    if (!schema.values?.includes(raw)) throw new StudioError("FIELD_INVALID");
    return raw;
  }
  if (schema.type === "string" || schema.type === "text") {
    if (
      (schema.maxLength !== undefined && Array.from(raw).length > schema.maxLength) ||
      (schema.type === "string" &&
        ((schema.minLength !== undefined && Array.from(raw).length < schema.minLength) ||
          (schema.pattern && !new RegExp(schema.pattern, "u").test(raw))))
    )
      throw new StudioError("FIELD_INVALID");
    return raw;
  }
  throw new StudioError("FIELD_INVALID");
}
export const supportsLiteral = (schema: Type) =>
  ["string", "text", "number", "boolean", "enum"].includes(schema.type);
export function sourceSupported(source: NonNullable<Manifest["dataSources"]>[string]): boolean {
  return Object.values(source.params ?? {}).every(
    (type) => !type.required || "default" in type || supportsLiteral(type),
  );
}
export function sourceOperation(target: string, key: string, use: DataSourceUse | null): Operation {
  if (!safeKey(key)) throw new StudioError("FIELD_INVALID");
  return { type: "document.setDataSource", target, payload: { key, use } };
}
const escape = (key: string) => key.replace(/~/g, "~0").replace(/\//g, "~1");
function references(value: unknown, pointer: string, prefixes: string[], found: string[]) {
  if (
    typeof value === "string" &&
    prefixes.some((prefix) => value === prefix || value.startsWith(prefix + "."))
  )
    found.push(pointer);
  else if (value && typeof value === "object")
    for (const [key, child] of Object.entries(value))
      if (!["lit", "default", "template", "format"].includes(key))
        references(child, pointer + "/" + escape(key), prefixes, found);
}
export function sourceChange(
  doc: IrDocument,
  manifest: Manifest,
  target: string,
  key: string,
  use: DataSourceUse | null,
) {
  const operation = sourceOperation(target, key, use),
    after = applyOperations(doc, [operation]);
  const affected: string[] = [],
    blocked: string[] = [],
    removals: Operation[] = [];
  const protectedNodes = new Set(
    layers(doc)
      .filter((node) => node.locked)
      .map((node) => node.id),
  );
  const repeats = new Set(
    Object.values(doc.nodes)
      .filter(
        (n) =>
          n.type === "Repeat" &&
          typeof n.bindings?.items === "string" &&
          (n.bindings.items === "$data." + key ||
            n.bindings.items.startsWith("$data." + key + ".")),
      )
      .map((n) => n.id),
  );
  const descendants = new Set<string>();
  const visit = (id: string): void => {
    if (descendants.has(id)) return;
    descendants.add(id);
    const node = doc.nodes[id];
    for (const child of [...(node?.children ?? []), ...Object.values(node?.slots ?? {}).flat()])
      visit(child);
  };
  for (const id of repeats) visit(id);
  for (const node of Object.values(doc.nodes)) {
    const prefixes = [
      "$data." + key,
      ...(descendants.has(node.id) ? ["$item", "$each", "$index"] : []),
    ];
    for (const [prop, binding] of Object.entries(node.bindings ?? {})) {
      const pointer = `/nodes/${escape(node.id)}/bindings/${escape(prop)}`,
        found: string[] = [];
      references(binding, pointer, prefixes, found);
      if (!found.length) continue;
      affected.push(...found);
      if (protectedNodes.has(node.id)) {
        blocked.push(...found);
        continue;
      }
      if (typeof binding !== "string" || !directPath.test(binding)) {
        blocked.push(...found);
        continue;
      }
      const previous = bindingFields(doc, manifest, node.id).fields.find((f) => f.path === binding),
        next = bindingFields(after, manifest, node.id).fields.find((f) => f.path === binding),
        receiver = contract(node.type, manifest)?.props?.[prop];
      if (!previous || !receiver) {
        blocked.push(pointer);
        continue;
      }
      if (!next || !compatible(next.schema, receiver))
        removals.push(...dataBinding(target, node, prop, null));
    }
    references(node.when, `/nodes/${escape(node.id)}/when`, prefixes, blocked);
    references(node.on, `/nodes/${escape(node.id)}/on`, prefixes, blocked);
    if (node.type === "Repeat" && descendants.has(node.id) && !repeats.has(node.id))
      blocked.push(`/nodes/${escape(node.id)}`);
  }
  for (const [other, declaration] of Object.entries(doc.dataSources ?? {}))
    if (other !== key)
      references(
        declaration.params,
        `/dataSources/${escape(other)}/params`,
        ["$data." + key],
        blocked,
      );
  const operations = [...removals, operation];
  blocked.push(...scopeProblems(doc, applyOperations(doc, operations), manifest));
  if (operations.length > 200) blocked.push("/dataSources/" + escape(key));
  return { affected, blocked, operations };
}
export function repeatTree(
  list: DataField,
  limit: number,
  key: string,
  taken: ReadonlySet<string>,
): NestedNode {
  if (
    list.schema.type !== "list" ||
    !list.schema.of ||
    !Number.isInteger(limit) ||
    limit < 1 ||
    limit > 200
  )
    throw new StudioError("FIELD_INVALID");
  const choices = walkFields("$item", { ...list.schema.of, required: true } as Type).fields.filter(
    (f) => ["string", "number"].includes(f.schema.type) && f.schema.required && f.path !== "$item",
  );
  if (key && !choices.some((f) => f.path === key)) throw new StudioError("FIELD_INVALID");
  const id = generateNodeId(taken),
    ids = new Set([...taken, id]),
    child = generateNodeId(ids);
  ids.add(child);
  const empty = generateNodeId(ids);
  return {
    id,
    type: "Repeat",
    props: { limit, ...(key ? { key } : {}) },
    bindings: { items: list.path },
    children: [{ id: child, type: "Stack", children: [] }],
    slots: { empty: [{ id: empty, type: "Stack", children: [] }] },
  };
}
export function repeatPlaces(doc: IrDocument, manifest: Manifest, tree: NestedNode) {
  return destinations(doc, manifest, tree).filter(
    (p) => doc.nodes[p.parentId]?.type !== "Repeat" && !itemScope(doc, manifest, p.parentId),
  );
}
export function insertRepeat(target: string, place: Place, tree: NestedNode): Operation {
  return insertion(target, place, tree);
}

export function scopeProblems(before: IrDocument, after: IrDocument, manifest: Manifest): string[] {
  const problems: string[] = [];
  for (const node of Object.values(after.nodes)) {
    for (const [prop, value] of Object.entries(node.bindings ?? {})) {
      if (typeof value !== "string" || !directPath.test(value)) continue;
      const old = bindingFields(before, manifest, node.id).fields.find((f) => f.path === value);
      const current = bindingFields(after, manifest, node.id).fields.find((f) => f.path === value);
      const target = contract(node.type, manifest)?.props?.[prop];
      if (old && (!current || !target || !compatible(current.schema, target)))
        problems.push(`/nodes/${escape(node.id)}/bindings/${escape(prop)}`);
    }
    if (
      node.type === "Repeat" &&
      node.props &&
      "key" in node.props &&
      typeof node.props.key === "string"
    ) {
      const binding = node.bindings?.items,
        list = resultFields(after, manifest).fields.find((f) => f.path === binding);
      if (list?.schema.type === "list" && list.schema.of) {
        const key = node.props.key.startsWith("$") ? node.props.key : "$item." + node.props.key;
        const field = walkFields("$item", {
          ...list.schema.of,
          required: true,
        } as Type).fields.find((f) => f.path === key);
        if (!field || !["string", "number"].includes(field.schema.type))
          problems.push(`/nodes/${escape(node.id)}/props/key`);
      }
    }
  }
  return problems;
}
