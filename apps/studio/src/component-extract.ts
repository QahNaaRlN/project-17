import type { IrDocument } from "@cms/ir";
import type { Type } from "@cms/manifest";
import { StudioError, type Operation } from "./types.js";

// STU-400…416: server plan, wizard state and component registry for Composed instances.

export interface ExtractCandidate {
  nodeId: string;
  nodeType: string;
  prop: string;
  type: Type;
  current: "none" | "literal" | "local" | "content" | "data" | "item" | "context" | "binding";
  content: boolean;
  external: boolean;
  suggestedName: string;
  restricted?: boolean;
  expression?: unknown;
}
export interface ExtractPlan {
  nodeId: string;
  blockers: { code: string; pointer?: string; nodeId?: string }[];
  candidates: ExtractCandidate[];
  localKeys: { key: string; nodeIds: string[]; usedOutside: boolean }[];
  slotRanges: { parentId: string; list?: string; nodeIds: string[]; types: string[] }[];
  slotPositions: { parentId: string; maxIndex: number }[];
  sourceListTypes: Type[];
  effectiveMode: string;
}
export interface WizardInput {
  /** `${nodeId}.${prop}` for an input from a property, `new:${n}` for a declared one. */
  id: string;
  name: string;
  enabled: boolean;
  required: boolean;
  source?: { nodeId: string; prop: string };
  /** Index in plan.sourceListTypes or a scalar type for a declared input. */
  declared?: { type: string; listType?: number };
}
export interface WizardSlot {
  name: string;
  parentId: string;
  /** Contiguous range in parent's list; empty for an empty slot at `index`. */
  nodeIds: string[];
  index: number;
  max?: number;
}
export interface Wizard {
  name: string;
  description: string;
  inputs: WizardInput[];
  slots: WizardSlot[];
  local: Record<string, "copy" | "move">;
}

const identifier = /^[A-Za-z][A-Za-z0-9_]*$/;
const reserved = ["constructor", "prototype", "__proto__"];
export const declaredTypes = ["text", "string", "number", "boolean", "url", "image"] as const;

export function validPlan(value: unknown, nodeId: string): ExtractPlan {
  const p = value as ExtractPlan;
  if (
    !p ||
    p.nodeId !== nodeId ||
    !Array.isArray(p.blockers) ||
    !Array.isArray(p.candidates) ||
    !Array.isArray(p.localKeys) ||
    !Array.isArray(p.slotRanges) ||
    !Array.isArray(p.slotPositions) ||
    !Array.isArray(p.sourceListTypes) ||
    p.candidates.some(
      (c) =>
        typeof c.nodeId !== "string" ||
        typeof c.prop !== "string" ||
        !c.type ||
        typeof c.type.type !== "string",
    )
  )
    throw new StudioError("RESPONSE_INVALID");
  return p;
}

/** Defaults: external dependencies are mandatory inputs; content properties become inputs. */
export function initialWizard(plan: ExtractPlan): Wizard {
  const used = new Set<string>();
  const inputs = plan.candidates.map((c) => {
    let name = c.suggestedName;
    for (let i = 2; used.has(name); i++) name = c.suggestedName + i;
    used.add(name);
    return {
      id: `${c.nodeId}.${c.prop}`,
      name,
      enabled: c.external || (c.content && c.current !== "none"),
      required: false,
      source: { nodeId: c.nodeId, prop: c.prop },
    };
  });
  return { name: "", description: "", inputs, slots: [], local: {} };
}

function candidate(plan: ExtractPlan, input: WizardInput) {
  return plan.candidates.find(
    (c) => c.nodeId === input.source?.nodeId && c.prop === input.source.prop,
  );
}

/** Keys of $local that stay referenced by component nodes (Q-4: copy by default). */
export function localChoices(plan: ExtractPlan, wizard: Wizard) {
  const slotted = new Set(wizard.slots.flatMap((s) => s.nodeIds));
  return plan.localKeys
    .map((key) => {
      const asInput = plan.candidates.some(
        (c) =>
          c.current === "local" &&
          c.expression === "$local." + key.key &&
          wizard.inputs.some(
            (i) => i.enabled && i.source?.nodeId === c.nodeId && i.source.prop === c.prop,
          ),
      );
      const inComponent = key.nodeIds.some((id) => !slotted.has(id));
      const others = plan.candidates.filter(
        (c) =>
          c.expression === "$local." + key.key &&
          !wizard.inputs.some(
            (i) => i.enabled && i.source?.nodeId === c.nodeId && i.source.prop === c.prop,
          ),
      );
      return {
        key: key.key,
        // move is safe only when nothing on the page keeps reading the key.
        movable: !key.usedOutside && !asInput && !key.nodeIds.some((id) => slotted.has(id)),
        shared: inComponent && (others.length > 0 || !asInput),
      };
    })
    .filter((k) => k.shared);
}

