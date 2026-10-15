import { useEffect, useMemo, useState, useRef } from "react";
import { useStore } from "zustand";
import { jsonLanguage } from "@codemirror/lang-json";
import type { CompletionSource } from "@codemirror/autocomplete";
import type { Editor } from "./editor.js";
import { JsonEditor } from "./json-editor.js";
import { analyzeJson, jsonSuggestions } from "./json-source.js";
import { layers } from "./fields.js";
const pretty = (value: unknown) => JSON.stringify(value, null, 2);
export default function JsonPanel({ editor }: { editor: Editor }) {
  const state = useStore(editor.store),
    ru = state.locale === "ru",
    session = state.session!,
    doc = state.document!;
  const [scope, setScope] = useState<"node" | "document">("node");
  const selected = scope === "node" ? state.selected! : undefined;
  const context = session.changeset.id + ":" + session.record!.id + ":" + (selected ?? "document");
  const capture = () => ({
    base: structuredClone(doc),
    seq: session.changeset.seq,
    context,
    nodeId: selected,
    text: pretty(selected ? doc.nodes[selected] : doc),
  });
  const [draft, setDraft] = useState(capture),
    [selection, setSelection] = useState<{ from: number; to: number }>();
  const dirty = draft.text !== pretty(draft.nodeId ? draft.base.nodes[draft.nodeId] : draft.base);
  const stale = draft.seq !== session.changeset.seq || context !== draft.context;
  const otherDirty = Object.entries(state.dirtyFields).some(
    ([key, value]) => key !== "json" && value,
  );
  const readOnly =
    state.status !== "ready" ||
    !session.review.canEdit ||
    session.changeset.state !== "open" ||
    otherDirty ||
    !!(selected && layers(doc).find((n) => n.id === selected)?.locked);
  const analysis = useMemo(
    () => analyzeJson(draft.base, draft.text, session.record!.id, draft.nodeId),
    [draft.base, draft.text, session.record!.id, draft.nodeId],
  );
  useEffect(() => {
    editor.formDirty("json", dirty);
    return () => editor.formDirty("json", false);
  }, [editor, dirty]);
  useEffect(() => {
    if (!dirty && (stale || pretty(selected ? doc.nodes[selected] : doc) !== draft.text))
      setDraft(capture());
  }, [context, session.changeset.seq, doc, dirty]);
  const save = async () => {
    const current = editor.store.getState();
    if (
      readOnly ||
      stale ||
      !analysis.operations.length ||
      analysis.diagnostics.length ||
      current.session?.changeset.seq !== draft.seq
    )
      return;
    await editor.apply(analysis.operations, current.selected ?? undefined, "json");
    const fresh = editor.store.getState();
    if (fresh.status === "ready" && !fresh.error && fresh.session && fresh.document) {
      const id = scope === "node" ? fresh.selected! : undefined;
      setDraft({
        base: structuredClone(fresh.document),
        seq: fresh.session.changeset.seq,
        context:
          fresh.session.changeset.id + ":" + fresh.session.record!.id + ":" + (id ?? "document"),
        nodeId: id,
        text: pretty(id ? fresh.document.nodes[id] : fresh.document),
      });
    }
  };
  const completion: CompletionSource = (ctx) => {
    const text = ctx.state.doc.toString(),
      token = ctx.matchBefore(/"[\w$.-]*$/);
    if (!token && !ctx.explicit) return null;
    const path: string[] = [];
    let node = jsonLanguage.parser.parse(text).resolveInner(ctx.pos, -1);
    for (let parent = node.parent; parent; parent = parent.parent)
      if (parent.name === "Property") {
        const name = parent.getChild("PropertyName");
        if (name && name !== node && ctx.pos > name.to) {
          try {
            path.unshift(JSON.parse(text.slice(name.from, name.to)));
          } catch {
            /* incomplete key */
          }
        }
        node = parent;
      }
    return {
      from: token ? token.from + 1 : ctx.pos,
      to: ctx.pos,
      options: jsonSuggestions(draft.base, session.manifest, "/" + path.join("/"), selected).map(
        (label) => ({
          label,
          apply: token ? label + (text[ctx.pos] === '"' ? "" : '"') : JSON.stringify(label),
          type: "property",
        }),
      ),
    };
  };
  const handlers = useRef({ completion });
  handlers.current = { completion };
  const stableCompletion = useMemo<CompletionSource>(
    () => (ctx) => handlers.current.completion(ctx),
    [],
  );
  return (
    <section className="json-panel" aria-label="Structure JSON">
      <div className="json-tools">
        <label>
          {ru ? "Область JSON" : "JSON scope"}
          <select
            aria-label={ru ? "Область JSON" : "JSON scope"}
            value={scope}
            disabled={dirty || state.status !== "ready"}
            onChange={(e) => setScope(e.target.value as typeof scope)}
          >
            <option value="node">{ru ? "Выбранный узел" : "Selected node"}</option>
            <option value="document">{ru ? "Вся страница" : "Whole page"}</option>
          </select>
        </label>
        <button
          disabled={
            readOnly || stale || !analysis.operations.length || !!analysis.diagnostics.length
          }
          onClick={() => {
            void save();
          }}
        >
          {ru ? "Сохранить JSON" : "Save JSON"}
        </button>
        <button
          disabled={state.status === "saving" || state.status === "loading"}
          onClick={() => {
            setDraft(capture());
            setSelection(undefined);
            editor.formDirty("json", false);
            editor.error("");
          }}
        >
          {ru ? "Сбросить ввод" : "Discard input"}
        </button>
        <button
          disabled={readOnly || !!analysis.diagnostics.length}
          onClick={() => setDraft((d) => ({ ...d, text: pretty(JSON.parse(d.text)) }))}
        >
          {ru ? "Форматировать JSON" : "Format JSON"}
        </button>
      </div>
      <p className="hint">
        {ru
          ? "Подсказки: Ctrl/⌘+Space. Сохранение: Ctrl/⌘+Enter. Отмена внутри JSON относится к вводу."
          : "Suggestions: Ctrl/⌘+Space. Save: Ctrl/⌘+Enter. Undo inside JSON edits the input."}
      </p>
      {stale && dirty && (
        <p role="alert">
          {ru
            ? "Черновик изменился после начала ввода. Перечитайте его и явно сбросьте ввод перед новой правкой."
            : "The draft changed after input started. Reload it and explicitly discard input before a new edit."}
        </p>
      )}
      {otherDirty && (
        <p role="alert">
          {ru
            ? "Сначала сохраните или сбросьте значения в панели свойств."
            : "Save or discard the property form values first."}
        </p>
      )}
      {readOnly && <p>{ru ? "JSON доступен только для чтения" : "JSON is read-only"}</p>}
      <JsonEditor
        value={draft.text}
        readOnly={readOnly}
        label={ru ? "Редактор JSON" : "JSON editor"}
        onChange={(text) => setDraft((d) => ({ ...d, text }))}
        onSave={() => {
          void save();
        }}
        completion={stableCompletion}
        diagnostics={analysis.diagnostics}
        selection={selection}
        onCursor={(pos) => {
          if (scope !== "document") return;
          const range = [...analysis.ranges]
            .reverse()
            .find((r) => r.from <= pos && r.to >= pos && r.pointer.startsWith("/nodes/"));
          const id = range?.pointer.split("/")[2]?.replace(/~1/g, "/").replace(/~0/g, "~");
          if (id && doc.nodes[id]) editor.select(id, null, "json");
        }}
      />
      <ul className="json-diagnostics" aria-label={ru ? "Диагностика JSON" : "JSON diagnostics"}>
        {analysis.diagnostics.map((d, i) => (
          <li key={i}>
            <button onClick={() => setSelection({ from: d.from, to: d.to })}>
              <code>{d.code}</code> {d.pointer || "/"}
            </button>
          </li>
        ))}
      </ul>
      <details open className="json-diff">
        <summary>
          {ru ? "Изменения перед сохранением" : "Changes before saving"} (
          {analysis.operations.length})
        </summary>
        {!analysis.operations.length && !analysis.diagnostics.length && (
          <p>{ru ? "Нет изменений документа" : "No document changes"}</p>
        )}
        <ol>
          {analysis.operations.map((op, i) => (
            <li key={i}>
              <code>{op.type}</code>
              <pre>{JSON.stringify(op.payload, null, 2)}</pre>
            </li>
          ))}
        </ol>
      </details>
      <p className="hint">
        {ru
          ? "Сервер проверит права, manifest и политики при сохранении; успешное сохранение ещё не является публикацией."
          : "The server checks permissions, manifest and policy on save; saving is not publishing."}
      </p>
    </section>
  );
}
