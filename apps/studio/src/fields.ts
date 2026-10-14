import { builtinCatalogue } from "@cms/manifest/builtins";
import type { IrDocument, Node } from "@cms/ir";
import type { Manifest, Type } from "@cms/manifest";
import { StudioError, type Operation } from "./types.js";
export interface Field {
  name: string;
  schema: Type;
  value: unknown;
  binding: unknown;
  editable: boolean;
  translations: Record<string, unknown>;
}
const safe = (key: string) =>
  /^[a-zA-Z][a-zA-Z0-9_]*$/.test(key) && !["constructor", "prototype", "__proto__"].includes(key);
// Matches policydoc's applicability for the two controls offered by Studio.
export function designFields(node: Node, manifest: Manifest): ("padding" | "gap")[] {
  const contract =
    (builtinCatalogue as unknown as Record<string, { container?: boolean }>)[node.type] ??
    manifest.components?.[node.type] ??
    manifest.primitives?.[node.type];
  return [
    ...(contract?.container || node.type === "Button" ? ["padding" as const] : []),
    ...(["Stack", "Flex", "Grid"].includes(node.type) ? ["gap" as const] : []),
  ];
}
export function fields(doc: IrDocument, node: Node, manifest: Manifest, locale = "ru"): Field[] {
  const contract =
    (builtinCatalogue as unknown as Record<string, { props?: Record<string, Type> }>)[node.type] ??
    manifest.primitives?.[node.type] ??
    manifest.components?.[node.type];
  return Object.entries(contract?.props ?? {}).map(([name, schema]) => {
    const binding = node.bindings?.[name];
    const local =
      typeof binding === "string" && /^\$local\.[a-zA-Z][a-zA-Z0-9_]*$/.test(binding)
        ? doc.localContent?.[binding.slice(7)]
        : undefined;
    const value =
      local?.type === "text"
        ? (local.value as Record<string, unknown>)[locale]
        : (node.props as Record<string, unknown> | undefined)?.[name];
    const scalar = ["string", "text", "number", "boolean", "enum"].includes(schema.type);
    return {
      name,
      schema,
      value,
      binding,
      translations: local?.type === "text" ? (local.value as Record<string, unknown>) : {},
      editable:
        safe(name) &&
        scalar &&
        (binding === undefined || local !== undefined) &&
        (value === undefined || typeof value !== "object") &&
        !(schema.content && schema.type !== "text"),
    };
  });
}
export function fieldOperations(
  documentId: string,
  node: Node,
  field: Field,
  raw: string,
  localId: string,
  locale = "ru",
): Operation[] {
  if (!field.editable) throw new StudioError("FIELD_READ_ONLY");
  const schema = field.schema;
  let value: string | number | boolean = raw;
  if (schema.type === "number") {
    value = raw.trim() === "" ? NaN : Number(raw);
    if (
      !Number.isFinite(value) ||
      (schema.integer && !Number.isInteger(value)) ||
      (schema.min !== undefined && value < schema.min) ||
      (schema.max !== undefined && value > schema.max)
    )
      throw new StudioError("FIELD_INVALID");
  } else if (schema.type === "boolean") {
    if (raw !== "true" && raw !== "false") throw new StudioError("FIELD_INVALID");
    value = raw === "true";
  } else if (schema.type === "enum" && !(schema.values ?? []).includes(raw))
    throw new StudioError("FIELD_INVALID");
  else if (
    (schema.type === "text" || schema.type === "string") &&
    ((schema.maxLength !== undefined && Array.from(raw).length > schema.maxLength) ||
      (schema.type === "string" &&
        schema.minLength !== undefined &&
        Array.from(raw).length < schema.minLength))
  )
    throw new StudioError("FIELD_INVALID");
  if (schema.type === "text" && schema.content) {
    // Always create a private local entry: a previous entry may be shared elsewhere.
    if (!/^t_[a-z0-9_]{2,40}$/.test(localId)) throw new StudioError("FIELD_INVALID");
    return [
      {
        type: "localContent.set",
        target: documentId,
        payload: { key: localId, type: "text", value: { ...field.translations, [locale]: value } },
      },
      ...(Object.hasOwn(node.props ?? {}, field.name)
        ? [
            {
              type: "node.setProps" as const,
              target: documentId,
              payload: { nodeId: node.id, unset: [field.name] },
            },
          ]
        : []),
      {
        type: "node.setBinding",
        target: documentId,
        payload: { nodeId: node.id, prop: field.name, binding: "$local." + localId },
      },
    ];
  }
  return [
    {
      type: "node.setProps",
      target: documentId,
      payload: { nodeId: node.id, set: { [field.name]: value } },
    },
  ];
}
export function designOperation(
  documentId: string,
  node: Node,
  name: string,
  raw: string,
  manifest: Manifest,
  breakpoint = "base",
): Operation {
  if (
    !["padding", "gap"].includes(name) ||
    (breakpoint !== "base" && !Object.hasOwn(manifest.breakpoints, breakpoint))
  )
    throw new StudioError("FIELD_INVALID");
  let value: unknown;
  if (Object.hasOwn(manifest.tokens?.spacing ?? {}, raw)) value = raw;
  else {
    const number = raw.trim() === "" ? NaN : Number(raw);
    if (!Number.isFinite(number) || number < 0 || number > 4000)
      throw new StudioError("FIELD_INVALID");
    value = { raw: number };
  }
  const previous = node.design?.[name];
  if (breakpoint !== "base")
    value = {
      ...(previous && typeof previous === "object" && "base" in previous
        ? previous
        : { base: previous ?? { raw: 0 } }),
      [breakpoint]: value,
    };
  else if (previous && typeof previous === "object" && "base" in previous)
    value = { ...previous, base: value };
  return {
    type: "node.setDesign",
    target: documentId,
    payload: { nodeId: node.id, set: { [name]: value } },
  };
}
export interface Layer {
  id: string;
  depth: number;
  parent: string | null;
  label: string;
  locked: boolean;
}
export function layers(document: IrDocument): Layer[] {
  const result: Layer[] = [],
    seen = new Set<string>();
  function walk(id: string, depth: number, parent: string | null, locked: boolean) {
    const node = document.nodes[id];
    if (!node || seen.has(id)) return;
    seen.add(id);
    locked ||= node.locked === true || node.zone?.locked === true;
    result.push({ id, depth, parent, label: node.name ?? node.type, locked });
    for (const child of [...(node.children ?? []), ...Object.values(node.slots ?? {}).flat()])
      walk(child, depth + 1, id, locked);
  }
  walk(document.root, 1, null, false);
  return result;
}
