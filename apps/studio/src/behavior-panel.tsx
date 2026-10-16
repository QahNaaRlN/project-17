import { useEffect, useRef, useState } from "react";
import { useStore } from "zustand";
import type { ActionCall, IrDocument } from "@cms/ir";
import type { Type } from "@cms/manifest";
import type { Editor } from "./editor.js";
import { Parameter } from "./data-sources-panel.js";
import { supportsLiteral, compatible, type DataField } from "./data-sources.js";
import { contract } from "./structure.js";
import { layers } from "./fields.js";
import {
  actionFields,
  behaviorFields,
  builtinBehavior,
  editableChain,
  editablePredicate,
  behaviorOperation,
  conditionOperation,
  conditionLocked,
  predicateStats,
  scalarCondition,
} from "./behavior.js";

function Argument({
  name,
  schema,
  value,
  fields,
  doc,
  ru,
  change,
}: {
  name: string;
  schema: Type;
  value: unknown;
  fields: DataField[];
  doc: IrDocument;
  ru: boolean;
  change: (value: unknown) => void;
}) {
  if (schema.type !== "link" && schema.type !== "nodeRef")
    return <Parameter {...{ name, schema, value, ru, change }} paths={fields} />;
  const operand = value as { lit?: unknown } | undefined;
  const link = operand?.lit as { kind?: string; url?: string; node?: string } | undefined;
  const mode =
    value === undefined
      ? "omit"
      : typeof value === "string"
        ? "path"
        : operand?.lit === null
          ? "null"
          : schema.type === "nodeRef"
            ? "target"
            : (link?.kind ?? "url");
  const choices = fields.filter((f) => compatible(f.schema, schema));
  const targets = Object.values(doc.nodes).filter(
    (n) => schema.type !== "nodeRef" || !schema.nodeType || n.type === schema.nodeType,
  );
  const choose = (mode: string) => {
    if (mode === "omit") change(undefined);
    else if (mode === "null") change({ lit: null });
    else if (mode === "path") change(choices[0]?.path ?? "");
    else if (mode === "url") change({ lit: { kind: "url", url: "" } });
    else
      change({
        lit:
          mode === "target"
            ? (targets[0]?.id ?? "")
            : { kind: "anchor", node: targets[0]?.id ?? "" },
      });
  };
  return (
    <fieldset>
      <legend>
        {name}
        {schema.required ? " *" : ""} · {schema.type}
      </legend>
      <label>
        {ru ? "Источник аргумента" : "Argument source"} {name}
        <select
          aria-label={(ru ? "Источник аргумента " : "Argument source ") + name}
          value={mode}
          onChange={(e) => choose(e.target.value)}
        >
          <option value="omit" disabled={!!schema.required && !("default" in schema)}>
            {ru ? "Не задавать / по умолчанию" : "Omit / default"}
          </option>
          <option value="null" disabled={!!schema.required}>
            null
          </option>
          <option value="path" disabled={!choices.length}>
            {ru ? "Поле данных" : "Data field"}
          </option>
          {schema.type === "link" ? (
            <>
              <option value="url">URL</option>
              <option value="anchor">{ru ? "Якорь" : "Anchor"}</option>
            </>
          ) : (
            <option value="target">{ru ? "Узел" : "Node"}</option>
          )}
        </select>
      </label>
      {mode === "url" && (
        <label>
          URL {name}
          <input
            value={link?.url ?? ""}
            onChange={(e) => change({ lit: { kind: "url", url: e.target.value } })}
          />
        </label>
      )}
      {(mode === "anchor" || mode === "target") && (
        <>
          <label>
            {ru ? "Целевой узел" : "Target node"} {name}
            <select
              aria-label={(ru ? "Целевой узел " : "Target node ") + name}
              value={mode === "anchor" ? (link?.node ?? "") : String(operand?.lit ?? "")}
              onChange={(e) =>
                change({
                  lit:
                    mode === "anchor" ? { kind: "anchor", node: e.target.value } : e.target.value,
                })
              }
            >
              {targets.map((n) => (
                <option key={n.id} value={n.id}>
                  {n.name ?? n.type} · {n.id}
                  {n.when ? (ru ? " · условный" : " · conditional") : ""}
                </option>
              ))}
            </select>
          </label>
          <p>
            {ru
              ? "Условно скрытая цель может отсутствовать в предпросмотре."
              : "A conditional target may be absent in preview."}
          </p>
        </>
      )}
      {mode === "path" && (
        <label>
          {ru ? "Поле аргумента" : "Argument field"} {name}
          <select
            aria-label={(ru ? "Поле аргумента " : "Argument field ") + name}
            value={String(value)}
            onChange={(e) => change(e.target.value)}
          >
            {choices.map((f) => (
              <option key={f.path}>{f.path}</option>
            ))}
          </select>
        </label>
      )}
    </fieldset>
  );
}

