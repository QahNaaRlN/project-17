import { useEffect, useRef, useState, lazy, Suspense, type ReactNode } from "react";
import { useStore } from "zustand";
import { useQuery } from "@tanstack/react-query";
import type { Node } from "@cms/ir";
import type { Editor } from "./editor.js";
import { fields, fieldOperations, designOperation, layers, type Field } from "./fields.js";
import { StudioError, type Api, type Session } from "./types.js";
import { Workspace } from "./workspace.js";
import { HistoryPanel } from "./history.js";
import { StructurePanel } from "./structure-ui.js";
import { PublishingPanel } from "./publishing.js";
import { WorkItems } from "./work-items.js";
import { PresencePanel } from "./presence.js";
import { AgentPanel } from "./agent-panel.js";
import { Signal, Button } from "./components.js";
const JsonPanel = lazy(() => import("./json-panel.js"));
const ContentPanel = lazy(() => import("./content-panel.js"));
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
    idle: "Откройте страницу и свой черновик из списка",
    ready: "Черновик загружен",
    saving: "Сохранение…",
    uncertain: "Исход сохранения неизвестен. Перечитайте черновик перед новой правкой.",
    error: "Не удалось открыть страницу",
    refresh: "Перечитать черновик",
    retry: "Повторить предпросмотр",
    readOnly: "Этот черновик доступен только для чтения",
    binding: "Связь с данными — её можно изменить в Structure",
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
    idle: "Open a page and your draft from the list",
    ready: "Draft loaded",
    saving: "Saving…",
    uncertain: "Save outcome is unknown. Reload the draft before editing again.",
    error: "Could not open page",
    refresh: "Reload draft",
    retry: "Retry preview",
    readOnly: "This Change Set is read-only",
    binding: "Data binding — edit it in Structure",
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
    CONTENT_DRAFT_PENDING: "Сохраните контент или сбросьте ввод перед другой правкой.",
    CONTENT_TARGET_MISMATCH: "Первым target CS должно быть окружение Studio.",
    JSON_DRAFT_PENDING: "Сохраните JSON или сбросьте ввод перед переходом к другой правке.",
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
    CONTENT_DRAFT_PENDING: "Save content or discard input before another edit.",
    CONTENT_TARGET_MISMATCH: "The first CS target must match the Studio environment.",
    JSON_DRAFT_PENDING: "Save JSON or discard the input before starting another change.",
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
  const initial = field.value === undefined ? "" : String(field.value);
  const [value, setValue] = useState(initial);
  const id = "property-" + field.name;
  useEffect(() => {
    editor.formDirty(id, field.editable && value !== initial);
    return () => editor.formDirty(id, false);
  }, [editor, id, field.editable, initial, value]);
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
  const initialText = initial === undefined ? "" : String(initial),
    id = "design-" + name;
  useEffect(() => {
    editor.formDirty(id, value !== initialText);
    return () => editor.formDirty(id, false);
  }, [editor, id, initialText, value]);
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
export function Studio({
  editor,
  api,
  accountControls,
}: {
  editor: Editor;
  api: Api;
  accountControls?: ReactNode;
}) {
  const [showContent, setShowContent] = useState(false);
  const state = useStore(editor.store),
    t = copy[state.locale];
  const saved = new URLSearchParams(
    typeof window === "undefined" ? "" : window.location.hash.slice(1),
  );
  const documentId = saved.get("document") ?? "",
    changesetId = saved.get("changeset") ?? "";
  const [screen, setScreen] = useState<"pages" | "editor" | "work">(
    documentId && changesetId ? "editor" : "pages",
  );
  const [point, setPoint] = useState("base");
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
  useEffect(() => {
    if (!state.canvasPick || editor.store.getState().mode !== "design") return;
    // In design mode, return focus from the cross-origin canvas to its keyboard alternative.
    [...document.querySelectorAll<HTMLElement>('[role="treeitem"]')]
      .find((item) => item.title === editor.store.getState().selected)
      ?.focus({ preventScroll: true });
  }, [editor, state.canvasPick]);
  useEffect(() => {
    const key = (event: KeyboardEvent) => {
      if (
        event.defaultPrevented ||
        event.isComposing ||
        event.altKey ||
        !(event.ctrlKey || event.metaKey) ||
        !(event.key.toLowerCase() === "z" || event.code === "KeyZ")
      )
        return;
      const target = event.target;
      if (
        target instanceof Element &&
        target.closest(
          'input,textarea,select,[contenteditable]:not([contenteditable="false"]),[role="textbox"]',
        )
      )
        return;
      const current = editor.store.getState(),
        action = event.shiftKey ? "redo" : "undo";
      if (
        current.status !== "ready" ||
        !current.session?.history[action === "undo" ? "canUndo" : "canRedo"]
      )
        return;
      event.preventDefault();
      void editor.changeHistory(action);
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [editor]);
  const session = state.session,
    doc = state.document;
  useEffect(() => {
    if (session) setScreen("editor");
  }, [session?.record.id, session?.changeset.id]);
  const tree = doc ? layers(doc) : [],
    selected = doc && state.selected ? doc.nodes[state.selected] : undefined;
  const busy = ["saving", "loading", "uncertain"].includes(state.status);
  const contentDirty = !!(state.dirtyFields.content || state.dirtyFields.contentUpload);
  const jsonDirty = !!state.dirtyFields.json || contentDirty;
  const navigationLocked = busy || Object.values(state.dirtyFields).some(Boolean);
  const disabled =
    busy ||
    jsonDirty ||
    !session?.review.canEdit ||
    session?.changeset.state !== "open" ||
    tree.find((item) => item.id === state.selected)?.locked === true;
  const rects =
    state.mode === "design" && state.selected
      ? (state.rects[state.selected] ?? []).filter(
          (rect) => !state.instance || rect.instance === state.instance,
        )
      : [];
  return (
    <main className="studio-shell">
      <a className="studio-skip-link" href="#studio-content">
        {state.locale === "ru" ? "К содержимому" : "Skip to content"}
      </a>
      <header>
        <div className="brand">
          <Signal />
          <div>
            <strong>Project 17</strong>
            <small>
              {config.data ? `${config.data.project} / ${config.data.environment}` : "Project 17"}
            </small>
          </div>
        </div>
        <div className="studio-header-controls">
          {accountControls}
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
        </div>
      </header>
      <div className="studio-layout">
        <nav
          className="studio-navigation"
          aria-label={state.locale === "ru" ? "Рабочее пространство" : "Workspace"}
        >
          <p className="studio-eyebrow">
            {state.locale === "ru" ? "Рабочее пространство" : "Workspace"}
          </p>
          <Button
            variant="ghost"
            aria-current={screen === "pages" ? "page" : undefined}
            disabled={navigationLocked}
            onClick={() => setScreen("pages")}
          >
            {state.locale === "ru" ? "Страницы" : "Pages"}
          </Button>
          <Button
            variant="ghost"
            aria-current={screen === "editor" ? "page" : undefined}
            disabled={navigationLocked || !session}
            onClick={() => setScreen("editor")}
          >
            {state.locale === "ru" ? "Редактор" : "Editor"}
          </Button>
          <Button
            variant="ghost"
            aria-current={screen === "work" ? "page" : undefined}
            disabled={navigationLocked}
            onClick={() => setScreen("work")}
          >
            {state.locale === "ru" ? "Работы" : "Work"}
          </Button>
          {session && navigationLocked && (
            <p className="hint">
              {state.locale === "ru"
                ? "Завершите или сбросьте текущий ввод перед переходом."
                : "Finish or discard your current input before navigating."}
            </p>
          )}
        </nav>
        <section className="studio-content" id="studio-content" tabIndex={-1}>
          <Workspace
            {...{ api, editor }}
            busy={navigationLocked}
            locale={state.locale}
            compact={screen !== "pages"}
            onOpened={() => setScreen("editor")}
          />
          <div hidden={screen === "pages"}>
            <WorkItems api={api} editor={editor} />
          </div>
          {config.isError && <p role="alert">STUDIO_CONFIG_UNAVAILABLE</p>}
          <div className="editor-surface" hidden={screen !== "editor"}>
            <PresencePanel {...{ api, editor }} />
            {session && (
              <div className="content-toggle">
                <button
                  aria-expanded={showContent}
                  disabled={contentDirty || !!state.dirtyFields.json || busy}
                  onClick={() => setShowContent((v) => !v)}
                >
                  {state.locale === "ru" ? "Контент" : "Content"}
                </button>
              </div>
            )}
            {showContent && session && doc && (
              <Suspense fallback={<p role="status">{t.loading}</p>}>
                <ContentPanel
                  key={session.changeset.id + ":" + session.record.id}
                  {...{ api, editor }}
                />
              </Suspense>
            )}
            <div className="workspace">
              <StructurePanel editor={editor} />
              <section className="canvas" aria-label={t.preview}>
                <div className="canvas-tools">
                  <div
                    role="group"
                    aria-label={state.locale === "ru" ? "Представление редактора" : "Editor view"}
                  >
                    <button
                      aria-pressed={state.viewMode === "visual"}
                      disabled={jsonDirty}
                      onClick={() => editor.viewMode("visual")}
                    >
                      {state.locale === "ru" ? "Визуальный" : "Visual"}
                    </button>
                    <button
                      aria-pressed={state.viewMode === "structure"}
                      disabled={jsonDirty}
                      onClick={() => editor.viewMode("structure")}
                    >
                      Structure
                    </button>
                    <button
                      aria-pressed={state.viewMode === "agent"}
                      disabled={jsonDirty}
                      onClick={() => editor.viewMode("agent")}
                    >
                      Agent
                    </button>
                  </div>
                  <div role="group" aria-label={t.mode}>
                    <button
                      aria-pressed={state.mode === "design"}
                      onClick={() => editor.mode("design")}
                    >
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
                {state.viewMode === "agent" && session && (
                  <AgentPanel
                    key={session.changeset.id + ":" + session.record.id}
                    {...{ api, editor }}
                  />
                )}
                {state.viewMode === "structure" && session && doc && (
                  <Suspense fallback={<p role="status">{t.loading}</p>}>
                    <JsonPanel
                      key={session.changeset.id + ":" + session.record.id}
                      editor={editor}
                    />
                  </Suspense>
                )}
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
                        key={`${session.changeset.id}:${session.record.id}:${selected.id}:${session.changeset.seq}:${session.changeset.state}:${state.locale}:${field.name}`}
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
                      {Object.entries(session.manifest.tokens.spacing ?? {}).map(
                        ([name, value]) => (
                          <option key={name} value={name}>
                            {value}
                          </option>
                        ),
                      )}
                    </datalist>
                    {(["padding", "gap"] as const).map((name) => (
                      <Design
                        key={`${session.changeset.id}:${session.record.id}:${selected.id}:${session.changeset.seq}:${session.changeset.state}:${point}:${name}`}
                        {...{ name, node: selected, session, editor, disabled, t, point }}
                      />
                    ))}
                  </>
                ) : (
                  <p className="hint">{t.select}</p>
                )}
              </aside>
            </div>
            <PublishingPanel editor={editor} />
            {session && (
              <HistoryPanel
                {...{ session, api, editor }}
                busy={busy || jsonDirty}
                locale={state.locale}
                revision={state.historyRevision}
                retry={state.historyRetry}
              />
            )}
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
              {session && (!session.review.canEdit || session.changeset.state !== "open") && (
                <p>{t.readOnly}</p>
              )}
              {state.error && (
                <p role="alert">
                  {errorText[state.locale][state.error] ?? t.problem} <code>{state.error}</code>
                </p>
              )}
            </footer>
          </div>
        </section>
      </div>
    </main>
  );
}
