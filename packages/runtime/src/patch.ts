import { validateDocument, type IrDocument } from "@cms/ir";
import { canonicalJson, manifestHash } from "@cms/manifest";
import { jsonValue, object } from "./values.js";

function invalid(): never {
  throw new Error("CMS_PREVIEW_PATCH_INVALID");
}
function pointer(input: unknown): string[] {
  if (
    typeof input !== "string" ||
    input.length > 2048 ||
    (input !== "" && !input.startsWith("/")) ||
    /~(?![01])/.test(input)
  )
    invalid();
  const parts =
    input === ""
      ? []
      : input
          .slice(1)
          .split("/")
          .map((part) => part.replaceAll("~1", "/").replaceAll("~0", "~"));
  if (parts.some((part) => ["__proto__", "constructor", "prototype"].includes(part))) invalid();
  return parts;
}
function index(input: string, length: number, add = false): number {
  if (add && input === "-") return length;
  if (!/^(0|[1-9][0-9]*)$/.test(input)) invalid();
  const result = Number(input);
  if (!Number.isSafeInteger(result) || result >= length + (add ? 1 : 0)) invalid();
  return result;
}
function get(root: unknown, path: string[]): unknown {
  let value = root;
  for (const part of path) {
    if (Array.isArray(value)) value = value[index(part, value.length)];
    else if (object(value) && Object.hasOwn(value, part)) value = value[part];
    else invalid();
  }
  return value;
}
function write(
  root: unknown,
  path: string[],
  operation: "add" | "remove" | "replace",
  value?: unknown,
): unknown {
  if (!path.length) return operation === "remove" ? undefined : value;
  const parent = get(root, path.slice(0, -1)),
    key = path.at(-1)!;
  if (Array.isArray(parent)) {
    const position = index(key, parent.length, operation === "add");
    if (operation === "add") parent.splice(position, 0, value);
    else if (operation === "remove") parent.splice(position, 1);
    else parent[position] = value;
  } else if (object(parent)) {
    if (operation !== "add" && !Object.hasOwn(parent, key)) invalid();
    if (operation === "remove") delete parent[key];
    else parent[key] = value;
  } else invalid();
  return root;
}
/** Atomic RFC 6902 application; validation occurs before a result is returned. */
export function applyDocumentPatch(document: IrDocument, input: unknown): IrDocument {
  try {
    const patch = jsonValue(input);
    if (!Array.isArray(patch) || patch.length > 128) invalid();
    let result = jsonValue(document);
    for (const operation of patch) {
      if (!object(operation)) invalid();
      const path = pointer(operation.path),
        op = operation.op;
      if (op === "add" || op === "replace" || op === "test") {
        if (!Object.hasOwn(operation, "value")) invalid();
        if (op === "test") {
          if (canonicalJson(get(result, path)) !== canonicalJson(operation.value)) invalid();
        } else result = write(result, path, op, jsonValue(operation.value));
      } else if (op === "remove") result = write(result, path, op);
      else if (op === "copy" || op === "move") {
        const from = pointer(operation.from);
        if (op === "move" && path.length > from.length && from.every((part, i) => path[i] === part))
          invalid();
        const value = jsonValue(get(result, from));
        if (op === "move") result = write(result, from, "remove");
        result = write(result, path, "add", value);
      } else invalid();
      if (result !== undefined) result = jsonValue(result);
    }
    if (!validateDocument(result).valid) invalid();
    return result as IrDocument;
  } catch {
    return invalid();
  }
}
export async function documentHash(document: IrDocument): Promise<string> {
  const value = jsonValue(document);
  if (!validateDocument(value).valid) throw new Error("CMS_PREVIEW_DOCUMENT_INVALID");
  return manifestHash(value);
}
