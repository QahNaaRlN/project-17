import type { IrDocument, Node } from "@cms/ir";
import type { Manifest, Type } from "@cms/manifest";
import { nodeContract } from "./structure.js";
import { StudioError, type Operation } from "./types.js";
import { behaviorDependencies } from "./behavior.js";

export const simpleContentPath = /^\$content(?:\.[A-Za-z][A-Za-z0-9_]*){1,8}$/;
const safe = (key: string) =>
  /^[A-Za-z][A-Za-z0-9_]*$/.test(key) && !["__proto__", "prototype", "constructor"].includes(key);
type FieldType = Type & { required?: boolean; default?: unknown };
export function contentCompatible(from: FieldType, to: FieldType): boolean {
  if (to.required && to.default === undefined && !from.required && from.default === undefined)
    return false;
  if (to.type === "text" || to.type === "string") {
    if (from.type !== "text" && from.type !== "string") return false;
    if (
      to.type === "string" &&
      (to.minLength ?? 0) > 0 &&
      (from.type !== "string" || (from.minLength ?? 0) < to.minLength!)
    )
      return false;
    const pattern = to.type === "string" ? to.pattern : undefined;
    return (
      (to.maxLength === undefined ||
        (from.maxLength !== undefined && from.maxLength <= to.maxLength)) &&
      (!pattern || (from.type === "string" && from.pattern === pattern))
    );
  }
  if (from.type !== to.type) return false;
  if (to.type === "asset" && from.type === "asset")
    return !to.assetKind || to.assetKind === from.assetKind;
  if (to.type === "number" && from.type === "number")
    return (
      (!to.integer || from.integer === true) &&
      (to.min === undefined || (from.min !== undefined && from.min >= to.min)) &&
      (to.max === undefined || (from.max !== undefined && from.max <= to.max))
    );
  if (to.type === "enum" && from.type === "enum")
    return !!from.values?.length && from.values.every((v) => to.values?.includes(v));
  return ["boolean", "date", "datetime", "url"].includes(to.type);
}
export interface ContentField {
  path: string;
  schema: Type;
  value: unknown;
  present: boolean;
}
export function contentFields(
  manifest: Manifest,
  name: string,
  data: Record<string, unknown> = {},
  locale = "ru",
): { fields: ContentField[]; limited: boolean } {
  const result: ContentField[] = [];
  let limited = false;
  function walk(
    fields: Record<string, Type>,
    values: Record<string, unknown>,
    prefix: string,
    depth: number,
    optional: boolean,
  ) {
    for (const [key, raw] of Object.entries(fields)) {
      if (!safe(key)) continue;
      if (depth > 8) {
        limited = true;
        continue;
      }
      const path = prefix + "." + key;
      const schema = { ...raw, ...(optional ? { required: false } : {}) } as Type;
      let value = values[key],
        present = Object.hasOwn(values, key);
      if (raw.localized && present) {
        const translations = object(value);
        present = Object.hasOwn(translations, locale);
        value = translations[locale];
      }
      if (raw.type === "object")
        walk(raw.fields ?? {}, object(value), path, depth + 1, optional || !raw.required);
      else if (
        [
          "string",
          "text",
          "number",
          "boolean",
          "enum",
          "asset",
          "date",
          "datetime",
          "url",
        ].includes(raw.type)
      ) {
        if (result.length >= 200) limited = true;
        else result.push({ path, schema, value, present });
      }
    }
  }
  walk(manifest.schemas?.[name]?.fields ?? {}, data, "$content", 1, false);
  return { fields: result, limited };
}
function object(v: unknown): Record<string, unknown> {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
}
export function bindingOperations(
  target: string,
  node: Node,
  prop: string,
  path: string | null,
): Operation[] {
  if (
    path !== null &&
    (!simpleContentPath.test(path) ||
      path
        .split(".")
        .slice(1)
        .some((key) => !safe(key)))
  )
    throw new StudioError("FIELD_INVALID");
  return [
    ...(path !== null && Object.hasOwn(node.props ?? {}, prop)
      ? [{ type: "node.setProps" as const, target, payload: { nodeId: node.id, unset: [prop] } }]
      : []),
    { type: "node.setBinding", target, payload: { nodeId: node.id, prop, binding: path } },
  ];
}
export function sourceChange(
  doc: IrDocument,
  manifest: Manifest,
  target: string,
  content: IrDocument["content"] | null,
  pagePath = "",
) {
  const choices = content ? contentFields(manifest, content.schema).fields : [];
  const previous = contentFields(manifest, doc.content?.schema ?? "").fields;
  const affected: { pointer: string; nodeId: string; prop: string }[] = [],
    blocked: string[] = [];
  const esc = (k: string) => k.replace(/~/g, "~0").replace(/\//g, "~1");
  const schemaChanged = doc.content?.schema !== content?.schema;
  const after = { ...doc, content: content ?? undefined } as IrDocument;
  const behaviorOps: Operation[] = [];
  function scan(v: unknown, pointer: string) {
    if (typeof v === "string" && v.startsWith("$content") && schemaChanged) blocked.push(pointer);
    else if (v && typeof v === "object")
      for (const [key, child] of Object.entries(v))
        if (!["lit", "default", "format", "template"].includes(key))
          scan(child, pointer + "/" + esc(key));
  }
  for (const node of Object.values(doc.nodes)) {
    for (const [prop, binding] of Object.entries(node.bindings ?? {})) {
      const pointer = `/nodes/${esc(node.id)}/bindings/${esc(prop)}`;
      if (typeof binding === "string" && simpleContentPath.test(binding)) {
        const field = choices.find((f) => f.path === binding),
          receiver = nodeContract(node, manifest)?.props?.[prop];
        if (field && receiver && contentCompatible(field.schema, receiver)) continue;
        if (doc.content && !previous.some((f) => f.path === binding)) {
          scan(binding, pointer);
          continue;
        }
        affected.push({ pointer, nodeId: node.id, prop });
      } else scan(binding, pointer);
    }
    if (schemaChanged) {
      const change = behaviorDependencies(
        doc,
        after,
        manifest,
        target,
        node.id,
        ["$content"],
        pagePath,
      );
      blocked.push(...change.blocked);
      behaviorOps.push(...change.operations);
      affected.push(
        ...change.affected.map((pointer) => ({ pointer, nodeId: node.id, prop: "on/when" })),
      );
    }
  }
  scan(doc.dataSources, "/dataSources");
  const operations: Operation[] = affected.map((item) => ({
    type: "node.setBinding",
    target,
    payload: { nodeId: item.nodeId, prop: item.prop, binding: null },
  }));
  operations.splice(
    0,
    operations.length,
    ...operations.filter((op) => op.payload.prop !== "on/when"),
    ...behaviorOps,
  );
  operations.push({ type: "document.setContent", target, payload: { content } });
  if (operations.length > 200) blocked.push("/content");
  return { affected, blocked, operations };
}
