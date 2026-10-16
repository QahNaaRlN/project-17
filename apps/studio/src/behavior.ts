import type { IrDocument, Node, ActionCall } from "@cms/ir";
import type { Manifest, Type, Fields } from "@cms/manifest";
import { linkHref } from "@cms/runtime";
import {
  bindingFields,
  compatible,
  literal,
  supportsLiteral,
  walkFields,
  type DataField,
} from "./data-sources.js";
import { contract, location } from "./structure.js";
import { layers } from "./fields.js";
import type { Operation } from "./types.js";

export const builtinBehavior: Record<string, Fields> = {
  navigate: { to: { type: "link", required: true } },
  scrollTo: { target: { type: "nodeRef", required: true } },
};
export function actionFields(manifest: Manifest, name: string): Fields | undefined {
  return Object.hasOwn(builtinBehavior, name)
    ? builtinBehavior[name]
    : Object.hasOwn(manifest.actions ?? {}, name)
      ? (manifest.actions![name]!.args ?? {})
      : undefined;
}
export function behaviorFields(doc: IrDocument, manifest: Manifest, id: string, path = "") {
  const scoped = bindingFields(doc, manifest, id);
  const fields: DataField[] = [
    ...scoped.fields,
    ...Object.entries(doc.localContent ?? {})
      .filter(([, v]) => v.type === "text")
      .map(([key]) => ({
        path: "$local." + key,
        schema: { type: "text", required: true } as Type,
      })),
    ...(doc.content
      ? walkFields("$content", {
          type: "object",
          required: true,
          fields: manifest.schemas?.[doc.content.schema]?.fields ?? {},
        }).fields
      : []),
    { path: "$context.locale", schema: { type: "string", required: true } },
    { path: "$context.route.path", schema: { type: "string", required: true } },
    { path: "$context.viewer.authenticated", schema: { type: "boolean", required: true } },
    ...[...path.matchAll(/:([A-Za-z][A-Za-z0-9_]*)/g)].map((m) => ({
      path: "$context.route.params." + m[1],
      schema: { type: "string", required: true } as Type,
    })),
  ];
  const ancestors: Node[] = [];
  const seen = new Set<string>();
  let current = id;
  while (!seen.has(current)) {
    seen.add(current);
    const parent = location(doc, current);
    if (!parent) break;
    current = parent.parentId;
    ancestors.unshift(doc.nodes[current]!);
  }
  const provides = Object.assign(
    {},
    ...ancestors.map((node) => contract(node.type, manifest)?.provides ?? {}),
  ) as Fields;
  for (const [name, type] of Object.entries(provides))
    fields.push(...walkFields("$context." + name, type).fields);
  const unique = [...new Map(fields.map((f) => [f.path, f])).values()];
  return { fields: unique.slice(0, 200), limited: scoped.limited || unique.length > 200 };
}
export const scalarCondition = (schema: Type) =>
  ["string", "text", "enum", "number", "boolean", "date", "datetime"].includes(schema.type);
const object = (value: unknown): Record<string, unknown> | undefined =>
  value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
const exact = (value: Record<string, unknown>, keys: string[]) =>
  Object.keys(value).every((key) => keys.includes(key));
