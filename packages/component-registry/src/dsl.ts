import { snapshot } from "./json.js";
import type { Slot, Type } from "@cms/manifest";
import type { InferValue, ReadonlyJson, SchemaType } from "./types.js";

export type FieldInput = SchemaType | Field<SchemaType>;
export type FieldsInput = Readonly<Record<string, FieldInput>>;
export type ResolveField<T extends FieldInput> =
  T extends Field<infer S> ? S : T extends SchemaType ? T : never;
export type ResolveFields<P extends FieldsInput> = { readonly [K in keyof P]: ResolveField<P[K]> };
type Kind = Type["type"];
type Has<S extends SchemaType, K extends Kind> = S["type"] extends K ? Field<S> : never;
type DefaultKind = "string" | "text" | "number" | "boolean" | "enum" | "color";
type Rich = Extract<SchemaType, { type: "richText" }>;

export function fields<P extends FieldsInput>(input: P): ResolveFields<P> {
  return Object.fromEntries(
    Object.entries(input).map(([key, value]) => [
      key,
      value instanceof Field ? value.schema : value,
    ]),
  ) as ResolveFields<P>;
}
/** Immutable descriptors. Only schema JSON crosses the manifest boundary. */
export class Field<S extends SchemaType> {
  readonly schema: S;
  constructor(schema: S) {
    this.schema = snapshot(schema);
    Object.freeze(this);
  }
  private with(change: object): Field<S> {
    return new Field({ ...this.schema, ...change });
  }
  required(): Field<S & { required: true }> {
    return new Field({ ...this.schema, required: true });
  }
  responsive(): Field<S & { responsive: true }> {
    return new Field({ ...this.schema, responsive: true });
  }
  localized(): Field<S & { localized: true }> {
    return new Field({ ...this.schema, localized: true });
  }
  unique(this: Has<S, "string" | "text" | "number">): Field<S> {
    return this.with({ unique: true });
  }
  content(this: Has<S, "text" | "richText" | "asset" | "link">): Field<S> {
    return this.with({ content: true });
  }
  description(value: string): Field<S> {
    return this.with({ description: value });
  }
  default(this: Has<S, DefaultKind>, value: InferValue<S>): Field<S> {
    return this.with({ default: value });
  }
  min(this: Has<S, "string" | "number" | "list">, value: number): Field<S> {
    return this.with(this.schema.type === "string" ? { minLength: value } : { min: value });
  }
  max(this: Has<S, "string" | "text" | "number" | "list">, value: number): Field<S> {
    return this.with(
      ["string", "text"].includes(this.schema.type) ? { maxLength: value } : { max: value },
    );
  }
  pattern(this: Has<S, "string">, value: string): Field<S> {
    return this.with({ pattern: value });
  }
  int(this: Has<S, "number">): Field<S> {
    return this.with({ integer: true });
  }
  multiline(this: Has<S, "text">): Field<S> {
    return this.with({ multiline: true });
  }
  schemes(this: Has<S, "url">, first: string, ...rest: string[]): Field<S> {
    return this.with({ schemes: [first, ...rest] });
  }
  mimeTypes(this: Has<S, "asset">, first: string, ...rest: string[]): Field<S> {
    return this.with({ mimeTypes: [first, ...rest] });
  }
  marks(this: Has<S, "richText">, ...values: NonNullable<Rich["marks"]>): Field<S> {
    return this.with({ marks: values });
  }
  blocks(this: Has<S, "richText">, ...values: NonNullable<Rich["blocks"]>): Field<S> {
    return this.with({ blocks: values });
  }
}
export class SlotField {
  readonly schema: ReadonlyJson<Slot>;
  constructor(schema: ReadonlyJson<Slot> = {}) {
    this.schema = snapshot(schema);
    Object.freeze(this);
  }
  allow(first: string, ...rest: string[]): SlotField {
    return new SlotField({ ...this.schema, allowedTypes: [first, ...rest] });
  }
  min(value: number): SlotField {
    return new SlotField({ ...this.schema, min: value });
  }
  max(value: number): SlotField {
    return new SlotField({ ...this.schema, max: value });
  }
  description(value: string): SlotField {
    return new SlotField({ ...this.schema, description: value });
  }
}
export type SlotsInput = Readonly<Record<string, ReadonlyJson<Slot> | SlotField>>;
export const t = Object.freeze({
  string: () => new Field({ type: "string" } as const),
  text: () => new Field({ type: "text" } as const),
  richText: () => new Field({ type: "richText" } as const),
  number: () => new Field({ type: "number" } as const),
  boolean: () => new Field({ type: "boolean" } as const),
  enum: <const V extends readonly [string, ...string[]]>(values: V) =>
    new Field({ type: "enum", values } as const),
  date: () => new Field({ type: "date" } as const),
  datetime: () => new Field({ type: "datetime" } as const),
  url: () => new Field({ type: "url" } as const),
  link: () => new Field({ type: "link" } as const),
  asset: (kind?: "image" | "video" | "file") =>
    new Field({ type: "asset", ...(kind ? { assetKind: kind } : {}) } as const),
  reference: (schema: string) => new Field({ type: "reference", schema } as const),
  list: <S extends FieldInput>(of: S) =>
    new Field({ type: "list", of: of instanceof Field ? of.schema : of } as {
      readonly type: "list";
      readonly of: ResolveField<S>;
    }),
  object: <const P extends FieldsInput>(input: P) =>
    new Field({ type: "object", fields: fields(input) } as const),
  color: () => new Field({ type: "color" } as const),
  nodeRef: (nodeType?: string) =>
    new Field({ type: "nodeRef", ...(nodeType ? { nodeType } : {}) } as const),
  slot: () => new SlotField(),
});
