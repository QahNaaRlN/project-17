import { CapabilityGithubPanel } from "./capability-github-panel.js";
import { useEffect, useRef, useState } from "react";
import { useStore } from "zustand";
import { canonicalJson, manifestHash, type Fields, type Type } from "@cms/manifest";
import type { ActionCall, IrDocument } from "@cms/ir";
import { Dialog } from "./components.js";
import { StudioError, type Api, type Session } from "./types.js";
import type { Editor } from "./editor.js";
import { contract, location } from "./structure.js";
import { Argument } from "./behavior-panel.js";
import { behaviorFields, editableChain } from "./behavior.js";
import type { CapabilityAttempt, CapabilityCommand, CapabilityRequest } from "./capabilities.js";
import { capabilityMessage, capabilityURL } from "./capabilities.js";
import { CapabilityDeliveryPanel } from "./capability-delivery-panel.js";
import { CapabilityPreviewPanel } from "./capability-preview-panel.js";

const types = ["string", "text", "number", "boolean", "enum", "reference", "link"] as const;
type Arg = {
  name: string;
  type: (typeof types)[number];
  required: boolean;
  values: string;
  schema: string;
  min: string;
  max: string;
};
const newArg = (): Arg => ({
  name: "",
  type: "string",
  required: false,
  values: "",
  schema: "",
  min: "",
  max: "",
});
const codeMode = (doc: IrDocument, id: string, policy: Session["review"]["capabilityPolicy"]) => {
  if (!policy) return false;
  const levels = [doc.policy ?? {}];
  const nodes = [];
  const seen = new Set<string>();
  let at = id;
  while (!seen.has(at) && doc.nodes[at]) {
    seen.add(at);
    nodes.unshift(doc.nodes[at]!);
    const parent = location(doc, at);
    if (!parent) break;
    at = parent.parentId;
  }
  levels.push(...nodes.map((n) => n.zone ?? {}));
  let mode = policy.defaultMode,
    max = policy.maxMode;
  const rank = (s: string) => ["STRICT", "SYSTEM", "FREE", "CODE"].indexOf(s);
  for (const p of levels) {
    const next = p.mode ?? mode,
      ceiling = p.maxMode ?? p.mode ?? max;
    if (rank(next) > rank(max) || rank(ceiling) > rank(max) || rank(next) > rank(ceiling))
      return false;
    mode = next;
    max = ceiling;
  }
  return mode === "CODE";
};

function useCommand(
  api: Api,
  editor: Editor,
  onDone: (request: CapabilityRequest) => Promise<void>,
) {
  const [error, setError] = useState(""),
    [pending, setPending] = useState(false),
    [uncertain, setUncertain] = useState(false);
  const attempt = useRef<CapabilityAttempt | null>(null),
    running = useRef(false),
    alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  async function send(name?: CapabilityCommand, payload?: Record<string, unknown>) {
    if (running.current || !api.capabilityCommand) return;
    if (!attempt.current) {
      if (!name || !payload) return;
      attempt.current = {
        name,
        payload: structuredClone(payload),
        key: crypto.randomUUID(),
        reason: "Studio: explicit capability workflow",
      };
    }
    running.current = true;
    setPending(true);
    setError("");
    editor.store.setState({ status: "saving" });
    try {
      const result = await api.capabilityCommand(attempt.current);
      attempt.current = null;
      if (!alive.current) return;
      setUncertain(false);
      editor.store.setState({ status: editor.store.getState().session ? "ready" : "idle" });
      await onDone(result.request);
    } catch (cause) {
      if (!alive.current) return;
      const retry = !!attempt.current && (!(cause instanceof StudioError) || cause.uncertain);
      setUncertain(retry);
      setError(cause instanceof StudioError ? cause.code : "CAPABILITY_REQUEST_FAILED");
      if (!retry) attempt.current = null;
      editor.store.setState({
        status: retry ? "uncertain" : editor.store.getState().session ? "ready" : "idle",
      });
    } finally {
      running.current = false;
      if (alive.current) setPending(false);
    }
  }
  return { send, error, pending, uncertain };
}

