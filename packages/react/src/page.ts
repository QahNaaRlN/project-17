import {
  createElement,
  Fragment,
  useLayoutEffect,
  useMemo,
  type ComponentType,
  type ReactNode,
} from "react";
import { RichText, type RichTextResources } from "./rich-text.js";
import {
  executeActions,
  linkHref,
  type PlanNode,
  type RenderPlan,
  type RuntimeAction,
  type ActionContext,
  type ActionError,
} from "@cms/runtime";
export interface NativeProps<V = Record<string, unknown>> {
  values: V;
  children: ReactNode;
  nodeId: string;
  instanceId: string;
  className: string;
  events?: Record<string, () => Promise<boolean>>;
  previewAttributes?: { "data-cms-node": string; "data-cms-instance"?: string };
}
export type NativeRegistry = Record<string, ComponentType<NativeProps>>;
// Draft placeholders and unconfigured image delivery do not break a page render.
const renderWarnings = new Set([
  "CMS_COMPONENT_PENDING_CAPABILITY",
  "CMS_ACTION_PENDING_CAPABILITY",
  "CMS_SOURCE_PENDING",
  "CMS_IMAGE_UNAVAILABLE",
]);
/**
 * STU-489: render diagnostics of a preview page as escaped JSON for the stage 8 check
 * runner; codes and node IDs only, never object bodies.
 */
export function renderDiagnostics(plan: RenderPlan): string {
  const list = plan.diagnostics.map((d) => ({
    code: d.code,
    nodeId: d.nodeId,
    severity: d.severity ?? (renderWarnings.has(d.code) ? "warning" : "error"),
  }));
  return JSON.stringify(list).replace(
    /[<>&\u2028\u2029]/g,
    (c) => "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"),
  );
}
export interface CmsPageProps {
  richText?: RichTextResources;
  plan: RenderPlan;
  registry?: NativeRegistry;
  actions?: readonly RuntimeAction[];
  app?: unknown;
  services?: Omit<ActionContext, "app" | "node" | "instance">;
  mode?: "design" | "interact";
  onActionError?: (error: ActionError) => void;
  canInteract?: () => boolean;
  preview?: boolean;
}

/** Render only plans prepared on a trusted server. Native code is application-owned. */
export function CmsPage({
  richText,
  plan,
  registry = {},
  actions = [],
  app,
  services = {},
  mode = plan.interactionMode ?? "interact",
  onActionError,
  canInteract,
  preview = plan.preview ?? false,
}: CmsPageProps): ReactNode {
  // A ready flag can become true again while an earlier handler is suspended.
  // Keep that chain tied to the committed plan and interaction mode instead.
  const lifetime = useMemo(() => ({ active: true }), [plan, mode]);
  useLayoutEffect(() => {
    lifetime.active = true;
    return () => {
      lifetime.active = false;
    };
  }, [lifetime]);
  function render(node: PlanNode): ReactNode {
    const props = node.props;
    const children = node.children.map(render);
    const previewAttributes = preview
      ? {
          "data-cms-node": node.id,
          ...(node.key === node.id ? {} : { "data-cms-instance": node.key }),
        }
      : undefined;
    const common = {
      key: node.key,
      id: node.key,
      className: node.className || undefined,
      ...previewAttributes,
    };
    const events = node.events
      ? Object.fromEntries(
          Object.entries(node.events).map(([event, chain]) => [
            event,
            () =>
              executeActions(
                chain,
                actions,
                {
                  app,
                  node: node.id,
                  instance: node.key,
                  navigate: (to) => {
                    const href = linkHref(to);
                    if (!href) throw new Error("CMS_ROUTE_UNRESOLVED");
                    window.location.assign(href);
                  },
                  scrollTo: (target) => {
                    const element = document.getElementById(target);
                    if (!element) throw new Error("CMS_ACTION_TARGET_MISSING");
                    element.scrollIntoView({ behavior: "smooth" });
                  },
                  openModal: (target) => {
                    const element = document.getElementById(target);
                    if (!(element instanceof HTMLDialogElement))
                      throw new Error("CMS_ACTION_TARGET_MISSING");
                    element.showModal();
                  },
                  closeModal: (target) => {
                    const element = document.getElementById(target);
                    if (!(element instanceof HTMLDialogElement))
                      throw new Error("CMS_ACTION_TARGET_MISSING");
                    element.close();
                  },
                  ...services,
                },
                {
                  mode,
                  isActive: () => lifetime.active && (canInteract?.() ?? true),
                  ...(onActionError ? { onError: onActionError } : {}),
                },
              ),
          ]),
        )
      : undefined;
    // STU-366: a component placeholder never runs application code; only the
    // preview shows a neutral block with the requested component name.
    if (node.pending !== undefined)
      return preview
        ? createElement(
            "div",
            {
              ...common,
              "data-cms-pending": node.pending,
              role: "group",
              "aria-label": node.type,
              style: { border: "1px dashed currentColor", padding: "12px", opacity: 0.8 },
            },
            createElement("span", null, node.type),
            ...children,
          )
        : null;
    switch (node.type) {
      case "Image": {
        const image = plan.images?.[node.key];
        return image
          ? createElement("img", { ...common, ...image })
          : createElement("span", {
              ...common,
              role: "img",
              "aria-label": String(props.alt ?? ""),
            });
      }
      case "Heading":
        return createElement(`h${props.level ?? 2}`, common, String(props.text ?? ""));
      case "Text":
        return createElement(String(props.as ?? "p"), common, String(props.text ?? ""));
      case "RichText":
        return createElement(
          "div",
          common,
          createElement(RichText, {
            ...richText,
            value: props.value,
            anchorPrefix: node.key.slice(0, -node.id.length),
          }),
        );
      case "Label":
        return createElement("span", common, String(props.text ?? ""));
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
            ...(events?.click
              ? {
                  onClick: (event: { preventDefault: () => void }) => {
                    event.preventDefault();
                    void events.click!();
                  },
                }
              : {}),
            ...(props.newTab ? { target: "_blank", rel: "noopener noreferrer" } : {}),
          },
          props.label as string | undefined,
          children,
        );
      case "Button":
        return createElement(
          "button",
          {
            ...common,
            type: "button",
            disabled: props.disabled === true,
            ...(events?.click
              ? {
                  onClick: () => {
                    void events.click!();
                  },
                }
              : {}),
          },
          String(props.label),
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
              ...(events ? { events } : {}),
              ...(previewAttributes ? { previewAttributes } : {}),
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
    preview
      ? createElement("script", {
          type: "application/json",
          id: "cms-render-diagnostics",
          dangerouslySetInnerHTML: { __html: renderDiagnostics(plan) },
        })
      : null,
  );
}
