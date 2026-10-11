import type { Type } from "@cms/manifest";
import { fieldValue, object, type ReadonlyContract } from "./values.js";
type Rich = ReadonlyContract<Extract<Type, { type: "richText" }>>;
const blocks = [
  "paragraph",
  "heading",
  "bulletList",
  "orderedList",
  "listItem",
  "blockquote",
  "image",
  "divider",
];
const marks = ["bold", "italic", "underline", "strike", "code", "link"];
function invalid(): never {
  throw new Error("CMS_VALUE_INVALID");
}
function keys(value: Record<string, unknown>, allowed: string[]): void {
  if (Object.keys(value).some((key) => !allowed.includes(key))) invalid();
}
export function richValue(value: Record<string, unknown>, schema: Rich): void {
  let visits = 0;
  function node(input: unknown, parent: string, depth: number): void {
    if (++visits > 5000 || depth > 32 || !object(input) || typeof input.type !== "string")
      invalid();
    const type = input.type;
    if (!["doc", ...blocks, "text", "hardBreak"].includes(type)) invalid();
    if (type === "doc" && parent) invalid();
    if (["paragraph", "heading"].includes(parent) && !["text", "hardBreak"].includes(type))
      invalid();
    if (["bulletList", "orderedList"].includes(parent) && type !== "listItem") invalid();
    if (parent === "doc" && !blocks.includes(type)) invalid();
    if (type === "listItem" && !["bulletList", "orderedList"].includes(parent)) invalid();
    if (
      blocks.includes(type) &&
      type !== "listItem" &&
      schema.blocks &&
      !schema.blocks.includes(type as never)
    )
      invalid();
    keys(input, ["type", "content", "attrs", "text", "marks"]);
    if (type === "text") {
      if (
        typeof input.text !== "string" ||
        input.content !== undefined ||
        input.attrs !== undefined
      )
        invalid();
      if (input.marks !== undefined) {
        if (!Array.isArray(input.marks)) invalid();
        for (const mark of input.marks) {
          if (
            !object(mark) ||
            typeof mark.type !== "string" ||
            !marks.includes(mark.type) ||
            (schema.marks && !schema.marks.includes(mark.type as never))
          )
            invalid();
          keys(mark, ["type", "attrs"]);
          if (mark.type === "link") {
            if (!object(mark.attrs)) invalid();
            keys(mark.attrs, ["to"]);
            fieldValue({ type: "link", required: true }, mark.attrs.to);
          } else if (mark.attrs !== undefined) invalid();
        }
      }
      return;
    }
    if (input.text !== undefined || input.marks !== undefined) invalid();
    if (type === "heading") {
      if (!object(input.attrs) || ![2, 3, 4].includes(Number(input.attrs.level))) invalid();
      keys(input.attrs, ["level"]);
    } else if (type === "image") {
      fieldValue({ type: "asset", required: true }, input.attrs);
    } else if (input.attrs !== undefined) invalid();
    if (["image", "divider", "hardBreak"].includes(type)) {
      if (input.content !== undefined) invalid();
    } else {
      if (!Array.isArray(input.content)) invalid();
      for (const child of input.content) node(child, type, depth + 1);
    }
  }
  node(value, "", 0);
}
