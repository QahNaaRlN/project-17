import type { Action, DataSource } from "@cms/manifest";
import type { ActionContext, LoaderContext, PageItems } from "@cms/runtime";
import {
  Field,
  fields,
  type FieldsInput,
  type ResolveFields,
  type FieldInput,
  type ResolveField,
} from "./dsl.js";
import { snapshot, metadataEntries } from "./json.js";
import type { InferProps, InferValue, ReadonlyJson, SchemaFields, SchemaType } from "./types.js";

export type ActionDefinition<P extends SchemaFields = SchemaFields, App = unknown> = Omit<
  ReadonlyJson<Action>,
  "args" | "sourceRef"
> & {
  name: string;
  args: P;
  handler: (args: InferProps<P>, context: ActionContext<App>) => unknown;
};
export type DataSourceDefinition<
  P extends SchemaFields = SchemaFields,
  S extends SchemaType = SchemaType,
  B extends boolean = boolean,
  App = unknown,
> = Omit<ReadonlyJson<DataSource>, "params" | "result" | "sourceRef" | "paginated"> & {
  name: string;
  params: P;
  result: S;
  paginated?: B;
  loader: (
    params: InferProps<P>,
    context: LoaderContext<App>,
  ) =>
    | Promise<
        B extends true ? PageItems<InferValue<S> extends (infer I)[] ? I : never> : InferValue<S>
      >
    | (B extends true ? PageItems<InferValue<S> extends (infer I)[] ? I : never> : InferValue<S>);
};
function nameValid(name: string): void {
  if (!/^[a-z][a-zA-Z0-9]*(\.[a-z][a-zA-Z0-9]*)+$/.test(name))
    throw new Error("CMS_REGISTRY_NAME_INVALID");
}
export function actionContract(definition: ActionDefinition): Action {
  const { description, args, capabilities, deprecated } = definition;
  return snapshot({
    args,
    ...(description === undefined ? {} : { description }),
    ...(capabilities === undefined ? {} : { capabilities }),
    ...(deprecated === undefined ? {} : { deprecated }),
  }) as Action;
}
export function sourceContract(definition: DataSourceDefinition): DataSource {
  const { description, params, result, paginated, cache, capabilities, deprecated } = definition;
  return snapshot({
    params,
    result,
    ...(description === undefined ? {} : { description }),
    ...(paginated === undefined ? {} : { paginated }),
    ...(cache === undefined ? {} : { cache }),
    ...(capabilities === undefined ? {} : { capabilities }),
    ...(deprecated === undefined ? {} : { deprecated }),
  }) as DataSource;
}
export function defineAction<const P extends FieldsInput, App = unknown>(
  definition: Omit<ActionDefinition<ResolveFields<P>, App>, "args"> & { args: P },
): ActionDefinition<ResolveFields<P>, App> {
  metadataEntries(definition);
  nameValid(definition.name);
  if (typeof definition.handler !== "function") throw new Error("CMS_REGISTRY_HANDLER_INVALID");
  const normalized = { ...definition, args: fields(definition.args) };
  const contract = actionContract(normalized as unknown as ActionDefinition);
  return Object.freeze({
    ...contract,
    name: definition.name,
    handler: definition.handler,
  }) as unknown as ActionDefinition<ResolveFields<P>, App>;
}
export function defineDataSource<
  const P extends FieldsInput,
  const S extends FieldInput,
  const B extends boolean = false,
  App = unknown,
>(
  definition: Omit<
    DataSourceDefinition<ResolveFields<P>, ResolveField<S>, B, App>,
    "params" | "result"
  > & { params: P; result: S },
): DataSourceDefinition<ResolveFields<P>, ResolveField<S>, B, App> {
  metadataEntries(definition);
  nameValid(definition.name);
  if (typeof definition.loader !== "function") throw new Error("CMS_REGISTRY_LOADER_INVALID");
  const normalized = {
    ...definition,
    params: fields(definition.params),
    result: snapshot(
      (definition.result instanceof Field
        ? definition.result.schema
        : definition.result) as SchemaType,
    ),
  };
  if (definition.paginated && normalized.result.type !== "list")
    throw new Error("CMS_REGISTRY_PAGINATION_INVALID");
  const contract = sourceContract(normalized as unknown as DataSourceDefinition);
  return Object.freeze({
    ...contract,
    name: definition.name,
    loader: definition.loader,
  }) as unknown as DataSourceDefinition<ResolveFields<P>, ResolveField<S>, B, App>;
}
