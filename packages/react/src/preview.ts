import { createPreviewController, type PreviewControllerOptions } from "./preview-core.js";
export { createPreviewController } from "./preview-core.js";
export type {
  PreviewSnapshot,
  PreviewMode,
  PreviewMessage,
  PreviewControllerOptions,
  PreviewController,
} from "./preview-core.js";
export interface PreviewBridgeOptions extends Omit<PreviewControllerOptions, "parent" | "send"> {
  root: HTMLElement;
  window?: Window;
}
/** Application-side protocol only. Studio owns selection/layout overlays. */
export async function attachPreviewBridge(options: PreviewBridgeOptions) {
  const win = (options.window ?? window) as Window & typeof globalThis,
    root = options.root;
  const controller = await createPreviewController({
    ...options,
    parent: win.parent,
    send: (message, origin) => win.parent.postMessage(message, origin),
  });
  let timer: ReturnType<typeof setTimeout> | undefined,
    lastLayout = -Infinity,
    disposed = false,
    hovered: string | null = null;
  const now = options.now ?? Date.now;
  function layout() {
    timer = undefined;
    if (disposed) return;
    lastLayout = now();
    const nodes: Record<string, unknown[]> = Object.create(null);
    const document = controller.snapshot.document;
    const elements = [
      ...(root.hasAttribute("data-cms-node") ? [root] : []),
      ...root.querySelectorAll<HTMLElement>("[data-cms-node]"),
    ];
    for (const element of elements.slice(0, 5000)) {
      const id = element.getAttribute("data-cms-node")!;
      if (!Object.hasOwn(document.nodes, id)) continue;
      const rect = element.getBoundingClientRect();
      (nodes[id] ??= []).push({
        x: rect.x,
        y: rect.y,
        w: rect.width,
        h: rect.height,
        ...(element.dataset.cmsInstance ? { instance: element.dataset.cmsInstance } : {}),
      });
    }
    controller.notify("layout", {
      nodes,
      viewport: { width: win.innerWidth, height: win.innerHeight },
      scroll: { x: win.scrollX, y: win.scrollY },
    });
  }
  function schedule() {
    if (timer === undefined && !disposed)
      timer = setTimeout(layout, Math.max(0, 60 - (now() - lastLayout)));
  }
  function target(event: Event): HTMLElement | null {
    const element =
      event.target instanceof win.Element
        ? event.target.closest<HTMLElement>("[data-cms-node]")
        : null;
    return element && root.contains(element)
      ? element
      : root.hasAttribute("data-cms-node")
        ? root
        : null;
  }
  function click(event: MouseEvent) {
    if (controller.mode !== "design") return;
    event.preventDefault();
    event.stopImmediatePropagation();
    const element = target(event);
    if (element)
      controller.notify("node.click", {
        nodeId: element.dataset.cmsNode,
        ...(element.dataset.cmsInstance ? { instance: element.dataset.cmsInstance } : {}),
        modifiers: {
          shift: event.shiftKey,
          alt: event.altKey,
          ctrl: event.ctrlKey,
          meta: event.metaKey,
        },
      });
  }
  function hover(event: Event) {
    const id = event.type === "mouseleave" ? null : (target(event)?.dataset.cmsNode ?? null);
    if (id !== hovered) {
      hovered = id;
      controller.notify("node.hover", { nodeId: id });
    }
  }
  const message = (event: MessageEvent) => {
    void controller.receive(event).then(schedule);
  };
  const observer =
    typeof win.MutationObserver === "function" ? new win.MutationObserver(schedule) : undefined;
  observer?.observe(root, { subtree: true, childList: true, attributes: true });
  win.addEventListener("message", message);
  win.addEventListener("resize", schedule);
  win.addEventListener("scroll", schedule, true);
  root.addEventListener("click", click, true);
  root.addEventListener("mouseover", hover);
  root.addEventListener("mouseleave", hover);
  schedule();
  return {
    controller,
    dispose() {
      disposed = true;
      controller.dispose();
      if (timer !== undefined) clearTimeout(timer);
      observer?.disconnect();
      win.removeEventListener("message", message);
      win.removeEventListener("resize", schedule);
      win.removeEventListener("scroll", schedule, true);
      root.removeEventListener("click", click, true);
      root.removeEventListener("mouseover", hover);
      root.removeEventListener("mouseleave", hover);
    },
  };
}
