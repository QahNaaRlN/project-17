import { createElement as h, Fragment, type ReactNode } from "react";
import { Image, linkHref, validateFieldValue, jsonValue, type ImageModel } from "@cms/runtime";
import type { RichTextNode, RichTextValue } from "@cms/component-registry";
export interface RichTextResources {
  images?: Readonly<Record<string, ImageModel>>;
  resolvePage?: (link: Record<string, unknown>) => string | undefined;
  locale?: string;
  anchorPrefix?: string;
}
// Only structured RichText v1 enters React; no HTML or arbitrary DOM attributes.
export function RichText({
  value,
  images = {},
  resolvePage,
  locale = "ru",
  anchorPrefix = "",
}: RichTextResources & { value: unknown }): ReactNode {
  let doc: RichTextValue;
  try {
    doc = validateFieldValue(
      { type: "richText", required: true },
      jsonValue(value),
    ) as RichTextValue;
  } catch {
    return null;
  }
  function render(node: RichTextNode, key: number): ReactNode {
    const content = node.content?.map(render),
      attrs = node.attrs ?? {};
    if (node.type === "text") {
      let text: ReactNode = node.text;
      for (const mark of node.marks ?? []) {
        if (mark.type === "link") {
          const to = mark.attrs!.to as Record<string, unknown>;
          let href =
            to.kind === "page" ? linkHref({ kind: "url", url: resolvePage?.(to) }) : linkHref(to);
          if (href && to.kind === "anchor") href = "#" + encodeURIComponent(anchorPrefix + to.node);
          if (href) text = h("a", { href }, text);
        } else {
          const tag = (
            { bold: "strong", italic: "em", underline: "u", strike: "s", code: "code" } as Record<
              string,
              string
            >
          )[mark.type]!;
          text = h(tag, null, text);
        }
      }
      return h(Fragment, { key }, text);
    }
    if (node.type === "image") {
      const alt =
        typeof attrs.alt === "string"
          ? attrs.alt
          : String((attrs.alt as Record<string, unknown> | undefined)?.[locale] ?? "");
      const image = images[String(attrs.assetId)];
      if (image) {
        try {
          return h("img", { key, ...Image(image, { alt, width: { base: "100vw" } }).props });
        } catch {
          /* unavailable/expired signed image uses accessible fallback */
        }
      }
      return alt ? h("span", { key, role: "img", "aria-label": alt }, alt) : null;
    }
    const tag =
      node.type === "heading"
        ? "h" + attrs.level
        : (
            {
              doc: "div",
              paragraph: "p",
              bulletList: "ul",
              orderedList: "ol",
              listItem: "li",
              blockquote: "blockquote",
              divider: "hr",
              hardBreak: "br",
            } as Record<string, string>
          )[node.type]!;
    return h(tag, { key }, content);
  }
  return render(doc, 0);
}