type Condition = { op: string; arg?: unknown; args?: unknown[] };
const ops = ["exists", "empty", "eq", "neq", "gt", "gte", "lt", "lte", "in", "and", "or", "not"];
function seed(fields: DataField[]): Condition {
  return { op: "exists", arg: fields[0]?.path ?? "" };
}
function PredicateForm({
  value,
  fields,
  doc,
  ru,
  change,
  depth = 0,
}: {
  value: Condition;
  fields: DataField[];
  doc: IrDocument;
  ru: boolean;
  change: (value: Condition) => void;
  depth?: number;
}) {
  const group = value.op === "and" || value.op === "or";
  const paths = fields.filter(
    (f) =>
      ["exists", "empty"].includes(value.op) ||
      (scalarCondition(f.schema) &&
        (!["gt", "gte", "lt", "lte"].includes(value.op) || f.schema.type === "number")),
  );
  const left = ["exists", "empty"].includes(value.op) ? value.arg : value.args?.[0];
  const schema = paths.find((f) => f.path === left)?.schema;
  const scalar = schema ? ({ ...schema, required: true } as Type) : undefined;
  const right = value.args?.[1];
  const list = (right as { lit?: unknown[] } | undefined)?.lit ?? [];
  function operation(op: string) {
    if (op === "and" || op === "or") change({ op, args: [value] });
    else if (op === "not") change({ op, arg: value });
    else if (op === "exists" || op === "empty") change({ op, arg: paths[0]?.path ?? "" });
    else {
      const candidates = fields.filter(
        (f) =>
          scalarCondition(f.schema) &&
          (!["gt", "gte", "lt", "lte"].includes(op) || f.schema.type === "number"),
      );
      change({ op, args: [candidates[0]?.path ?? "", op === "in" ? { lit: [] } : undefined] });
    }
  }
  return (
    <fieldset className="condition-form">
      <legend>
        {ru ? "Условие" : "Condition"} · {depth + 1}
      </legend>
      <label>
        {ru ? "Оператор" : "Operator"}
        <select
          aria-label={ru ? "Оператор" : "Operator"}
          value={value.op}
          onChange={(e) => operation(e.target.value)}
        >
          {ops.map((op) => (
            <option key={op} disabled={depth >= 8 && ["not", "and", "or"].includes(op)}>
              {op}
            </option>
          ))}
        </select>
      </label>
      {group ? (
        <>
          {(value.args ?? []).map((v, i) => (
            <div key={i}>
              <PredicateForm
                value={v as Condition}
                {...{ fields, doc, ru }}
                depth={depth + 1}
                change={(next) =>
                  change({ ...value, args: value.args!.map((old, at) => (at === i ? next : old)) })
                }
              />
              <button
                type="button"
                disabled={value.args!.length <= 1}
                onClick={() => change({ ...value, args: value.args!.filter((_, at) => at !== i) })}
              >
                {ru ? "Удалить условие" : "Remove condition"} {i + 1}
              </button>
            </div>
          ))}
          <button
            type="button"
            disabled={(value.args?.length ?? 0) >= 16 || depth >= 8}
            onClick={() => change({ ...value, args: [...value.args!, seed(fields)] })}
          >
            {ru ? "Добавить условие" : "Add condition"}
          </button>
        </>
      ) : value.op === "not" ? (
        <PredicateForm
          value={value.arg as Condition}
          {...{ fields, doc, ru }}
          depth={depth + 1}
          change={(arg) => change({ ...value, arg })}
        />
      ) : (
        <>
          <label>
            {ru ? "Поле условия" : "Condition field"}
            <select
              aria-label={ru ? "Поле условия" : "Condition field"}
              value={String(left ?? "")}
              onChange={(e) =>
                change(
                  ["exists", "empty"].includes(value.op)
                    ? { ...value, arg: e.target.value }
                    : {
                        ...value,
                        args: [e.target.value, value.op === "in" ? { lit: [] } : undefined],
                      },
                )
              }
            >
              {!paths.some((f) => f.path === left) && (
                <option value="">{ru ? "Выберите поле" : "Choose a field"}</option>
              )}
              {paths.map((f) => (
                <option key={f.path}>{f.path}</option>
              ))}
            </select>
          </label>
          {scalar &&
            !["exists", "empty"].includes(value.op) &&
            (value.op === "in" ? (
              <>
                {list.map((v, i) => (
                  <div key={i}>
                    <Parameter
                      name={String(i + 1)}
                      schema={scalar}
                      value={{ lit: v }}
                      paths={[]}
                      ru={ru}
                      change={(next) =>
                        change({
                          ...value,
                          args: [
                            left,
                            {
                              lit: list.map((old, at) =>
                                at === i ? (next as { lit?: unknown })?.lit : old,
                              ),
                            },
                          ],
                        })
                      }
                    />
                    <button
                      type="button"
                      onClick={() =>
                        change({
                          ...value,
                          args: [left, { lit: list.filter((_, at) => at !== i) }],
                        })
                      }
                    >
                      {ru ? "Удалить значение" : "Remove value"} {i + 1}
                    </button>
                  </div>
                ))}
                <button
                  type="button"
                  disabled={list.length >= 16}
                  onClick={() => change({ ...value, args: [left, { lit: [...list, undefined] }] })}
                >
                  {ru ? "Добавить значение" : "Add value"}
                </button>
              </>
            ) : (
              <Parameter
                name={ru ? "Сравнение" : "Comparison"}
                schema={{ ...scalar, required: !["eq", "neq"].includes(value.op) } as Type}
                value={right}
                paths={fields.filter((f) => scalarCondition(f.schema))}
                ru={ru}
                change={(next) => change({ ...value, args: [left, next] })}
              />
            ))}
        </>
      )}
    </fieldset>
  );
}