export interface WizardProblem {
  field: string;
  code: string;
}
export function wizardProblems(plan: ExtractPlan, wizard: Wizard): WizardProblem[] {
  const problems: WizardProblem[] = [];
  const name = wizard.name.trim();
  if (!name || [...name].length > 80) problems.push({ field: "name", code: "NAME_INVALID" });
  if ([...wizard.description].length > 500)
    problems.push({ field: "description", code: "DESCRIPTION_INVALID" });
  const enabled = wizard.inputs.filter((i) => i.enabled);
  const names = new Set<string>();
  for (const input of enabled) {
    if (!identifier.test(input.name) || reserved.includes(input.name) || names.has(input.name))
      problems.push({ field: "input:" + input.id, code: "INPUT_NAME_INVALID" });
    names.add(input.name);
    const c = candidate(plan, input);
    if (input.required && c?.current === "none")
      problems.push({ field: "input:" + input.id, code: "INPUT_REQUIRED_EMPTY" });
    if (c?.type.type === "list" && !c.type.of && input.declared?.listType === undefined)
      problems.push({ field: "input:" + input.id, code: "INPUT_LIST_TYPE_REQUIRED" });
  }
  if (enabled.length > 32) problems.push({ field: "inputs", code: "INPUT_LIMIT" });
  for (const c of plan.candidates)
    if (
      c.external &&
      !enabled.some((i) => i.source?.nodeId === c.nodeId && i.source.prop === c.prop)
    )
      problems.push({ field: `input:${c.nodeId}.${c.prop}`, code: "INPUT_EXTERNAL_REQUIRED" });
  const slotNames = new Set<string>();
  const slotted = new Set<string>();
  for (const slot of wizard.slots) {
    if (!identifier.test(slot.name) || reserved.includes(slot.name) || slotNames.has(slot.name))
      problems.push({ field: "slot:" + slot.name, code: "SLOT_NAME_INVALID" });
    slotNames.add(slot.name);
    for (const id of slot.nodeIds) {
      if (slotted.has(id)) problems.push({ field: "slot:" + slot.name, code: "SLOT_OVERLAP" });
      slotted.add(id);
      if (enabled.some((i) => i.source?.nodeId === id))
        problems.push({ field: "slot:" + slot.name, code: "SLOT_HAS_INPUT" });
    }
    if (slot.max !== undefined && slot.max < Math.max(1, slot.nodeIds.length))
      problems.push({ field: "slot:" + slot.name, code: "SLOT_MAX_INVALID" });
  }
  if (wizard.slots.length > 8) problems.push({ field: "slots", code: "SLOT_LIMIT" });
  return problems;
}

/** Payload of component.extract; IDs are assigned by the server. */
export function extractOperation(target: string, plan: ExtractPlan, wizard: Wizard): Operation {
  const inputs: Record<string, unknown> = {};
  for (const input of wizard.inputs.filter((i) => i.enabled)) {
    const entry: Record<string, unknown> = {};
    if (input.source) entry.from = [input.source];
    if (input.declared) {
      const list =
        input.declared.listType !== undefined
          ? plan.sourceListTypes[input.declared.listType]
          : undefined;
      if (input.source && list?.type === "list") entry.def = { of: list.of };
      else if (list) entry.def = structuredClone(list);
      else if (input.declared.type === "image") entry.def = { type: "asset", assetKind: "image" };
      else entry.def = { type: input.declared.type };
    }
    if (input.required) entry.required = true;
    inputs[input.name] = entry;
  }
  const slots: Record<string, unknown> = {};
  for (const slot of wizard.slots)
    slots[slot.name] = slot.nodeIds.length
      ? { nodeIds: slot.nodeIds, ...(slot.max !== undefined ? { max: slot.max } : {}) }
      : {
          nodeIds: [],
          at: { parentId: slot.parentId, index: slot.index },
          min: 0,
          ...(slot.max !== undefined ? { max: slot.max } : {}),
        };
  const local = Object.fromEntries(
    localChoices(plan, wizard)
      .filter((k) => wizard.local[k.key] === "move" && k.movable)
      .map((k) => [k.key, "move"]),
  );
  return {
    type: "component.extract",
    target,
    payload: {
      nodeId: plan.nodeId,
      name: wizard.name.trim(),
      ...(wizard.description.trim() ? { description: wizard.description.trim() } : {}),
      ...(Object.keys(inputs).length ? { inputs } : {}),
      ...(Object.keys(slots).length ? { slots } : {}),
      ...(Object.keys(local).length ? { localContent: local } : {}),
    },
  };
}

/** Future structure shown on the review step (STU-413). */
export function previewTree(doc: IrDocument, plan: ExtractPlan, wizard: Wizard) {
  const slotted = new Map<string, string>();
  for (const slot of wizard.slots) for (const id of slot.nodeIds) slotted.set(id, slot.name);
  const component: { depth: number; label: string }[] = [];
  const walk = (id: string, depth: number) => {
    const node = doc.nodes[id];
    if (!node) return;
    const slot = slotted.get(id);
    if (slot !== undefined) {
      if (wizard.slots.find((s) => s.name === slot)!.nodeIds[0] === id)
        component.push({ depth, label: `Slot · ${slot}` });
      return;
    }
    component.push({ depth, label: node.name ?? node.type });
    for (const empty of wizard.slots.filter((s) => !s.nodeIds.length && s.parentId === id))
      component.push({ depth: depth + 1, label: `Slot · ${empty.name}` });
    for (const child of [...(node.children ?? []), ...Object.values(node.slots ?? {}).flat()])
      walk(child, depth + 1);
  };
  walk(plan.nodeId, 0);
  return component;
}

export {
  registerComponents,
  componentOf,
  componentTitle,
  nodeContract,
  componentRefs,
} from "./composed.js";