export function validOperand(
  value: unknown,
  schema: Type,
  fields: DataField[],
  doc: IrDocument,
): boolean {
  if (value === undefined) return !schema.required || "default" in schema;
  if (typeof value === "string")
    return fields.some((f) => f.path === value && compatible(f.schema, schema));
  const operand = object(value);
  if (!operand || !exact(operand, ["lit"]) || !Object.hasOwn(operand, "lit")) return false;
  const v = operand.lit;
  if (v === null) return !schema.required;
  if (schema.type === "link") {
    const link = object(v);
    return (
      !!link &&
      (link.kind === "url"
        ? exact(link, ["kind", "url"]) && !!linkHref(link)
        : link.kind === "anchor" &&
          exact(link, ["kind", "node"]) &&
          typeof link.node === "string" &&
          Object.hasOwn(doc.nodes, link.node))
    );
  }
  if (schema.type === "nodeRef")
    return (
      typeof v === "string" &&
      Object.hasOwn(doc.nodes, v) &&
      (!schema.nodeType || doc.nodes[v]!.type === schema.nodeType)
    );
  if (schema.type === "date" || schema.type === "datetime") {
    return (
      typeof v === "string" &&
      Number.isFinite(Date.parse(v)) &&
      (schema.type === "date"
        ? /^\d{4}-\d{2}-\d{2}$/.test(v) && new Date(v).toISOString().slice(0, 10) === v
        : /^\d{4}-\d{2}-\d{2}T/.test(v) && Number.isFinite(Date.parse(v)))
    );
  }
  if (supportsLiteral(schema)) {
    try {
      return JSON.stringify(literal(schema, String(v))) === JSON.stringify(v);
    } catch {
      return false;
    }
  }
  return false;
}
export function editableChain(
  value: unknown,
  manifest: Manifest,
  fields: DataField[],
  doc: IrDocument,
): value is ActionCall[] | ActionCall {
  const chain = Array.isArray(value) ? value : [value];
  return (
    chain.length >= 1 &&
    chain.length <= 8 &&
    chain.every((call) => {
      const a = object(call);
      if (
        !a ||
        !exact(a, ["action", "args"]) ||
        typeof a.action !== "string" ||
        a.action.startsWith("pending:")
      )
        return false;
      const schema = actionFields(manifest, a.action),
        args = object(a.args ?? {});
      return (
        !!schema &&
        !!args &&
        Object.keys(args).every((key) => Object.hasOwn(schema, key)) &&
        Object.entries(schema).every(([key, type]) => validOperand(args[key], type, fields, doc))
      );
    })
  );
}
export function predicateStats(value: unknown): { count: number; depth: number } {
  const v = object(value);
  if (!v) return { count: 0, depth: 0 };
  const children =
    v.op === "and" || v.op === "or" ? (v.args as unknown[]) : v.op === "not" ? [v.arg] : [];
  const stats = (Array.isArray(children) ? children : []).map(predicateStats);
  return {
    count: 1 + stats.reduce((sum, s) => sum + s.count, 0),
    depth: stats.length ? 1 + Math.max(...stats.map((s) => s.depth)) : 0,
  };
}
export function editablePredicate(value: unknown, fields: DataField[], doc: IrDocument): boolean {
  const stats = predicateStats(value);
  if (stats.count > 64 || stats.depth > 8) return false;
  function visit(value: unknown): boolean {
    const p = object(value);
    if (!p || typeof p.op !== "string") return false;
    if (p.op === "not") return exact(p, ["op", "arg"]) && visit(p.arg);
    if (p.op === "and" || p.op === "or")
      return (
        exact(p, ["op", "args"]) &&
        Array.isArray(p.args) &&
        p.args.length >= 1 &&
        p.args.length <= 16 &&
        p.args.every(visit)
      );
    if (p.op === "exists" || p.op === "empty")
      return exact(p, ["op", "arg"]) && fields.some((f) => f.path === p.arg);
    if (!exact(p, ["op", "args"]) || !Array.isArray(p.args) || p.args.length !== 2) return false;
    const schema = fields.find((f) => f.path === (p.args as unknown[])[0])?.schema;
    if (!schema || !scalarCondition(schema)) return false;
    // Comparisons do not require presence of an optional field.
    const scalar = { ...schema, required: false } as Type;
    const right = p.args[1],
      lit = object(right);
    if (right === undefined) return false;
    if (p.op === "eq" || p.op === "neq") return validOperand(right, scalar, fields, doc);
    if (["gt", "gte", "lt", "lte"].includes(p.op))
      return (
        schema.type === "number" && lit?.lit !== null && validOperand(right, scalar, fields, doc)
      );
    if (p.op === "in")
      return (
        !!lit &&
        exact(lit, ["lit"]) &&
        Array.isArray(lit.lit) &&
        lit.lit.length >= 1 &&
        lit.lit.length <= 16 &&
        lit.lit.every((v) => v !== null && validOperand({ lit: v }, scalar, fields, doc))
      );
    return false;
  }
  return visit(value);
}
export function conditionLocked(doc: IrDocument, id: string): boolean {
  const tree = layers(doc);
  if (tree.find((n) => n.id === id)?.locked) return true;
  const visit = (current: string): boolean => {
    const n = doc.nodes[current];
    return (
      !!n &&
      (n.locked === true ||
        n.zone?.locked === true ||
        [...(n.children ?? []), ...Object.values(n.slots ?? {}).flat()].some(visit))
    );
  };
  return visit(id);
}
export function behaviorOperation(
  target: string,
  nodeId: string,
  event: string,
  chain: ActionCall[] | null,
): Operation {
  return {
    type: "node.setBehavior",
    target,
    payload: { nodeId, event, handlers: chain?.length ? chain : null },
  };
}
export function conditionOperation(target: string, nodeId: string, when: unknown): Operation {
  return { type: "node.setCondition", target, payload: { nodeId, when } };
}

/** Whole-event/whole-condition removals are offered only for the supported direct subset. */
export function behaviorDependencies(
  before: IrDocument,
  after: IrDocument,
  manifest: Manifest,
  target: string,
  id: string,
  prefixes: string[],
  path = "",
) {
  const node = before.nodes[id]!,
    next = after.nodes[id]!;
  if (!next.on && !next.when) return { affected: [], blocked: [], operations: [] as Operation[] };
  const oldFields = behaviorFields(before, manifest, id, path).fields;
  const newFields = behaviorFields(after, manifest, id, path).fields;
  const affected: string[] = [],
    blocked: string[] = [],
    operations: Operation[] = [];
  function uses(value: unknown): boolean {
    if (typeof value === "string")
      return prefixes.some((p) => value === p || value.startsWith(p + "."));
    if (Array.isArray(value)) return value.some(uses);
    return (
      !!object(value) &&
      Object.entries(value as Record<string, unknown>).some(
        ([k, v]) => !["lit", "default", "template", "format"].includes(k) && uses(v),
      )
    );
  }
  const escape = (s: string) => s.replace(/~/g, "~0").replace(/\//g, "~1");
  const locked = layers(before).find((n) => n.id === id)?.locked;
  for (const [event, chain] of Object.entries(next.on ?? {})) {
    if (!uses(chain)) continue;
    const pointer = `/nodes/${escape(id)}/on/${escape(event)}`;
    affected.push(pointer);
    if (locked || !editableChain(node.on?.[event], manifest, oldFields, before))
      blocked.push(pointer);
    else if (!editableChain(chain, manifest, newFields, after))
      operations.push(behaviorOperation(target, id, event, null));
  }
  if (uses(next.when)) {
    const pointer = `/nodes/${escape(id)}/when`;
    affected.push(pointer);
    if (conditionLocked(before, id) || !editablePredicate(node.when, oldFields, before))
      blocked.push(pointer);
    else if (!editablePredicate(next.when, newFields, after))
      operations.push(conditionOperation(target, id, null));
  }
  return { affected, blocked, operations };
}
