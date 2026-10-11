import type { Component, Fields, Manifest } from "@cms/manifest";
import type { NativeDefinition } from "@cms/runtime";
import type { SchemaFields, ReadonlyJson } from "./types.js";

export type ComponentContract = Omit<Component, "props" | "sourceRef"> & { props: Fields };
export type ComponentDefinition<P extends SchemaFields = SchemaFields, I = unknown> = Omit<
  ReadonlyJson<Component>,
  "props" | "sourceRef"
> & {
  name: string;
  props: P;
  component: I;
  validate?: (values: Record<string, unknown>) => boolean;
};
export interface RegistryOptions {
  components: readonly ComponentDefinition[];
  breakpoints?: Manifest["breakpoints"];
  tokens?: Manifest["tokens"];
  icons?: string[];
}
export interface Registry {
  readonly components: readonly ComponentDefinition[];
  readonly breakpoints: Manifest["breakpoints"];
  readonly tokens: Manifest["tokens"];
  readonly icons: string[];
}
const metadata = [
  "origin",
  "description",
  "category",
  "group",
  "container",
  "props",
  "slots",
  "events",
  "provides",
  "capabilities",
  "since",
  "deprecated",
] as const;
/** Reject non-JSON metadata rather than silently dropping executable values. */
function snapshot<T>(value: T, seen = new Set<object>()): T {
  if (value === null || typeof value === "string" || typeof value === "boolean") return value;
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value !== "object" || seen.has(value))
    throw new Error("CMS_REGISTRY_METADATA_INVALID");
  if (
    !Array.isArray(value) &&
    Object.getPrototypeOf(value) !== Object.prototype &&
    Object.getPrototypeOf(value) !== null
  )
    throw new Error("CMS_REGISTRY_METADATA_INVALID");
  if (
    Reflect.ownKeys(value).some(
      (key) =>
        typeof key !== "string" ||
        !Object.hasOwn(Object.getOwnPropertyDescriptor(value, key)!, "value"),
    )
  )
    throw new Error("CMS_REGISTRY_METADATA_INVALID");
  seen.add(value);
  const result = Array.isArray(value)
    ? Array.from(value, (item) => snapshot(item, seen))
    : Object.fromEntries(Object.entries(value).map(([key, item]) => [key, snapshot(item, seen)]));
  seen.delete(value);
  return Object.freeze(result) as T;
}
export function componentContract(definition: ComponentDefinition): ComponentContract {
  return Object.fromEntries(
    metadata.filter((key) => definition[key] !== undefined).map((key) => [key, definition[key]]),
  ) as ComponentContract;
}
export function defineComponent<const P extends SchemaFields, I>(
  definition: ComponentDefinition<P, I>,
): ComponentDefinition<P, I> {
  if (!/^[A-Z][a-zA-Z0-9]*$/.test(definition.name)) throw new Error("CMS_REGISTRY_NAME_INVALID");
  if (
    Object.keys(definition.props).some((key) =>
      ["__proto__", "constructor", "prototype"].includes(key),
    )
  )
    throw new Error("CMS_REGISTRY_PROP_INVALID");
  const contract = snapshot(componentContract(definition));
  return Object.freeze({
    ...contract,
    name: definition.name,
    component: definition.component,
    ...(definition.validate === undefined ? {} : { validate: definition.validate }),
  }) as unknown as ComponentDefinition<P, I>;
}
export function createRegistry(options: RegistryOptions): Registry {
  const components = options.components.map((component) => defineComponent(component));
  if (new Set(components.map((component) => component.name)).size !== components.length)
    throw new Error("CMS_REGISTRY_NAME_DUPLICATE");
  return Object.freeze({
    components: Object.freeze(components),
    breakpoints: snapshot(options.breakpoints ?? {}),
    tokens: snapshot(options.tokens ?? {}),
    icons: snapshot(options.icons ?? []),
  });
}
/** A fresh runtime view; modifying it cannot modify the source registry. */
export function nativeDefinitions(registry: Registry): Record<string, NativeDefinition> {
  return Object.fromEntries(
    registry.components.map((definition) => [
      definition.name,
      {
        props: Object.fromEntries(
          Object.entries(definition.props).map(([name, prop]) => [
            name,
            prop.responsive === undefined ? {} : { responsive: prop.responsive },
          ]),
        ),
        ...(definition.container === undefined ? {} : { container: definition.container }),
        ...(definition.validate === undefined ? {} : { validate: definition.validate }),
      },
    ]),
  );
}
