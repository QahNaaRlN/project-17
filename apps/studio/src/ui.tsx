import { GithubConfigPanel } from "./capability-github-panel.js";
import { useEffect, useRef, useState, lazy, type ReactNode } from "react";
import { useStore } from "zustand";
import { useQuery } from "@tanstack/react-query";
import type { Node } from "@cms/ir";
import type { Editor } from "./editor.js";
import {
  fields,
  fieldOperations,
  designOperation,
  designFields,
  layers,
  type Field,
} from "./fields.js";
import { StudioError, type Api, type Session } from "./types.js";
import { Workspace } from "./workspace.js";
import { HistoryPanel } from "./history.js";
import { PublishingPanel } from "./publishing.js";
import { WorkItems } from "./work-items.js";
import { PublicationHistory } from "./publication-history-panel.js";
import { PresencePanel } from "./presence.js";
import { DeferredPanel } from "./deferred-panel.js";
import { ContentBindingsPanel } from "./content-bindings-panel.js";
import { DataSourcesPanel } from "./data-sources-panel.js";
import { BehaviorPanel } from "./behavior-panel.js";
import { CapabilityComposer, CapabilityQueue } from "./capability-panel.js";
import { DeliveryTargetPanel } from "./capability-delivery-panel.js";
const AgentPanel = lazy(() =>
  import("./agent-panel.js").then((module) => ({ default: module.AgentPanel })),
);
import { Signal, Button } from "./components.js";
const JsonPanel = lazy(() => import("./json-panel.js"));
const ContentPanel = lazy(() => import("./content-panel.js"));
const StructurePanel = lazy(() =>
  import("./structure-ui.js").then((module) => ({ default: module.StructurePanel })),
);
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
    PREVIEW_READ_ONLY:
      "Режим предпросмотра PR: только просмотр. Вернитесь в рабочее окружение для правки.",
    PREVIEW_OUTDATED: "Сборка preview изменилась. Перезапустите предпросмотр.",
    FIELD_DRAFT_PENDING:
      "Сохраните или сбросьте введённое значение перед выбором другого элемента.",
    CONTENT_DRAFT_PENDING: "Сохраните контент или сбросьте ввод перед другой правкой.",
    CONTENT_TARGET_MISMATCH: "Первым target CS должно быть окружение Studio.",
    BINDING_VALUE_MISSING: "Значение контента отсутствует в выбранной локали.",
    DATA_SCOPE_CHANGED:
      "Изменение нарушает привязки элемента или ключ повторения. Сначала снимите зависимые привязки.",
    CMS_SOURCE_UNKNOWN: "Источник отсутствует в приложении.",
    CMS_DATA_VALUE_INVALID: "Значение данных недоступно или не соответствует свойству блока.",
    CMS_SOURCE_TIMEOUT: "Источник не ответил вовремя. Повторите предпросмотр.",
    CMS_SOURCE_FAILED: "Не удалось загрузить данные источника.",
    CMS_SOURCE_PARAMS_INVALID: "Параметры источника не соответствуют контракту.",
    CMS_SOURCE_RESULT_INVALID: "Источник вернул данные неверного типа.",
    CMS_REPEAT_INVALID: "Повторение не получило корректный список.",
    CMS_REPEAT_KEY_INVALID: "У элемента отсутствует корректный ключ.",
    CMS_REPEAT_KEY_DUPLICATE: "Ключи элементов повторяются.",
    BINDING_PATH_UNRESOLVED: "Путь контента не поддерживается или недоступен.",
    CMS_IMAGE_UNAVAILABLE: "Изображение недоступно или ещё не готово.",
    CONTENT_REFERENCE_UNPUBLISHED: "Источник или ассет недоступен в этом окружении.",
    SCHEMA_UPCAST_REQUIRED: "Версия источника не соответствует схеме окружения.",
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
    PREVIEW_READ_ONLY: "PR preview mode is view-only. Return to the working environment to edit.",
    PREVIEW_OUTDATED: "The preview build changed. Restart the preview.",
    FIELD_DRAFT_PENDING: "Save or discard the field input before selecting another element.",
    BINDING_VALUE_MISSING: "The content value is missing in the selected locale.",
    DATA_SCOPE_CHANGED:
      "This change breaks item bindings or a repeat key. Clear dependent bindings first.",
    CMS_SOURCE_UNKNOWN: "The application source is unavailable.",
    CMS_DATA_VALUE_INVALID: "A data value is unavailable or does not match the block property.",
    CMS_SOURCE_TIMEOUT: "The source timed out. Retry preview.",
    CMS_SOURCE_FAILED: "Could not load source data.",
    CMS_SOURCE_PARAMS_INVALID: "Source parameters do not match the contract.",
    CMS_SOURCE_RESULT_INVALID: "The source returned an invalid result.",
    CMS_REPEAT_INVALID: "Repeat did not receive a valid list.",
    CMS_REPEAT_KEY_INVALID: "An item has an invalid or missing key.",
    CMS_REPEAT_KEY_DUPLICATE: "Item keys are duplicated.",
    BINDING_PATH_UNRESOLVED: "The content path is unsupported or unavailable.",
    CMS_IMAGE_UNAVAILABLE: "The image is unavailable or not ready yet.",
    CONTENT_REFERENCE_UNPUBLISHED: "The source or asset is unavailable in this environment.",
    SCHEMA_UPCAST_REQUIRED: "The source version does not match the environment schema.",
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
        {(field.binding !== undefined || field.value !== undefined) && (
          <details>
            <summary>{t === copy.ru ? "Показать значение" : "Show value"}</summary>
            <code>{JSON.stringify(field.binding ?? field.value)}</code>
          </details>
        )}
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
  rights,
  actorId = "service",
}: {
  editor: Editor;
  api: Api;
  accountControls?: ReactNode;
  rights?: string[];
  actorId?: string;
}) {
  const [showContent, setShowContent] = useState(false);
  const [contentRevision, setContentRevision] = useState(0);
  const [contentSelection, setContentSelection] = useState<
    { id: string; kind: "entity" | "asset"; changesetId: string } | undefined
  >();
  const [formRevision, setFormRevision] = useState(0);
  const [fitCanvas, setFitCanvas] = useState(true);
  const [canvasWidth, setCanvasWidth] = useState(1024);
  const canvasScroll = useRef<HTMLDivElement>(null);
  const state = useStore(editor.store),
    t = copy[state.locale];
  const saved = new URLSearchParams(
    typeof window === "undefined" ? "" : window.location.hash.slice(1),
  );
  const documentId = saved.get("document") ?? "",
    changesetId = saved.get("changeset") ?? "";
  const [screen, setScreenValue] = useState<
    | "pages"
    | "editor"
    | "work"
    | "publications"
    | "capabilities"
    | "delivery-target"
    | "github-config"
  >(
    saved.has("publication")
      ? "publications"
      : saved.has("work") || (rights && !rights.includes("design.read"))
        ? "work"
        : documentId && changesetId
          ? "editor"
          : "pages",
  );
  const workView = useRef<{ y: number; id: string } | null>(null);
  function setScreen(next: typeof screen) {
    const id = editor.store.getState().session?.changeset.id;
    if (screen === "work" && next !== "work" && id) workView.current = { y: window.scrollY, id };
    setScreenValue(next);
  }
  useEffect(() => {
    if (screen !== "work" || !workView.current) return;
    const { y, id } = workView.current;
    document
      .querySelector<HTMLElement>('[data-work-id="' + id + '"]')
      ?.focus({ preventScroll: true });
    if (window.scrollY !== y) window.scrollTo({ top: y });
  }, [screen]);
  const [openedWork, setOpenedWork] = useState(0);
  const [diagnosticNode, setDiagnosticNode] = useState<string | null>(null);
  useEffect(() => {
    if (openedWork) document.getElementById("work-card-title")?.focus();
  }, [openedWork]);
  useEffect(() => {
    if (!openedWork && screen === "work" && state.session?.work && state.status === "ready")
      document.getElementById("work-card-title")?.focus();
  }, [state.session?.changeset.id]);
  useEffect(() => {
    if (!diagnosticNode || screen !== "editor") return;
    [...document.querySelectorAll<HTMLElement>('[role="treeitem"]')]
      .find((item) => item.title === diagnosticNode)
      ?.focus();
    setDiagnosticNode(null);
  }, [diagnosticNode, screen]);
  useEffect(() => {
    const refresh = () => {
      const current = editor.store.getState();
      if (
        !current.session?.work ||
        current.status !== "ready" ||
        document.visibilityState === "hidden"
      )
        return;
      editor.remoteChanged();
      void editor.refreshRemote();
    };
    const timer = setInterval(refresh, 30000);
    window.addEventListener("focus", refresh);
    return () => {
      clearInterval(timer);
      window.removeEventListener("focus", refresh);
    };
  }, [editor]);
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
    if (saved.has("work")) void editor.openWork(saved.get("work")!);
    else if (documentId && changesetId && (!rights || rights.includes("design.read")))
      void editor.open(documentId, changesetId);
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
    const canvas = canvasScroll.current;
    if (!canvas || typeof ResizeObserver === "undefined") return;
    const measure = () => {
      const style = getComputedStyle(canvas);
      const width =
        canvas.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
      if (width > 0) setCanvasWidth(width);
    };
    const observer = new ResizeObserver(measure);
    observer.observe(canvas);
    measure();
    return () => observer.disconnect();
  }, [state.previewSrc]);
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
        Object.values(current.dirtyFields).some(Boolean) ||
        !current.session?.history[action === "undo" ? "canUndo" : "canRedo"]
      )
        return;
      event.preventDefault();
      void editor.changeHistory(action);
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [editor]);
  const work = state.session;
  const objectNames: Record<string, string> =
    state.locale === "ru"
      ? { page: "Страница", component: "Компонент", entity: "Сущность", asset: "Ассет" }
      : { page: "Page", component: "Component", entity: "Entity", asset: "Asset" };
  const session = work?.record ? { ...work, record: work.record } : null,
    doc = state.document;
  const tree = doc ? layers(doc) : [],
    selected = doc && state.selected ? doc.nodes[state.selected] : undefined;
  const busy = ["saving", "loading", "uncertain"].includes(state.status);
  const contentDirty = !!(state.dirtyFields.content || state.dirtyFields.contentUpload);
  const jsonDirty = !!state.dirtyFields.json || contentDirty;
  const dirty = Object.values(state.dirtyFields).some(Boolean);
  const navigationLocked = busy || dirty;
  const zoom = fitCanvas ? Math.min(1, canvasWidth / state.width) : 1;
  const disabled =
    busy ||
    jsonDirty ||
    !!state.dirtyFields["property-behavior"] ||
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
    <main className="studio-shell" data-screen={screen}>
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
              disabled={navigationLocked}
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
            disabled={navigationLocked || (!!rights && !rights.includes("design.read"))}
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
          <Button
            variant="ghost"
            aria-current={screen === "publications" ? "page" : undefined}
            disabled={navigationLocked}
            onClick={() => setScreen("publications")}
          >
            {state.locale === "ru" ? "Публикации" : "Publications"}
          </Button>
          {session && navigationLocked && (
            <p className="hint">
              {state.locale === "ru"
                ? "Завершите или сбросьте текущий ввод перед переходом."
                : "Finish or discard your current input before navigating."}
            </p>
          )}
          {api.capabilities &&
            (!rights ||
              rights.includes("capability.request") ||
              rights.includes("code.propose")) && (
              <Button
                variant="ghost"
                aria-current={screen === "capabilities" ? "page" : undefined}
                disabled={navigationLocked}
                onClick={() => setScreen("capabilities")}
              >
                {state.locale === "ru" ? "Запросы возможностей" : "Capability requests"}
              </Button>
            )}
          {api.githubConfig && api.githubCommand && rights?.includes("project.admin") && (
            <Button
              variant="ghost"
              aria-current={screen === "github-config" ? "page" : undefined}
              disabled={navigationLocked}
              onClick={() => setScreen("github-config")}
            >
              {state.locale === "ru" ? "Интеграция GitHub" : "GitHub integration"}
            </Button>
          )}
          {api.deliveryEndpoints &&
            api.deliveryTarget &&
            api.deliveryCommand &&
            rights?.includes("project.admin") && (
              <Button
                variant="ghost"
                aria-current={screen === "delivery-target" ? "page" : undefined}
                disabled={navigationLocked}
                onClick={() => setScreen("delivery-target")}
              >
                {state.locale === "ru" ? "Получатель запросов" : "Request recipient"}
              </Button>
            )}
        </nav>
        <section className="studio-content" id="studio-content" tabIndex={-1}>
          {screen === "capabilities" && <CapabilityQueue {...{ api, editor }} />}
          {screen === "delivery-target" && <DeliveryTargetPanel {...{ api, editor }} />}
          {screen === "github-config" && <GithubConfigPanel {...{ api, editor }} />}
          {screen === "publications" && api.publications && (
            <PublicationHistory api={api} editor={editor} />
          )}
          {state.publication && screen !== "publications" && (
            <Button
              disabled={navigationLocked}
              onClick={() => {
                const hash = new URLSearchParams(window.location.hash.slice(1));
                hash.set("publication", state.publication!.id);
                window.history.replaceState({}, "", "/editor#" + hash);
                setScreen("publications");
              }}
            >
              {state.locale === "ru" ? "Открыть публикацию" : "Open publication"}
            </Button>
          )}
          {(!rights || rights.includes("design.read")) && (
            <details className="editor-selector" open={screen === "pages"}>
              <summary>{state.locale === "ru" ? "Страница и черновик" : "Page and draft"}</summary>
              <Workspace
                {...{ api, editor }}
                busy={navigationLocked}
                locale={state.locale}
                compact={screen !== "pages"}
                onOpened={() => setScreen("editor")}
              />
            </details>
          )}
          {config.isError && <p role="alert">STUDIO_CONFIG_UNAVAILABLE</p>}
          <div className="editor-surface" hidden={screen !== "editor"}>
            <div className="editor-heading">
              <div className="editor-document">
                <span className="studio-eyebrow">
                  {state.locale === "ru" ? "Редактор страницы" : "Page editor"}
                </span>
                <h1>{session ? doc?.nodes[doc.root]?.name || session.record.path : t.page}</h1>
                {session && (
                  <p>
                    <code>{session.record.path}</code>
                    <span>{session.changeset.title}</span>
                  </p>
                )}
              </div>
              <div className="editor-save-state" role="status" aria-live="polite">
                <span className="studio-badge accent">
                  {!busy && state.status !== "error" && dirty
                    ? state.locale === "ru"
                      ? "Есть несохранённый ввод"
                      : "Unsaved input"
                    : t[state.status]}
                </span>
                {session && <span>#{session.changeset.seq}</span>}
                {session && (
                  <div className="content-toggle">
                    <button
                      aria-expanded={showContent}
                      disabled={navigationLocked}
                      onClick={() => setShowContent((v) => !v)}
                    >
                      {state.locale === "ru" ? "Контент" : "Content"}
                    </button>
                  </div>
                )}
              </div>
            </div>
            <div className="canvas-tools">
              <div
                role="group"
                aria-label={state.locale === "ru" ? "Представление редактора" : "Editor view"}
              >
                <button
                  aria-pressed={state.viewMode === "visual"}
                  disabled={navigationLocked}
                  onClick={() => editor.viewMode("visual")}
                >
                  {state.locale === "ru" ? "Визуальный" : "Visual"}
                </button>
                <button
                  aria-pressed={state.viewMode === "structure"}
                  disabled={navigationLocked}
                  onClick={() => editor.viewMode("structure")}
                >
                  Structure
                </button>
                <button
                  aria-pressed={state.viewMode === "agent"}
                  disabled={navigationLocked}
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
              <button aria-pressed={fitCanvas} onClick={() => setFitCanvas((value) => !value)}>
                {state.locale === "ru" ? "Вписать" : "Fit"} · {Math.round(zoom * 100)}%
              </button>
            </div>

            <PresencePanel {...{ api, editor }} />
            {showContent && session && doc && (
              <DeferredPanel locale={state.locale} canReload={!navigationLocked}>
                <ContentPanel
                  key={
                    session.changeset.id +
                    ":" +
                    session.record.id +
                    ":" +
                    formRevision +
                    ":" +
                    (contentSelection?.id ?? "")
                  }
                  initialSelection={
                    contentSelection?.changesetId === session.changeset.id
                      ? contentSelection
                      : undefined
                  }
                  {...{ api, editor }}
                />
              </DeferredPanel>
            )}
            <div className="workspace">
              {session ? (
                <DeferredPanel locale={state.locale} canReload={!navigationLocked}>
                  <StructurePanel editor={editor} />
                </DeferredPanel>
              ) : (
                <aside className="layers">
                  <h2>{t.layers}</h2>
                  <p className="hint">{t.idle}</p>
                </aside>
              )}
              <section className="canvas" aria-label={t.preview}>
                {state.viewMode === "agent" && session && (
                  <DeferredPanel locale={state.locale} canReload={!navigationLocked}>
                    <AgentPanel
                      key={session.changeset.id + ":" + session.record.id + ":" + formRevision}

                      {...{ api, editor }}
                    />
                  </DeferredPanel>
                )}
                {state.viewMode === "structure" && session && doc && (
                  <DeferredPanel locale={state.locale} canReload={!navigationLocked}>
                    <JsonPanel
                      key={session.changeset.id + ":" + session.record.id + ":" + formRevision}

                      editor={editor}
                    />
                  </DeferredPanel>
                )}
                {state.viewTarget && (
                  <div role="status" className="preview-banner">
                    {state.locale === "ru"
                      ? `Режим предпросмотра PR #${state.viewTarget.pullNumber}: только просмотр · ${state.viewTarget.environment} · коммит `
                      : `PR #${state.viewTarget.pullNumber} preview mode: view only · ${state.viewTarget.environment} · commit `}
                    <code title={state.viewTarget.commit}>
                      {state.viewTarget.commit.slice(0, 7)}
                    </code>
                    <button onClick={() => void editor.viewEnvironment(state.viewTarget)}>
                      {state.locale === "ru" ? "Перезапустить предпросмотр" : "Restart preview"}
                    </button>
                    <button onClick={() => void editor.viewEnvironment(null)}>
                      {state.locale === "ru"
                        ? "Вернуться в рабочее окружение"
                        : "Back to the working environment"}
                    </button>
                  </div>
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
                  <div className="canvas-scroll" ref={canvasScroll}>
                    <div
                      className="canvas-zoom"
                      style={{ width: state.width * zoom, height: 800 * zoom }}
                    >
                      <div
                        className="frame-wrap"
                        style={{
                          width: state.width,
                          transform: `scale(${zoom})`,
                          transformOrigin: "top left",
                        }}
                      >
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
                  </div>
                ) : (
                  <div className="empty-canvas">
                    <span>◇</span>
                    <p>{t[state.status]}</p>
                  </div>
                )}
              </section>
              <aside className="properties" inert={state.viewTarget ? true : undefined}>
                <ContentBindingsPanel {...{ api, editor, rights }} />
                <DataSourcesPanel {...{ editor, rights }} />
                <BehaviorPanel {...{ editor, rights }} />
                <CapabilityComposer {...{ api, editor, rights }} />
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
                        key={`${session.changeset.id}:${session.record.id}:${selected.id}:${session.changeset.seq}:${session.changeset.state}:${state.locale}:${formRevision}:${field.name}`}
                        {...{ field, node: selected, session, editor, t }}
                        disabled={
                          disabled ||
                          Object.entries(state.dirtyFields).some(
                            ([key, value]) => value && key !== `property-${field.name}`,
                          )
                        }
                      />
                    ))}
                    <h2>{t.design}</h2>
                    <label>
                      {t.point}
                      <select
                        aria-label={t.point}
                        disabled={navigationLocked}
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
                    {designFields(selected, session.manifest).length === 0 && (
                      <p className="hint">
                        {state.locale === "ru"
                          ? "Настройки отступов доступны у контейнеров и кнопок."
                          : "Spacing controls are available for containers and buttons."}
                      </p>
                    )}
                    {designFields(selected, session.manifest).map((name) => (
                      <Design
                        key={`${session.changeset.id}:${session.record.id}:${selected.id}:${session.changeset.seq}:${session.changeset.state}:${point}:${formRevision}:${name}`}
                        {...{ name, node: selected, session, editor, t, point }}
                        disabled={
                          disabled ||
                          Object.entries(state.dirtyFields).some(
                            ([key, value]) => value && key !== `design-${name}`,
                          )
                        }
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
                    disabled={
                      state.status === "saving" ||
                      state.status === "loading" ||
                      (dirty && state.status !== "uncertain")
                    }
                    onClick={() => {
                      void editor.refresh();
                    }}
                  >
                    {t.refresh}
                  </button>
                  {dirty && (
                    <button
                      disabled={busy}
                      onClick={() => {
                        void editor.refresh().then(() => {
                          if (editor.store.getState().status === "ready")
                            setFormRevision((revision) => revision + 1);
                        });
                      }}
                    >
                      {state.locale === "ru"
                        ? "Сбросить ввод и перечитать"
                        : "Discard input and reload"}
                    </button>
                  )}
                </>
              )}
              {session &&
                (!(
                  session.review.canEdit ||
                  session.review.canEditBehavior ||
                  session.review.canEditCondition
                ) ||
                  session.changeset.state !== "open") && <p>{t.readOnly}</p>}
              {state.error && (
                <p role="alert">
                  {errorText[state.locale][state.error] ?? t.problem}{" "}
                  {state.error !== "FIELD_DRAFT_PENDING" && <code>{state.error}</code>}
                </p>
              )}
            </footer>
          </div>
          <div hidden={screen !== "editor"}>
            <WorkItems
              api={api}
              editor={editor}
              actorId={actorId}
              rights={rights}
              onOpened={() => setScreen("editor")}
            />
          </div>
          {
            <div hidden={screen !== "work"}>
              <WorkItems
                api={api}
                editor={editor}
                actorId={actorId}
                rights={rights}
                compact={false}
                onOpened={() => setOpenedWork((value) => value + 1)}
              />
            </div>
          }
          <div className="work-review" hidden={screen === "pages" || screen === "publications"}>
            {screen === "work" &&
              (work ? (
                <div className="work-card-heading">
                  <div>
                    <p className="studio-eyebrow">
                      {state.locale === "ru" ? "Выбранная работа" : "Selected work"}
                    </p>
                    <h2 id="work-card-title" tabIndex={-1}>
                      {work.changeset.title}
                    </h2>
                    <p>
                      <code>
                        {work.record?.path ??
                          (state.locale === "ru" ? "Весь состав работы" : "All work objects")}
                      </code>{" "}
                      · #{work.changeset.seq}
                    </p>
                    {work.work && (
                      <p>
                        {work.work.item.ownerName} ·{" "}
                        <time dateTime={work.work.item.updatedAt}>
                          {new Date(work.work.item.updatedAt).toLocaleString(state.locale, {
                            timeZoneName: "short",
                          })}
                        </time>
                      </p>
                    )}
                  </div>
                  <div className="work-card-actions">
                    {session && (
                      <Button disabled={navigationLocked} onClick={() => setScreen("editor")}>
                        {state.locale === "ru" ? "Открыть редактор" : "Open editor"}
                      </Button>
                    )}
                    <Button
                      disabled={state.status === "saving" || state.status === "loading" || dirty}
                      onClick={() => {
                        void editor.refresh();
                      }}
                    >
                      {t.refresh}
                    </Button>
                  </div>
                </div>
              ) : (
                <p className="studio-notice" role="status">
                  {["FORBIDDEN", "NOT_FOUND", "WORK_QUEUE_ITEM_NOT_FOUND"].includes(
                    state.error ?? "",
                  ) &&
                    (state.locale === "ru"
                      ? "Работа больше недоступна. "
                      : "The work is no longer accessible. ")}
                  {state.locale === "ru"
                    ? "Откройте работу из очереди, чтобы увидеть изменения, проверки и решения."
                    : "Open a work item to see changes, checks and decisions."}
                </p>
              ))}
            {screen === "work" && work?.work && (
              <ul aria-label={state.locale === "ru" ? "Состав работы" : "Work objects"}>
                {work.work.objects.map((o) => (
                  <li key={o.id}>
                    <strong>{o.title || objectNames[o.docKind ?? o.kind] || o.kind}</strong> ·{" "}
                    <code>{o.workingPath ?? o.basePath ?? o.id.slice(-8)}</code>
                    {o.deleted && (state.locale === "ru" ? " · Удалено" : " · Deleted")}
                    {o.kind === "document" &&
                      o.docKind === "page" &&
                      !o.deleted &&
                      o.workingPath && (
                        <Button
                          disabled={navigationLocked}
                          onClick={async () => {
                            await editor.open(o.id, work.changeset.id);
                            if (editor.store.getState().session?.record?.id === o.id) {
                              window.history.replaceState(
                                {},
                                "",
                                "/editor#" +
                                  new URLSearchParams({
                                    document: o.id,
                                    changeset: work.changeset.id,
                                  }),
                              );
                              setScreen("editor");
                            }
                          }}
                        >
                          {(state.locale === "ru" ? "Открыть страницу " : "Open page ") +
                            o.workingPath}
                        </Button>
                      )}
                    {(o.kind === "entity" || o.kind === "asset") && !o.deleted && (
                      <Button
                        disabled={navigationLocked}
                        onClick={() => {
                          setContentSelection({
                            id: o.id,
                            kind: o.kind as "entity" | "asset",
                            changesetId: work.changeset.id,
                          });
                          setShowContent(true);
                        }}
                      >
                        {state.locale === "ru" ? "Открыть контент" : "Open content"}
                      </Button>
                    )}
                  </li>
                ))}
                {!work.work.objects.length && (
                  <li>{state.locale === "ru" ? "Нет изменений" : "No changes"}</li>
                )}
              </ul>
            )}
            {screen === "work" &&
              work?.work &&
              !work.record &&
              work.review.canEdit &&
              work.changeset.state === "open" && (
                <div className="work-card-actions">
                  {!work.work.objects.length && (
                    <p>
                      {state.locale === "ru"
                        ? "В работе пока нет изменений"
                        : "This work has no changes yet"}
                    </p>
                  )}
                  {(["entity", "asset"] as const).map((kind) => {
                    const right = kind === "entity" ? "content" : "asset";
                    if (
                      rights &&
                      (!rights.includes("content.read") || !rights.includes(right + ".write"))
                    )
                      return null;
                    return (
                      <Button
                        key={kind}
                        disabled={navigationLocked}
                        onClick={() => {
                          setContentRevision((value) => value + 1);
                          setContentSelection({ id: "", kind, changesetId: work.changeset.id });
                          setShowContent(true);
                        }}
                      >
                        {kind === "entity"
                          ? state.locale === "ru"
                            ? "Создать сущность"
                            : "Create entity"
                          : state.locale === "ru"
                            ? "Загрузить файл"
                            : "Upload file"}
                      </Button>
                    );
                  })}
                </div>
              )}
            {screen === "work" &&
              work &&
              !work.record &&
              showContent &&
              contentSelection?.changesetId === work.changeset.id && (
                <DeferredPanel locale={state.locale} canReload={!navigationLocked}>
                  <ContentPanel
                    key={
                      work.changeset.id + ":" + contentRevision + ":" + (contentSelection?.id ?? "")
                    }
                    api={api}
                    editor={editor}
                    rights={rights}
                    {...(contentSelection ? { initialSelection: contentSelection } : {})}
                  />
                </DeferredPanel>
              )}
            <PublishingPanel
              api={api}
              onContent={(id, kind) => {
                if (navigationLocked || !work) return;
                setContentSelection({ id, kind, changesetId: work.changeset.id });
                setShowContent(true);
                if (work.record) setScreen("editor");
              }}
              editor={editor}
              onSelectDiagnostic={(node) => {
                setDiagnosticNode(node);
                setScreen("editor");
              }}
            />
            {work && (
              <HistoryPanel
                session={work}
                api={api}
                editor={editor}
                busy={navigationLocked}
                locale={state.locale}
                revision={state.historyRevision}
                retry={state.historyRetry}
              />
            )}
          </div>
        </section>
      </div>
    </main>
  );
}
