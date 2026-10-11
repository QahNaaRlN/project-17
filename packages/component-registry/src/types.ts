import type { Fields, Type } from "@cms/manifest";
export type ReadonlyJson<T> = T extends object
  ? { readonly [K in keyof T]: ReadonlyJson<T[K]> }
  : T;
export type SchemaFields = ReadonlyJson<Fields>;
export type SchemaType = ReadonlyJson<Type>;
export interface RichTextValue {
  type: "doc";
  content: RichTextNode[];
}
export interface RichTextNode {
  type: string;
  text?: string;
  attrs?: Record<string, unknown>;
  content?: RichTextNode[];
  marks?: { type: string; attrs?: Record<string, unknown> }[];
}
export type LinkValue =
  | { kind: "url"; url: string }
  | { kind: "anchor"; node: string }
  | { kind: "page"; page: string; params?: Record<string, unknown> };
/** Types describe resolved values, after responsive base unwrapping. */
export type InferValue<T extends SchemaType> = T extends {
  type: "enum";
  values: infer V extends readonly string[];
}
  ? V[number]
  : T extends { type: "string" | "text" | "date" | "datetime" | "url" | "color" | "nodeRef" }
    ? string
    : T extends { type: "number" }
      ? number
      : T extends { type: "boolean" }
        ? boolean
        : T extends { type: "list"; of: infer V extends SchemaType }
          ? InferValue<V>[]
          : T extends { type: "object"; fields: infer P extends SchemaFields }
            ? InferProps<P>
            : T extends { type: "asset" }
              ? { assetId: string; alt?: string }
              : T extends { type: "reference" }
                ? { entityId: string }
                : T extends { type: "richText" }
                  ? RichTextValue
                  : T extends { type: "link" }
                    ? LinkValue
                    : unknown;
export type InferProps<P extends SchemaFields> = {
  [K in keyof P as P[K] extends { required: true } ? K : never]: InferValue<P[K]>;
} & {
  [K in keyof P as P[K] extends { required: true } ? never : K]?: InferValue<P[K]> | null;
};
