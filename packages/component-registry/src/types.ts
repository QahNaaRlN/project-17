import type { Fields, Type } from "@cms/manifest";
export type ReadonlyJson<T> = T extends object
  ? { readonly [K in keyof T]: ReadonlyJson<T[K]> }
  : T;
export type SchemaFields = ReadonlyJson<Fields>;
export type SchemaType = ReadonlyJson<Type>;
/** Types describe resolved values, after responsive base unwrapping. */
export type InferValue<T extends SchemaType> = T extends {
  type: "enum";
  values: infer V extends readonly string[];
}
  ? V[number]
  : T extends { type: "string" | "text" | "date" | "datetime" | "url" | "color" }
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
              ? { assetId: string }
              : T extends { type: "reference" }
                ? { entityId: string }
                : unknown;
export type InferProps<P extends SchemaFields> = {
  [K in keyof P as P[K] extends { required: true } ? K : never]: InferValue<P[K]>;
} & {
  [K in keyof P as P[K] extends { required: true } ? never : K]?: InferValue<P[K]> | null;
};
