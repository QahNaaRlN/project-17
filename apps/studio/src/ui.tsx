import { useEffect, useRef, useState } from "react";
import { useStore } from "zustand";
import { useQuery } from "@tanstack/react-query";
import type { Node } from "@cms/ir";
import type { Editor } from "./editor.js";
import { fields, fieldOperations, designOperation, layers, type Field } from "./fields.js";
import { StudioError, type Api, type Session } from "./types.js";
const copy = {
  ru: {
    page: "Страница",
    draft: "Черновик",
    open: "Открыть",
    layers: "Слои",
    properties: "Свойства",
    design: "Дизайн",
    save: "Сохранить",
    select: "Выберите элемент в дереве или на странице",
    preview: "Предпросмотр страницы",
    loading: "Загрузка…",
    idle: "Откройте страницу и черновик по ID",
    ready: "Черновик загружен",
    saving: "Сохранение…",
    uncertain: "Исход сохранения неизвестен. Перечитайте черновик перед новой правкой.",
    error: "Не удалось открыть страницу",
    refresh: "Перечитать черновик",
    retry: "Повторить предпросмотр",
    readOnly: "Этот черновик доступен только для чтения",
    binding: "Связь с данными — редактирование появится в следующем пакете",
    unsupported: "Это поле пока доступно только для чтения",
    mode: "Режим",
    interact: "Взаимодействие",
    padding: "Внутренний отступ",
    gap: "Промежуток",
    point: "Размер экрана",
    base: "Базовое значение",
    width: "Ширина предпросмотра",
    locale: "Язык",
    connecting: "Подключение предпросмотра…",
    connected: "Предпросмотр подключён",
    selected: "Выбранный элемент",
    token: "Токен или число пикселей",
    problem: "Изменение не подтверждено",
    local: "Текст сохраняется в локальном контенте страницы",
    instance: "Экземпляр повторения; правка меняет шаблон",
    locked: "Элемент заблокирован",
  },
  en: {
    page: "Page",
    draft: "Change Set",
    open: "Open",
    layers: "Layers",
    properties: "Properties",
    design: "Design",
    save: "Save",
    select: "Select an element in the tree or page",
    preview: "Page preview",
    loading: "Loading…",
    idle: "Open a page and Change Set by ID",
    ready: "Draft loaded",
    saving: "Saving…",
    uncertain: "Save outcome is unknown. Reload the draft before editing again.",
    error: "Could not open page",
    refresh: "Reload draft",
    retry: "Retry preview",
    readOnly: "This Change Set is read-only",
    binding: "Data binding — editing will be available in a later package",
    unsupported: "This field is currently read-only",
    mode: "Mode",
    interact: "Interact",
    padding: "Padding",
    gap: "Gap",
    point: "Breakpoint",
    base: "Base value",
    width: "Preview width",
    locale: "Language",
    connecting: "Connecting preview…",
    connected: "Preview connected",
    selected: "Selected element",
    token: "Token or pixels",
    problem: "Change was not confirmed",
    local: "Text is saved in the page's local content",
    instance: "Repeat instance; editing changes the template",
    locked: "Element is locked",
  },
};
type Copy = typeof copy.ru;
const errorText: Record<"ru" | "en", Record<string, string>> = {
  ru: {
    FORBIDDEN: "У вашей учётной записи нет права на эту правку.",
    CHANGESET_NOT_OWNER: "Этот черновик принадлежит другому пользователю.",
    CHANGESET_SEQ_CONFLICT: "Черновик изменился. Проверьте актуальную версию и повторите правку.",
    NETWORK_UNCERTAIN: "Ответ не получен. Проверьте состояние черновика перед повторной правкой.",
    FIELD_INVALID: "Значение не подходит этому свойству.",
    IDENTIFIER_INVALID: "Проверьте ID страницы и черновика.",
    SESSION_INVALID: "Страница или черновик не подходят выбранному окружению.",
    PREVIEW_HANDSHAKE_TIMEOUT:
      "Приложение не подключилось. Проверьте его адрес и настройки предпросмотра.",
    PREVIEW_DOCUMENT_MISMATCH: "Приложение открыло другую страницу. Проверьте её маршрут.",
  },
  en: {
    FORBIDDEN: "Your account cannot make this change.",
    CHANGESET_NOT_OWNER: "This Change Set belongs to another user.",
    CHANGESET_SEQ_CONFLICT: "The draft changed. Check its current version and try again.",
    NETWORK_UNCERTAIN: "No response was received. Check the draft before trying again.",
    FIELD_INVALID: "This value does not match the property.",
    IDENTIFIER_INVALID: "Check the page and Change Set IDs.",
    SESSION_INVALID: "The page or Change Set does not match this environment.",
    PREVIEW_HANDSHAKE_TIMEOUT:
      "The application did not connect. Check its address and preview settings.",
    PREVIEW_DOCUMENT_MISMATCH: "The application opened another page. Check its route.",
  },
};
function Property({
  field,
  node,
  session,
  editor,
  disabled,
  t,
}: {
  field: Field;
  node: Node;
  session: Session;
  editor: Editor;
  disabled: boolean;
  t: Copy;
}) {
  const [value, setValue] = useState(field.value === undefined ? "" : String(field.value));
  const id = "property-" + field.name;
  if (!field.editable)
    return (
      <div className="readonly-field">
        <strong>{field.name}</strong>
        <p>{field.binding !== undefined ? t.binding : t.unsupported}</p>
        <code>{JSON.stringify(field.binding ?? field.value)}</code>
      </div>
    );
  return (
    <form
      className="field"
      onSubmit={(event) => {
        event.preventDefault();
        try {
          void editor.apply(
            fieldOperations(
              session.record.id,
              node,
              field,
              value,
              "t_" + crypto.randomUUID().replace(/-/g, ""),
              editor.store.getState().locale,
            ),
          );
        } catch (error) {
          editor.error(error instanceof StudioError ? error.code : "FIELD_INVALID");
        }
      }}
    >
      <label htmlFor={id}>{field.name}</label>
      {field.schema.type === "enum" || field.schema.type === "boolean" ? (
        <select
          id={id}
          value={value}
          disabled={disabled}
          onChange={(event) => setValue(event.target.value)}
        >
          <option value="">—</option>
          {(field.schema.type === "boolean" ? ["true", "false"] : (field.schema.values ?? [])).map(
            (item) => (
              <option key={item}>{item}</option>
            ),
          )}
        </select>
      ) : (
        <input
          id={id}
          value={value}
          disabled={disabled}
          onChange={(event) => setValue(event.target.value)}
          type={field.schema.type === "number" ? "number" : "text"}
          step={field.schema.type === "number" && field.schema.integer ? 1 : "any"}
        />
      )}
      {field.schema.content && <small>{t.local}</small>}
      <button disabled={disabled}>
        {t.save} {field.name}
      </button>
    </form>
  );
}
function Design({
  name,
  node,
  session,
  editor,
  disabled,
  t,
  point,
}: {
  name: "padding" | "gap";
  node: Node;
  session: Session;
  editor: Editor;
  disabled: boolean;
  t: Copy;
  point: string;
}) {
  let initial: unknown = node.design?.[name];
  if (initial && typeof initial === "object" && "base" in initial)
    initial = (initial as Record<string, unknown>)[point] ?? initial.base;
  if (initial && typeof initial === "object" && "raw" in initial) initial = initial.raw;
  const [value, setValue] = useState(initial === undefined ? "" : String(initial));
  return (
    <form
      className="field"
      onSubmit={(event) => {
        event.preventDefault();
        try {
          void editor.apply([
            designOperation(session.record.id, node, name, value, session.manifest, point),
          ]);
        } catch {
          editor.error("FIELD_INVALID");
        }
      }}
    >
      <label htmlFor={"design-" + name}>{t[name]}</label>
      <input
        id={"design-" + name}
        list="spacing-tokens"
        placeholder={t.token}
        value={value}
        disabled={disabled}
        onChange={(event) => setValue(event.target.value)}
      />
      <button disabled={disabled}>
        {t.save} {t[name]}
      </button>
    </form>
  );
}
export function Studio({ editor, api }: { editor: Editor; api: Api }) {
  const state = useStore(editor.store),
    t = copy[state.locale];
  const saved = new URLSearchParams(
    typeof window === "undefined" ? "" : window.location.hash.slice(1),
  );
  const [documentId, setDocumentId] = useState(saved.get("document") ?? ""),
    [changesetId, setChangesetId] = useState(saved.get("changeset") ?? ""),
    [point, setPoint] = useState("base");
  const frame = useRef<HTMLIFrameElement>(null);
  const config = useQuery({
    queryKey: ["studio-config"],
    queryFn: ({ signal }) => api.config(signal),
    retry: false,
  });
  useEffect(() => {
    const message = (event: MessageEvent) => editor.receive(event);
    window.addEventListener("message", message);
    if (documentId && changesetId) void editor.open(documentId, changesetId);
    return () => {
      window.removeEventListener("message", message);
      editor.dispose();
    };
    // Initial deep link is read once. Subsequent openings are explicit.
  }, [editor]);
  useEffect(() => {
    editor.bindFrame(frame.current?.contentWindow ?? null);
  }, [editor, state.previewSrc]);
  useEffect(() => {
    document.documentElement.lang = state.locale;
  }, [state.locale]);
  const session = state.session,
    doc = state.document;
  const tree = doc ? layers(doc) : [],
    selected = doc && state.selected ? doc.nodes[state.selected] : undefined;
  const busy = ["saving", "loading", "uncertain"].includes(state.status);
  const disabled =
    busy ||
    session?.changeset.state !== "open" ||
    tree.find((item) => item.id === state.selected)?.locked === true;
  const rects =
    state.mode === "design" && state.selected
      ? (state.rects[state.selected] ?? []).filter(
          (rect) => !state.instance || rect.instance === state.instance,
        )
      : [];
  return (
    <main>
      <header>
        <div className="brand">
          <span className="mark">C</span>
          <div>
            <strong>CMS Studio</strong>
            <small>
              {config.data ? `${config.data.project} / ${config.data.environment}` : "Project 17"}
            </small>
          </div>
        </div>
        <label className="language">
          {t.locale}
          <select
            aria-label={t.locale}
            value={state.locale}
            onChange={(event) => editor.locale(event.target.value as "ru" | "en")}
          >
            <option value="ru">Русский</option>
            <option value="en">English</option>
          </select>
        </label>
      </header>
      <form
        className="open-bar"
        onSubmit={(event) => {
          event.preventDefault();
          window.location.hash = new URLSearchParams({
            document: documentId,
            changeset: changesetId,
          }).toString();
          void editor.open(documentId.trim(), changesetId.trim());
        }}
      >
        <label>
          {t.page}
          <input
            value={documentId}
            onChange={(event) => setDocumentId(event.target.value)}
            placeholder="UUID"
            required
            disabled={busy}
          />
        </label>
        <label>
          {t.draft}
          <input
            value={changesetId}
            onChange={(event) => setChangesetId(event.target.value)}
            placeholder="UUID"
            required
            disabled={busy}
          />
        </label>
        <button disabled={busy}>{t.open}</button>
      </form>
      {config.isError && <p role="alert">STUDIO_CONFIG_UNAVAILABLE</p>}
      <div className="workspace">
        <aside className="layers">
          <h2>{t.layers}</h2>
          <div
            role="tree"
            aria-label={t.layers}
            onKeyDown={(event) => {
              const index = tree.findIndex((item) => item.id === state.selected);
              let next: string | undefined;
              if (event.key === "ArrowDown") next = tree[Math.min(index + 1, tree.length - 1)]?.id;
              if (event.key === "ArrowUp") next = tree[Math.max(0, index - 1)]?.id;
              if (event.key === "Home") next = tree[0]?.id;
              if (event.key === "End") next = tree.at(-1)?.id;
              if (event.key === "ArrowLeft" || event.key === "Escape")
                next = tree[index]?.parent ?? undefined;
              if (event.key === "ArrowRight" && tree[index + 1]?.parent === state.selected)
                next = tree[index + 1]?.id;
              if (next) {
                event.preventDefault();
                editor.select(next);
                const buttons =
                  event.currentTarget.querySelectorAll<HTMLElement>("[role=treeitem]");
                buttons[tree.findIndex((item) => item.id === next)]?.focus();
              }
            }}
          >
            {tree.map((item) => (
              <button
                key={item.id}
                role="treeitem"
                aria-level={item.depth}
                aria-selected={state.selected === item.id}
                aria-expanded={tree.some((child) => child.parent === item.id) ? true : undefined}
                tabIndex={state.selected === item.id ? 0 : -1}
                style={{ paddingLeft: 12 + (item.depth - 1) * 16 }}
                title={item.id}
                onClick={() => editor.select(item.id)}
              >
                <span className="node-icon">◇</span>
                {item.label}
                {item.locked && <span aria-label={t.locked}> · 🔒</span>}
              </button>
            ))}
          </div>
        </aside>
        <section className="canvas" aria-label={t.preview}>
          <div className="canvas-tools">
            <div role="group" aria-label={t.mode}>
              <button aria-pressed={state.mode === "design"} onClick={() => editor.mode("design")}>
                {t.design}
              </button>
              <button
                aria-pressed={state.mode === "interact"}
                onClick={() => editor.mode("interact")}
              >
                {t.interact}
              </button>
            </div>
            <label>
              {t.width}
              <select
                aria-label={t.width}
                value={state.width}
                onChange={(event) => editor.width(Number(event.target.value))}
              >
                {[375, 768, 1024, 1440].map((width) => (
                  <option key={width}>{width}</option>
                ))}
              </select>
            </label>
          </div>
          {state.previewError && (
            <div role="alert" className="preview-error">
              {errorText[state.locale][state.previewError] ?? state.previewError}
              <button
                onClick={() => {
                  void editor.retryPreview();
                }}
              >
                {t.retry}
              </button>
            </div>
          )}
          {state.previewSrc ? (
            <div className="canvas-scroll">
              <div className="frame-wrap" style={{ width: state.width }}>
                <iframe
                  ref={frame}
                  title={t.preview}
                  src={state.previewSrc}
                  sandbox="allow-scripts allow-same-origin allow-forms"
                  referrerPolicy="no-referrer"
                  onLoad={() => editor.bindFrame(frame.current?.contentWindow ?? null)}
                />
                <div className="overlays" aria-hidden="true">
                  {rects.map((rect, index) => (
                    <div
                      key={index}
                      className="selection-frame"
                      style={{ left: rect.x, top: rect.y, width: rect.w, height: rect.h }}
                    />
                  ))}
                </div>
              </div>
            </div>
          ) : (
            <div className="empty-canvas">
              <span>◇</span>
              <p>{t[state.status]}</p>
            </div>
          )}
        </section>
        <aside className="properties">
          <h2>{t.properties}</h2>
          {selected && session && doc ? (
            <>
              <div className="selected-name">
                <strong>{selected.name ?? selected.type}</strong>
                <code>{selected.id}</code>
              </div>
              {state.instance && <p className="hint">{t.instance}</p>}
              {fields(doc, selected, session.manifest, state.locale).map((field) => (
                <Property
                  key={`${selected.id}:${session.changeset.seq}:${state.locale}:${field.name}`}
                  {...{ field, node: selected, session, editor, disabled, t }}
                />
              ))}
              <h2>{t.design}</h2>
              <label>
                {t.point}
                <select
                  aria-label={t.point}
                  value={point}
                  onChange={(event) => setPoint(event.target.value)}
                >
                  <option value="base">{t.base}</option>
                  {Object.keys(session.manifest.breakpoints).map((name) => (
                    <option key={name}>{name}</option>
                  ))}
                </select>
              </label>
              <datalist id="spacing-tokens">
                {Object.entries(session.manifest.tokens.spacing ?? {}).map(([name, value]) => (
                  <option key={name} value={name}>
                    {value}
                  </option>
                ))}
              </datalist>
              {(["padding", "gap"] as const).map((name) => (
                <Design
                  key={`${selected.id}:${session.changeset.seq}:${point}:${name}`}
                  {...{ name, node: selected, session, editor, disabled, t, point }}
                />
              ))}
            </>
          ) : (
            <p className="hint">{t.select}</p>
          )}
        </aside>
      </div>
      <footer>
        <div role="status">
          {t[state.status]}
          {session && (
            <span>
              {" "}
              · {session.changeset.title} · #{session.changeset.seq}
            </span>
          )}
        </div>
        {session && (
          <>
            <span className="connection">{state.connected ? t.connected : t.connecting}</span>
            <button
              disabled={state.status === "saving" || state.status === "loading"}
              onClick={() => {
                void editor.refresh();
              }}
            >
              {t.refresh}
            </button>
          </>
        )}
        {session && session.changeset.state !== "open" && <p>{t.readOnly}</p>}
        {state.error && (
          <p role="alert">
            {errorText[state.locale][state.error] ?? t.problem} <code>{state.error}</code>
          </p>
        )}
      </footer>
    </main>
  );
}
