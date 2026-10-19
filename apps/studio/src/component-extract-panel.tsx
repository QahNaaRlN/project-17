import { useEffect, useRef, useState } from "react";
import { useStore } from "zustand";
import type { Editor } from "./editor.js";
import { Dialog } from "./components.js";
import {
  declaredTypes,
  extractOperation,
  initialWizard,
  localChoices,
  previewTree,
  wizardProblems,
  type ExtractPlan,
  type Wizard,
  type WizardSlot,
} from "./component-extract.js";
import { StudioError } from "./types.js";

// STU-413/418: «Сохранить как компонент» — name → inputs → slots → review.

const messages: Record<string, [string, string]> = {
  COMPONENT_EXTRACT_ROOT_DENIED: [
    "Корень страницы нельзя сохранить как компонент.",
    "The page root cannot become a component.",
  ],
  COMPONENT_EXTRACT_TARGET_KIND: [
    "Сохранить как компонент можно только блок страницы.",
    "Only a page block can become a component.",
  ],
  COMPONENT_EXTRACT_PROTECTED: [
    "Блок содержит зону, защищённый узел или заглушку возможности.",
    "The block contains a zone, a protected node or a capability stub.",
  ],
  COMPONENT_EXTRACT_EXTERNAL_SCOPE: [
    "Выражение блока зависит от данных страницы: сделайте свойство входом или измените условие/действие.",
    "An expression depends on page data: make the property an input or change the condition/action.",
  ],
  COMPONENT_EXTRACT_NODE_REF: [
    "Ссылка на узел пересекает границу компонента.",
    "A node reference crosses the component boundary.",
  ],
  COMPONENT_EXTRACT_LOCAL_IN_USE: [
    "Текст ещё нужен странице — его можно только скопировать.",
    "The text is still used by the page; it can only be copied.",
  ],
  COMPONENT_EXTRACT_INPUT_INVALID: ["Проверьте имя и тип входа.", "Check the input name and type."],
  COMPONENT_EXTRACT_INPUT_INCOMPATIBLE: [
    "Свойства одного входа различаются значением или типом.",
    "Properties of one input differ in value or type.",
  ],
  COMPONENT_EXTRACT_SLOT_INVALID: [
    "Проверьте узлы и ограничения слота.",
    "Check the slot nodes and limits.",
  ],
  COMPONENT_NAME_TAKEN: [
    "Компонент с таким именем уже есть в проекте.",
    "A component with this name already exists.",
  ],
  POLICY_COMPONENT_MODE_REQUIRED: [
    "В строгой зоне нужен сертифицированный компонент: извлеките блок в другом месте, затем сертифицируйте.",
    "A strict zone requires a certified component: extract elsewhere, then certify.",
  ],
  POLICY_TYPE_FORBIDDEN: [
    "Зона не разрешает компонент на этом месте.",
    "The zone does not allow a component here.",
  ],
  FORBIDDEN: [
    "Нужны права component.write и design.compose.",
    "component.write and design.compose are required.",
  ],
  CHANGESET_KIND: ["Нужен обычный черновик.", "A standard draft is required."],
  CHANGESET_READ_ONLY: ["Черновик закрыт для записи.", "The draft is read-only."],
  CHANGESET_SEQ_CONFLICT: [
    "Черновик изменился. Перечитайте план и проверьте ответы.",
    "The draft changed. Reload the plan and review your answers.",
  ],
  NAME_INVALID: ["Имя: от 1 до 80 символов.", "Name: 1 to 80 characters."],
  DESCRIPTION_INVALID: ["Описание: до 500 символов.", "Description: up to 500 characters."],
  INPUT_NAME_INVALID: [
    "Имя входа — латинский идентификатор, уникальный в компоненте.",
    "An input name is a unique Latin identifier.",
  ],
  INPUT_REQUIRED_EMPTY: [
    "Обязательный вход должен иметь значение на экземпляре.",
    "A required input needs a value on the instance.",
  ],
  INPUT_LIST_TYPE_REQUIRED: [
    "Выберите тип элемента списка из контракта источника.",
    "Choose the list item type from a source contract.",
  ],
  INPUT_EXTERNAL_REQUIRED: [
    "Зависимость от данных страницы обязана стать входом.",
    "A page data dependency must become an input.",
  ],
  INPUT_LIMIT: ["Не больше 32 входов.", "At most 32 inputs."],
  SLOT_NAME_INVALID: [
    "Имя слота — латинский идентификатор, уникальный в компоненте.",
    "A slot name is a unique Latin identifier.",
  ],
  SLOT_OVERLAP: ["Слоты не должны пересекаться.", "Slots must not overlap."],
  SLOT_HAS_INPUT: [
    "Узел-источник входа нельзя поместить в слот.",
    "An input source node cannot be in a slot.",
  ],
  SLOT_MAX_INVALID: ["Максимум слота меньше числа узлов.", "Slot maximum is below the node count."],
  SLOT_LIMIT: ["Не больше 8 слотов.", "At most 8 slots."],
};
export function extractMessage(code: string, ru: boolean) {
  const m = messages[code];
  return m ? m[ru ? 0 : 1] : code;
}
const steps = ["name", "inputs", "slots", "review"] as const;
function stepOf(pointer: string, code: string) {
  if (pointer.includes("/slots") || code.includes("SLOT")) return 2;
  if (
    pointer.includes("/inputs") ||
    pointer.includes("/localContent") ||
    code.includes("INPUT") ||
    code.includes("SCOPE") ||
    code.includes("LOCAL")
  )
    return 1;
  if (pointer.includes("/name") || code.includes("NAME")) return 0;
  return 3;
}

