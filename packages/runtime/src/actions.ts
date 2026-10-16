import { BUILTIN_ACTIONS, type Action, type Fields } from "@cms/manifest";
import { readPath, type BindingScope } from "@cms/core";
import type { Node, Operand, ActionArg } from "@cms/ir";
import { fieldValues, jsonValue, object, type ReadonlyContract } from "./values.js";
import { linkHref } from "./plan.js";
import { sourceError } from "./source-error.js";

export interface ActionContext<App = unknown> {
  app: App;
  node: string;
  instance: string;
  navigate?: (to: unknown) => unknown;
  openModal?: (target: string) => unknown;
  closeModal?: (target: string) => unknown;
  scrollTo?: (target: string) => unknown;
  track?: (event: string, props: Record<string, unknown>) => unknown;
  loadMore?: (source: string) => unknown;
}
export interface RuntimeAction {
  name: string;
  args: ReadonlyContract<Action["args"]>;
  handler: (args: never, context: never) => unknown;
}
export interface PlannedAction {
  action: string;
  args: Record<string, unknown>;
}
export type PlannedEvents = Record<string, PlannedAction[]>;
export function actionTarget(
  node: string,
  target: string,
  nodes: Record<string, Node>,
  prefix: string,
): string {
  if (!Object.hasOwn(nodes, target)) throw new Error("CMS_ACTION_TARGET_INVALID");
  // Preserve external/composed prefixes. Repeat segments belong only to targets
  // in that same template; an outer target has no current-item prefix.
  const parents = new Map<string, string>();
  for (const n of Object.values(nodes))
    for (const child of [...(n.children ?? []), ...Object.values(n.slots ?? {}).flat()])
      parents.set(child, n.id);
  const ancestors = new Set<string>();
  let at = target;
  while (parents.has(at)) {
    const parent = parents.get(at)!;
    if (ancestors.has(parent)) break;
    ancestors.add(parent);
    at = parent;
  }
  const segments = prefix.split("/");
  if (Object.hasOwn(nodes, node)) {
    for (const id of ancestors) {
      if (
        nodes[id]?.type === "Repeat" &&
        !segments.some((part) => part.startsWith(id + ":") || part === id)
      )
        throw new Error("CMS_ACTION_TARGET_INVALID");
    }
    for (let i = segments.length - 1; i >= 0; i--) {
      const id = segments[i]!.split(":")[0]!;
      if (nodes[id]?.type === "Repeat" && !ancestors.has(id)) {
        if (segments[i + 1] === "empty") segments.splice(i, 2);
        else segments.splice(i, 1);
      }
    }
  }
  return segments.join("/") + target;
}
export interface ActionError {
  code: string;
  action: string;
  node: string;
  instance: string;
}

