import { useEffect, useState } from "react";
import { useStore } from "zustand";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import type { Editor } from "./editor.js";
import { StudioError, type Api, type ContentOperation, type ContentRecord } from "./types.js";
import { assetFields, contentDiff, contentProblems, equal } from "./content.js";
import { ContentValue } from "./content-fields.js";
import { AssetUpload } from "./asset-upload.js";
type Draft = {
  manifestHash: string;
  base: ContentRecord | null;
  data: Record<string, unknown>;
  seq: number;
  schema: string;
};
export default function ContentPanel({
  api,
  editor,
  initialSelection,
  rights,
}: {
  api: Api;
  editor: Editor;
  initialSelection?: { id: string; kind: "entity" | "asset" } | undefined;
  rights?: string[] | undefined;
}) {
  const state = useStore(editor.store),
    session = state.session!,
    ru = state.locale === "ru";
  const canRead = (_kind: "entity" | "asset") => !rights || rights.includes("content.read");
  const [kind, setKind] = useState<"entity" | "asset">(
      initialSelection?.kind ?? (canRead("entity") ? "entity" : "asset"),
    ),
    [filter, setFilter] = useState(""),
    [id, setId] = useState(initialSelection?.id ?? ""),
    [draft, setDraft] = useState<Draft | null>(null),
    [errors, setErrors] = useState<Record<string, boolean>>({}),
    [fieldReset, setFieldReset] = useState<Record<string, number>>({}),
    [serial, setSerial] = useState(0),
    [uploadSerial, setUploadSerial] = useState(0),
    [locale, setLocale] = useState<string>(state.locale);
  const schemas = session.manifest.schemas ?? {},
    fields = kind === "asset" ? assetFields : (schemas[draft?.schema ?? filter]?.fields ?? {});
  const dirty =
    !!draft && (!equal(draft.data, draft.base?.data ?? {}) || Object.values(errors).some(Boolean));
  const uploadDirty = !!state.dirtyFields.contentUpload;
  const blocked =
    (!!rights && !rights.includes((kind === "entity" ? "content" : "asset") + ".write")) ||
    state.status !== "ready" ||
    !session.review.canEdit ||
    session.changeset.state !== "open" ||
    session.changeset.targets[0] !== session.config.environment ||
    Object.entries(state.dirtyFields).some(
      ([key, value]) => !["content", "contentUpload"].includes(key) && value,
    );
  const schemaMismatch =
    !!draft?.base &&
    draft.base.kind === "entity" &&
    draft.base.schemaVersion !== schemas[draft.schema]?.version;
  const list = useInfiniteQuery({
    queryKey: [
      "content",
      session.changeset.id,
      session.changeset.seq,
      state.historyRevision,
      kind,
      filter,
    ],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      api.contentCatalog(session, kind, filter, pageParam, signal),
    getNextPageParam: (page) => page.nextCursor ?? undefined,
    retry: false,
  });
  const read = useQuery({
    queryKey: [
      "content-record",
      session.changeset.id,
      id,
      session.changeset.seq,
      state.historyRevision,
    ],
    queryFn: ({ signal }) => api.content(session, id, signal),
    enabled: !!id,
    retry: false,
  });
  const catalogContext = list.data?.pages[0];
  const stale =
    !!draft &&
    (draft.seq !== session.changeset.seq ||
      draft.manifestHash !== session.manifestHash ||
      (!!draft.base && !!read.data && draft.base.versionId !== read.data.versionId));
  useEffect(() => {
    if (
      catalogContext?.locales.length &&
      !dirty &&
      !uploadDirty &&
      !catalogContext.locales.includes(locale)
    )
      setLocale(catalogContext.defaultLocale);
  }, [catalogContext?.defaultLocale, catalogContext?.locales.join(","), dirty, uploadDirty]);
  useEffect(() => {
    editor.formDirty("content", dirty);
    return () => editor.formDirty("content", false);
  }, [editor, dirty]);
  useEffect(() => {
    if (read.data && !dirty && !editor.store.getState().dirtyFields.contentUpload) {
      setDraft({
        base: structuredClone(read.data),
        data: structuredClone(read.data.data),
        seq: session.changeset.seq,
        manifestHash: session.manifestHash,
        schema: read.data.schema ?? "",
      });
      setErrors({});
      setSerial((v) => v + 1);
    }
  }, [read.data]);
  const reset = () => {
    if (read.data && id)
      setDraft({
        base: structuredClone(read.data),
        data: structuredClone(read.data.data),
        seq: session.changeset.seq,
        manifestHash: session.manifestHash,
        schema: read.data.schema ?? "",
      });
    else setDraft(null);
    setErrors({});
    setSerial((v) => v + 1);
    setUploadSerial((v) => v + 1);
    editor.formDirty("content", false);
    editor.error("");
  };
  const finish = (ids: string[] | null) => {
    if (ids) {
      editor.formDirty("content", false);
      setDraft(null);
      setErrors({});
      setSerial((v) => v + 1);
      setId(ids.at(-1)!);
    }
  };
  const saveOps = async (ops: ContentOperation[], seq: number) => {
    const ids = await editor.applyContent(ops, seq);
    finish(ids);
    if (ids && ops.some((op) => op.type === "asset.create" || op.type === "asset.replaceFile"))
      setUploadSerial((v) => v + 1);
  };
  const save = () => {
    if (!draft || blocked || stale || uploadDirty || schemaMismatch) return;
    try {
      const ops = draft.base
        ? contentDiff(draft.base, draft.data, fields)
        : [
            {
              type: "entity.create" as const,
              payload: {
                environment: session.config.environment,
                schema: draft.schema,
                data: draft.data,
              },
            },
          ];
      void saveOps(ops, draft.seq);
    } catch (cause) {
      editor.error(cause instanceof StudioError ? cause.code : "FIELD_INVALID");
    }
  };
  const problems = draft
    ? contentProblems(draft.data, fields, catalogContext?.locales ?? [], kind === "entity")
    : [];
  const disabled = blocked || stale || uploadDirty || schemaMismatch || draft?.base?.deleted;
  const locales = [
    ...new Set([
      ...(catalogContext?.locales.length
        ? catalogContext.locales
        : ["ru", "en", catalogContext?.defaultLocale ?? "ru"]),
      ...Object.entries(fields)
        .filter(([_name, field]) => field.localized)
        .flatMap(([name]) => Object.keys((draft?.data[name] ?? {}) as object)),
    ]),
  ];
  return (
    <section className="content-panel" aria-label={ru ? "Редактор контента" : "Content editor"}>
      <h2>{ru ? "Контент черновика" : "Draft content"}</h2>
      {session.changeset.targets[0] !== session.config.environment && (
        <p role="alert">
          {ru
            ? "Для правки контента первым target CS должно быть окружение Studio."
            : "To edit content, the first CS target must match the Studio environment."}
        </p>
      )}
      {schemaMismatch && (
        <p role="alert">
          {ru
            ? "Версия сущности и схема формы различаются. Перечитайте черновик."
            : "The entity version and form schema differ. Reload the draft."}
        </p>
      )}
      <div className="content-toolbar">
        <select
          aria-label={ru ? "Тип контента" : "Content kind"}
          value={kind}
          disabled={dirty || uploadDirty || state.status !== "ready"}
          onChange={(e) => {
            setKind(e.target.value as typeof kind);
            setFilter("");
            setId("");
            setDraft(null);
          }}
        >
          {canRead("entity") && <option value="entity">{ru ? "Сущности" : "Entities"}</option>}
          {canRead("asset") && <option value="asset">{ru ? "Ассеты" : "Assets"}</option>}
        </select>
        {kind === "entity" && (
          <select
            aria-label={ru ? "Схема контента" : "Content schema"}
            value={filter}
            disabled={dirty || uploadDirty || state.status !== "ready"}
            onChange={(e) => {
              setFilter(e.target.value);
              setId("");
              setDraft(null);
            }}
          >
            <option value="">{ru ? "Все схемы" : "All schemas"}</option>
            {Object.entries(schemas).map(([name, s]) => (
              <option key={name} value={name}>
                {s.title ?? name}
              </option>
            ))}
          </select>
        )}
        <button
          type="button"
          disabled={dirty || uploadDirty || state.status === "saving"}
          onClick={() => {
            void list.refetch();
            if (id) void read.refetch();
          }}
        >
          {ru ? "Обновить контент" : "Refresh content"}
        </button>
        {kind === "entity" && (
          <button
            type="button"
            disabled={blocked || dirty || uploadDirty || !filter || list.isPending}
            onClick={() => {
              setId("");
              setErrors({});
              setDraft({
                base: null,
                data: {},
                seq: session.changeset.seq,
                schema: filter,
                manifestHash: session.manifestHash,
              });
              setSerial((v) => v + 1);
            }}
          >
            {ru ? "Новая сущность" : "New entity"}
          </button>
        )}
      </div>
      {kind === "entity" && !Object.keys(schemas).length && (
        <p role="status">
          {ru
            ? "В manifest этого окружения нет схем сущностей. Обновите контент после регистрации схем."
            : "This environment's manifest has no entity schemas. Refresh content after registering schemas."}
        </p>
      )}
      {list.isPending && <p role="status">{ru ? "Загрузка…" : "Loading…"}</p>}
      {list.isError && (
        <p role="alert">{(list.error as StudioError).code ?? "CONTENT_READ_FAILED"}</p>
      )}
      <ul className="content-catalog">
        {list.data?.pages
          .flatMap((p) => p.items)
          .map((item) => (
            <li key={item.id}>
              <button
                type="button"
                aria-pressed={id === item.id}
                disabled={dirty || uploadDirty || state.status !== "ready"}
                onClick={() => {
                  setId(item.id);
                  setDraft(null);
                  setErrors({});
                }}
              >
                {item.title || item.schema || item.assetKind || kind} · {item.id.slice(-8)}
                {item.deleted ? " · deleted" : ""}
              </button>
            </li>
          ))}
      </ul>
      {list.data?.pages.every((p) => !p.items.length) && (
        <p>{ru ? "Нет доступного контента" : "No available content"}</p>
      )}
      {list.hasNextPage && (
        <button
          type="button"
          disabled={list.isFetching || dirty || uploadDirty}
          onClick={() => void list.fetchNextPage()}
        >
          {ru ? "Ещё контент" : "More content"}
        </button>
      )}
      {id && read.isPending && <p role="status">{ru ? "Чтение сущности…" : "Reading content…"}</p>}
      {read.isError && (
        <p role="alert">
          <code>{(read.error as StudioError).code ?? "CONTENT_READ_FAILED"}</code>
        </p>
      )}
      {state.contentRetry && (
        <button
          disabled={state.status === "saving"}
          onClick={() => void editor.retryContent().then(finish)}
        >
          {ru ? "Проверить сохранение контента" : "Check content save"}
        </button>
      )}
      {draft && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            save();
          }}
        >
          <h3>
            {draft.schema || "asset"}
            {draft.base?.deleted ? " · deleted" : ""}
          </h3>
          {draft.base &&
            (schemaMismatch || !session.review.canEdit || session.changeset.state !== "open") && (
              <details>
                <summary>{ru ? "Сохранённые данные" : "Saved data"}</summary>
                <pre>{JSON.stringify(draft.base.data, null, 2)}</pre>
              </details>
            )}
          <label>
            {ru ? "Локаль контента" : "Content locale"}
            <select
              value={locale}
              disabled={Object.values(errors).some(Boolean)}
              onChange={(e) => setLocale(e.target.value as typeof locale)}
            >
              {locales.map((v) => (
                <option key={v}>{v}</option>
              ))}
            </select>
          </label>
          {stale && (
            <p role="alert">
              {ru
                ? "Версия черновика изменилась. Перечитайте контент и явно сбросьте ввод."
                : "The draft version changed. Reload content and explicitly discard input."}
            </p>
          )}
          <fieldset disabled={!!disabled}>
            {Object.entries(fields).map(([name, field]) => {
              const map = (draft.data[name] ?? {}) as Record<string, unknown>,
                value = field.localized ? map[locale] : draft.data[name];
              const change = (v: unknown) =>
                setDraft((d) => {
                  if (!d) return d;
                  const data = { ...d.data };
                  if (field.localized) {
                    const values = { ...(data[name] as object) } as Record<string, unknown>;
                    if (v === undefined) delete values[locale];
                    else values[locale] = v;
                    if (Object.keys(values).length) data[name] = values;
                    else delete data[name];
                  } else if (v === undefined) delete data[name];
                  else data[name] = v;
                  return { ...d, data };
                });
              return (
                <div key={name} className="content-field">
                  <label>
                    {name}
                    {field.required ? " *" : ""}
                  </label>
                  <ContentValue
                    key={
                      serial +
                      ":" +
                      name +
                      ":" +
                      (field.localized ? locale : "") +
                      ":" +
                      (fieldReset[name] ?? 0)
                    }
                    {...{ api, session, ru }}
                    schema={field}
                    value={value}
                    label={"Content " + name + (field.localized ? " " + locale : "")}
                    onChange={change}
                    onInvalid={(bad) => setErrors((e) => ({ ...e, [name]: bad }))}
                  />
                  <button
                    type="button"
                    onClick={() => {
                      change(undefined);
                      setFieldReset((reset) => ({ ...reset, [name]: (reset[name] ?? 0) + 1 }));
                      setErrors((e) => ({ ...e, [name]: false }));
                    }}
                  >
                    {ru ? "Убрать значение" : "Remove value"}
                  </button>
                </div>
              );
            })}
          </fieldset>
          {problems.map((p) => (
            <p key={p.path} role={p.warning ? "status" : "alert"}>
              {p.warning
                ? ru
                  ? "Предупреждение: отсутствует обязательное поле"
                  : "Warning: missing required field"
                : ru
                  ? "Неверное значение"
                  : "Invalid value"}{" "}
              · {p.path}
            </p>
          ))}
          {Object.values(errors).some(Boolean) && <p role="alert">JSON_SYNTAX</p>}
          <button
            disabled={
              !!disabled ||
              problems.some((p) => !p.warning) ||
              Object.values(errors).some(Boolean) ||
              (draft.base !== null && !dirty)
            }
          >
            {ru ? "Сохранить контент" : "Save content"}
          </button>
          <button
            type="button"
            disabled={state.status === "saving" || state.contentRetry}
            onClick={reset}
          >
            {ru ? "Сбросить контент" : "Discard content"}
          </button>
          {draft.base?.kind === "entity" && (
            <button
              type="button"
              disabled={blocked || dirty || uploadDirty || stale}
              onClick={() => {
                if (
                  draft.base!.deleted ||
                  window.confirm(
                    ru ? "Удалить сущность в черновике?" : "Delete this entity in the draft?",
                  )
                )
                  void saveOps(
                    [
                      {
                        type: draft.base!.deleted ? "entity.restore" : "entity.delete",
                        target: draft.base!.id,
                        expectedVersionId: draft.base!.versionId,
                        payload: {},
                      },
                    ],
                    draft.seq,
                  );
              }}
            >
              {draft.base.deleted
                ? ru
                  ? "Восстановить сущность"
                  : "Restore entity"
                : ru
                  ? "Удалить сущность"
                  : "Delete entity"}
            </button>
          )}
        </form>
      )}
      {kind === "asset" && (
        <AssetUpload
          key={id + ":" + uploadSerial}
          {...{ api, editor, ru }}
          disabled={blocked || dirty || stale || (!!id && (!draft || read.isPending))}
          target={draft?.base?.id}
          expectedVersionId={draft?.base?.versionId}
          onAttach={saveOps}
        />
      )}
      <p className="hint">
        {ru
          ? "Сохранение меняет только текущий CS. Сервер проверяет схему, права и ссылки; публикация требует отдельного действия."
          : "Saving changes the current CS. The server checks schema, permissions and references; publishing requires a separate action."}
      </p>
    </section>
  );
}