function DraftFields({
  title,
  description,
  acceptance,
  args,
  setTitle,
  setDescription,
  setAcceptance,
  setArgs,
  schemas,
  ru,
  disabled,
}: {
  title: string;
  description: string;
  acceptance: string;
  args: Arg[];
  setTitle: (s: string) => void;
  setDescription: (s: string) => void;
  setAcceptance: (s: string) => void;
  setArgs: (a: Arg[]) => void;
  schemas: string[];
  ru: boolean;
  disabled: boolean;
}) {
  const text = (r: string, e: string) => (ru ? r : e);
  return (
    <fieldset disabled={disabled}>
      <label>
        {text("Заголовок", "Title")}
        <input required maxLength={200} value={title} onChange={(e) => setTitle(e.target.value)} />
      </label>
      <label>
        {text("Описание задачи", "Task description")}
        <textarea
          required
          maxLength={4000}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
      </label>
      <label>
        {text("Критерии приёмки, по одному в строке", "Acceptance criteria, one per line")}
        <textarea required value={acceptance} onChange={(e) => setAcceptance(e.target.value)} />
      </label>
      {args.map((arg, i) => {
        const change = (v: Partial<Arg>) =>
          setArgs(args.map((a, n) => (n === i ? { ...a, ...v } : a)));
        return (
          <fieldset key={i}>
            <legend>
              {text("Аргумент", "Argument")} {i + 1}
            </legend>
            <label>
              {text("Имя аргумента", "Argument name")}
              <input
                required
                pattern="[a-z][a-zA-Z0-9]*"
                value={arg.name}
                onChange={(e) => change({ name: e.target.value })}
              />
            </label>
            <label>
              {text("Тип аргумента", "Argument type")}
              <select
                value={arg.type}
                onChange={(e) => change({ type: e.target.value as Arg["type"] })}
              >
                {types.map((t) => (
                  <option key={t}>{t}</option>
                ))}
              </select>
            </label>
            <label>
              <input
                type="checkbox"
                checked={arg.required}
                onChange={(e) => change({ required: e.target.checked })}
              />
              {text("Обязательный", "Required")}
            </label>
            {arg.type === "enum" && (
              <label>
                {text("Варианты через запятую", "Comma separated values")}
                <input
                  required
                  value={arg.values}
                  onChange={(e) => change({ values: e.target.value })}
                />
              </label>
            )}
            {arg.type === "reference" && (
              <label>
                {text("Схема", "Schema")}
                <select
                  required
                  value={arg.schema}
                  onChange={(e) => change({ schema: e.target.value })}
                >
                  <option value="">—</option>
                  {schemas.map((s) => (
                    <option key={s}>{s}</option>
                  ))}
                </select>
              </label>
            )}
            {["number", "string", "text"].includes(arg.type) && (
              <>
                {arg.type !== "text" && (
                  <label>
                    {text("Минимум", "Minimum")}
                    <input
                      type="number"
                      step={arg.type === "number" ? "any" : "1"}
                      min={arg.type === "number" ? undefined : 0}
                      value={arg.min}
                      onChange={(e) => change({ min: e.target.value })}
                    />
                  </label>
                )}
                <label>
                  {text("Максимум", "Maximum")}
                  <input
                    type="number"
                    step={arg.type === "number" ? "any" : "1"}
                    min={arg.type === "number" ? undefined : 1}
                    value={arg.max}
                    onChange={(e) => change({ max: e.target.value })}
                  />
                </label>
              </>
            )}
            <button type="button" onClick={() => setArgs(args.filter((_, n) => n !== i))}>
              {text("Удалить аргумент", "Remove argument")}
            </button>
          </fieldset>
        );
      })}
      <button
        type="button"
        disabled={args.length >= 32}
        onClick={() => setArgs([...args, newArg()])}
      >
        {text("Добавить аргумент", "Add argument")}
      </button>
    </fieldset>
  );
}
function proposed(args: Arg[]): Fields {
  const fields: Fields = {};
  for (const arg of args) {
    if (
      Object.hasOwn(fields, arg.name) ||
      ["constructor", "prototype", "__proto__"].includes(arg.name)
    )
      throw new StudioError("CAPABILITY_CONTRACT_INVALID");
    const field: Record<string, unknown> = { type: arg.type, required: arg.required };
    if (arg.type === "enum")
      field.values = arg.values
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
    if (arg.type === "reference") field.schema = arg.schema;
    if (["number", "string", "text"].includes(arg.type)) {
      if (arg.type !== "text" && arg.min !== "")
        field[arg.type === "number" ? "min" : "minLength"] = Number(arg.min);
      if (arg.max !== "") field[arg.type === "number" ? "max" : "maxLength"] = Number(arg.max);
    }
    fields[arg.name] = field as unknown as Type;
  }
  return fields;
}
function argsFor(fields: Fields): Arg[] {
  return Object.entries(fields).map(([name, f]) => ({
    ...newArg(),
    name,
    type: f.type as Arg["type"],
    required: !!f.required,
    values: f.type === "enum" ? f.values.join(", ") : "",
    schema: f.type === "reference" ? f.schema : "",
    min: String(
      (f as { min?: number; minLength?: number }).min ??
        (f as { minLength?: number }).minLength ??
        "",
    ),
    max: String(
      (f as { max?: number; maxLength?: number }).max ??
        (f as { maxLength?: number }).maxLength ??
        "",
    ),
  }));
}

