import { useEffect, useMemo, useRef, useState } from "react";
import { useStore } from "zustand";
import type { DataSourceUse, IrDocument } from "@cms/ir";
import type { Type, Manifest } from "@cms/manifest";
import type { Editor } from "./editor.js";
import { layers } from "./fields.js";
import { contract, location } from "./structure.js";
import {
  compatible,
  registeredSource,
  resultFields,
  bindingFields,
  parameterFields,
  supportsLiteral,
  sourceSupported,
  safeKey,
  literal,
  sourceChange,
  dataBinding,
  repeatTree,
  repeatPlaces,
  insertRepeat,
  walkFields,
} from "./data-sources.js";
import { StudioError } from "./types.js";

type ParamMode = "omit" | "literal" | "path" | "null" | "legacy";
export function Parameter({
  name,
  schema,
  value,
  paths,
  change,
  ru,
}: {
  name: string;
  schema: Type;
  value: unknown;
  paths: ReturnType<typeof parameterFields>;
  change: (value: unknown) => void;
  ru: boolean;
}) {
  const mode: ParamMode =
    value === undefined
      ? "omit"
      : typeof value === "string"
        ? "path"
        : value && typeof value === "object" && "lit" in value
          ? value.lit === null
            ? "null"
            : supportsLiteral(schema)
              ? "literal"
              : "legacy"
          : "legacy";
  const [raw, setRaw] = useState(
    value && typeof value === "object" && "lit" in value ? String(value.lit ?? "") : "",
  );
  const [error, setError] = useState(false);
  const emitted = useRef(value);
  useEffect(() => {
    if (value !== emitted.current) {
      emitted.current = value;
      setRaw(value && typeof value === "object" && "lit" in value ? String(value.lit ?? "") : "");
      setError(false);
    }
  }, [value]);
  function update(next: unknown) {
    emitted.current = next;
    change(next);
  }
  const choices = paths.filter((f) => compatible(f.schema, schema));
  function input(raw: string) {
    setRaw(raw);
    try {
      update({ lit: literal(schema, raw) });
      setError(false);
    } catch {
      setError(true);
      update({ lit: { invalid: true } });
    }
  }
  return (
    <fieldset>
      <legend>
        {name}
        {schema.required ? " *" : ""} · {schema.type}
      </legend>
      <label>
        {ru ? "Значение параметра" : "Parameter value"} {name}
        <select
          aria-label={(ru ? "Значение параметра " : "Parameter value ") + name}
          value={mode}
          onChange={(e) => {
            setError(false);
            const mode = e.target.value;
            if (mode === "omit") update(undefined);
            else if (mode === "null") update({ lit: null });
            else if (mode === "path") update(choices[0]?.path ?? "");
            else input(raw);
          }}
        >
          <option value="omit" disabled={!!schema.required && !("default" in schema)}>
            {ru ? "Не задавать / по умолчанию" : "Omit / default"}
            {"default" in schema ? ` (${String(schema.default)})` : ""}
          </option>
          <option value="literal" disabled={!supportsLiteral(schema)}>
            {ru ? "Задать значение" : "Literal"}
          </option>
          <option value="path" disabled={!choices.length}>
            {ru ? "Поле данных" : "Data field"}
          </option>
          <option value="null" disabled={!!schema.required}>
            null
          </option>
          {mode === "legacy" && (
            <option value="legacy">
              {ru ? "Существующее выражение (чтение)" : "Existing expression (read only)"}
            </option>
          )}
        </select>
      </label>
      {mode === "path" && (
        <label>
          {ru ? "Путь параметра" : "Parameter path"} {name}
          <select
            aria-label={(ru ? "Путь параметра " : "Parameter path ") + name}
            value={String(value)}
            onChange={(e) => update(e.target.value)}
          >
            {!choices.some((p) => p.path === value) && (
              <option value={String(value)}>{String(value)}</option>
            )}
            {choices.map((f) => (
              <option key={f.path} value={f.path}>
                {f.path}
              </option>
            ))}
          </select>
        </label>
      )}
      {mode === "literal" && (
        <label>
          {ru ? "Литерал" : "Literal"} {name}
          {schema.type === "boolean" || schema.type === "enum" ? (
            <select
              aria-label={(ru ? "Литерал " : "Literal ") + name}
              value={raw}
              onChange={(e) => input(e.target.value)}
            >
              <option value="" disabled>
                {ru ? "Выберите значение" : "Choose a value"}
              </option>
              {(schema.type === "boolean" ? ["true", "false"] : (schema.values ?? [])).map((v) => (
                <option key={v}>{v}</option>
              ))}
            </select>
          ) : (
            <input
              aria-label={(ru ? "Литерал " : "Literal ") + name}
              type={schema.type === "number" ? "number" : "text"}
              value={raw}
              onChange={(e) => input(e.target.value)}
              aria-invalid={error || undefined}
            />
          )}
        </label>
      )}
      {mode === "legacy" && (
        <p>
          {ru ? "Выражение сохраняется без изменений." : "The expression is preserved unchanged."}
        </p>
      )}
      {error && (
        <p role="alert">
          {ru ? "Значение не соответствует контракту." : "The value does not match the contract."}
        </p>
      )}
    </fieldset>
  );
}

