import { createElement, Fragment, type ComponentType, type ReactNode } from "react";
import type { PlanNode, RenderPlan } from "@cms/runtime";
export interface NativeProps {
  values: Record<string, unknown>;
  children: ReactNode;
  nodeId: string;
  instanceId: string;
  className: string;
}
export type NativeRegistry = Record<string, ComponentType<NativeProps>>;
export interface CmsPageProps {
  plan: RenderPlan;
  registry?: NativeRegistry;
}

/** Render only plans prepared on a trusted server. Native code is application-owned. */
export function CmsPage({ plan, registry = {} }: CmsPageProps): ReactNode {
  function render(node: PlanNode): ReactNode {
    const props = node.props;
    const children = node.children.map(render);
    const common = { key: node.key, id: node.key, className: node.className || undefined };
    switch (node.type) {
      case "Heading":
        return createElement(`h${props.level ?? 2}`, common, String(props.text));
      case "Text":
        return createElement(String(props.as ?? "p"), common, String(props.text));
      case "Label":
        return createElement("span", common, String(props.text));
      case "Divider":
        return createElement("hr", common);
      case "Box":
        return createElement(String(props.as ?? "div"), common, children);
      case "Stack":
      case "Flex":
      case "Grid":
        return createElement("div", common, children);
      case "Link":
        return createElement(
          "a",
          {
            ...common,
            href: String(props.href),
            ...(props.newTab ? { target: "_blank", rel: "noopener noreferrer" } : {}),
          },
          props.label as string | undefined,
          children,
        );
      default: {
        const Component = Object.hasOwn(registry, node.type) ? registry[node.type] : undefined;
        return Component
          ? createElement(Component, {
              key: node.key,
              values: props,
              children,
              nodeId: node.id,
              instanceId: node.key,
              className: node.className,
            })
          : null;
      }
    }
  }
  return createElement(
    Fragment,
    null,
    plan.css ? createElement("style", null, plan.css) : null,
    plan.roots.map(render),
  );
}