export function CapabilityComposer({
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
  const [scope, setScope] = useState<{ session: Session; node: string; event: string } | null>(
      null,
    ),
    [created, setCreated] = useState<CapabilityRequest | null>(null);
  const [key, setKey] = useState(""),
    [title, setTitle] = useState(""),
    [description, setDescription] = useState(""),
    [acceptance, setAcceptance] = useState(""),
    [args, setArgs] = useState<Arg[]>([]);
  const [localError, setLocalError] = useState("");
  const command = useCommand(api, editor, async (r) => {
    setCreated(r);
    setCreated(await api.capability!(r.id));
  });
  const open = !!scope || !!created;
  useEffect(() => {
    editor.formDirty("property-capability", open);
    return () => editor.formDirty("property-capability", false);
  }, [open, editor]);
  if (!api.capability || !api.capabilityCommand || !session?.record || !doc || !node) return null;
  const eligible =
    !!session.review.capabilityPolicy &&
    codeMode(doc, node.id, session.review.capabilityPolicy) &&
    (!rights || (rights.includes("capability.request") && rights.includes("design.read")));
  const blocked =
    state.status !== "ready" ||
    Object.entries(state.dirtyFields).some(([k, v]) => v && k !== "property-capability") ||
    session.changeset.state !== "open";
  const close = () => {
    if (
      command.pending ||
      command.uncertain ||
      ["saving", "uncertain"].includes(editor.store.getState().status)
    )
      return;
    setScope(null);
    setCreated(null);
    setLocalError("");
  };
  return (
    <section className="capability-panel" aria-label={text("Запрос действия", "Action request")}>
      {Object.entries(node.on ?? {}).flatMap(([event, value]) =>
        (Array.isArray(value) ? value : [value])
          .filter((call) => call.action.startsWith("pending:"))
          .map((call) => (
            <button
              key={event + call.action}
              disabled={blocked || open}
              onClick={() => {
                void api.capability!(call.action.slice(8))
                  .then(setCreated)
                  .catch((cause) =>
                    setLocalError(
                      cause instanceof StudioError ? cause.code : "CAPABILITY_REQUEST_FAILED",
                    ),
                  );
              }}
            >
              {text("Открыть запрос для", "Open request for")} {event}
            </button>
          )),
      )}
      {!open && localError && <p role="alert">{capabilityMessage(localError, ru)}</p>}
      {eligible &&
        Object.keys(contract(node.type, session.manifest)?.events ?? {}).map((event) => (
          <button
            key={event}
            disabled={blocked || open}
            onClick={() => {
              setScope({ session: { ...session, record: session.record! }, node: node.id, event });
              setKey("");
              setTitle("");
              setDescription("");
              setAcceptance("");
              setArgs([]);
            }}
          >
            {text("Запросить действие для", "Request action for")} {event}
          </button>
        ))}
      {open && (
        <Dialog
          title={text("Недостающее действие", "Missing action")}
          onDismiss={close}
          locked={command.pending || command.uncertain}
        >
          {created ? (
            <CapabilityCard {...{ api, editor }} initial={created} onClose={close} />
          ) : (
            scope && (
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  setLocalError("");
                  try {
                    const fields = proposed(args);
                    void command.send("create-capability-request", {
                      environment: scope.session.config.environment,
                      expectedManifestHash: scope.session.manifestHash,
                      changesetId: scope.session.changeset.id,
                      expectedSeq: scope.session.changeset.seq,
                      documentId: scope.session.record.id,
                      nodeId: scope.node,
                      event: scope.event,
                      key,
                      title,
                      description,
                      proposedContract: { args: fields },
                      acceptance: acceptance
                        .split("\n")
                        .map((s) => s.trim())
                        .filter(Boolean),
                    });
                  } catch (e) {
                    setLocalError(
                      e instanceof StudioError ? e.code : "CAPABILITY_CONTRACT_INVALID",
                    );
                  }
                }}
              >
                <p>
                  {scope.node} · {scope.event}
                </p>
                <label>
                  {text("Ключ действия", "Action key")}
                  <input
                    required
                    pattern="[a-z][a-zA-Z0-9]*(\.[a-z][a-zA-Z0-9]*)+"
                    maxLength={200}
                    disabled={command.pending || command.uncertain}
                    value={key}
                    onChange={(e) => setKey(e.target.value)}
                  />
                </label>
                <p>
                  {text("Похожие действия:", "Existing actions:")}{" "}
                  {Object.keys(scope.session.manifest.actions ?? {})
                    .filter((k) => k.includes(key))
                    .slice(0, 20)
                    .join(", ") || "—"}
                </p>
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
                  schemas={Object.keys(scope.session.manifest.schemas ?? {})}
                  disabled={command.pending || command.uncertain}
                />
                {(localError || command.error) && (
                  <p role="alert">{capabilityMessage(localError || command.error, ru)}</p>
                )}
                <button disabled={command.pending || command.uncertain || !eligible}>
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
                <button
                  type="button"
                  disabled={command.pending || command.uncertain}
                  onClick={close}
                >
                  {text("Отмена", "Cancel")}
                </button>
              </form>
            )
          )}
        </Dialog>
      )}
    </section>
  );
}

