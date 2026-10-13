import { useEffect, useRef } from "react";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView, keymap, lineNumbers, highlightActiveLine } from "@codemirror/view";
import { history, historyKeymap, defaultKeymap, indentWithTab } from "@codemirror/commands";
import { json } from "@codemirror/lang-json";
import { syntaxHighlighting, defaultHighlightStyle } from "@codemirror/language";
import { autocompletion, completionKeymap, type CompletionSource } from "@codemirror/autocomplete";
import { setDiagnostics } from "@codemirror/lint";
import type { JsonDiagnostic } from "./json-source.js";
export function JsonEditor({
  value,
  readOnly,
  label,
  onChange,
  onSave,
  onCursor,
  diagnostics,
  completion,
  selection,
}: {
  value: string;
  readOnly: boolean;
  label: string;
  onChange: (text: string) => void;
  onSave: () => void;
  diagnostics: JsonDiagnostic[];
  completion: CompletionSource;
  selection?: { from: number; to: number } | undefined;
  onCursor?: (position: number) => void;
}) {
  const parent = useRef<HTMLDivElement>(null),
    view = useRef<EditorView | null>(null);
  const callbacks = useRef({ onChange, onSave, onCursor });
  callbacks.current = { onChange, onSave, onCursor };
  const editable = useRef(new Compartment()),
    complete = useRef(new Compartment()),
    accessible = useRef(new Compartment());
  const extensions = () => [
    json(),
    lineNumbers(),
    highlightActiveLine(),
    syntaxHighlighting(defaultHighlightStyle),
    history(),
    EditorView.lineWrapping,
    keymap.of([
      {
        key: "Mod-Enter",
        run: () => {
          callbacks.current.onSave();
          return true;
        },
      },
      ...completionKeymap,
      ...historyKeymap,
      ...defaultKeymap,
      indentWithTab,
    ]),
    editable.current.of([EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly)]),
    complete.current.of(autocompletion({ override: [completion] })),
    accessible.current.of(
      EditorView.contentAttributes.of({
        "aria-label": label,
        role: "textbox",
        "aria-multiline": "true",
      }),
    ),
    EditorView.updateListener.of((update) => {
      if (update.docChanged) callbacks.current.onChange(update.state.doc.toString());
      if (update.selectionSet) callbacks.current.onCursor?.(update.state.selection.main.head);
    }),
  ];
  const settings = useRef({ value, extensions });
  settings.current = { value, extensions };
  useEffect(() => {
    const current = new EditorView({
      parent: parent.current!,
      state: EditorState.create({
        doc: settings.current.value,
        extensions: settings.current.extensions(),
      }),
    });
    view.current = current;
    return () => {
      view.current = null;
      current.destroy();
    };
    // The view survives React rerenders; only explicit external resets replace its document.
  }, []);
  useEffect(() => {
    const v = view.current!;
    if (v.state.doc.toString() !== value)
      v.setState(EditorState.create({ doc: value, extensions: settings.current.extensions() }));
  }, [value]);
  useEffect(() => {
    view.current!.dispatch({
      effects: [
        editable.current.reconfigure([
          EditorState.readOnly.of(readOnly),
          EditorView.editable.of(!readOnly),
        ]),
        complete.current.reconfigure(autocompletion({ override: [completion] })),
        accessible.current.reconfigure(
          EditorView.contentAttributes.of({
            "aria-label": label,
            role: "textbox",
            "aria-multiline": "true",
          }),
        ),
      ],
    });
  }, [readOnly, label, completion]);
  useEffect(() => {
    const v = view.current!;
    v.dispatch(
      setDiagnostics(
        v.state,
        diagnostics.map((d) => ({
          ...d,
          message: d.code + " " + d.pointer,
          severity: "error" as const,
        })),
      ),
    );
  }, [diagnostics]);
  useEffect(() => {
    if (selection) {
      view.current!.dispatch({
        selection: { anchor: selection.from, head: selection.to },
        scrollIntoView: true,
      });
      view.current!.focus();
    }
  }, [selection]);
  return <div className="json-code" ref={parent} />;
}