export function ComponentExtractDialog({
  editor,
  nodeId,
  onClose,
}: {
  editor: Editor;
  nodeId: string;
  onClose: () => void;
}) {
  const state = useStore(editor.store),
    ru = state.locale === "ru",
    t = (r: string, e: string) => (ru ? r : e),
    doc = state.document!,
    target = state.session!.record!.id;
  const [plan, setPlan] = useState<ExtractPlan | null>(null),
    [loadError, setLoadError] = useState<string | null>(null),
    [wizard, setWizard] = useState<Wizard | null>(null),
    [step, setStep] = useState(0),
    [server, setServer] = useState<{ code: string; pointers: string[] } | null>(null),
    [uncertain, setUncertain] = useState(false),
    [saving, setSaving] = useState(false),
    [declared, setDeclared] = useState({ name: "", type: "text" }),
    [slot, setSlot] = useState({
      name: "footer",
      range: "0",
      from: "0",
      to: "0",
      empty: false,
      position: "0",
      index: "0",
      max: "",
    });
  const body = useRef<HTMLDivElement>(null);
  async function load() {
    setLoadError(null);
    try {
      const next = await editor.extractPlan(nodeId);
      setPlan(next);
      setWizard((current) => current ?? initialWizard(next));
    } catch (cause) {
      setLoadError(cause instanceof StudioError ? cause.code : "PLAN_FAILED");
    }
  }
  useEffect(() => {
    void load();
    editor.formDirty("componentExtract", true);
    return () => editor.formDirty("componentExtract", false);
  }, []);
  useEffect(() => {
    body.current
      ?.querySelector<HTMLElement>(
        "input:not(:disabled),select:not(:disabled),button:not(:disabled)",
      )
      ?.focus();
  }, [step, plan]);
  const problems = plan && wizard ? wizardProblems(plan, wizard) : [];
  const stepProblems = problems.filter((p) =>
    step === 0
      ? p.field === "name" || p.field === "description"
      : step === 1
        ? p.field.startsWith("input")
        : step === 2
          ? p.field.startsWith("slot")
          : true,
  );
  const update = (change: (w: Wizard) => Wizard) =>
    setWizard((w) => (w ? change(structuredClone(w)) : w));
  async function save() {
    if (!plan || !wizard || problems.length) return;
    setSaving(true);
    setServer(null);
    const outcome = uncertain
      ? await editor.retryExtract()
      : await editor.extract(extractOperation(target, plan, wizard), wizard.name.trim());
    setSaving(false);
    if (outcome.ok) {
      onClose();
      return;
    }
    setUncertain(outcome.uncertain);
    const code = outcome.operation?.code ?? outcome.code;
    const pointers = (outcome.operation?.diagnostics ?? []).map((d) => d.pointer);
    setServer({ code, pointers });
    if (!outcome.uncertain) setStep(stepOf(pointers[0] ?? "", code));
    if (code === "CHANGESET_SEQ_CONFLICT") void load();
  }
  const title = t("Сохранить как компонент", "Save as component");
  if (loadError)
    return (
      <Dialog title={title} onDismiss={onClose}>
        <p role="alert">{extractMessage(loadError, ru)}</p>
        <button type="button" onClick={() => void load()}>
          {t("Повторить", "Retry")}
        </button>
        <button type="button" onClick={onClose}>
          {t("Закрыть", "Close")}
        </button>
      </Dialog>
    );
  if (!plan || !wizard)
    return (
      <Dialog title={title} onDismiss={onClose}>
        <p role="status">{t("Загрузка плана…", "Loading plan…")}</p>
      </Dialog>
    );
  const choices = localChoices(plan, wizard);
  const range = plan.slotRanges[Number(slot.range)];
  function addSlot() {
    const next: WizardSlot = slot.empty
      ? {
          name: slot.name,
          parentId: plan!.slotPositions[Number(slot.position)]!.parentId,
          nodeIds: [],
          index: Number(slot.index),
        }
      : {
          name: slot.name,
          parentId: range!.parentId,
          nodeIds: range!.nodeIds.slice(
            Math.min(+slot.from, +slot.to),
            Math.max(+slot.from, +slot.to) + 1,
          ),
          index: 0,
        };
    if (slot.max) next.max = Number(slot.max);
    update((w) => ({ ...w, slots: [...w.slots, next] }));
  }
  const blocked = plan.blockers.length > 0;
  return (
    <Dialog title={title} onDismiss={onClose} locked={saving}>
      <ol className="extract-steps" aria-label={t("Шаги", "Steps")}>
        {steps.map((s, i) => (
          <li key={s} aria-current={i === step ? "step" : undefined}>
            {[t("Имя", "Name"), t("Входы", "Inputs"), t("Слоты", "Slots"), t("Итог", "Review")][i]}
          </li>
        ))}
      </ol>
      {blocked && (
        <div role="alert">
          <p>
            {t(
              "Сохранить этот блок как компонент нельзя:",
              "This block cannot become a component:",
            )}
          </p>
          <ul>
            {plan.blockers.map((b, i) => (
              <li key={i}>
                {extractMessage(b.code, ru)} <code>{b.code}</code>
              </li>
            ))}
          </ul>
        </div>
      )}
      <div ref={body} className="extract-body">
        {step === 0 && (
          <>
            <label>
              {t("Имя компонента", "Component name")}
              <input
                value={wizard.name}
                maxLength={80}
                disabled={blocked}
                onChange={(e) => update((w) => ({ ...w, name: e.target.value }))}
              />
            </label>
            <label>
              {t("Описание", "Description")}
              <textarea
                value={wizard.description}
                maxLength={500}
                disabled={blocked}
                onChange={(e) => update((w) => ({ ...w, description: e.target.value }))}
              />
            </label>
            <p className="hint">
              {t("Режим места:", "Place mode:")} {plan.effectiveMode}
            </p>
          </>
        )}
        {step === 1 && (
          <>
            <table className="extract-inputs">
              <thead>
                <tr>
                  <th>{t("Вход", "Input")}</th>
                  <th>{t("Свойство", "Property")}</th>
                  <th>{t("Значение", "Value")}</th>
                  <th>{t("Обязательный", "Required")}</th>
                </tr>
              </thead>
              <tbody>
                {wizard.inputs.map((input, i) => {
                  const c = plan.candidates.find(
                    (x) => x.nodeId === input.source?.nodeId && x.prop === input.source.prop,
                  );
                  const label = c
                    ? `${doc.nodes[c.nodeId]?.name ?? c.nodeType}.${c.prop}`
                    : t("Новый вход", "New input");
                  return (
                    <tr key={input.id}>
                      <td>
                        <label>
                          <input
                            type="checkbox"
                            aria-label={t("Сделать входом ", "Make input ") + label}
                            checked={input.enabled}
                            disabled={!!c?.external}
                            onChange={(e) =>
                              update((w) => ((w.inputs[i]!.enabled = e.target.checked), w))
                            }
                          />
                        </label>
                        <input
                          aria-label={t("Имя входа ", "Input name ") + label}
                          value={input.name}
                          disabled={!input.enabled}
                          aria-invalid={problems.some((p) => p.field === "input:" + input.id)}
                          onChange={(e) => update((w) => ((w.inputs[i]!.name = e.target.value), w))}
                        />
                      </td>
                      <td>
                        {label} · {c?.type.type ?? input.declared?.type}
                        {c?.external && (
                          <span> · {t("зависит от страницы", "depends on page")}</span>
                        )}
                      </td>
                      <td>
                        {c?.restricted
                          ? t("скрыто без content.read", "hidden without content.read")
                          : (c?.current ?? "—")}
                        {c?.type.type === "list" && !c.type.of && (
                          <select
                            aria-label={t("Тип элемента списка", "List item type")}
                            value={input.declared?.listType ?? ""}
                            onChange={(e) =>
                              update(
                                (w) => (
                                  (w.inputs[i]!.declared = {
                                    type: "list",
                                    listType: Number(e.target.value),
                                  }),
                                  w
                                ),
                              )
                            }
                          >
                            <option value="">—</option>
                            {plan.sourceListTypes.map((s, n) =>
                              s.type === "list" ? (
                                <option key={n} value={n}>
                                  list ·{" "}
                                  {Object.keys((s.of as { fields?: object })?.fields ?? {}).join(
                                    ", ",
                                  )}
                                </option>
                              ) : null,
                            )}
                          </select>
                        )}
                      </td>
                      <td>
                        <input
                          type="checkbox"
                          aria-label={t("Обязательный ", "Required ") + label}
                          checked={input.required}
                          disabled={!input.enabled}
                          onChange={(e) =>
                            update((w) => ((w.inputs[i]!.required = e.target.checked), w))
                          }
                        />
                        {!c && (
                          <button
                            type="button"
                            onClick={() =>
                              update((w) => ({ ...w, inputs: w.inputs.filter((_, n) => n !== i) }))
                            }
                          >
                            {t("Удалить", "Remove")}
                          </button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            <fieldset>
              <legend>{t("Новый вход", "New input")}</legend>
              <label>
                {t("Имя", "Name")}
                <input
                  aria-label={t("Имя нового входа", "New input name")}
                  value={declared.name}
                  onChange={(e) => setDeclared({ ...declared, name: e.target.value })}
                />
              </label>
              <label>
                {t("Тип", "Type")}
                <select
                  aria-label={t("Тип нового входа", "New input type")}
                  value={declared.type}
                  onChange={(e) => setDeclared({ ...declared, type: e.target.value })}
                >
                  {declaredTypes.map((d) => (
                    <option key={d} value={d}>
                      {d}
                    </option>
                  ))}
                  {plan.sourceListTypes.map((s, n) => (
                    <option key={"source" + n} value={"source:" + n}>
                      {s.type} · {t("контракт источника", "source contract")} {n + 1}
                    </option>
                  ))}
                </select>
              </label>
              <button
                type="button"
                disabled={!declared.name}
                onClick={() => {
                  const source = declared.type.startsWith("source:")
                    ? Number(declared.type.slice(7))
                    : undefined;
                  update((w) => ({
                    ...w,
                    inputs: [
                      ...w.inputs,
                      {
                        id: "new:" + w.inputs.length + ":" + declared.name,
                        name: declared.name,
                        enabled: true,
                        required: false,
                        declared:
                          source === undefined
                            ? { type: declared.type }
                            : { type: "list", listType: source },
                      },
                    ],
                  }));
                  setDeclared({ name: "", type: "text" });
                }}
              >
                {t("Добавить вход", "Add input")}
              </button>
            </fieldset>
            {choices.length > 0 && (
              <fieldset>
                <legend>{t("Текст без входа", "Text without input")}</legend>
                <p className="hint">
                  {t(
                    "Текст станет общим для всех экземпляров. По умолчанию он копируется.",
                    "The text becomes shared by all instances. It is copied by default.",
                  )}
                </p>
                {choices.map((k) => (
                  <label key={k.key}>
                    <code>{k.key}</code>
                    <select
                      aria-label={t("Текст ", "Text ") + k.key}
                      value={wizard.local[k.key] === "move" && k.movable ? "move" : "copy"}
                      onChange={(e) =>
                        update((w) => ((w.local[k.key] = e.target.value as "copy" | "move"), w))
                      }
                    >
                      <option value="copy">{t("Копировать", "Copy")}</option>
                      <option value="move" disabled={!k.movable}>
                        {t("Перенести", "Move")}
                      </option>
                    </select>
                  </label>
                ))}
              </fieldset>
            )}
          </>
        )}
        {step === 2 && (
          <>
            <ul>
              {wizard.slots.map((s, i) => (
                <li key={s.name + i}>
                  <strong>{s.name}</strong> ·{" "}
                  {s.nodeIds.length
                    ? s.nodeIds.map((id) => doc.nodes[id]?.name ?? doc.nodes[id]?.type).join(", ")
                    : t("пустой", "empty")}
                  <button
                    type="button"
                    onClick={() =>
                      update((w) => ({ ...w, slots: w.slots.filter((_, n) => n !== i) }))
                    }
                  >
                    {t("Удалить", "Remove")}
                  </button>
                </li>
              ))}
            </ul>
            <fieldset>
              <legend>{t("Новый слот", "New slot")}</legend>
              <label>
                {t("Имя слота", "Slot name")}
                <input
                  value={slot.name}
                  onChange={(e) => setSlot({ ...slot, name: e.target.value })}
                />
              </label>
              <label>
                <input
                  type="checkbox"
                  checked={slot.empty}
                  onChange={(e) => setSlot({ ...slot, empty: e.target.checked })}
                />
                {t("Пустой слот (min 0)", "Empty slot (min 0)")}
              </label>
              {slot.empty ? (
                <>
                  <label>
                    {t("Контейнер", "Container")}
                    <select
                      aria-label={t("Контейнер", "Container")}
                      value={slot.position}
                      onChange={(e) => setSlot({ ...slot, position: e.target.value, index: "0" })}
                    >
                      {plan.slotPositions.map((p, n) => (
                        <option key={p.parentId} value={n}>
                          {doc.nodes[p.parentId]?.name ?? doc.nodes[p.parentId]?.type}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    {t("Позиция", "Position")}
                    <input
                      type="number"
                      min={0}
                      max={plan.slotPositions[Number(slot.position)]?.maxIndex ?? 0}
                      value={slot.index}
                      onChange={(e) => setSlot({ ...slot, index: e.target.value })}
                    />
                  </label>
                </>
              ) : (
                <>
                  <label>
                    {t("Родитель", "Parent")}
                    <select
                      aria-label={t("Родитель", "Parent")}
                      value={slot.range}
                      onChange={(e) =>
                        setSlot({ ...slot, range: e.target.value, from: "0", to: "0" })
                      }
                    >
                      {plan.slotRanges.map((r, n) => (
                        <option key={r.parentId + (r.list ?? "")} value={n}>
                          {doc.nodes[r.parentId]?.name ?? doc.nodes[r.parentId]?.type}
                          {r.list ? " · " + r.list : ""}
                        </option>
                      ))}
                    </select>
                  </label>
                  {(["from", "to"] as const).map((end) => (
                    <label key={end}>
                      {end === "from"
                        ? t("Первый узел", "First node")
                        : t("Последний узел", "Last node")}
                      <select
                        aria-label={
                          end === "from"
                            ? t("Первый узел", "First node")
                            : t("Последний узел", "Last node")
                        }
                        value={slot[end]}
                        onChange={(e) => setSlot({ ...slot, [end]: e.target.value })}
                      >
                        {range?.nodeIds.map((id, n) => (
                          <option key={id} value={n}>
                            {doc.nodes[id]?.name ?? range.types[n]}
                          </option>
                        ))}
                      </select>
                    </label>
                  ))}
                </>
              )}
              <label>
                {t("Максимум узлов", "Maximum nodes")}
                <input
                  type="number"
                  min={1}
                  value={slot.max}
                  onChange={(e) => setSlot({ ...slot, max: e.target.value })}
                />
              </label>
              <button
                type="button"
                disabled={!slot.name || (slot.empty ? !plan.slotPositions.length : !range)}
                onClick={addSlot}
              >
                {t("Добавить слот", "Add slot")}
              </button>
            </fieldset>
          </>
        )}
        {step === 3 && (
          <>
            <p>
              {t("Компонент", "Component")} <strong>{wizard.name.trim()}</strong> ·{" "}
              {t("входы", "inputs")}:{" "}
              {wizard.inputs
                .filter((i) => i.enabled)
                .map((i) => i.name)
                .join(", ") || "—"}{" "}
              · {t("слоты", "slots")}: {wizard.slots.map((s) => s.name).join(", ") || "—"}
            </p>
            <ul aria-label={t("Будущая структура компонента", "Future component structure")}>
              {previewTree(doc, plan, wizard).map((n, i) => (
                <li key={i} style={{ paddingInlineStart: n.depth * 12 }}>
                  {n.label}
                </li>
              ))}
            </ul>
            <p className="hint">
              {t(
                "На странице блок заменит один экземпляр компонента; слоты останутся узлами страницы.",
                "On the page the block becomes one component instance; slot nodes stay on the page.",
              )}
            </p>
          </>
        )}
      </div>
      {stepProblems.length > 0 && (
        <ul role="alert">
          {stepProblems.map((p, i) => (
            <li key={i}>{extractMessage(p.code, ru)}</li>
          ))}
        </ul>
      )}
      {server && (
        <div role="alert">
          <p>
            {uncertain
              ? t(
                  "Результат сохранения неизвестен. Повторите то же сохранение — оно не создаст второй компонент.",
                  "The save outcome is unknown. Retry the same save; it will not create a second component.",
                )
              : extractMessage(server.code, ru)}{" "}
            <code>{server.code}</code>
          </p>
          {server.pointers.length > 0 && <p className="hint">{server.pointers.join(", ")}</p>}
        </div>
      )}
      <div className="structure-actions">
        <button
          type="button"
          disabled={step === 0 || saving || uncertain}
          onClick={() => setStep(step - 1)}
        >
          {t("Назад", "Back")}
        </button>
        {step < 3 ? (
          <button
            type="button"
            disabled={blocked || stepProblems.length > 0}
            onClick={() => setStep(step + 1)}
          >
            {t("Далее", "Next")}
          </button>
        ) : (
          <button
            type="button"
            disabled={blocked || saving || (!uncertain && problems.length > 0)}
            onClick={() => void save()}
          >
            {uncertain ? t("Повторить сохранение", "Retry save") : t("Сохранить", "Save")}
          </button>
        )}
        <button type="button" disabled={saving || uncertain} onClick={onClose}>
          {t("Отмена", "Cancel")}
        </button>
      </div>
    </Dialog>
  );
}