const builtinFields: Record<string, Fields> = {
  navigate: { to: { type: "link", required: true } },
  openModal: { target: { type: "nodeRef", required: true } },
  closeModal: { target: { type: "nodeRef", required: true } },
  scrollTo: { target: { type: "nodeRef", required: true } },
  track: { event: { type: "string", required: true } },
  "data.loadMore": { source: { type: "string", required: true } },
};
export function operationArgs(
  name: string,
  args: Record<string, unknown>,
  actions: readonly RuntimeAction[],
): Record<string, unknown> {
  const builtin = (BUILTIN_ACTIONS as readonly string[]).includes(name);
  const definition = actions.find((item) => item.name === name);
  if (!builtin && !definition) throw new Error("CMS_ACTION_UNKNOWN");
  if (name === "track") {
    const { props, ...rest } = args;
    const normalized = fieldValues(builtinFields.track!, rest);
    if (props !== undefined && !object(props)) throw new Error("CMS_ACTION_ARGS_INVALID");
    return { ...normalized, ...(props === undefined ? {} : { props: jsonValue(props) }) };
  }
  return fieldValues(builtin ? builtinFields[name]! : (definition!.args ?? {}), args);
}
export function operands(
  values: Record<string, Operand | ActionArg>,
  scope: BindingScope,
  source = false,
): Record<string, unknown> {
  const result: Record<string, unknown> = Object.create(null);
  for (const [key, operand] of Object.entries(values)) {
    if (typeof operand === "string") {
      if (source && !/^\$(content|local|props|context)(\.|$)/.test(operand))
        throw new Error("CMS_SOURCE_PARAMS_INVALID");
      result[key] = readPath(operand, scope);
    } else if (
      object(operand) &&
      Object.keys(operand).length === 1 &&
      Object.hasOwn(operand, "lit")
    )
      result[key] = (operand as unknown as { lit: unknown }).lit;
    else if (!source && object(operand) && typeof operand.kind === "string") {
      result[key] =
        operand.kind === "page"
          ? {
              ...operand,
              ...(operand.params === undefined
                ? {}
                : { params: operands(operand.params as Record<string, Operand>, scope) }),
            }
          : operand;
    } else throw new Error("CMS_ACTION_ARGS_INVALID");
  }
  return result;
}
export function planEvents(
  node: Node,
  scope: BindingScope,
  events: readonly string[],
  actions: readonly RuntimeAction[],
  nodes: Record<string, Node>,
  prefix: string,
  sourceKeys: string[],
): PlannedEvents | undefined {
  if (!node.on || !Object.keys(node.on).length) return undefined;
  const result: PlannedEvents = Object.create(null);
  for (const [event, value] of Object.entries(node.on)) {
    if (!events.includes(event)) throw new Error("CMS_EVENT_UNKNOWN");
    const chain = Array.isArray(value) ? value : [value];
    if (!chain.length || chain.length > 8) throw new Error("CMS_ACTION_CHAIN_INVALID");
    result[event] = chain.map((item) => {
      if (sourceError(item.args, scope)) throw new Error("CMS_ACTION_SOURCE_UNAVAILABLE");
      const args = operationArgs(item.action, operands(item.args ?? {}, scope), actions);
      if (item.action === "navigate" && object(args.to) && args.to.kind === "anchor") {
        const target = String(args.to.node);
        if (!Object.hasOwn(nodes, target)) throw new Error("CMS_ACTION_TARGET_INVALID");
        args.to = {
          kind: "url",
          url: `#${encodeURIComponent(actionTarget(node.id, target, nodes, prefix))}`,
        };
      }
      if (["openModal", "closeModal", "scrollTo"].includes(item.action)) {
        const target = String(args.target);
        if (
          !Object.hasOwn(nodes, target) ||
          (item.action !== "scrollTo" && nodes[target]!.type !== "Modal")
        )
          throw new Error("CMS_ACTION_TARGET_INVALID");
        args.target = actionTarget(node.id, target, nodes, prefix);
      }
      if (item.action === "data.loadMore" && !sourceKeys.includes(String(args.source)))
        throw new Error("CMS_ACTION_TARGET_INVALID");
      return { action: item.action, args: jsonValue(args) as Record<string, unknown> };
    });
  }
  return result;
}
/** App error callbacks receive stable codes, never exception text or arguments. */
export async function executeActions(
  chain: readonly PlannedAction[],
  actions: readonly RuntimeAction[],
  context: ActionContext,
  options: {
    mode?: "design" | "interact";
    isActive?: () => boolean;
    onError?: (error: ActionError) => void;
  } = {},
): Promise<boolean> {
  if (options.mode === "design") return false;
  for (const item of chain) {
    if (options.isActive && !options.isActive()) return false;
    try {
      // Scoped DOM IDs can contain Repeat/Composed prefixes; they were checked in planEvents.
      const targetAction = ["openModal", "closeModal", "scrollTo"].includes(item.action);
      const args = targetAction
        ? item.args
        : operationArgs(item.action, jsonValue(item.args) as Record<string, unknown>, actions);
      if (
        targetAction &&
        (typeof args.target !== "string" || !args.target || Object.keys(args).length !== 1)
      )
        throw new Error("CMS_ACTION_ARGS_INVALID");
      let handler: (() => unknown) | undefined;
      switch (item.action) {
        case "navigate":
          if (!object(args.to) || (args.to.kind !== "page" && !linkHref(args.to)))
            throw new Error("CMS_ACTION_ARGS_INVALID");
          if (context.navigate) handler = () => context.navigate!(args.to);
          break;
        case "openModal":
          if (context.openModal) handler = () => context.openModal!(String(args.target));
          break;
        case "closeModal":
          if (context.closeModal) handler = () => context.closeModal!(String(args.target));
          break;
        case "scrollTo":
          if (context.scrollTo) handler = () => context.scrollTo!(String(args.target));
          break;
        case "track":
          if (context.track)
            handler = () =>
              context.track!(String(args.event), (args.props ?? {}) as Record<string, unknown>);
          break;
        case "data.loadMore":
          if (context.loadMore) handler = () => context.loadMore!(String(args.source));
          break;
        default: {
          const definition = actions.find((action) => action.name === item.action);
          if (definition) handler = () => definition.handler(args as never, context as never);
        }
      }
      if (!handler) throw new Error("CMS_ACTION_HANDLER_MISSING");
      await handler();
    } catch (error) {
      const allowed = [
        "CMS_ACTION_UNKNOWN",
        "CMS_ACTION_HANDLER_MISSING",
        "CMS_ACTION_ARGS_INVALID",
        "CMS_VALUE_INVALID",
      ];
      const code =
        error instanceof Error && allowed.includes(error.message)
          ? error.message
          : "CMS_ACTION_FAILED";
      try {
        options.onError?.({
          code,
          action: item.action,
          node: context.node,
          instance: context.instance,
        });
      } catch {
        /* Error reporting cannot restart a chain. */
      }
      return false;
    }
  }
  return true;
}