function validParams(
  use: DataSourceUse,
  source: NonNullable<Manifest["dataSources"]>[string],
  doc: IrDocument,
  manifest: Manifest,
  baseline?: DataSourceUse,
): boolean {
  const paths = parameterFields(doc, manifest);
  return Object.entries(source.params ?? {}).every(([name, schema]) => {
    const value = use.params?.[name];
    const unchanged =
      baseline?.source === use.source &&
      JSON.stringify(baseline.params?.[name]) === JSON.stringify(value);
    if (value === undefined) return !schema.required || "default" in schema;
    if (typeof value === "string")
      return unchanged || paths.some((f) => f.path === value && compatible(f.schema, schema));
    if (value && typeof value === "object" && "lit" in value) {
      if (value.lit === null) return !schema.required;
      if (!supportsLiteral(schema)) return unchanged;
      try {
        return JSON.stringify(literal(schema, String(value.lit))) === JSON.stringify(value.lit);
      } catch {
        return false;
      }
    }
    return unchanged;
  });
}

export function DataSourcesPanel({
  editor,
  rights,
}: {
  editor: Editor;
  rights?: string[] | undefined;
}) {
  const state = useStore(editor.store),
    session = state.session;
  const revision = useRef(state.historyRevision);
  if (!state.dirtyFields["property-dataSource"] && !state.dirtyFields["property-repeatSettings"])
    revision.current = state.historyRevision;
  if (!session?.record || state.document?.kind !== "page") return null;
  return (
    <DataPanel
      key={`${session.changeset.id}:${session.record.id}:${revision.current}:${session.manifestHash}`}
      {...{ editor, rights }}
    />
  );
}
function DataPanel({ editor, rights }: { editor: Editor; rights?: string[] | undefined }) {
  const state = useStore(editor.store),
    session = state.session!,
    doc = state.document!,
    manifest = session.manifest,
    target = session.record!.id,
    ru = state.locale === "ru";
  const text = (ruText: string, enText: string) => (ru ? ruText : enText);
  const [dialog, setDialog] = useState<"source" | "binding" | "repeat" | null>(null);
  const [baseKey, setBaseKey] = useState<string | null>(null),
    [key, setKey] = useState("featured"),
    [use, setUse] = useState<DataSourceUse>({ source: "", params: {} }),
    [remove, setRemove] = useState(false),
    [confirm, setConfirm] = useState(false);
  const [prop, setProp] = useState(""),
    [path, setPath] = useState(""),
    [limit, setLimit] = useState(12),
    [repeatKey, setRepeatKey] = useState(""),
    [place, setPlace] = useState("");
  const panel = useRef<HTMLFieldSetElement>(null),
    trigger = useRef<HTMLButtonElement | null>(null),
    openedSeq = useRef(session.changeset.seq);
  useEffect(() => {
    editor.formDirty("property-dataSource", dialog !== null);
    if (dialog) {
      openedSeq.current = session.changeset.seq;
      panel.current?.querySelector<HTMLElement>("input,select,button")?.focus();
    } else trigger.current?.focus();
    return () => editor.formDirty("property-dataSource", false);
  }, [dialog, editor]);
  useEffect(() => {
    if (
      dialog &&
      state.status === "ready" &&
      !state.error &&
      session.changeset.seq > openedSeq.current
    )
      close();
  }, [state.status, state.error, session.changeset.seq, dialog]);
  const selected = state.selected ? doc.nodes[state.selected] : undefined;
  const locked = layers(doc).find((n) => n.id === state.selected)?.locked;
  const securityReadOnly =
    state.status !== "ready" ||
    !session.review.canEdit ||
    session.changeset.state !== "open" ||
    session.changeset.kind !== "standard" ||
    (!!rights && !rights.includes("design.compose"));
  const rootLocked = !!layers(doc).find((node) => node.id === doc.root)?.locked;
  const readOnly =
    securityReadOnly ||
    rootLocked ||
    Object.entries(state.dirtyFields).some(([k, v]) => v && k !== "property-dataSource");
  const canReadContent = !rights || rights.includes("content.read");
  const catalogue = Object.entries(manifest.dataSources ?? {});
  const results = resultFields(doc, manifest),
    choices = selected ? bindingFields(doc, manifest, selected.id) : { fields: [], limited: false };
  const properties = selected
    ? Object.entries(contract(selected.type, manifest)?.props ?? {}).filter(
        ([name]) => selected.type !== "Repeat" || name === "items",
      )
    : [];
  const receiver = properties.find(([name]) => name === prop)?.[1];
  const paths = receiver ? choices.fields.filter((f) => compatible(f.schema, receiver)) : [];
  const source = registeredSource(manifest, use.source);
  let change: ReturnType<typeof sourceChange> | null = null;
  try {
    if (dialog === "source")
      change = sourceChange(
        doc,
        manifest,
        target,
        key,
        remove ? null : use,
        session.record!.path ?? "",
      );
  } catch {
    /* Invalid key is represented by the disabled submit. */
  }
  const list = results.fields.find(
    (f) => f.path === path && f.schema.type === "list" && f.schema.of,
  );
  const keys =
    list?.schema.type === "list" && list.schema.of
      ? walkFields("$item", { ...list.schema.of, required: true } as Type).fields.filter(
          (f) =>
            f.path !== "$item" && f.schema.required && ["string", "number"].includes(f.schema.type),
        )
      : [];
  const tree = useMemo(() => {
    try {
      return list ? repeatTree(list, limit, repeatKey, new Set(Object.keys(doc.nodes))) : null;
    } catch {
      return null;
    }
  }, [path, limit, repeatKey, doc, manifest]);
  const places = tree ? repeatPlaces(doc, manifest, tree) : [];
  const insertion =
    places.find((p) => JSON.stringify(p) === place) ??
    places.filter((p) => p.parentId === state.selected).at(-1) ??
    places.filter((p) => p.parentId === doc.root).at(-1) ??
    places[0];
  function close() {
    setDialog(null);
    setConfirm(false);
    setRemove(false);
  }
  function sourceDialog(existing?: string) {
    let key = existing ?? "featured";
    for (let i = 2; !existing && Object.hasOwn(doc.dataSources ?? {}, key); i++)
      key = "featured" + i;
    setBaseKey(existing ?? null);
    setKey(key);
    setUse(
      structuredClone(
        existing ? doc.dataSources![existing]! : { source: catalogue[0]?.[0] ?? "", params: {} },
      ),
    );
    setConfirm(false);
    setRemove(false);
    setDialog("source");
  }
  async function save() {
    if (readOnly) return;
    try {
      if (dialog === "source") {
        if (
          !change ||
          change.blocked.length ||
          (!confirm && change.affected.length) ||
          (!remove &&
            (!source ||
              !validParams(
                use,
                source,
                doc,
                manifest,
                baseKey ? doc.dataSources?.[baseKey] : undefined,
              )))
        )
          return;
        await editor.applyBindings(change.operations);
      } else if (dialog === "binding" && selected && prop) {
        if (
          locked ||
          (!remove && !paths.some((f) => f.path === path)) ||
          (selected.bindings?.[prop] !== undefined && !confirm)
        )
          return;
        await editor.applyBindings(dataBinding(target, selected, prop, remove ? null : path));
      } else if (dialog === "repeat" && tree && insertion)
        await editor.applyBindings([insertRepeat(target, insertion, tree)], tree.id);
      const fresh = editor.store.getState();
      if (fresh.status === "ready" && !fresh.error) close();
    } catch (cause) {
      editor.error(cause instanceof StudioError ? cause.code : "FIELD_INVALID");
    }
  }
  function chooseList(path: string) {
    setPath(path);
    const list = results.fields.find((f) => f.path === path);
    const id =
      list?.schema.type === "list" && list.schema.of
        ? walkFields("$item", { ...list.schema.of, required: true } as Type).fields.find(
            (f) =>
              f.path === "$item.id" &&
              f.schema.required &&
              ["string", "number"].includes(f.schema.type),
          )
        : undefined;
    setRepeatKey(id?.path ?? "");
  }
  return (
    <section className="content-bindings" aria-label={text("Источники данных", "Data sources")}>
      <h2>{text("Источники данных", "Data sources")}</h2>
      <ul>
        {Object.entries(doc.dataSources ?? {}).map(([key, use]) => (
          <li key={key}>
            <strong>{key}</strong> · {use.source}
            {registeredSource(manifest, use.source)?.paginated && (
              <p>
                {text(
                  "Первая порция; загрузка следующих порций пока не настраивается.",
                  "First page only; loading more is not configurable yet.",
                )}
              </p>
            )}
            {!registeredSource(manifest, use.source) && (
              <p role="status">
                {text(
                  "Источник отсутствует в активном контракте.",
                  "Source is absent from the active contract.",
                )}
              </p>
            )}
            <button
              disabled={readOnly || !!dialog || !safeKey(key)}
              onClick={(e) => {
                trigger.current = e.currentTarget;
                sourceDialog(key);
              }}
            >
              {text("Настроить", "Configure")} {key}
            </button>
          </li>
        ))}
      </ul>
      <button
        disabled={
          readOnly ||
          !!dialog ||
          !catalogue.length ||
          Object.keys(doc.dataSources ?? {}).length >= 64
        }
        onClick={(e) => {
          trigger.current = e.currentTarget;
          sourceDialog();
        }}
      >
        {text("Подключить источник", "Connect source")}
      </button>
      {!catalogue.length && (
        <p>
          {text(
            "В приложении нет зарегистрированных источников.",
            "The application has no registered sources.",
          )}
        </p>
      )}
      <details>
        <summary>{text("Каталог и результаты", "Catalogue and results")}</summary>
        <ul>
          {catalogue.map(([name, source]) => (
            <li key={name}>
              {name} · {source.description} · {source.result.type}
              {source.paginated && <p>{text("Первая порция", "First page only")}</p>}
              {!sourceSupported(source) && (
                <p>
                  {text(
                    "Обязательный параметр требует неподдерживаемого редактора.",
                    "A required parameter needs an unsupported editor.",
                  )}
                </p>
              )}
            </li>
          ))}
        </ul>
        <ul>
          {results.fields.map((f) => (
            <li key={f.path}>
              <code>{f.path}</code> · {f.schema.type}
            </li>
          ))}
        </ul>
      </details>
      <button
        disabled={readOnly || !!dialog || !selected || locked}
        onClick={(e) => {
          trigger.current = e.currentTarget;
          setProp(properties[0]?.[0] ?? "");
          setPath("");
          setRemove(false);
          setConfirm(false);
          setDialog("binding");
        }}
      >
        {text("Привязать данные к свойству", "Bind data to property")}
      </button>
      <button
        disabled={
          readOnly ||
          !!dialog ||
          !results.fields.some((f) => f.schema.type === "list" && f.schema.of)
        }
        onClick={(e) => {
          trigger.current = e.currentTarget;
          chooseList(results.fields.find((f) => f.schema.type === "list" && f.schema.of)!.path);
          setDialog("repeat");
        }}
      >
        {text("Добавить повторение", "Add repeat")}
      </button>
      {(results.limited || choices.limited) && (
        <p role="status">
          {text(
            "Показано не больше 200 путей, глубина до 8.",
            "Showing at most 200 paths, up to depth 8.",
          )}
        </p>
      )}
      {selected?.type === "Repeat" && (
        <RepeatSettings
          key={selected.id}
          {...{ editor, doc, manifest, target, selected, securityReadOnly, locked, ru }}
        />
      )}
      {dialog && (
        <fieldset
          ref={panel}
          disabled={readOnly}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.preventDefault();
              close();
            }
          }}
        >
          <legend>
            {dialog === "source"
              ? text("Подключение источника", "Source connection")
              : dialog === "binding"
                ? text("Привязка данных", "Data binding")
                : text("Повторение списка", "Repeat a list")}
          </legend>
          {dialog === "source" && (
            <>
              <label>
                {text("Ключ подключения", "Connection key")}
                <input
                  value={key}
                  readOnly={baseKey !== null}
                  onChange={(e) => {
                    setKey(e.target.value);
                    setConfirm(false);
                  }}
                />
              </label>
              <label>
                {text("Источник", "Source")}
                <select
                  aria-label={text("Источник", "Source")}
                  value={use.source}
                  onChange={(e) => {
                    setUse({ source: e.target.value, params: {} });
                    setConfirm(false);
                  }}
                >
                  {!source && (
                    <option value={use.source}>
                      {use.source || text("Выберите источник", "Choose source")}
                    </option>
                  )}
                  {catalogue.map(([name, source]) => (
                    <option key={name} value={name} disabled={!sourceSupported(source)}>
                      {name}
                    </option>
                  ))}
                </select>
              </label>
              {source && (
                <>
                  <p>{source.description}</p>
                  {source.paginated && (
                    <p>
                      {text(
                        "Отображается только первая порция.",
                        "Only the first page is displayed.",
                      )}
                    </p>
                  )}
                  {Object.entries(source.params ?? {}).map(([name, schema]) => (
                    <Parameter
                      key={use.source + ":" + name}
                      {...{ name, schema, ru }}
                      value={use.params?.[name]}
                      paths={parameterFields(doc, manifest).filter(
                        (p) => canReadContent || !p.path.startsWith("$content"),
                      )}
                      change={(value) => {
                        setUse((use) => {
                          const params = { ...use.params };
                          if (value === undefined) delete params[name];
                          else params[name] = value as NonNullable<DataSourceUse["params"]>[string];
                          return { ...use, params };
                        });
                        setConfirm(false);
                      }}
                    />
                  ))}
                </>
              )}
              {baseKey && (
                <label>
                  <input
                    type="checkbox"
                    checked={remove}
                    onChange={(e) => {
                      setRemove(e.target.checked);
                      setConfirm(false);
                    }}
                  />
                  {text("Удалить подключение", "Remove connection")}
                </label>
              )}
              {change && change.affected.length > 0 && (
                <>
                  <p>
                    {text(
                      "Затронутые привязки; несовместимые будут сняты:",
                      "Affected bindings; incompatible bindings will be cleared:",
                    )}
                  </p>
                  <ul>
                    {change.affected.map((pointer) => (
                      <li key={pointer}>
                        <code>{pointer}</code>
                      </li>
                    ))}
                  </ul>
                  <label>
                    <input
                      type="checkbox"
                      checked={confirm}
                      onChange={(e) => setConfirm(e.target.checked)}
                    />
                    {text("Подтверждаю изменение зависимостей", "Confirm dependency changes")}
                  </label>
                </>
              )}
              {!!change?.blocked.length && (
                <div role="alert">
                  {text(
                    "Сложные зависимости требуют отдельной правки:",
                    "Complex dependencies require a separate edit:",
                  )}
                  <ul>
                    {change.blocked.map((pointer, i) => (
                      <li key={i}>
                        <code>{pointer}</code>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </>
          )}
          {dialog === "binding" && (
            <>
              <label>
                {text("Свойство блока", "Block property")}
                <select
                  aria-label={text("Свойство блока", "Block property")}
                  value={prop}
                  onChange={(e) => {
                    setProp(e.target.value);
                    setPath("");
                    setConfirm(false);
                  }}
                >
                  {properties.map(([name, type]) => (
                    <option key={name} value={name}>
                      {name} · {type.type}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                {text("Поле результата или элемента", "Result or item field")}
                <select
                  aria-label={text("Поле результата или элемента", "Result or item field")}
                  value={path}
                  onChange={(e) => setPath(e.target.value)}
                >
                  <option value="">{text("Выберите поле", "Choose a field")}</option>
                  {paths.map((f) => (
                    <option key={f.path} value={f.path}>
                      {f.path} · {f.schema.type}
                    </option>
                  ))}
                </select>
              </label>
              {selected?.bindings?.[prop] !== undefined && (
                <>
                  <p>
                    {text(
                      "Существующая привязка будет заменена или снята явно.",
                      "The existing binding will be explicitly replaced or cleared.",
                    )}
                  </p>
                  <label>
                    <input
                      type="checkbox"
                      checked={remove}
                      onChange={(e) => setRemove(e.target.checked)}
                    />
                    {text("Снять привязку", "Clear binding")}
                  </label>
                  <label>
                    <input
                      type="checkbox"
                      checked={confirm}
                      onChange={(e) => setConfirm(e.target.checked)}
                    />
                    {text("Подтверждаю замену или снятие", "Confirm replacement or clearing")}
                  </label>
                </>
              )}
            </>
          )}
          {dialog === "repeat" && (
            <>
              <label>
                {text("Список для повторения", "Repeat list")}
                <select
                  aria-label={text("Список для повторения", "Repeat list")}
                  value={path}
                  onChange={(e) => chooseList(e.target.value)}
                >
                  {results.fields
                    .filter((f) => f.schema.type === "list" && f.schema.of)
                    .map((f) => (
                      <option key={f.path}>{f.path}</option>
                    ))}
                </select>
              </label>
              <label>
                {text("Предел элементов", "Item limit")}
                <input
                  type="number"
                  min={1}
                  max={200}
                  value={limit}
                  onChange={(e) => setLimit(Number(e.target.value))}
                />
              </label>
              <label>
                {text("Ключ элемента", "Item key")}
                <select
                  aria-label={text("Ключ элемента", "Item key")}
                  value={repeatKey}
                  onChange={(e) => setRepeatKey(e.target.value)}
                >
                  <option value="">
                    {text(
                      "Индекс (нестабилен при перестановке)",
                      "Index (unstable when reordered)",
                    )}
                  </option>
                  {keys.map((f) => (
                    <option key={f.path}>{f.path}</option>
                  ))}
                </select>
              </label>
              {!repeatKey && (
                <p role="status">
                  {text(
                    "Индекс меняет идентичность карточек при перестановке списка.",
                    "Index keys change card identity when the list is reordered.",
                  )}
                </p>
              )}
              <label>
                {text("Место вставки повторения", "Repeat destination")}
                <select
                  aria-label={text("Место вставки повторения", "Repeat destination")}
                  value={insertion ? JSON.stringify(insertion) : ""}
                  onChange={(e) => setPlace(e.target.value)}
                >
                  {places.map((p) => (
                    <option key={JSON.stringify(p)} value={JSON.stringify(p)}>
                      {doc.nodes[p.parentId]?.name ?? doc.nodes[p.parentId]?.type} ·{" "}
                      {p.slot || text("Дочерние элементы", "Children")} · {p.index + 1}
                    </option>
                  ))}
                </select>
              </label>
              <p>
                {text(
                  "Добавьте блоки в шаблон и слот empty через дерево. Создание вложенных Repeat пока недоступно.",
                  "Add blocks to the template and empty slot using the tree. Creating nested Repeat is not supported yet.",
                )}
              </p>
            </>
          )}
          <button
            disabled={
              dialog === "source"
                ? !change ||
                  !!change.blocked.length ||
                  (!!change.affected.length && !confirm) ||
                  (!remove &&
                    (!source ||
                      !validParams(
                        use,
                        source,
                        doc,
                        manifest,
                        baseKey ? doc.dataSources?.[baseKey] : undefined,
                      ))) ||
                  (!baseKey && Object.hasOwn(doc.dataSources ?? {}, key))
                : dialog === "binding"
                  ? !prop ||
                    (!remove && !paths.some((f) => f.path === path)) ||
                    (selected?.bindings?.[prop] !== undefined && !confirm)
                  : !tree || !insertion
            }
            onClick={() => void save()}
          >
            {text("Сохранить данные", "Save data")}
          </button>
        </fieldset>
      )}
      {dialog && (
        <button disabled={state.status === "saving"} onClick={close}>
          {text("Отмена", "Cancel")}
        </button>
      )}
    </section>
  );
}

function RepeatSettings({
  editor,
  doc,
  manifest,
  target,
  selected,
  securityReadOnly,
  locked,
  ru,
}: {
  editor: Editor;
  doc: IrDocument;
  manifest: Manifest;
  target: string;
  selected: IrDocument["nodes"][string];
  securityReadOnly: boolean;
  locked?: boolean | undefined;
  ru: boolean;
}) {
  const props = (selected.props ?? {}) as Record<string, unknown>;
  const [limit, setLimit] = useState(Number(props.limit ?? 200));
  const list = resultFields(doc, manifest).fields.find((f) => f.path === selected.bindings?.items);
  const keys =
    list?.schema.type === "list" && list.schema.of
      ? walkFields("$item", { ...list.schema.of, required: true } as Type).fields.filter(
          (f) =>
            f.path !== "$item" && f.schema.required && ["string", "number"].includes(f.schema.type),
        )
      : [];
  const [key, setKey] = useState(String(props.key ?? ""));
  const dirty = limit !== Number(props.limit ?? 200) || key !== String(props.key ?? "");
  useEffect(() => {
    editor.formDirty("property-repeatSettings", dirty);
    return () => editor.formDirty("property-repeatSettings", false);
  }, [dirty, editor]);
  const state = useStore(editor.store);
  const ownDirtyOnly = Object.entries(state.dirtyFields).every(
    ([key, value]) => !value || key === "property-repeatSettings",
  );
  const legacy =
    Object.hasOwn(selected.bindings ?? {}, "key") ||
    Object.hasOwn(selected.bindings ?? {}, "limit");
  const disabled = !!locked || securityReadOnly || !ownDirtyOnly || legacy;
  const place = location(doc, selected.id);
  return (
    <>
      <fieldset disabled={disabled} key={selected.id}>
        <legend>{ru ? "Настройки повторения" : "Repeat settings"}</legend>
        {legacy && (
          <p>
            {ru
              ? "Существующие динамические настройки доступны только для чтения."
              : "Existing dynamic settings are read only."}
          </p>
        )}
        <p>
          {ru
            ? "Правка шаблона меняет все карточки. Empty доступен в дереве."
            : "Template edits update every card. Empty is available in the tree."}
        </p>
        <label>
          {ru ? "Лимит повторения" : "Repeat limit"}
          <input
            type="number"
            min={1}
            max={200}
            value={limit}
            onChange={(e) => setLimit(Number(e.target.value))}
          />
        </label>
        <label>
          {ru ? "Ключ повторения" : "Repeat key"}
          <select
            aria-label={ru ? "Ключ повторения" : "Repeat key"}
            value={key}
            onChange={(e) => setKey(e.target.value)}
          >
            <option value="">{ru ? "Индекс (нестабилен)" : "Index (unstable)"}</option>
            {!keys.some((f) => f.path === key) && key && <option value={key}>{key}</option>}
            {keys.map((f) => (
              <option key={f.path}>{f.path}</option>
            ))}
          </select>
        </label>
        <button
          disabled={
            !dirty ||
            !Number.isInteger(limit) ||
            limit < 1 ||
            limit > 200 ||
            (key !== "" && key !== String(props.key ?? "") && !keys.some((f) => f.path === key))
          }
          onClick={async () => {
            await editor.applyBindings([
              {
                type: "node.setProps",
                target,
                payload: {
                  nodeId: selected.id,
                  set: { limit, ...(key ? { key } : {}) },
                  ...(!key ? { unset: ["key"] } : {}),
                },
              },
            ]);
            if (!editor.store.getState().error) editor.formDirty("property-repeatSettings", false);
          }}
        >
          {ru ? "Сохранить повторение" : "Save repeat"}
        </button>
        {place && <p>{selected.bindings?.items as string}</p>}
      </fieldset>
      <button
        disabled={!dirty || state.status === "saving"}
        onClick={() => {
          setLimit(Number(props.limit ?? 200));
          setKey(String(props.key ?? ""));
        }}
      >
        {ru ? "Сбросить" : "Reset"}
      </button>
    </>
  );
}
