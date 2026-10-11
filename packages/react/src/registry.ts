import type { ComponentType } from "react";
import {
  type ComponentDefinition,
  type InferProps,
  type Registry,
  type FieldsInput,
  type ResolveFields,
} from "@cms/component-registry";
import { registerComponent, type DefinitionInput } from "@cms/component-registry/adapter";
import type { NativeProps, NativeRegistry } from "./page.js";

export function defineReactComponent<const P extends FieldsInput>(
  definition: DefinitionInput<P, ComponentType<NativeProps<NoInfer<InferProps<ResolveFields<P>>>>>>,
): ComponentDefinition<ResolveFields<P>, ComponentType<NativeProps<InferProps<ResolveFields<P>>>>> {
  return registerComponent(definition);
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