export function BehaviorPanel({
  editor,
  rights,
}: {
  editor: Editor;
  rights?: string[] | undefined;
}) {
  const state = useStore(editor.store);
  const revision = useRef(state.historyRevision);
  if (!state.dirtyFields["property-behavior"]) revision.current = state.historyRevision;
  if (!state.session?.record || state.document?.kind !== "page" || !state.selected) return null;
  return (
    <Panel
      key={[
        state.session.changeset.id,
        state.session.record.id,
        state.selected,
        state.session.manifestHash,
        revision.current,
      ].join(":")}
      {...{ editor, rights }}
    />
  );
}
function Panel({ editor, rights }: { editor: Editor; rights?: string[] | undefined }) {
  const state = useStore(editor.store),
    session = state.session!,
    doc = state.document!;
  const node = doc.nodes[state.selected!]!,
    manifest = session.manifest,
    ru = state.locale === "ru";
  const text = (r: string, e: string) => (ru ? r : e);
  const [dialog, setDialog] = useState<"behavior" | "condition" | null>(null);
  const [event, setEvent] = useState(""),
    [chain, setChain] = useState<ActionCall[]>([]);
  const [when, setWhen] = useState<Condition | null>(null),
    [legacy, setLegacy] = useState(false);
  const [confirm, setConfirm] = useState(false),
    [action, setAction] = useState("");
  const openedSeq = useRef(session.changeset.seq),
    form = useRef<HTMLFormElement>(null);
  const stepKeys = useRef<number[]>([]),
    nextStep = useRef(0);
  const trigger = useRef<HTMLButtonElement | null>(null);
  const dirty = dialog !== null;
  useEffect(() => {
    editor.formDirty("property-behavior", dirty);
    if (dirty) form.current?.querySelector<HTMLElement>("select,input,button")?.focus();
    else trigger.current?.focus();
    return () => editor.formDirty("property-behavior", false);
  }, [dirty, editor]);
  useEffect(() => {
    if (
      dialog &&
      state.status === "ready" &&
      !state.error &&
      session.changeset.seq > openedSeq.current
    )
      setDialog(null);
  }, [dialog, state.status, state.error, session.changeset.seq]);
  const paths = behaviorFields(doc, manifest, node.id, session.record!.path ?? "");
  const fields = paths.fields;
  const commonBlocked =
    state.status !== "ready" ||
    session.changeset.state !== "open" ||
    session.changeset.kind !== "standard" ||
    Object.entries(state.dirtyFields).some(([k, v]) => v && k !== "property-behavior");
  const manage = rights?.includes("design.zones.manage") === true;
  const contentBlocked = !!doc.content && !!rights && !rights.includes("content.read");
  const behaviorBlocked =
    commonBlocked ||
    contentBlocked ||
    session.review.canEditBehavior !== true ||
    (!!rights && !rights.includes("behavior.use")) ||
    (!manage && !!layers(doc).find((n) => n.id === node.id)?.locked);
  const conditionBlocked =
    commonBlocked ||
    contentBlocked ||
    session.review.canEditCondition !== true ||
    (!!rights && !rights.includes("design.compose")) ||
    (!manage && conditionLocked(doc, node.id));
  const declared = Object.keys(contract(node.type, manifest)?.events ?? {});
  const events = [...new Set([...declared, ...Object.keys(node.on ?? {})])];
  const catalog = [...Object.keys(builtinBehavior), ...Object.keys(manifest.actions ?? {})];
  const available = (name: string) =>
    Object.values(actionFields(manifest, name) ?? {}).every(
      (schema) =>
        !schema.required ||
        "default" in schema ||
        supportsLiteral(schema) ||
        ["link", "nodeRef"].includes(schema.type) ||
        fields.some((f) => compatible(f.schema, schema)),
    );
  function open(
    kind: "behavior" | "condition",
    button: HTMLButtonElement,
    selectedEvent = declared[0] ?? "",
  ) {
    trigger.current = button;
    openedSeq.current = session.changeset.seq;
    setDialog(kind);
    setConfirm(false);
    if (kind === "behavior") {
      const current = node.on?.[selectedEvent];
      setEvent(selectedEvent);
      setLegacy(
        current !== undefined &&
          (!declared.includes(selectedEvent) || !editableChain(current, manifest, fields, doc)),
      );
      setChain(
        current === undefined ? [] : structuredClone(Array.isArray(current) ? current : [current]),
      );
      stepKeys.current = (
        current === undefined ? [] : Array.isArray(current) ? current : [current]
      ).map(() => ++nextStep.current);
      setAction(catalog.find(available) ?? "");
    } else {
      setLegacy(!!node.when && !editablePredicate(node.when, fields, doc));
      setWhen(node.when ? (structuredClone(node.when) as Condition) : seed(fields));
    }
  }
  const valid =
    dialog === "behavior"
      ? chain.length === 0 || editableChain(chain, manifest, fields, doc)
      : when === null || editablePredicate(when, fields, doc);
  const blocked = dialog === "behavior" ? behaviorBlocked : conditionBlocked;
  const stats = predicateStats(when);
  async function save(remove = false) {
    if (blocked || (legacy && !confirm) || (!remove && !valid)) return;
    if (session.changeset.seq !== openedSeq.current) {
      editor.store.setState({ error: "CHANGESET_SEQ_CONFLICT" });
      return;
    }
    await editor.applyBindings([
      dialog === "behavior"
        ? behaviorOperation(session.record!.id, node.id, event, remove ? null : chain)
        : conditionOperation(session.record!.id, node.id, remove ? null : when),
    ]);
  }
  const close = () => {
    setDialog(null);
    editor.store.setState({ error: null });
  };
  return (
    <section
      className="behavior-panel"
      aria-label={text("Поведение и видимость", "Behavior and visibility")}
    >
      <h2>{text("Поведение", "Behavior")}</h2>
      {!events.length && (
        <p>{text("У этого узла нет объявленных событий.", "This node has no declared events.")}</p>
      )}
      {events.map((name) => (
        <div key={name}>
          <span>
            {name}
            {!declared.includes(name)
              ? text(" · событие отсутствует в контракте", " · event is missing from the contract")
              : ""}
          </span>
          <button
            disabled={dirty || behaviorBlocked}
            onClick={(e) => open("behavior", e.currentTarget, name)}
          >
            {text("Настроить событие", "Configure event")} {name}
          </button>
        </div>
      ))}
      <h2>{text("Условие показа", "Visibility condition")}</h2>
      <p>
        {node.when
          ? text(
              "Показывать по условию. Скрытый узел доступен в дереве.",
              "Shown conditionally. Hidden nodes remain in the tree.",
            )
          : text("Показывать всегда.", "Always shown.")}
      </p>
      <button
        disabled={dirty || conditionBlocked}
        onClick={(e) => open("condition", e.currentTarget)}
      >
        {text("Настроить условие", "Configure condition")}
      </button>
      {dialog && (
        <form
          ref={form}
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape" && state.status !== "saving") {
              e.preventDefault();
              close();
            }
          }}
        >
          <h3>
            {dialog === "behavior"
              ? text("Событие", "Event") + " " + event
              : text("Условие показа", "Visibility condition")}
          </h3>
          {legacy ? (
            <div>
              <p>
                {text(
                  "Существующее выражение доступно только для чтения: неподдержанный контракт, аргумент, область или предел. Сравнения дат поддерживают только eq/neq/in. Оно сохраняется без изменений.",
                  "The existing expression is read only: unsupported contract, operand, scope or limit. Dates support only eq/neq/in. It is preserved unchanged.",
                )}
              </p>
              <pre>
                {JSON.stringify(dialog === "behavior" ? node.on?.[event] : node.when, null, 2)}
              </pre>
              <label>
                <input
                  type="checkbox"
                  checked={confirm}
                  onChange={(e) => setConfirm(e.target.checked)}
                />
                {text(
                  "Подтверждаю замену или удаление всего выражения",
                  "Confirm replacing or deleting the entire expression",
                )}
              </label>
              <button
                type="button"
                disabled={
                  blocked || !confirm || (dialog === "behavior" && !declared.includes(event))
                }
                onClick={() => {
                  setLegacy(false);
                  setChain([]);
                  stepKeys.current = [];
                  setWhen(seed(fields));
                }}
              >
                {text("Заменить выражение", "Replace expression")}
              </button>
            </div>
          ) : (
            <fieldset disabled={blocked}>
              {dialog === "behavior" ? (
                <>
                  <ol>
                    {chain.map((call, i) => (
                      <li key={stepKeys.current[i]}>
                        <strong>
                          {i + 1}. {call.action}
                        </strong>
                        {manifest.actions?.[call.action]?.description && (
                          <p>{manifest.actions[call.action]!.description}</p>
                        )}
                        {Object.entries(actionFields(manifest, call.action) ?? {}).map(
                          ([name, schema]) => (
                            <Argument
                              key={name}
                              {...{ name, schema, doc, fields, ru }}
                              value={call.args?.[name]}
                              change={(v) =>
                                setChain(
                                  chain.map((old, at) => {
                                    if (at !== i) return old;
                                    const args = { ...old.args };
                                    if (v === undefined) delete args[name];
                                    else args[name] = v as never;
                                    return { ...old, args };
                                  }),
                                )
                              }
                            />
                          ),
                        )}
                        <button
                          type="button"
                          disabled={i === 0}
                          onClick={() => {
                            const next = [...chain];
                            [next[i - 1], next[i]] = [next[i]!, next[i - 1]!];
                            [stepKeys.current[i - 1], stepKeys.current[i]] = [
                              stepKeys.current[i]!,
                              stepKeys.current[i - 1]!,
                            ];
                            setChain(next);
                          }}
                        >
                          {text("Выше", "Move up")} {i + 1}
                        </button>
                        <button
                          type="button"
                          disabled={i === chain.length - 1}
                          onClick={() => {
                            const next = [...chain];
                            [next[i], next[i + 1]] = [next[i + 1]!, next[i]!];
                            [stepKeys.current[i], stepKeys.current[i + 1]] = [
                              stepKeys.current[i + 1]!,
                              stepKeys.current[i]!,
                            ];
                            setChain(next);
                          }}
                        >
                          {text("Ниже", "Move down")} {i + 1}
                        </button>
                        <button
                          type="button"
                          onClick={() => {
                            stepKeys.current.splice(i, 1);
                            setChain(chain.filter((_, at) => at !== i));
                          }}
                        >
                          {text("Удалить действие", "Remove action")} {i + 1}
                        </button>
                      </li>
                    ))}
                  </ol>
                  <label>
                    {text("Действие", "Action")}
                    <select
                      aria-label={text("Действие", "Action")}
                      value={action}
                      onChange={(e) => setAction(e.target.value)}
                    >
                      <option value="">{text("Выберите действие", "Choose an action")}</option>
                      {catalog.map((name) => (
                        <option key={name} disabled={!available(name)} value={name}>
                          {name}
                          {!available(name)
                            ? text(
                                " · нет формы или совместимого поля",
                                " · no form or compatible field",
                              )
                            : ""}
                        </option>
                      ))}
                    </select>
                  </label>
                  <button
                    type="button"
                    disabled={chain.length >= 8 || !action || !available(action)}
                    onClick={() => {
                      stepKeys.current.push(++nextStep.current);
                      setChain([...chain, { action, args: {} }]);
                    }}
                  >
                    {text("Добавить действие", "Add action")}
                  </button>
                  <p>
                    {chain.length}/8 ·{" "}
                    {text(
                      "Исполняется по порядку; ошибка останавливает цепочку.",
                      "Runs in order; an error stops the chain.",
                    )}
                  </p>
                </>
              ) : (
                when && (
                  <>
                    <PredicateForm value={when} {...{ fields, doc, ru }} change={setWhen} />
                    <p>
                      {stats.count}/64 · {text("Глубина", "Depth")} {stats.depth}/8
                    </p>
                  </>
                )
              )}
              {paths.limited && (
                <p>
                  {text(
                    "Показаны первые 200 путей; глубина до 8.",
                    "Showing the first 200 paths; depth up to 8.",
                  )}
                </p>
              )}
              {!valid && (
                <p role="alert">
                  {text(
                    "Заполните значения по контракту и проверьте пределы.",
                    "Complete values according to the contract and check the limits.",
                  )}
                </p>
              )}
            </fieldset>
          )}
          <button type="submit" disabled={blocked || legacy || !valid}>
            {text("Сохранить", "Save")}
          </button>
          <button
            type="button"
            disabled={blocked || (legacy && !confirm)}
            onClick={() => {
              void save(true);
            }}
          >
            {text("Снять", "Remove")}
          </button>
          <button type="button" disabled={state.status === "saving"} onClick={close}>
            {text("Сбросить ввод", "Discard input")}
          </button>
        </form>
      )}
    </section>
  );
}
