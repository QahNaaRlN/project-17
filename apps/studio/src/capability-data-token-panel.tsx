import { useEffect, useState } from "react";
import { useStore } from "zustand";
import type { Fields } from "@cms/manifest";
import { Dialog } from "./components.js";
import { StudioError, type Api } from "./types.js";
import type { Editor } from "./editor.js";
import { designFields } from "./fields.js";
import type { CapabilityRequest } from "./capabilities.js";
import { capabilityMessage } from "./capabilities.js";
import {
  CapabilityCard,
  DraftFields,
  codeMode,
  newArg,
  proposed,
  useCommand,
  type Arg,
} from "./capability-panel.js";

const sourceKey = /^[a-z][a-zA-Z0-9]*(\.[a-z][a-zA-Z0-9]*)+$/;
const tokenName = /^[a-z][A-Za-z0-9]{0,63}$/;
const resultTypes = ["string", "text", "number", "boolean", "link"] as const;
type Mode = "source" | "token";

/** STU-387: a literal param operand from the form value. */
function literal(field: Fields[string], raw: string): unknown {
  if (raw === "") return undefined;
  if (field.type === "number") return { lit: Number(raw) };
  if (field.type === "boolean") return { lit: raw === "true" };
  return { lit: raw };
}

/** Pending marker of a design value of the node, if any. */
function tokenMarkers(design: Record<string, unknown> | undefined): string[] {
  const out: string[] = [];
  for (const value of Object.values(design ?? {})) {
    const values =
      value && typeof value === "object" && !("raw" in value)
        ? Object.values(value as Record<string, unknown>)
        : [value];
    for (const v of values)
      if (typeof v === "string" && v.startsWith("pending:")) out.push(v.slice(8));
  }
  return out;
}

/**
 * STU-380/384/392: request a missing data source for the page or a missing
 * spacing token for the selected node; open the request of a placeholder.
 */
