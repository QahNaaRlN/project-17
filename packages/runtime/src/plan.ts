import { evaluateBinding, evaluatePredicate, readPath, type BindingScope } from "@cms/core";
import type { IrDocument } from "@cms/ir";
import { atom, designDeclarations, responsive, type DesignOptions } from "./design.js";
import { planEvents, type PlannedEvents, type RuntimeAction } from "./actions.js";
import { fieldValue, jsonValue } from "./values.js";
import type { Image } from "./image.js";

export interface PlanNode {
  id: string;
  key: string;
  type: string;
  props: Record<string, unknown>;
  className: string;
  children: PlanNode[];
  events?: PlannedEvents;
}
export interface PlanDiagnostic {
  code: string;
  nodeId: string;
}
export interface RenderPlan {
  images?: Record<string, ReturnType<typeof Image>["props"]>;
  roots: PlanNode[];
  css: string;
  diagnostics: PlanDiagnostic[];
  interactionMode?: "design" | "interact";
  preview?: boolean;
}
export interface NativeDefinition {
  props: Record<string, { responsive?: boolean }>;
  container?: boolean;
  validate?: (props: Record<string, unknown>) => boolean;
  events?: readonly string[];
  provide?: (props: Record<string, unknown>) => Record<string, unknown>;
}
export interface PlanOptions extends DesignOptions {
  scope?: BindingScope;
  components?: Record<string, IrDocument>;
  natives?: Record<string, NativeDefinition>;
  actions?: readonly RuntimeAction[];
  documentScope?: (document: IrDocument, scope: BindingScope, instance: string) => BindingScope;
}
const primitiveProps: Record<string, string[]> = {
  Box: ["as"],
  Stack: ["direction"],
  Flex: ["wrap"],
  Grid: [],
  Heading: ["level", "text"],
  Text: ["as", "text"],
  RichText: ["value"],
  Image: ["src", "alt", "fit", "loading", "decorative"],
  Label: ["text"],
  Divider: ["orientation"],
  Link: ["to", "newTab", "label"],
  Button: ["label", "variant", "size", "disabled"],
  Repeat: ["items", "as", "limit", "key"],
  Slot: ["name"],
};
const containers = new Set(["Box", "Stack", "Flex", "Grid", "Link"]);
export function linkHref(link: unknown): string | undefined {
  if (!link || typeof link !== "object") return undefined;
  const value = link as Record<string, unknown>;
  if (
    value.kind === "anchor" &&
    typeof value.node === "string" &&
    /^[A-Za-z0-9_-]{4,32}$/.test(value.node)
  )
    return `#${value.node}`;
  if (
    value.kind !== "url" ||
    typeof value.url !== "string" ||
    Array.from(value.url).some(
      (char) => char.charCodeAt(0) <= 32 || char === "\u007f" || char === "\\",
    )
  )
    return undefined;
  const url = value.url;
  if (url && !url.startsWith("//") && !url.includes(":")) return url;
  if (/^\/(?!\/)/.test(url) || /^[?#]/.test(url)) return url;
  if (/^(https?:\/\/|mailto:|tel:)/i.test(url)) {
    try {
      const parsed = new URL(url);
      if (!parsed.username && !parsed.password) return url;
    } catch {
      return undefined;
    }
  }
  return undefined;
}
interface SlotScope {
  document: IrDocument;
  scope: BindingScope;
  ids: string[];
  frame: Frame;
}
interface Frame {
  slots: Record<string, SlotScope>;
  references: string[];
  ancestors: string[];
}
function localScope(document: IrDocument, scope: BindingScope): BindingScope {
  const local: Record<string, unknown> = Object.create(null);
  for (const [key, item] of Object.entries(document.localContent ?? {})) {
    const values = item.value as Record<string, unknown>;
    const locale = scope.context?.locale;
    if (locale && Object.hasOwn(values, locale)) local[key] = values[locale];
  }
  return { ...scope, local };
}
export function preparePage(document: IrDocument, options: PlanOptions = {}): RenderPlan {
  const diagnostics: PlanDiagnostic[] = [];
  const rules = new Map<string, { css: string; point: number }>();
  let visited = 0;
  let outputBytes = 0;
  function reject(code: string, nodeId: string): PlanNode[] {
    diagnostics.push({ code, nodeId });
    return [];
  }
  function walk(
    doc: IrDocument,
    id: string,
    scope: BindingScope,
    prefix: string,
    frame: Frame,
    depth: number,
  ): PlanNode[] {
    if (++visited > 5000 || depth > 32) throw new Error("CMS_EXPANSION_LIMIT");
    if (frame.ancestors.includes(id)) return reject("CMS_NODE_CYCLE", id);
    const node = Object.hasOwn(doc.nodes, id) ? doc.nodes[id] : undefined;
    if (!node) return reject("CMS_NODE_MISSING", id);
    const dataBound = Object.values(node.bindings ?? {}).some(
      (binding) => typeof binding === "string" && /^\$(data|item)(\.|$)/.test(binding),
    );
    const unavailable = (code: string): PlanNode[] => {
      diagnostics.push({ code, nodeId: id });
      return [
        {
          id,
          key: `${prefix}${id}`,
          type: "Text",
          props: {
            text: scope.context?.locale === "ru" ? "Данные недоступны" : "Data unavailable",
          },
          className: "",
          children: [],
        },
      ];
    };
    const childFrame = { ...frame, ancestors: [...frame.ancestors, id] };
    try {
      if (node.when && !evaluatePredicate(node.when, scope)) return [];
      const props: Record<string, unknown> = Object.assign(Object.create(null), node.props);
      if (
        Object.keys(props).some((name) => ["constructor", "prototype", "__proto__"].includes(name))
      )
        return reject("CMS_PROP_INVALID", id);
      for (const [name, binding] of Object.entries(node.bindings ?? {})) {
        if (["constructor", "prototype", "__proto__"].includes(name))
          return reject("CMS_PROP_INVALID", id);
        if (Object.hasOwn(props, name)) return reject("CMS_PROP_CONFLICT", id);
        if (typeof binding === "string") {
          const match = /^\$data\.([A-Za-z][A-Za-z0-9_]*)(?:\.(.*))?$/.exec(binding);
          const source = match
            ? (scope.data as Record<string, unknown> | undefined)?.[match[1]!]
            : undefined;
          if (
            source &&
            typeof source === "object" &&
            Object.hasOwn(source, "error") &&
            Object.keys(source).length === 1 &&
            !/^error(?:\.|$)/.test(match?.[2] ?? "")
          ) {
            const code = (source as { error?: { code?: unknown } }).error?.code;
            return unavailable(
              typeof code === "string" &&
                /^CMS_SOURCE_(UNKNOWN|TIMEOUT|ABORTED|TIMEOUT_INVALID|CURSOR_INVALID|PARAMS_INVALID|RESULT_INVALID|KEY_INVALID|FAILED)$/.test(
                  code,
                )
                ? code
                : "CMS_SOURCE_FAILED",
            );
          }
        }
        const value = evaluateBinding(binding, scope);
        if (value !== undefined) props[name] = value;
      }
      const children = (
        ids: string[],
        childScope = scope,
        childPrefix = prefix,
        nextFrame = childFrame,
        childDoc = doc,
      ) =>
        ids.flatMap((child) =>
          walk(childDoc, child, childScope, childPrefix, nextFrame, depth + 1),
        );
      if (node.type === "Composed") {
        if (!node.ref || (node.ref.version !== undefined && node.ref.version !== "live"))
          return reject("CMS_COMPONENT_VERSION_UNSUPPORTED", id);
        const ref = node.ref.component;
        if (frame.references.includes(ref)) return reject("CMS_COMPONENT_CYCLE", id);
        const component =
          options.components && Object.hasOwn(options.components, ref)
            ? options.components[ref]
            : undefined;
        if (!component || component.kind !== "component")
          return reject("CMS_COMPONENT_MISSING", id);
        if (node.design && Object.keys(node.design).length)
          return reject("CMS_COMPONENT_DESIGN_UNSUPPORTED", id);
        const inputs = component.inputs ?? {};
        if (Object.keys(props).some((key) => !Object.hasOwn(inputs, key)))
          return reject("CMS_PROP_INVALID", id);
        for (const [key, input] of Object.entries(inputs)) {
          if (props[key] === undefined && input.default !== undefined) props[key] = input.default;
          if (input.responsive) props[key] = baseProp(props[key]);
          if (input.required && props[key] == null) return reject("CMS_INPUT_MISSING", id);
        }
        const slots: Record<string, SlotScope> = Object.create(null);
        for (const [name, ids] of Object.entries(node.slots ?? {})) {
          if (!Object.hasOwn(component.slotDefs ?? {}, name)) return reject("CMS_SLOT_INVALID", id);
          const definition = component.slotDefs![name]!;
          if (
            ids.length < (definition.min ?? 0) ||
            ids.length > (definition.max ?? 5000) ||
            (definition.allowedTypes &&
              ids.some((child) => !definition.allowedTypes!.includes(doc.nodes[child]?.type ?? "")))
          )
            return reject("CMS_SLOT_INVALID", id);
          slots[name] = { document: doc, scope, ids, frame: childFrame };
        }
        if (
          Object.entries(component.slotDefs ?? {}).some(
            ([name, definition]) => (definition.min ?? 0) > 0 && !Object.hasOwn(slots, name),
          )
        )
          return reject("CMS_SLOT_INVALID", id);
        const instance = `${prefix}${id}/`;
        let childScope = localScope(component, { ...scope, props });
        childScope = options.documentScope?.(component, childScope, instance) ?? childScope;
        return walk(
          component,
          component.root,
          childScope,
          instance,
          { slots, references: [...frame.references, ref], ancestors: [] },
          depth + 1,
        );
      }
      const native =
        options.natives && Object.hasOwn(options.natives, node.type)
          ? options.natives[node.type]
          : undefined;
      const allowed = Object.hasOwn(primitiveProps, node.type)
        ? primitiveProps[node.type]!
        : native
          ? Object.keys(native.props)
          : undefined;
      if (!allowed) return reject("CMS_NODE_UNKNOWN", id);
      if (node.type === "Image") {
        const ref = props.src as Record<string, unknown> | null | undefined;
        if (
          (ref != null &&
            (typeof ref !== "object" ||
              typeof ref.assetId !== "string" ||
              !/^[0-9a-f-]{36}$/i.test(ref.assetId))) ||
          (props.alt != null && typeof props.alt !== "string") ||
          (props.fit !== undefined && !["contain", "cover"].includes(String(props.fit))) ||
          (props.loading !== undefined && !["lazy", "eager"].includes(String(props.loading)))
        )
          return reject("CMS_IMAGE_INVALID", id);
      }
      if (Object.keys(props).some((name) => !allowed.includes(name)))
        return reject("CMS_PROP_INVALID", id);
      if (native) {
        for (const [name, definition] of Object.entries(native.props))
          if (definition.responsive) props[name] = baseProp(props[name]);
        if (native.validate && !native.validate(props))
          return reject(dataBound ? "CMS_DATA_VALUE_INVALID" : "CMS_PROP_INVALID", id);
      }
      const provided = native?.provide?.(props);
      const eventScope = provided
        ? { ...scope, context: { ...scope.context, ...provided } }
        : scope;
      const declaredEvents =
        native?.events ?? (node.type === "Link" || node.type === "Button" ? ["click"] : []);
      if (node.on && Object.keys(node.on).length && !declaredEvents.length)
        return reject("CMS_ACTION_UNSUPPORTED", id);
      let events: PlannedEvents | undefined;
      try {
        events = planEvents(
          node,
          eventScope,
          declaredEvents,
          options.actions ?? [],
          doc.nodes,
          prefix,
          Object.keys(doc.dataSources ?? {}),
        );
      } catch (error) {
        const code =
          error instanceof Error && /^CMS_(ACTION|EVENT|VALUE)_/.test(error.message)
            ? error.message
            : "CMS_ACTION_ARGS_INVALID";
        return reject(code, id);
      }
      if (node.type === "Slot") {
        if (node.design && Object.keys(node.design).length)
          return reject("CMS_DESIGN_UNSUPPORTED", id);
        if (typeof props.name !== "string") return reject("CMS_SLOT_INVALID", id);
        const supplied = Object.hasOwn(frame.slots, props.name)
          ? frame.slots[props.name]
          : undefined;
        return supplied
          ? children(
              supplied.ids,
              supplied.scope,
              `${prefix}${id}/slot:${props.name}/`,
              { ...supplied.frame, references: childFrame.references },
              supplied.document,
            )
          : children(node.children ?? []);
      }
      if (node.type === "Repeat") {
        const binding = node.bindings?.items;
        const expression = typeof binding === "string" ? binding : undefined;
        const sourceKey =
          expression && /^\$data\.([A-Za-z][A-Za-z0-9_]*)(?:\.|$)/.exec(expression)?.[1];
        if (
          (node.design && Object.keys(node.design).length) ||
          Object.keys(node.slots ?? {}).some((name) => name !== "empty")
        )
          return reject("CMS_REPEAT_INVALID", id);
        const items = props.items;
        if (sourceKey && (items == null || !Array.isArray(items)))
          return unavailable("CMS_REPEAT_INVALID");
        if (items != null && !Array.isArray(items)) return reject("CMS_REPEAT_INVALID", id);
        const list = (items ?? []) as unknown[];
        const limit = props.limit ?? 200;
        if (!Number.isInteger(limit) || (limit as number) < 1 || (limit as number) > 200)
          return reject("CMS_REPEAT_INVALID", id);
        if (
          props.as !== undefined &&
          (typeof props.as !== "string" ||
            !/^[A-Za-z][A-Za-z0-9_]*$/.test(props.as) ||
            ["constructor", "prototype", "__proto__"].includes(props.as))
        )
          return reject("CMS_REPEAT_INVALID", id);
        if (
          props.key !== undefined &&
          (typeof props.key !== "string" ||
            !/^(?:\$item|(?:\$item\.)?[A-Za-z][A-Za-z0-9_]*(?:\.(?:[A-Za-z][A-Za-z0-9_]*|[0-9]+))*)$/.test(
              props.key,
            ))
        )
          return reject("CMS_REPEAT_INVALID", id);
        if (!list.length) return children(node.slots?.empty ?? [], scope, `${prefix}${id}/empty/`);
        const keys = new Set<string>();
        const instances = list.slice(0, limit as number).map((item, index) => {
          const each = Object.assign(Object.create(null), scope.each);
          if (props.as) each[String(props.as)] = item;
          const childScope = { ...scope, item, index, each };
          const keyPath =
            typeof props.key === "string"
              ? props.key.startsWith("$")
                ? props.key
                : "$item." + props.key
              : undefined;
          const keyValue = keyPath ? readPath(keyPath, childScope) : index;
          if (
            (typeof keyValue !== "string" && typeof keyValue !== "number") ||
            !String(keyValue).length ||
            String(keyValue).length > 256 ||
            (typeof keyValue === "number" && !Number.isFinite(keyValue))
          )
            throw new Error("CMS_REPEAT_KEY_INVALID");
          const key = `${typeof keyValue}:${String(keyValue)}`;
          if (keys.has(key)) throw new Error("CMS_REPEAT_KEY_DUPLICATE");
          keys.add(key);
          return { childScope, key };
        });
        return instances.flatMap(({ childScope, key }) =>
          children(node.children ?? [], childScope, `${prefix}${id}:${encodeURIComponent(key)}/`),
        );
      }
      if (node.slots && Object.keys(node.slots).length) return reject("CMS_SLOT_UNSUPPORTED", id);
      if (node.children?.length && !(containers.has(node.type) || native?.container))
        return reject("CMS_CHILDREN_INVALID", id);
      validatePrimitive(node.type, props);
      if (node.type === "Link" && (props.to as { kind: string }).kind === "anchor")
        props.href = `#${encodeURIComponent(`${prefix}${(props.to as { node: string }).node}`)}`;
      const atoms: string[] = [];
      const add = (declaration: string, point?: number) => {
        const result = atom(declaration, point);
        rules.set(result.className, { css: result.css, point: point ?? 0 });
        atoms.push(result.className);
      };
      if (node.type === "Stack" || node.type === "Flex") add("display:flex");
      if (node.type === "Grid") add("display:grid");
      const design = { ...(node.design ?? {}) } as Record<string, unknown>;
      if (node.type === "Stack") design.direction = props.direction ?? "vertical";
      if (node.type === "Flex") design.wrap = props.wrap ?? false;
      for (const [property, value] of Object.entries(design))
        for (const [entry, point] of responsive(value, options)) {
          const declaration =
            property === "hidden" && entry === false
              ? `display:${node.type === "Stack" || node.type === "Flex" ? "flex" : node.type === "Grid" ? "grid" : "revert"}`
              : designDeclarations(property, entry, options);
          add(declaration, point);
        }
      delete props.direction;
      delete props.wrap;
      // Plain JSON is also the hydration transport. Reject cyclic/non-JSON native values.
      const json = JSON.stringify(props);
      outputBytes += new TextEncoder().encode(json).length;
      if (outputBytes > 2 * 1024 * 1024) throw new Error("CMS_EXPANSION_LIMIT");
      const serializedProps = JSON.parse(json) as Record<string, unknown>;
      if (events) {
        outputBytes += new TextEncoder().encode(JSON.stringify(events)).length;
        if (outputBytes > 2 * 1024 * 1024) throw new Error("CMS_EXPANSION_LIMIT");
      }
      return [
        {
          id,
          key: `${prefix}${id}`,
          type: node.type,
          props: serializedProps,
          className: atoms.join(" "),
          children: children(node.children ?? [], eventScope),
          ...(events === undefined ? {} : { events }),
        },
      ];
    } catch (error) {
      if (error instanceof Error && error.message === "CMS_EXPANSION_LIMIT") throw error;
      if (error instanceof Error && /^CMS_REPEAT_KEY_(INVALID|DUPLICATE)$/.test(error.message))
        return reject(error.message, id);
      return reject(dataBound ? "CMS_DATA_VALUE_INVALID" : "CMS_NODE_INVALID", id);
    }
  }
  try {
    const roots = walk(
      document,
      document.root,
      options.documentScope?.(document, localScope(document, options.scope ?? {}), "") ??
        localScope(document, options.scope ?? {}),
      "",
      { slots: Object.create(null), references: [], ancestors: [] },
      0,
    );
    const used = new Set<string>();
    const collect = (nodes: PlanNode[]) => {
      for (const node of nodes) {
        for (const name of node.className.split(" ")) used.add(name);
        collect(node.children);
      }
    };
    collect(roots);
    return {
      roots,
      css: [...rules]
        .filter(([name]) => used.has(name))
        .sort((a, b) => a[1].point - b[1].point)
        .map(([, rule]) => rule.css)
        .join(""),
      diagnostics,
    };
  } catch {
    return {
      roots: [],
      css: "",
      diagnostics: [{ code: "CMS_EXPANSION_LIMIT", nodeId: document.root }],
    };
  }
}
function baseProp(value: unknown): unknown {
  if (value === null || typeof value !== "object") return value;
  if (
    Array.isArray(value) ||
    !Object.hasOwn(value, "base") ||
    Object.keys(value).some((key) => key !== "base")
  )
    throw new Error("CMS_RESPONSIVE_UNSUPPORTED");
  return (value as { base: unknown }).base;
}
function validatePrimitive(type: string, props: Record<string, unknown>): void {
  if (type === "RichText")
    props.value = fieldValue({ type: "richText", required: true }, jsonValue(props.value));
  const fail = () => {
    throw new Error("CMS_PROP_INVALID");
  };
  if (["Heading", "Text", "Label"].includes(type) && typeof props.text !== "string") fail();
  if (
    type === "Heading" &&
    props.level !== undefined &&
    (!Number.isInteger(props.level) || Number(props.level) < 1 || Number(props.level) > 6)
  )
    fail();
  if (type === "Text" && props.as !== undefined && props.as !== "p" && props.as !== "span") fail();
  if (
    type === "Box" &&
    props.as !== undefined &&
    !["div", "section", "article", "aside", "header", "footer", "nav", "main"].includes(
      String(props.as),
    )
  )
    fail();
  if (type === "Divider" && props.orientation !== undefined && props.orientation !== "horizontal")
    fail();
  if (
    type === "Button" &&
    (typeof props.label !== "string" ||
      (props.disabled !== undefined && typeof props.disabled !== "boolean") ||
      (props.variant !== undefined &&
        !["default", "primary", "secondary", "ghost"].includes(String(props.variant))) ||
      (props.size !== undefined && !["sm", "md", "lg"].includes(String(props.size))))
  )
    fail();
  if (type === "Link") {
    const href = linkHref(props.to);
    if (
      !href ||
      (props.label !== undefined && typeof props.label !== "string") ||
      (props.newTab !== undefined && typeof props.newTab !== "boolean")
    )
      fail();
    props.href = href;
  }
}