export function CapabilityCard({
  api,
  editor,
  initial,
  onClose,
}: {
  api: Api;
  editor: Editor;
  initial: CapabilityRequest;
  onClose: () => void;
}) {
  const state = useStore(editor.store),
    ru = state.locale === "ru",
    text = (r: string, e: string) => (ru ? r : e);
  const [request, setRequest] = useState(initial),
    [preview, setPreview] = useState<{
      hash?: string;
      pack: Record<string, unknown>;
      expectedSeq: number;
      expectedRevision: number;
      expectedManifestHash: string;
    } | null>(null),
    [error, setError] = useState("");
  useEffect(() => {
    setRequest((current) =>
      initial.id !== current.id || initial.revision >= current.revision ? initial : current,
    );
  }, [initial]);
  const [share, setShare] = useState(false),
    [replace, setReplace] = useState(false),
    [reason, setReason] = useState(""),
    [pr, setPR] = useState(""),
    [values, setValues] = useState<Record<string, unknown>>({}),
    [editing, setEditing] = useState(false);
  const [title, setTitle] = useState(initial.title),
    [description, setDescription] = useState(initial.description),
    [acceptance, setAcceptance] = useState(initial.acceptance.join("\n")),
    [args, setArgs] = useState(argsFor(initial.proposedContract.args));
  const command = useCommand(api, editor, async (r) => {
    setRequest(r);
    setRequest(await api.capability!(r.id));
    setPreview(null);
    setShare(false);
    setReplace(false);
    setEditing(false);
    if (editor.store.getState().session?.changeset.id === r.changesetId) await editor.refresh();
  });
  const dirty =
    editing ||
    share ||
    reason !== "" ||
    pr !== "" ||
    Object.keys(values).length > 0 ||
    command.uncertain;
  useEffect(() => {
    editor.formDirty("property-capability-card", dirty);
    return () => editor.formDirty("property-capability-card", false);
  }, [dirty, editor]);
  const session = state.session,
    doc = state.document,
    matches =
      !!session?.record &&
      session.changeset.id === request.changesetId &&
      session.record.id === request.documentId;
  const fields =
    matches && doc
      ? behaviorFields(doc, session.manifest, request.nodeId, session.record!.path ?? "").fields
      : [];
  const actual = matches ? session.manifest.actions?.[request.key]?.args : undefined;
  const handlers: ActionCall[] = [
    { action: request.key, args: values as NonNullable<ActionCall["args"]> },
  ];
  const actionPayload = {
    id: request.id,
    expectedRevision: request.revision,
    expectedSeq: session?.changeset.seq ?? request.seq,
    expectedManifestHash: session?.manifestHash,
  };
  const locked =
    command.pending ||
    command.uncertain ||
    !!state.dirtyFields["property-capability-delivery"] ||
    !!state.dirtyFields["property-capability-github"];
  const act = (name: CapabilityCommand, extra: Record<string, unknown> = {}) =>
    command.send(name, { ...actionPayload, ...extra });
  const transitions = request.transitions ?? [];
  async function readPreview() {
    setError("");
    try {
      const value = await api.capabilityContext!(request.id);
      const pinned = value.pack.pinned as { seq?: number; manifestHash?: string } | undefined;
      const next = value.pack.request as { id?: string; revision?: number } | undefined;
      if (
        next?.id !== request.id ||
        !Number.isSafeInteger(next.revision) ||
        next.revision! < 2 ||
        !Number.isSafeInteger(pinned?.seq) ||
        pinned!.seq! < 1 ||
        !/^sha256:[a-f0-9]{64}$/.test(pinned?.manifestHash ?? "")
      )
        throw new StudioError("RESPONSE_INVALID");
      // Consent applies to the reviewed snapshot, even if realtime refreshes the
      // editor or request before the user submits. The server repeats these gates.
      setPreview({
        ...value,
        expectedSeq: pinned!.seq! - (request.status === "draft" ? 1 : 0),
        expectedRevision: next.revision! - 1,
        expectedManifestHash: pinned!.manifestHash!,
      });
      setShare(false);
    } catch (e) {
      setError(e instanceof StudioError ? e.code : "CAPABILITY_CONTEXT_FAILED");
    }
  }
  async function download(version: number) {
    setError("");
    try {
      const value = await api.capabilityContext!(request.id, version);
      const bytes = canonicalJson(value.pack);
      const packHash = await manifestHash(value.pack);
      if (packHash !== request.packs?.find((p) => p.revision === version)?.hash)
        throw new StudioError("RESPONSE_INVALID");
      const url = URL.createObjectURL(new Blob([bytes], { type: "application/json" }));
      const link = document.createElement("a");
      link.href = url;
      link.download = `capability-${request.id}-v${version}.json`;
      link.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      setError(e instanceof StudioError ? e.code : "CAPABILITY_CONTEXT_FAILED");
    }
  }
  return (
    <article className="capability-card">
      <h3>{request.title}</h3>
      {api.github && api.githubHistory && api.githubCommand && request.status !== "draft" && (
        <CapabilityGithubPanel key={request.id} {...{ api, editor, request }} />
      )}
      {api.capabilityPreview && request.status !== "draft" && (
        <CapabilityPreviewPanel key={"preview-" + request.id} {...{ api, editor, request }} />
      )}
      {api.deliveryCommand && api.deliveries && api.delivery && api.deliveryPreview && (
        <CapabilityDeliveryPanel {...{ api, editor, request }} />
      )}
      <p>
        {request.key} · {request.status} · v{request.revision}
      </p>
      {request.status === "pr_open" && (
        <p>
          {text(
            "Статус pr_open сообщён разработчиком; подтверждение GitHub показано отдельно.",
            "The pr_open status was reported by an engineer; GitHub verification is shown separately.",
          )}
        </p>
      )}
      <p>{request.description}</p>
      <ul>
        {request.acceptance.map((s, i) => (
          <li key={i}>{s}</li>
        ))}
      </ul>
      <p>
        {request.nodeId} · {request.event}
      </p>
      {request.stale && (
        <p role="status">
          {text(
            "Context pack устарел. Проверьте новый состав перед обновлением.",
            "Context pack is stale. Review the new context before refreshing.",
          )}
        </p>
      )}
      {request.fulfillmentReverted && (
        <p role="status">
          {text(
            "Реализация была применена, затем отменена или изменена на странице. Статус запроса сохраняется.",
            "The applied implementation was undone or changed on the page. Request status is retained.",
          )}
        </p>
      )}
      {request.anchorChanged && request.status !== "draft" && request.status !== "fulfilled" && (
        <p role="status">
          {text(
            "Заглушка снята или изменена. История запроса сохранена.",
            "Placeholder removed or changed. Request history is preserved.",
          )}
        </p>
      )}
      {(command.error || error) && (
        <p role="alert">{capabilityMessage(command.error || error, ru)}</p>
      )}
      <button
        disabled={locked}
        onClick={() => {
          setError("");
          void api.capability!(request.id)
            .then(setRequest)
            .catch((cause) =>
              setError(cause instanceof StudioError ? cause.code : "CAPABILITY_REQUEST_FAILED"),
            );
        }}
      >
        {text("Обновить карточку", "Refresh request")}
      </button>
      {request.packs?.map((p) => (
        <div key={p.revision}>
          <code>
            v{p.revision} · {p.hash}
          </code>
          <button disabled={locked} onClick={() => void download(p.revision)}>
            {text("Скачать context pack", "Download context pack")}
          </button>
        </div>
      ))}
      {request.externalRefs.filter(capabilityURL).map((url) => (
        <p key={url}>
          <a href={url} target="_blank" rel="noopener noreferrer">
            {url}
          </a>
        </p>
      ))}
      {transitions.includes("edit") && (
        <button disabled={locked} onClick={() => setEditing(!editing)}>
          {text("Изменить черновик", "Edit draft")}
        </button>
      )}
      {editing && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            try {
              void command.send("update-capability-request", {
                id: request.id,
                expectedRevision: request.revision,
                draft: {
                  title,
                  description,
                  acceptance: acceptance
                    .split("\n")
                    .map((s) => s.trim())
                    .filter(Boolean),
                  proposedContract: { args: proposed(args) },
                },
              });
            } catch {
              setError("CAPABILITY_CONTRACT_INVALID");
            }
          }}
        >
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
            schemas={Object.keys(session?.manifest.schemas ?? {})}
            disabled={locked}
          />
          <button disabled={locked}>{text("Сохранить изменения", "Save changes")}</button>
        </form>
      )}
      {(transitions.includes("submit") || transitions.includes("refresh")) &&
        matches &&
        !editing && (
          <>
            <button disabled={locked} onClick={() => void readPreview()}>
              {text("Проверить передаваемый контекст", "Review shared context")}
            </button>
            {preview && (
              <>
                <pre className="capability-context">{JSON.stringify(preview.pack, null, 2)}</pre>
                <p>{preview.hash}</p>
                <label>
                  <input
                    type="checkbox"
                    checked={share}
                    onChange={(e) => setShare(e.target.checked)}
                    disabled={locked}
                  />
                  {text(
                    "Передать этот context pack участникам проекта с code.propose",
                    "Share this context pack with project members who have code.propose",
                  )}
                </label>
                {request.status === "draft" && (
                  <>
                    <p>
                      {text(
                        "Текущая цепочка будет заменена одной заглушкой:",
                        "The current chain will be replaced with one placeholder:",
                      )}
                    </p>
                    <pre>
                      {JSON.stringify(
                        doc?.nodes[request.nodeId]?.on?.[request.event] ?? [],
                        null,
                        2,
                      )}
                    </pre>
                    <pre>{JSON.stringify([{ action: "pending:" + request.id }], null, 2)}</pre>
                    <label>
                      <input
                        type="checkbox"
                        checked={replace}
                        onChange={(e) => setReplace(e.target.checked)}
                        disabled={locked}
                      />
                      {text("Подтверждаю замену цепочки", "Confirm replacing the chain")}
                    </label>
                  </>
                )}
                <button
                  disabled={
                    locked ||
                    !share ||
                    (request.status === "draft" && !replace) ||
                    state.status !== "ready"
                  }
                  onClick={() =>
                    void command.send(
                      request.status === "draft"
                        ? "submit-capability-request"
                        : "refresh-capability-context",
                      {
                        ...actionPayload,
                        expectedSeq: preview.expectedSeq,
                        expectedRevision: preview.expectedRevision,
                        expectedManifestHash: preview.expectedManifestHash,
                        ...(request.status === "draft" ? { confirmReplaceExisting: replace } : {}),
                      },
                    )
                  }
                >
                  {request.status === "draft"
                    ? text("Отправить запрос", "Submit request")
                    : text("Обновить context pack", "Refresh context pack")}
                </button>
              </>
            )}
          </>
        )}
      {transitions.includes("resolve") && matches && actual && doc && (
        <fieldset disabled={locked}>
          <legend>{text("Аргументы готового действия", "Available action arguments")}</legend>
          {Object.entries(actual).map(([name, schema]) => (
            <Argument
              key={name}
              {...{ name, schema, doc, fields, ru }}
              value={values[name]}
              change={(v) =>
                setValues((old) => {
                  const next = { ...old };
                  if (v === undefined) delete next[name];
                  else next[name] = v;
                  return next;
                })
              }
            />
          ))}
          <button
            disabled={locked || !editableChain(handlers, session.manifest, fields, doc)}
            onClick={() => void act("resolve-capability-request", { handlers })}
          >
            {text("Применить доступное действие", "Apply available action")}
          </button>
        </fieldset>
      )}
      {!matches && transitions.some((s) => ["submit", "refresh", "resolve"].includes(s)) && (
        <button
          disabled={locked || dirty}
          onClick={() => void editor.open(request.documentId, request.changesetId)}
        >
          {text("Открыть исходный черновик", "Open source draft")}
        </button>
      )}
      {transitions.some((s) => ["rejected", "pr_open"].includes(s)) && (
        <>
          <label>
            {text("Причина решения", "Decision reason")}
            <textarea
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              disabled={locked}
            />
          </label>
          <label>
            {text("Ссылка на PR", "PR link")}
            <input
              type="url"
              value={pr}
              onChange={(e) => setPR(e.target.value)}
              disabled={locked}
            />
          </label>
        </>
      )}
      {transitions
        .filter((s) => ["accepted", "in_progress", "pr_open", "rejected", "cancelled"].includes(s))
        .map((transition) => (
          <button
            key={transition}
            disabled={
              locked ||
              (transition === "rejected" && !reason.trim()) ||
              (transition === "pr_open" && !/^https:\/\//.test(pr))
            }
            onClick={() =>
              void command.send("update-capability-request", {
                id: request.id,
                expectedRevision: request.revision,
                transition,
                ...(transition === "rejected" ? { reason } : {}),
                ...(transition === "pr_open" ? { prUrl: pr } : {}),
              })
            }
          >
            {
              (
                {
                  accepted: text("Принять запрос", "Accept request"),
                  in_progress: text("Начать работу", "Start work"),
                  pr_open: text("Добавить PR", "Add PR"),
                  rejected: text("Отклонить запрос", "Reject request"),
                  cancelled: text("Отменить запрос", "Cancel request"),
                } as Record<string, string>
              )[transition]
            }
          </button>
        ))}
      <details>
        <summary>{text("История запроса", "Request history")}</summary>
        {request.audit?.map((a) => (
          <p key={a.revision}>
            v{a.revision} · {a.event} · {a.reason}
          </p>
        ))}
      </details>
      {command.uncertain && (
        <button disabled={command.pending} onClick={() => void command.send()}>
          {text("Повторить тот же запрос", "Retry exact request")}
        </button>
      )}
      <button
        disabled={locked}
        onClick={() => {
          editor.formDirty("property-capability-card", false);
          onClose();
        }}
      >
        {text("Закрыть", "Close")}
      </button>
    </article>
  );
}