export function DataTokenRequestComposer({
  api,
  editor,
  rights,
}: {
  api: Api;
  editor: Editor;
  rights?: string[] | undefined;
}) {
  const state = useStore(editor.store),
    session = state.session,
    doc = state.document,
    node = doc?.nodes[state.selected ?? ""],
    ru = state.locale === "ru",
    text = (r: string, e: string) => (ru ? r : e);
  const [mode, setMode] = useState<Mode | null>(null),
    [created, setCreated] = useState<CapabilityRequest | null>(null),
    [key, setKey] = useState(""),
    [place, setPlace] = useState(""),
    [title, setTitle] = useState(""),
    [description, setDescription] = useState(""),
    [acceptance, setAcceptance] = useState(""),
    [args, setArgs] = useState<Arg[]>([]),
    [fields, setFields] = useState<{ name: string; type: string }[]>([]),
    [list, setList] = useState(true),
    [paginated, setPaginated] = useState(false),
    [values, setValues] = useState<Record<string, string>>({}),
    [property, setProperty] = useState("padding"),
    [breakpoint, setBreakpoint] = useState(""),
    [value, setValue] = useState(""),
    [localError, setLocalError] = useState(""),
    [inserted, setInserted] = useState(false);
  const command = useCommand(api, editor, async (r) => {
    setCreated(await api.capability!(r.id));
  });
  const open = !!mode || !!created;
  useEffect(() => {
    editor.formDirty("property-capability-data-token", open);
    return () => editor.formDirty("property-capability-data-token", false);
  }, [open, editor]);
  if (!api.capability || !api.capabilityCommand || !session?.record || !doc) return null;
  const record = session.record;
  const allowed =
    !!session.review.capabilityPolicy &&
    (!rights ||
      ["capability.request", "design.read", "design.compose"].every((r) => rights.includes(r)));
  const pageCode =
    allowed && doc.kind === "page" && codeMode(doc, doc.root, session.review.capabilityPolicy);
  const spacing = node ? designFields(node, session.manifest) : [];
  const nodeCode =
    allowed &&
    !!node &&
    spacing.length > 0 &&
    typeof node.pending !== "string" &&
    codeMode(doc, node.id, session.review.capabilityPolicy);
  const blocked =
    state.status !== "ready" ||
    Object.entries(state.dirtyFields).some(
      ([k, v]) => v && k !== "property-capability-data-token",
    ) ||
    session.changeset.state !== "open";
  const busy = command.pending || command.uncertain;
  const pendingSources = Object.entries(doc.dataSources ?? {}).flatMap(([k, use]) =>
    typeof (use as { pending?: string }).pending === "string"
      ? [[k, (use as { pending: string }).pending] as const]
      : [],
  );
  const pendingTokens = node ? tokenMarkers(node.design as Record<string, unknown>) : [];
  const close = () => {
    if (busy || ["saving", "uncertain"].includes(editor.store.getState().status)) return;
    setMode(null);
    setCreated(null);
    setLocalError("");
    setInserted(false);
  };
  const start = (next: Mode) => {
    setMode(next);
    setKey("");
    setPlace("");
    setTitle("");
    setDescription("");
    setAcceptance("");
    setArgs(next === "source" ? [{ ...newArg(), name: "limit", type: "number" }] : []);
    setFields([{ name: "title", type: "string" }]);
    setList(true);
    setPaginated(false);
    setValues({});
    setProperty(spacing[0] ?? "padding");
    setBreakpoint("");
    setValue("");
  };
  const params = (): Fields => proposed(args.filter((a) => a.name));
  const placement = () =>
    mode === "source"
      ? { dataSourceKey: place }
      : { nodeId: node!.id, property, ...(breakpoint ? { breakpoint } : {}) };
  const initialProps = (): Record<string, unknown> | undefined => {
    if (mode !== "source") return undefined;
    const p = params();
    return Object.fromEntries(
      Object.entries(p).flatMap(([name, field]) => {
        const v = literal(field, values[name] ?? "");
        return v === undefined ? [] : [[name, v]];
      }),
    );
  };
  const fullKey = mode === "token" ? "spacing." + key : key;
  async function insertInstance() {
    setLocalError("");
    try {
      const open = await api.openRequest!(fullKey);
      const initial = initialProps();
      await api.fulfillmentCommand!({
        name: "attach-capability-request",
        payload: {
          requestId: open.id,
          expectedRequestRevision: open.revision,
          changesetId: session!.changeset.id,
          expectedSeq: session!.changeset.seq,
          documentId: record.id,
          expectedManifestHash: session!.manifestHash,
          placement: placement(),
          ...(initial ? { initialProps: initial } : {}),
        },
        key: crypto.randomUUID(),
        reason: "Studio: join an open data source or token request",
      });
      setInserted(true);
      await editor.refresh();
    } catch (e) {
      setLocalError(e instanceof StudioError ? e.code : "CAPABILITY_REQUEST_FAILED");
    }
  }
  const resultFields = Object.fromEntries(
    fields.filter((f) => f.name).map((f) => [f.name, { type: f.type }]),
  );
  const result = list
    ? { type: "list", of: { type: "object", fields: resultFields } }
    : { type: "object", fields: resultFields };
  let paramFields: Fields = {};
  try {
    paramFields = params();
  } catch {
    paramFields = {};
  }
  const openRequest = (id: string) =>
    void api.capability!(id)
      .then(setCreated)
      .catch((cause) =>
        setLocalError(cause instanceof StudioError ? cause.code : "CAPABILITY_REQUEST_FAILED"),
      );
  return (
    <section
      className="capability-panel"
      aria-label={text("Запрос источника или токена", "Data source or token request")}
    >
      {pendingSources.map(([k, id]) => (
        <p key={k}>
          <span role="status">
            {text("Ожидает источник", "Awaiting data source")} $data.{k}
          </span>{" "}
          <button disabled={blocked || open} onClick={() => openRequest(id)}>
            {text("Открыть запрос источника", "Open data source request")}
          </button>
        </p>
      ))}
      {pendingTokens.map((id) => (
        <p key={id}>
          <span role="status">{text("Ожидает токен", "Awaiting token")}</span>{" "}
          <button disabled={blocked || open} onClick={() => openRequest(id)}>
            {text("Открыть запрос токена", "Open token request")}
          </button>
        </p>
      ))}
      {!open && localError && <p role="alert">{capabilityMessage(localError, ru)}</p>}
      {pageCode && (
        <button disabled={blocked || open} onClick={() => start("source")}>
          {text("Нужен другой источник…", "Need another data source…")}
        </button>
      )}
      {nodeCode && (
        <button disabled={blocked || open} onClick={() => start("token")}>
          {text("Нужен другой токен…", "Need another token…")}
        </button>
      )}
      {open && (
        <Dialog
          title={
            mode === "token" || created?.kind === "token"
              ? text("Недостающий токен", "Missing token")
              : text("Недостающий источник данных", "Missing data source")
          }
          onDismiss={close}
          locked={busy}
        >
          {created ? (
            <CapabilityCard {...{ api, editor }} initial={created} onClose={close} />
          ) : (
            <form
              onSubmit={(e) => {
                e.preventDefault();
                setLocalError("");
                try {
                  const initial = initialProps();
                  void command.send("create-capability-request", {
                    kind: mode === "source" ? "dataSource" : "token",
                    environment: session.config.environment,
                    expectedManifestHash: session.manifestHash,
                    changesetId: session.changeset.id,
                    expectedSeq: session.changeset.seq,
                    documentId: record.id,
                    placement: placement(),
                    ...(initial ? { initialProps: initial } : {}),
                    key: fullKey,
                    title,
                    description,
                    proposedContract:
                      mode === "source"
                        ? { params: params(), result, ...(paginated ? { paginated } : {}) }
                        : { category: "spacing", value },
                    acceptance: acceptance
                      .split("\n")
                      .map((s) => s.trim())
                      .filter(Boolean),
                  });
                } catch (cause) {
                  setLocalError(
                    cause instanceof StudioError ? cause.code : "CAPABILITY_CONTRACT_INVALID",
                  );
                }
              }}
            >
              {mode === "source" ? (
                <>
                  <label>
                    {text("Ключ на странице", "Page key")}
                    <input
                      required
                      pattern="[A-Za-z][A-Za-z0-9_]*"
                      disabled={busy}
                      value={place}
                      onChange={(e) => setPlace(e.target.value)}
                    />
                  </label>
                  <label>
                    {text("Ключ источника", "Data source key")}
                    <input
                      required
                      pattern="[a-z][a-zA-Z0-9]*(\.[a-z][a-zA-Z0-9]*)+"
                      disabled={busy}
                      value={key}
                      onChange={(e) => setKey(e.target.value)}
                    />
                  </label>
                  <p>
                    {text("Похожие источники:", "Similar data sources:")}{" "}
                    {Object.keys(session.manifest.dataSources ?? {})
                      .filter((k) => key.length >= 3 && k.split(".")[0] === key.split(".")[0])
                      .slice(0, 5)
                      .join(", ") || "—"}
                  </p>
                </>
              ) : (
                <>
                  <label>
                    {text("Свойство", "Property")}
                    <select
                      disabled={busy}
                      value={property}
                      onChange={(e) => setProperty(e.target.value)}
                    >
                      {spacing.map((p) => (
                        <option key={p}>{p}</option>
                      ))}
                    </select>
                  </label>
                  <label>
                    {text("Ширина экрана", "Breakpoint")}
                    <select
                      disabled={busy}
                      value={breakpoint}
                      onChange={(e) => setBreakpoint(e.target.value)}
                    >
                      <option value="">{text("Базовое значение", "Base value")}</option>
                      {Object.keys(session.manifest.breakpoints ?? {}).map((b) => (
                        <option key={b}>{b}</option>
                      ))}
                    </select>
                  </label>
                  <label>
                    {text("Имя токена отступа", "Spacing token name")}
                    <input
                      required
                      pattern="[a-z][A-Za-z0-9]{0,63}"
                      disabled={busy}
                      value={key}
                      onChange={(e) => setKey(e.target.value)}
                    />
                  </label>
                  <label>
                    {text("Значение, например 48px", "Value, e.g. 48px")}
                    <input
                      required
                      pattern="-?[0-9]+(\.[0-9]+)?(px|rem)"
                      disabled={busy}
                      value={value}
                      onChange={(e) => setValue(e.target.value)}
                    />
                  </label>
                  <p>
                    {text("Есть токены:", "Existing tokens:")}{" "}
                    {Object.keys(
                      (session.manifest.tokens as Record<string, object> | undefined)?.spacing ??
                        {},
                    ).join(", ") || "—"}
                  </p>
                </>
              )}
              <DraftFields
                {...{
                  title,
                  description,
                  acceptance,
                  args,
                  setTitle,
                  setDescription,
                  setAcceptance,
                  setArgs,
                  ru,
                }}
                schemas={Object.keys(session.manifest.schemas ?? {})}
                disabled={busy}
                noun="parameter"
                typed={mode === "source"}
              />
              {mode === "source" && (
                <>
                  <fieldset disabled={busy}>
                    <legend>{text("Значения параметров", "Parameter values")}</legend>
                    {Object.entries(paramFields).map(([name, field]) => (
                      <label key={name}>
                        {name}
                        <input
                          type={field.type === "number" ? "number" : "text"}
                          required={!!field.required}
                          value={values[name] ?? ""}
                          onChange={(e) => setValues({ ...values, [name]: e.target.value })}
                        />
                      </label>
                    ))}
                  </fieldset>
                  <fieldset disabled={busy}>
                    <legend>{text("Форма результата", "Result shape")}</legend>
                    <label>
                      <input
                        type="checkbox"
                        checked={list}
                        onChange={(e) => setList(e.target.checked)}
                      />
                      {text("Список элементов", "A list of items")}
                    </label>
                    <label>
                      <input
                        type="checkbox"
                        checked={paginated}
                        disabled={!list}
                        onChange={(e) => setPaginated(e.target.checked)}
                      />
                      {text("Постраничная загрузка", "Paginated")}
                    </label>
                    {fields.map((f, i) => (
                      <fieldset key={i}>
                        <legend>
                          {text("Поле результата", "Result field")} {i + 1}
                        </legend>
                        <label>
                          {text("Имя поля", "Field name")}
                          <input
                            required
                            pattern="[a-z][a-zA-Z0-9]*"
                            value={f.name}
                            onChange={(e) =>
                              setFields(
                                fields.map((x, n) =>
                                  n === i ? { ...x, name: e.target.value } : x,
                                ),
                              )
                            }
                          />
                        </label>
                        <label>
                          {text("Тип поля", "Field type")}
                          <select
                            value={f.type}
                            onChange={(e) =>
                              setFields(
                                fields.map((x, n) =>
                                  n === i ? { ...x, type: e.target.value } : x,
                                ),
                              )
                            }
                          >
                            {resultTypes.map((t) => (
                              <option key={t}>{t}</option>
                            ))}
                          </select>
                        </label>
                        <button
                          type="button"
                          onClick={() => setFields(fields.filter((_, n) => n !== i))}
                        >
                          {text("Удалить поле", "Remove field")}
                        </button>
                      </fieldset>
                    ))}
                    <button
                      type="button"
                      disabled={fields.length >= 32}
                      onClick={() => setFields([...fields, { name: "", type: "string" }])}
                    >
                      {text("Добавить поле", "Add field")}
                    </button>
                  </fieldset>
                </>
              )}
              {(localError || command.error) && (
                <p role="alert">{capabilityMessage(localError || command.error, ru)}</p>
              )}
              {command.error === "CAPABILITY_KEY_TAKEN" &&
                api.openRequest &&
                api.fulfillmentCommand &&
                !inserted && (
                  <button type="button" onClick={() => void insertInstance()}>
                    {text("Присоединить к открытому запросу", "Join the open request")}
                  </button>
                )}
              {inserted && (
                <>
                  <p role="status">
                    {text(
                      "Заглушка присоединена к открытому запросу.",
                      "The placeholder joined the open request.",
                    )}
                  </p>
                  <button type="button" onClick={close}>
                    {text("Закрыть", "Close")}
                  </button>
                </>
              )}
              <button
                disabled={
                  busy ||
                  (mode === "source" ? !sourceKey.test(key) || !place : !tokenName.test(key))
                }
              >
                {text("Сохранить черновик запроса", "Save request draft")}
              </button>
              {command.uncertain && (
                <button
                  type="button"
                  disabled={command.pending}
                  onClick={() => void command.send()}
                >
                  {text("Повторить тот же запрос", "Retry exact request")}
                </button>
              )}
              <button type="button" disabled={busy} onClick={close}>
                {text("Отмена", "Cancel")}
              </button>
            </form>
          )}
        </Dialog>
      )}
    </section>
  );
}
