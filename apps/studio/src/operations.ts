import type { IrDocument } from "@cms/ir";
import { structuralApply } from "./structure.js";
import { StudioError, type Operation } from "./types.js";
// Mirrors public operation semantics for preview only. CMS authorizes and persists the batch.
export function applyOperations(source: IrDocument, operations: Operation[]): IrDocument {
  const doc = structuredClone(source);
  for (const { type, payload: p } of operations) {
    if (type === "document.setContent") {
      if (p.content === null) delete doc.content;
      else doc.content = structuredClone(p.content) as NonNullable<IrDocument["content"]>;
    } else if (type === "localContent.set") {
      const key = String(p.key),
        old = doc.localContent?.[key];
      (doc.localContent ??= {})[key] = {
        type: "text",
        value: p.locale
          ? {
              ...(old?.type === "text" && typeof old.value === "object" ? old.value : {}),
              [String(p.locale)]: p.value,
            }
          : (p.value as Record<string, unknown>),
      };
    } else if (type === "localContent.remove") {
      delete doc.localContent?.[String(p.key)];
      if (doc.localContent && Object.keys(doc.localContent).length === 0) delete doc.localContent;
    } else if (["node.insert", "node.remove", "node.move"].includes(type))
      structuralApply(doc, { type, target: "", payload: p });
    else {
      const n = doc.nodes[String(p.nodeId)] as unknown as Record<string, unknown>;
      if (!n) throw new StudioError("NODE_NOT_FOUND");
      if (type === "node.rename" || type === "node.setCondition") {
        const key = type === "node.rename" ? "name" : "when";
        if (p[key] === null) delete n[key];
        else n[key] = p[key];
      } else {
        const key =
          type === "node.setBinding"
            ? "bindings"
            : type === "node.setBehavior"
              ? "on"
              : type === "node.setProps"
                ? "props"
                : "design";
        const map = (n[key] ??= {}) as Record<string, unknown>;
        if (type === "node.setBinding" || type === "node.setBehavior") {
          const entry = String(type === "node.setBinding" ? p.prop : p.event),
            value = type === "node.setBinding" ? p.binding : p.handlers;
          if (value === null) delete map[entry];
          else map[entry] = value;
        } else {
          Object.assign(map, p.set);
          for (const entry of (p.unset ?? []) as string[]) delete map[entry];
        }
        if (Object.keys(map).length === 0) delete n[key];
      }
    }
  }
  return doc;
}
