import { useEffect, useState } from "react";
import { useStore } from "zustand";
import type { Fields } from "@cms/manifest";
import { Dialog } from "./components.js";
import { StudioError, type Api } from "./types.js";
import type { Editor } from "./editor.js";
import { contract } from "./composed.js";
import type { CapabilityRequest } from "./capabilities.js";
import { capabilityMessage } from "./capabilities.js";
import {
  CapabilityCard,
  DraftFields,
  codeMode,
  proposed,
  useCommand,
  type Arg,
} from "./capability-panel.js";

const componentKey = /^[A-Z][A-Za-z0-9]{0,63}$/;

/** Initial value inputs for the placeholder of this place (STU-359/360). */
function initialValue(field: Fields[string], raw: string): unknown {
  if (field.type === "number") return raw === "" ? undefined : Number(raw);
  if (field.type === "boolean") return raw === "true";
  return raw === "" ? undefined : raw;
}

/**
 * STU-359/363/372: request a missing application component for the selected
 * container; open the request of a selected placeholder.
 */
export function ComponentRequestComposer({
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
  const [composing, setComposing] = useState(false),
    [created, setCreated] = useState<CapabilityRequest | null>(null),
    [key, setKey] = useState(""),
    [title, setTitle] = useState(""),
    [description, setDescription] = useState(""),
    [acceptance, setAcceptance] = useState(""),
    [args, setArgs] = useState<Arg[]>([]),
    [container, setContainer] = useState(false),
    [values, setValues] = useState<Record<string, string>>({}),
    [localError, setLocalError] = useState(""),
    [inserted, setInserted] = useState(false);
  const command = useCommand(api, editor, async (r) => {
    setCreated(await api.capability!(r.id));
  });
  const open = composing || !!created;
  useEffect(() => {
    editor.formDirty("property-capability-component", open);
    return () => editor.formDirty("property-capability-component", false);
  }, [open, editor]);
  if (!api.capability || !api.capabilityCommand || !session?.record || !doc || !node) return null;
  const record = session.record;
  const placeholder = typeof node.pending === "string" ? node.pending : null;
  const parent = contract(node.type, session.manifest);
  const eligible =
    !placeholder &&
    !!parent?.container &&
    !!session.review.capabilityPolicy &&
    codeMode(doc, node.id, session.review.capabilityPolicy) &&
    (!rights || ["capability.request", "design.read", "design.compose"].every((r) => rights.includes(r)));
  const blocked =
    state.status !== "ready" ||
    Object.entries(state.dirtyFields).some(([k, v]) => v && k !== "property-capability-component") ||
    session.changeset.state !== "open";
  const busy = command.pending || command.uncertain;
  const close = () => {
    if (busy || ["saving", "uncertain"].includes(editor.store.getState().status)) return;
    setComposing(false);
    setCreated(null);
    setLocalError("");
    setInserted(false);
  };
  const placement = { parentId: node.id, index: node.children?.length ?? 0 };
  const initialProps = (fields: Fields) =>
    Object.fromEntries(
      Object.entries(fields).flatMap(([name, field]) => {
        const v = initialValue(field, values[name] ?? "");
        return v === undefined ? [] : [[name, v]];
      }),
    );
  // STU-363: another instance joins the open request of the same key.
  async function insertInstance() {
    setLocalError("");
    try {
      const open = await api.openRequest!(key);
      const fields = proposed(args);
      await api.fulfillmentCommand!({
        name: "attach-capability-request",
        payload: {
          requestId: open.id,
          expectedRequestRevision: open.revision,
          changesetId: session!.changeset.id,
          expectedSeq: session!.changeset.seq,
          documentId: record.id,
          expectedManifestHash: session!.manifestHash,
          placement,
          initialProps: initialProps(fields),
        },
        key: crypto.randomUUID(),
        reason: "Studio: insert another instance of a requested component",
      });
      setInserted(true);
      await editor.refresh();
    } catch (e) {
      setLocalError(e instanceof StudioError ? e.code : "CAPABILITY_REQUEST_FAILED");
    }
  }
  const similar = Object.entries(session.manifest.components ?? {})
    .filter(
      ([name, c]) =>
        (key.length >= 3 && name.toLowerCase().includes(key.slice(0, 3).toLowerCase())) ||
        args.some((a) => a.name && Object.hasOwn(c.props ?? {}, a.name)),
    )
    .map(([name]) => name)
    .slice(0, 5);
  let fields: Fields = {};
  try {
    fields = proposed(args.filter((a) => a.name));
  } catch {
    fields = {};
  }
  return (
    <section
      className="capability-panel"
      aria-label={text("Запрос компонента", "Component request")}
    >
      {placeholder && (
        <>
          <p role="status">
            {text("Ожидает компонент", "Awaiting component")} {node.type}
          </p>
          <button
            disabled={blocked || open}
            onClick={() =>
              void api.capability!(placeholder)
                .then(setCreated)
                .catch((cause) =>
                  setLocalError(
                    cause instanceof StudioError ? cause.code : "CAPABILITY_REQUEST_FAILED",
                  ),
                )
            }
          >
            {text("Открыть запрос компонента", "Open component request")}
          </button>
        </>
      )}
      {!open && localError && <p role="alert">{capabilityMessage(localError, ru)}</p>}
      {eligible && (
        <button
          disabled={blocked || open}
          onClick={() => {
            setComposing(true);
            setKey("");
            setTitle("");
            setDescription("");
            setAcceptance("");
            setArgs([]);
            setContainer(false);
            setValues({});
          }}
        >
          {text("Нужен другой компонент…", "Need another component…")}
        </button>
      )}
      {open && (
        <Dialog
          title={text("Недостающий компонент", "Missing component")}
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
                  const props = proposed(args);
                  void command.send("create-capability-request", {
                    kind: "component",
                    environment: session.config.environment,
                    expectedManifestHash: session.manifestHash,
                    changesetId: session.changeset.id,
                    expectedSeq: session.changeset.seq,
                    documentId: record.id,
                    placement,
                    initialProps: initialProps(props),
                    key,
                    title,
                    description,
                    proposedContract: { props, container },
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
              <p>
                {text("Место:", "Place:")} {node.name ?? node.type} · {placement.index + 1}
              </p>
              <label>
                {text("Имя компонента", "Component name")}
                <input
                  required
                  pattern="[A-Z][A-Za-z0-9]{0,63}"
                  disabled={busy}
                  value={key}
                  onChange={(e) => setKey(e.target.value)}
                />
              </label>
              <p>
                {text("Похожие компоненты:", "Similar components:")} {similar.join(", ") || "—"}
              </p>
              <DraftFields
                {...{ title, description, acceptance, args, setTitle, setDescription, setAcceptance, setArgs, ru }}
                schemas={Object.keys(session.manifest.schemas ?? {})}
                disabled={busy}
                noun="property"
              />
              <label>
                <input
                  type="checkbox"
                  checked={container}
                  disabled={busy}
                  onChange={(e) => setContainer(e.target.checked)}
                />
                {text("Принимает вложенные блоки", "Accepts nested blocks")}
              </label>
              <fieldset disabled={busy}>
                <legend>{text("Значения для этого места", "Values for this place")}</legend>
                {Object.entries(fields).map(([name, field]) => (
                  <label key={name}>
                    {name}
                    {field.type === "boolean" ? (
                      <input
                        type="checkbox"
                        checked={values[name] === "true"}
                        onChange={(e) =>
                          setValues({ ...values, [name]: e.target.checked ? "true" : "" })
                        }
                      />
                    ) : field.type === "enum" ? (
                      <select
                        value={values[name] ?? ""}
                        onChange={(e) => setValues({ ...values, [name]: e.target.value })}
                      >
                        <option value="">—</option>
                        {field.values.map((v) => (
                          <option key={v}>{v}</option>
                        ))}
                      </select>
                    ) : (
                      <input
                        type={field.type === "number" ? "number" : "text"}
                        required={!!field.required}
                        value={values[name] ?? ""}
                        onChange={(e) => setValues({ ...values, [name]: e.target.value })}
                      />
                    )}
                  </label>
                ))}
              </fieldset>
              {(localError || command.error) && (
                <p role="alert">{capabilityMessage(localError || command.error, ru)}</p>
              )}
              {command.error === "CAPABILITY_KEY_TAKEN" &&
                api.openRequest &&
                api.fulfillmentCommand &&
                !inserted && (
                  <button type="button" onClick={() => void insertInstance()}>
                    {text("Вставить ещё экземпляр", "Insert another instance")}
                  </button>
                )}
              {inserted && (
                <>
                  <p role="status">
                    {text(
                      "Экземпляр запрошенного компонента вставлен.",
                      "An instance of the requested component was inserted.",
                    )}
                  </p>
                  <button type="button" onClick={close}>
                    {text("Закрыть", "Close")}
                  </button>
                </>
              )}
              <button disabled={busy || !eligible || !componentKey.test(key)}>
                {text("Сохранить черновик запроса", "Save request draft")}
              </button>
              {command.uncertain && (
                <button type="button" disabled={command.pending} onClick={() => void command.send()}>
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
