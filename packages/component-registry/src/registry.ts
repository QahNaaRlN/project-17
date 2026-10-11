import { snapshot } from "./json.js";
import type { Component, Fields, Manifest } from "@cms/manifest";
import type { NativeDefinition } from "@cms/runtime";
import type { SchemaFields, ReadonlyJson } from "./types.js";
import type { InferProps } from "./types.js";
import { fields, SlotField, type FieldsInput, type ResolveFields, type SlotsInput } from "./dsl.js";

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
export type DefinitionInput<P extends FieldsInput, I> = Omit<
  ComponentDefinition<ResolveFields<P>, I>,
  "props" | "provides" | "slots"
> & { props: P; provides?: FieldsInput; slots?: SlotsInput };
export interface RegistryOptions {
  components: readonly ComponentDefinition[];
  breakpoints?: Manifest["breakpoints"];
  tokens?: Manifest["tokens"];
  icons?: string[];
  schemas?: Manifest["schemas"];
  capabilities?: Manifest["capabilities"];
}
export interface Registry {
  readonly components: readonly ComponentDefinition[];
  readonly breakpoints: Manifest["breakpoints"];
  readonly tokens: Manifest["tokens"];
  readonly icons: string[];
  readonly schemas: NonNullable<Manifest["schemas"]>;
  readonly capabilities: NonNullable<Manifest["capabilities"]>;
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
export function componentContract(definition: ComponentDefinition): ComponentContract {
  return Object.fromEntries(
    metadata.filter((key) => definition[key] !== undefined).map((key) => [key, definition[key]]),
  ) as ComponentContract;
}
export function registerComponent<const P extends FieldsInput, I>(
  definition: DefinitionInput<P, I>,
): ComponentDefinition<ResolveFields<P>, I> {
  if (!/^[A-Z][a-zA-Z0-9]*$/.test(definition.name)) throw new Error("CMS_REGISTRY_NAME_INVALID");
  if (
    Object.keys(definition.props).some((key) =>
      ["__proto__", "constructor", "prototype"].includes(key),
    )
  )
    throw new Error("CMS_REGISTRY_PROP_INVALID");
  const { provides: _provides, slots: _slots, ...base } = definition;
  const normalized = {
    ...base,
    props: fields(definition.props),
    ...(definition.provides === undefined ? {} : { provides: fields(definition.provides) }),
    ...(definition.slots === undefined
      ? {}
      : {
          slots: Object.fromEntries(
            Object.entries(definition.slots).map(([key, value]) => [
              key,
              value instanceof SlotField ? value.schema : value,
            ]),
          ),
        }),
  };
  const contract = snapshot(componentContract(normalized));
  return Object.freeze({
    ...contract,
    name: definition.name,
    component: definition.component,
    ...(definition.validate === undefined ? {} : { validate: definition.validate }),
  }) as unknown as ComponentDefinition<ResolveFields<P>, I>;
}
export function defineComponent<const P extends FieldsInput, R>(
  definition: DefinitionInput<P, (props: NoInfer<InferProps<ResolveFields<P>>>) => R>,
): ComponentDefinition<ResolveFields<P>, (props: InferProps<ResolveFields<P>>) => R> {
  return registerComponent(definition);
}
export function createRegistry(options: RegistryOptions): Registry {
  const components = options.components.map((component) => registerComponent(component));
  if (new Set(components.map((component) => component.name)).size !== components.length)
    throw new Error("CMS_REGISTRY_NAME_DUPLICATE");
  return Object.freeze({
    components: Object.freeze(components),
    breakpoints: snapshot(options.breakpoints ?? {}),
    tokens: snapshot(options.tokens ?? {}),
    icons: snapshot(options.icons ?? []),
    schemas: snapshot(options.schemas ?? {}),
    capabilities: snapshot(options.capabilities ?? {}),
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
