import { useState, type ReactNode } from "react";
import { jsonValue } from "@cms/runtime";
import type { Type } from "@cms/manifest";
import type { RichTextNode, RichTextValue } from "@cms/component-registry";
import type { Api, Session } from "./types.js";
import { JsonValue, ReferencePicker } from "./content-fields.js";
const blocks = [
  "paragraph",
  "heading",
  "bulletList",
  "orderedList",
  "blockquote",
  "image",
  "divider",
];
const marks = ["bold", "italic", "underline", "strike", "code", "link"];
const labels: Record<string, [string, string]> = {
  doc: ["Документ", "Document"],
  paragraph: ["Абзац", "Paragraph"],
  heading: ["Заголовок", "Heading"],
  bulletList: ["Маркированный список", "Bullet list"],
  orderedList: ["Нумерованный список", "Numbered list"],
  listItem: ["Пункт списка", "List item"],
  blockquote: ["Цитата", "Quote"],
  image: ["Изображение", "Image"],
  divider: ["Разделитель", "Divider"],
  text: ["Текст", "Text"],
  hardBreak: ["Перенос строки", "Line break"],
  bold: ["Жирный", "Bold"],
  italic: ["Курсив", "Italic"],
  underline: ["Подчёркнутый", "Underline"],
  strike: ["Зачёркнутый", "Strikethrough"],
  code: ["Код", "Code"],
  link: ["Ссылка", "Link"],
};
const empty = (type: string): RichTextNode => ({
  type,
  ...(type === "text"
    ? { text: "" }
    : type === "heading"
      ? { attrs: { level: 2 }, content: [] }
      : ["image", "divider", "hardBreak"].includes(type)
        ? {}
        : { content: [] }),
});
export function RichEditor({
  value,
  schema,
  label,
  onChange,
  onInvalid,
  api,
  session,
  ru,
}: {
  value: unknown;
  schema: Extract<Type, { type: "richText" }>;
  label: string;
  onChange: (v: unknown) => void;
  onInvalid: (bad: boolean) => void;
  api: Api;
  session: Session;
  ru: boolean;
}) {
  const [advanced, setAdvanced] = useState(false),
    [rawInvalid, setRawInvalid] = useState(false);
  const doc = (value ?? { type: "doc", content: [] }) as RichTextValue;
  let invalid = false;
  try {
    jsonValue(doc);
    let visits = 0;
    const safe = (n: RichTextNode): boolean =>
      ++visits <= 5000 &&
      !!n &&
      typeof n === "object" &&
      ["doc", ...blocks, "listItem", "text", "hardBreak"].includes(n.type) &&
      (n.content === undefined || (Array.isArray(n.content) && n.content.every(safe))) &&
      (n.marks === undefined ||
        (Array.isArray(n.marks) &&
          n.marks.every((m) => !!m && typeof m === "object" && marks.includes(m.type))));
    invalid = doc.type !== "doc" || !Array.isArray(doc.content) || !safe(doc);
  } catch {
    invalid = true;
  }
  if (advanced || invalid)
    return (
      <div>
        <p>
          {ru
            ? "Структурный RichText v1 — HTML не принимается"
            : "Structured RichText v1 — HTML is not accepted"}
        </p>
        <JsonValue
          {...{ value: doc, label: label + " JSON", onChange }}
          onInvalid={(bad) => {
            setRawInvalid(bad);
            onInvalid(bad);
          }}
        />
        <button
          type="button"
          disabled={invalid || rawInvalid}
          onClick={() => {
            onInvalid(false);
            setAdvanced(false);
          }}
        >
          {ru ? "Редактор блоков" : "Block editor"}
        </button>
      </div>
    );
  function node(n: RichTextNode, change: (v: RichTextNode) => void, path: string): ReactNode {
    const choices = ["paragraph", "heading"].includes(n.type)
      ? ["text", "hardBreak"]
      : ["bulletList", "orderedList"].includes(n.type)
        ? ["listItem"]
        : blocks.filter((type) => !schema.blocks || schema.blocks.includes(type as never));
    return (
      <fieldset className="rich-node" key={path}>
        <legend>{labels[n.type]?.[ru ? 0 : 1] ?? n.type}</legend>
        {n.type === "text" && (
          <>
            <textarea
              aria-label={path + " text"}
              value={n.text ?? ""}
              onChange={(e) => change({ ...n, text: e.target.value })}
            />
            <div>
              {marks
                .filter((m) => !schema.marks || schema.marks.includes(m as never))
                .map((m) => (
                  <label key={m}>
                    <input
                      type="checkbox"
                      checked={!!n.marks?.some((v) => v.type === m)}
                      onChange={(e) =>
                        change({
                          ...n,
                          marks: e.target.checked
                            ? [
                                ...(n.marks ?? []),
                                {
                                  type: m,
                                  ...(m === "link"
                                    ? { attrs: { to: { kind: "url", url: "https://" } } }
                                    : {}),
                                },
                              ]
                            : (n.marks ?? []).filter((v) => v.type !== m),
                        })
                      }
                    />
                    {labels[m]![ru ? 0 : 1]}
                  </label>
                ))}
            </div>
            {n.marks?.some((m) => m.type === "link") && (
              <JsonValue
                key="link"
                label={path + " link"}
                value={n.marks.find((m) => m.type === "link")!.attrs?.to}
                onInvalid={onInvalid}
                onChange={(to) =>
                  change({
                    ...n,
                    marks: n.marks!.map((m) => (m.type === "link" ? { ...m, attrs: { to } } : m)),
                  })
                }
              />
            )}
          </>
        )}
        {n.type === "heading" && (
          <select
            aria-label={path + " level"}
            value={Number(n.attrs?.level ?? 2)}
            onChange={(e) => change({ ...n, attrs: { level: Number(e.target.value) } })}
          >
            {[2, 3, 4].map((v) => (
              <option key={v}>{v}</option>
            ))}
          </select>
        )}
        {n.type === "image" && (
          <>
            <ReferencePicker
              {...{ api, session, ru }}
              schema={{ type: "asset", assetKind: "image" }}
              label={path + " image"}
              value={n.attrs}
              onChange={(attrs) => change({ ...n, attrs: attrs as Record<string, unknown> })}
            />
            <input
              aria-label={path + " alt"}
              value={typeof n.attrs?.alt === "string" ? n.attrs.alt : ""}
              onChange={(e) => change({ ...n, attrs: { ...n.attrs, alt: e.target.value } })}
            />
          </>
        )}
        {n.content && (
          <>
            <ol>
              {n.content.map((child, i) => (
                <li key={i}>
                  {node(
                    child,
                    (v) =>
                      change({ ...n, content: n.content!.map((old, at) => (at === i ? v : old)) }),
                    path + "/" + i,
                  )}
                  <button
                    type="button"
                    onClick={() =>
                      change({ ...n, content: n.content!.filter((_v, at) => at !== i) })
                    }
                  >
                    {ru ? "Удалить блок" : "Remove block"}
                  </button>
                  <button
                    type="button"
                    disabled={i === 0}
                    onClick={() => {
                      const content = [...n.content!];
                      [content[i - 1], content[i]] = [content[i]!, content[i - 1]!];
                      change({ ...n, content });
                    }}
                  >
                    {ru ? "Выше" : "Move up"}
                  </button>
                </li>
              ))}
            </ol>
            <select
              aria-label={path + " add"}
              value=""
              onChange={(e) => {
                if (e.target.value)
                  change({ ...n, content: [...n.content!, empty(e.target.value)] });
              }}
            >
              <option value="">{ru ? "Добавить элемент" : "Add element"}</option>
              {choices.map((type) => (
                <option key={type} value={type}>
                  {labels[type]![ru ? 0 : 1]}
                </option>
              ))}
            </select>
          </>
        )}
      </fieldset>
    );
  }
  return (
    <div aria-label={label}>
      {node(doc, (v) => onChange(v), label)}
      <button type="button" onClick={() => setAdvanced(true)}>
        {ru ? "RichText JSON" : "RichText JSON"}
      </button>
    </div>
  );
}
