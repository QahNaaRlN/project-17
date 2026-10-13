import { jsonLanguage } from "@codemirror/lang-json";
import { IR_LIMITS, irSchema, type IrDocument } from "@cms/ir";
import { canonicalJson, type Manifest } from "@cms/manifest";
import { contract } from "./structure.js";
import { JsonProblem, documentDiff, editedDocument } from "./json-diff.js";
import type { Operation } from "./types.js";
export interface JsonDiagnostic {
  code: string;
  pointer: string;
  from: number;
  to: number;
}
export interface JsonRange {
  pointer: string;
  from: number;
  to: number;
}
type SyntaxNode = ReturnType<typeof jsonLanguage.parser.parse>["topNode"];
export function jsonRanges(text: string): JsonRange[] {
  const tree = jsonLanguage.parser.parse(text),
    ranges: JsonRange[] = [];
  let depth = 0;
  tree.iterate({
    enter(n) {
      if (n.type.isError) throw new JsonProblem("JSON_SYNTAX", String(n.from));
      if (["Object", "Array"].includes(n.name) && ++depth > 64)
        throw new JsonProblem("JSON_DEPTH_LIMIT");
    },
    leave(n) {
      if (["Object", "Array"].includes(n.name)) depth--;
    },
  });
  const walk = (node: SyntaxNode, path: string) => {
    ranges.push({ pointer: path, from: node.from, to: node.to });
    if (node.name === "Object") {
      const seen = new Set<string>();
      for (let p = node.firstChild; p; p = p.nextSibling)
        if (p.name === "Property") {
          const name = p.getChild("PropertyName")!,
            key = JSON.parse(text.slice(name.from, name.to)) as string;
          const childPath = path + "/" + key.replace(/~/g, "~0").replace(/\//g, "~1");
          if (seen.has(key)) throw new JsonProblem("JSON_DUPLICATE_KEY", childPath);
          if (["__proto__", "constructor", "prototype"].includes(key))
            throw new JsonProblem("JSON_UNSAFE_KEY", childPath);
          seen.add(key);
          const value = name.nextSibling!.nextSibling!;
          ranges.push({ pointer: childPath, from: name.from, to: value.to });
          walk(value, childPath);
        }
    } else if (node.name === "Array") {
      let index = 0;
      for (let c = node.firstChild; c; c = c.nextSibling)
        if (!["[", "]", ","].includes(c.name)) walk(c, path + "/" + index++);
    }
  };
  walk(tree.topNode.firstChild!, "");
  return ranges;
}
export function pointerRange(ranges: JsonRange[], pointer: string): { from: number; to: number } {
  let path = pointer;
  while (true) {
    const found = [...ranges].reverse().find((r) => r.pointer === path);
    if (found) return { from: found.from, to: found.to };
    if (!path) return { from: 0, to: 0 };
    path = path.slice(0, path.lastIndexOf("/"));
  }
}
export function analyzeJson(
  base: IrDocument,
  text: string,
  target: string,
  nodeId?: string,
): { operations: Operation[]; diagnostics: JsonDiagnostic[]; ranges: JsonRange[] } {
  let ranges: JsonRange[] = [];
  try {
    if (new TextEncoder().encode(text).length > IR_LIMITS.maxBodyBytes)
      throw new JsonProblem("JSON_BODY_LIMIT");
    ranges = jsonRanges(text);
    const parsed = JSON.parse(text);
    const finite = (v: unknown): void => {
      if (typeof v === "number" && !Number.isFinite(v))
        throw new JsonProblem("JSON_NUMBER_INVALID");
      if (v && typeof v === "object") Object.values(v).forEach(finite);
    };
    finite(parsed);
    canonicalJson(parsed); // Same Unicode restrictions used by shared JSON canonicalization.
    return {
      operations: documentDiff(base, editedDocument(base, parsed, nodeId), target),
      diagnostics: [],
      ranges,
    };
  } catch (cause) {
    const error = cause instanceof JsonProblem ? cause : new JsonProblem("JSON_VALUE_INVALID");
    const prefix = nodeId ? "/nodes/" + nodeId.replace(/~/g, "~0").replace(/\//g, "~1") : "";
    const pointer =
      prefix && error.pointer.startsWith(prefix)
        ? error.pointer.slice(prefix.length)
        : error.pointer;
    const range =
      error.code === "JSON_SYNTAX"
        ? { from: Number(error.pointer), to: Math.min(text.length, Number(error.pointer) + 1) }
        : pointerRange(ranges, pointer);
    return { operations: [], diagnostics: [{ code: error.code, pointer, ...range }], ranges };
  }
}
type ObjectSchema = { properties?: Record<string, unknown>; $defs?: Record<string, ObjectSchema> };
export function jsonSuggestions(
  doc: IrDocument,
  manifest: Manifest,
  pointer: string,
  nodeId?: string,
): string[] {
  const schema = irSchema as unknown as ObjectSchema;
  const prefix = nodeId ? "/nodes/" + nodeId : "";
  const path = prefix + pointer,
    parts = path.split("/").filter(Boolean);
  const id = parts[0] === "nodes" ? parts[1] : undefined,
    node = id ? doc.nodes[id] : undefined;
  if (node) {
    if (parts[2] === "props" || parts[2] === "bindings")
      return Object.keys(contract(node.type, manifest)?.props ?? {});
    if (parts[2] === "design")
      return [
        ...new Set([
          "padding",
          "gap",
          ...Object.keys(schema.$defs?.design?.properties ?? {}),
          ...Object.keys(node.design ?? {}),
        ]),
      ];
    if (parts[2] === "type")
      return [
        ...new Set([
          "Box",
          "Stack",
          "Heading",
          "Text",
          ...Object.keys(manifest.primitives ?? {}),
          ...Object.keys(manifest.components ?? {}),
        ]),
      ];
    return Object.keys(schema.$defs?.node?.properties ?? {});
  }
  return Object.keys(schema.properties ?? {});
}
