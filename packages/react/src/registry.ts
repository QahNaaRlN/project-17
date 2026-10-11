import type { ComponentType } from "react";
import {
  defineComponent,
  type ComponentDefinition,
  type InferProps,
  type Registry,
  type SchemaFields,
} from "@cms/component-registry";
import type { NativeProps, NativeRegistry } from "./page.js";

export function defineReactComponent<const P extends SchemaFields>(
  definition: ComponentDefinition<P, ComponentType<NativeProps<InferProps<P>>>>,
): ComponentDefinition<P, ComponentType<NativeProps<InferProps<P>>>> {
  return defineComponent(definition);
}
/** Registry implementations are application code. Plans must be validated on the server. */
export function createReactRegistry(registry: Registry): NativeRegistry {
  return Object.fromEntries(
    registry.components.map((definition) => [
      definition.name,
      definition.component as ComponentType<NativeProps>,
    ]),
  );
}