export function CapabilityQueue({ api, editor }: { api: Api; editor: Editor }) {
  const state = useStore(editor.store),
    ru = state.locale === "ru",
    text = (r: string, e: string) => (ru ? r : e);
  const [items, setItems] = useState<CapabilityRequest[]>([]),
    [cursor, setCursor] = useState<string | null>(null),
    [status, setStatus] = useState(""),
    [key, setKey] = useState(""),
    [selected, setSelected] = useState<CapabilityRequest | null>(null),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(false);
  async function read(more = false) {
    if (!api.capabilities) return;
    setLoading(true);
    setError("");
    try {
      const list = await api.capabilities({ status, key, ...(more && cursor ? { cursor } : {}) });
      setItems((old) => (more ? [...old, ...list.items] : list.items));
      setCursor(list.nextCursor ?? null);
    } catch (e) {
      setError(e instanceof StudioError ? e.code : "CAPABILITY_REQUEST_FAILED");
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void read();
  }, []);
  return (
    <section className="capability-queue">
      <h1>{text("Запросы возможностей", "Capability requests")}</h1>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void read();
        }}
      >
        <label>
          {text("Ключ действия", "Action key")}
          <input value={key} onChange={(e) => setKey(e.target.value)} />
        </label>
        <label>
          {text("Статус", "Status")}
          <select value={status} onChange={(e) => setStatus(e.target.value)}>
            {[
              "",
              "draft",
              "submitted",
              "accepted",
              "in_progress",
              "pr_open",
              "fulfilled",
              "rejected",
              "cancelled",
            ].map((s) => (
              <option key={s} value={s}>
                {s || text("Все", "All")}
              </option>
            ))}
          </select>
        </label>
        <button disabled={loading}>{text("Найти запросы", "Find requests")}</button>
      </form>
      {error && <p role="alert">{capabilityMessage(error, ru)}</p>}
      {!loading && !items.length && (
        <p>{text("Нет доступных запросов", "No available requests")}</p>
      )}
      <ul>
        {items.map((r) => (
          <li key={r.id}>
            <button
              disabled={loading}
              onClick={() => {
                setLoading(true);
                void api.capability!(r.id)
                  .then(setSelected)
                  .catch((e) =>
                    setError(e instanceof StudioError ? e.code : "CAPABILITY_REQUEST_FAILED"),
                  )
                  .finally(() => setLoading(false));
              }}
            >
              {r.title} · {r.key} · {r.status}
            </button>
          </li>
        ))}
      </ul>
      {cursor && (
        <button disabled={loading} onClick={() => void read(true)}>
          {text("Ещё запросы", "More requests")}
        </button>
      )}
      {selected && (
        <Dialog
          title={text("Запрос возможности", "Capability request")}
          onDismiss={() => {
            if (
              !["saving", "uncertain"].includes(editor.store.getState().status) &&
              !editor.store.getState().dirtyFields["property-capability-delivery"] &&
              !editor.store.getState().dirtyFields["property-capability-github"]
            ) {
              setSelected(null);
              void read();
            }
          }}
          locked={
            ["saving", "uncertain"].includes(state.status) ||
            !!state.dirtyFields["property-capability-delivery"] ||
            !!state.dirtyFields["property-capability-github"]
          }
        >
          <CapabilityCard
            {...{ api, editor }}
            initial={selected}
            onClose={() => {
              setSelected(null);
              void read();
            }}
          />
        </Dialog>
      )}
    </section>
  );
}
