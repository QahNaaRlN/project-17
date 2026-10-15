import { useEffect, useRef, useState } from "react";
import { useStore } from "zustand";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import type { IrDocument } from "@cms/ir";
import type { Editor } from "./editor.js";
import type { Api, Session } from "./types.js";
import {
  contentCompatible,
  contentFields,
  sourceChange,
  bindingOperations,
} from "./content-bindings.js";
import { fields } from "./fields.js";

export function ContentBindingsPanel({
  api,
  editor,
  rights,
}: {
  api: Api;
  editor: Editor;
  rights?: string[] | undefined;
}) {
  const state = useStore(editor.store),
    session = state.session;
  if (!session?.record || !state.document || state.document.kind !== "page") return null;
  if (rights && (!rights.includes("content.read") || !rights.includes("design.read")))
    return (
      <p>
        {state.locale === "ru"
          ? "Для данных страницы нужно право чтения контента."
          : "Content read access is required for page data."}
      </p>
    );
  return (
    <Bindings
      key={`${session.changeset.id}:${session.record.id}:${state.historyRevision}:${session.manifestHash}`}
      {...{ api, editor, rights }}
      session={{ ...session, record: session.record }}
    />
  );
}

function Bindings({
  api,
  editor,
  session,
  rights,
}: {
  api: Api;
  editor: Editor;
  session: Session;
  rights?: string[] | undefined;
}) {
  const state = useStore(editor.store),
    doc = state.document!,
    ru = state.locale === "ru";
  const [dialog, setDialog] = useState<"source" | string | null>(null),
    [schema, setSchema] = useState(doc.content?.schema ?? ""),
    [search, setSearch] = useState(""),
    [entityId, setEntityId] = useState(""),
    [path, setPath] = useState(""),
    [remove, setRemove] = useState(false);
  const trigger = useRef<HTMLButtonElement | null>(null),
    panel = useRef<HTMLFieldSetElement>(null);
  const open = dialog !== null;
  useEffect(() => {
    editor.formDirty("property-contentBinding", open);
    if (open) panel.current?.querySelector<HTMLElement>("input,select,button")?.focus();
    else trigger.current?.focus();
    return () => editor.formDirty("property-contentBinding", false);
  }, [open, editor]);
  function close() {
    setDialog(null);
    setRemove(false);
    setEntityId("");
    setPath("");
  }
  const blocked =
    state.status !== "ready" ||
    !session.review.canEdit ||
    session.changeset.state !== "open" ||
    session.changeset.kind !== "standard" ||
    (!!rights && !rights.includes("design.compose")) ||
    Object.entries(state.dirtyFields).some(
      ([key, value]) => value && key !== "property-contentBinding",
    );
  const sourceId = doc.content?.resolve.by === "entity" ? doc.content.resolve.entityId : "";
  const current = useQuery({
    queryKey: [
      "page-primary",
      session.config.project,
      session.changeset.id,
      session.changeset.seq,
      sourceId,
    ],
    queryFn: ({ signal }) => api.content(session, sourceId, signal),
    enabled: !!sourceId,
    retry: false,
  });
  const list = useInfiniteQuery({
    queryKey: [
      "page-primary-catalog",
      session.config.project,
      session.changeset.id,
      session.changeset.seq,
      schema,
      search,
    ],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      api.contentCatalog(session, "entity", schema, pageParam, signal, search),
    getNextPageParam: (page) => page.nextCursor ?? undefined,
    enabled: dialog === "source",
    retry: false,
  });
  const candidate = useQuery({
    queryKey: [
      "page-primary-candidate",
      session.config.project,
      session.changeset.id,
      session.changeset.seq,
      entityId,
    ],
    queryFn: ({ signal }) => api.content(session, entityId, signal),
    enabled: !!entityId && dialog === "source",
    retry: false,
  });
  const items =
    list.data?.pages
      .flatMap((p) => p.items)
      .filter((i) => !i.deleted && !!i.schema && !!session.manifest.schemas?.[i.schema]) ?? [];
  const selected = state.selected ? doc.nodes[state.selected] : undefined;
  const property =
    selected && dialog && dialog !== "source"
      ? fields(doc, selected, session.manifest).find((f) => f.name === dialog)
      : undefined;
  const projection = contentFields(
    session.manifest,
    doc.content?.schema ?? "",
    current.isError || current.data?.deleted ? undefined : current.data?.data,
    state.locale,
  );
  const sourceReady =
    doc.content?.resolve.by === "entity" &&
    !current.isError &&
    !!current.data &&
    !current.data.deleted &&
    current.data.schemaVersion === session.manifest.schemas?.[doc.content.schema]?.version;
  const sourceTitle = current.data?.data.title;
  const sourceName =
    typeof sourceTitle === "string"
      ? sourceTitle
      : sourceTitle &&
          typeof sourceTitle === "object" &&
          Object.hasOwn(sourceTitle, state.locale) &&
          typeof (sourceTitle as Record<string, unknown>)[state.locale] === "string"
        ? String((sourceTitle as Record<string, unknown>)[state.locale])
        : ru
          ? "Сущность подключена"
          : "Entity connected";
  const choices =
    property && sourceReady
      ? projection.fields.filter((f) => contentCompatible(f.schema, property.schema))
      : [];
  const declaration: IrDocument["content"] | null = remove
    ? null
    : candidate.data?.schema
      ? { schema: candidate.data.schema, resolve: { by: "entity", entityId: candidate.data.id } }
      : null;
  const change = sourceChange(doc, session.manifest, session.record.id, declaration);
  const candidateValid =
    remove ||
    (!candidate.isError &&
      !!candidate.data &&
      !candidate.data.deleted &&
      candidate.data.schemaVersion ===
        session.manifest.schemas?.[candidate.data.schema ?? ""]?.version);
  async function save() {
    if (blocked) return;
    const operations =
      dialog === "source"
        ? change.operations
        : selected && property
          ? bindingOperations(session.record.id, selected, property.name, remove ? null : path)
          : [];
    if (dialog === "source" && (!candidateValid || change.blocked.length)) return;
    await editor.apply(operations);
    if (editor.store.getState().status === "ready" && !editor.store.getState().error) close();
  }
  function begin(button: HTMLButtonElement, target: string) {
    trigger.current = button;
    setDialog(target);
    setRemove(false);
    setPath("");
    setEntityId("");
  }
  return (
    <section className="content-bindings" aria-label={ru ? "Данные страницы" : "Page data"}>
      <h2>{ru ? "Данные" : "Data"}</h2>
      {state.bindingRetry && (
        <button onClick={() => void editor.retryBinding()}>
          {ru ? "Повторить точное сохранение" : "Retry exact save"}
        </button>
      )}
      <p>
        {doc.content
          ? `${doc.content.schema} · ${doc.content.resolve.by === "entity" ? (current.isPending ? (ru ? "Загрузка…" : "Loading…") : current.isError || current.data?.deleted ? (ru ? "Источник недоступен" : "Source unavailable") : sourceName) : "routeParam"}`
          : ru
            ? "Основной контент не выбран"
            : "No primary content"}
      </p>
      {doc.content?.resolve.by === "routeParam" && (
        <p>
          {ru
            ? "Маршрутный источник доступен только для чтения. Замена требует подтверждения."
            : "Route source is read-only. Replacement requires confirmation."}
        </p>
      )}
      {current.data &&
        current.data.schemaVersion !==
          session.manifest.schemas?.[doc.content?.schema ?? ""]?.version && (
          <p role="alert">{ru ? "Версия схемы несовместима" : "Schema version mismatch"}</p>
        )}
      <button disabled={blocked || open} onClick={(e) => begin(e.currentTarget, "source")}>
        {ru ? "Выбрать сущность" : "Choose entity"}
      </button>
      {selected && (
        <div>
          <h3>{ru ? "Привязки свойств" : "Property bindings"}</h3>
          {fields(doc, selected, session.manifest).map((f) => (
            <div key={f.name}>
              <strong>{f.name}</strong>
              {f.binding !== undefined && (
                <code className="binding-expression">{JSON.stringify(f.binding)}</code>
              )}
              <button
                disabled={blocked || open || (!sourceReady && f.binding === undefined)}
                onClick={(e) => begin(e.currentTarget, f.name)}
              >
                {ru ? "Связать с контентом" : "Bind to content"} · {f.name}
              </button>
            </div>
          ))}
        </div>
      )}
      {open && (
        <fieldset
          ref={panel}
          className="binding-dialog"
          role="dialog"
          aria-modal="false"
          aria-label={
            dialog === "source"
              ? ru
                ? "Основной контент"
                : "Primary content"
              : ru
                ? "Привязка свойства"
                : "Property binding"
          }
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.stopPropagation();
              close();
            }
          }}
        >
          <legend>
            {dialog === "source"
              ? ru
                ? "Основной контент"
                : "Primary content"
              : `${selected?.type}.${dialog}`}
          </legend>
          {dialog === "source" ? (
            <>
              <label>
                {ru ? "Схема" : "Schema"}
                <select
                  value={schema}
                  aria-label={ru ? "Схема" : "Schema"}
                  disabled={remove || blocked}
                  onChange={(e) => {
                    setSchema(e.target.value);
                    setEntityId("");
                  }}
                >
                  <option value="">{ru ? "Все схемы" : "All schemas"}</option>
                  {Object.keys(session.manifest.schemas ?? {}).map((name) => (
                    <option key={name}>{name}</option>
                  ))}
                </select>
              </label>
              <label>
                {ru ? "Поиск сущности" : "Search entities"}
                <input
                  value={search}
                  aria-label={ru ? "Поиск сущности" : "Search entities"}
                  disabled={remove || blocked}
                  onChange={(e) => {
                    setSearch(e.target.value);
                    setEntityId("");
                  }}
                />
              </label>
              <label>
                {ru ? "Сущность" : "Entity"}
                <select
                  value={entityId}
                  aria-label={ru ? "Сущность" : "Entity"}
                  disabled={remove || blocked}
                  onChange={(e) => setEntityId(e.target.value)}
                >
                  <option value="">—</option>
                  {items.map((i) => (
                    <option key={i.id} value={i.id}>
                      {i.title || i.schema} · {i.schema}
                    </option>
                  ))}
                </select>
              </label>
              {list.isPending && <p>{ru ? "Загрузка…" : "Loading…"}</p>}
              {list.isError && (
                <button onClick={() => void list.refetch()}>
                  {ru ? "Повторить каталог" : "Retry catalog"}
                </button>
              )}
              {!list.isPending && !list.isError && !items.length && (
                <p>{ru ? "Сущностей нет" : "No entities"}</p>
              )}
              {list.hasNextPage && (
                <button
                  disabled={list.isFetching || blocked}
                  onClick={() => void list.fetchNextPage()}
                >
                  {ru ? "Ещё варианты" : "More choices"}
                </button>
              )}
              {candidate.isError && (
                <p role="alert">{ru ? "Источник недоступен" : "Source unavailable"}</p>
              )}
              {(remove || candidate.data) && (
                <>
                  {change.affected.length > 0 && (
                    <div>
                      <p>
                        {ru
                          ? "Подтверждение снимет следующие несовместимые привязки одной группой:"
                          : "Confirmation removes these incompatible bindings in one action:"}
                      </p>
                      <ul>
                        {change.affected.map((d) => (
                          <li key={d.pointer}>
                            {d.nodeId} · {d.prop}
                          </li>
                        ))}
                      </ul>
                    </div>
                  )}
                  {change.blocked.length > 0 && (
                    <div role="alert">
                      {ru
                        ? "Сначала измените неподдержанные зависимости в Structure:"
                        : "Update unsupported dependencies in Structure first:"}
                      <ul>
                        {change.blocked.map((p) => (
                          <li key={p}>{p}</li>
                        ))}
                      </ul>
                    </div>
                  )}
                </>
              )}
            </>
          ) : (
            <>
              <label>
                {ru ? "Поле контента" : "Content field"}
                <select
                  value={path}
                  aria-label={ru ? "Поле контента" : "Content field"}
                  disabled={remove || blocked}
                  onChange={(e) => setPath(e.target.value)}
                >
                  <option value="">—</option>
                  {choices.map((f) => (
                    <option key={f.path} value={f.path}>
                      {f.path} · {f.schema.type}
                    </option>
                  ))}
                </select>
              </label>
              {!choices.length && <p>{ru ? "Нет совместимых полей" : "No compatible fields"}</p>}
              {projection.limited && (
                <p role="alert">
                  {ru
                    ? "Выбор ограничен 200 полями и глубиной 8"
                    : "Selection is limited to 200 fields and depth 8"}
                </p>
              )}
              {path && (
                <p>
                  {choices.find((f) => f.path === path)?.present
                    ? JSON.stringify(choices.find((f) => f.path === path)?.value)
                    : ru
                      ? "Значение отсутствует в этой локали"
                      : "Value is missing in this locale"}
                </p>
              )}
              {(property?.binding !== undefined ||
                (selected && Object.hasOwn(selected.props ?? {}, dialog!))) && (
                <p>
                  {ru
                    ? "Подтверждение явно заменит текущее значение или привязку. Локальный контент сохранится."
                    : "Confirmation replaces the current value or binding. Local content is retained."}
                </p>
              )}
            </>
          )}
          {((dialog === "source" && doc.content) || property?.binding !== undefined) && (
            <label>
              <input
                type="checkbox"
                checked={remove}
                disabled={blocked}
                onChange={(e) => setRemove(e.target.checked)}
              />
              {ru ? "Убрать привязку" : "Remove binding"}
            </label>
          )}
          <button
            disabled={
              blocked ||
              (dialog === "source"
                ? !candidateValid || change.blocked.length > 0
                : !remove && !path)
            }
            onClick={() => void save()}
          >
            {ru ? "Подтвердить" : "Confirm"}
          </button>
          <button disabled={state.status === "saving"} onClick={close}>
            {ru ? "Отмена" : "Cancel"}
          </button>
        </fieldset>
      )}
    </section>
  );
}
