import { describe, expect, it } from "vitest";
import type { IrDocument, Node } from "@cms/ir";
import type { Type } from "@cms/manifest";
import {
  extractOperation,
  initialWizard,
  localChoices,
  previewTree,
  validPlan,
  wizardProblems,
  type ExtractPlan,
} from "../src/component-extract.js";
import {
  componentRefs,
  componentTitle,
  nodeContract,
  registerComponents,
} from "../src/composed.js";
import { bindingFields, directPath, itemScope, resultFields } from "../src/data-sources.js";
import { fields, layers } from "../src/fields.js";
import { applyOperations } from "../src/operations.js";
import { fixture } from "./fixture.js";

const list = {
  type: "list",
  of: { type: "object", fields: { title: { type: "text" } } },
} as unknown as Type;
const text = { type: "text", content: true } as Type;
const plan: ExtractPlan = {
  nodeId: "n_block",
  blockers: [],
  candidates: [
    {
      nodeId: "n_title",
      nodeType: "Heading",
      prop: "text",
      type: text,
      current: "local",
      content: true,
      external: false,
      suggestedName: "text",
      expression: "$local.t_title",
    },
    {
      nodeId: "n_note",
      nodeType: "Text",
      prop: "text",
      type: text,
      current: "local",
      content: true,
      external: false,
      suggestedName: "text",
      expression: "$local.t_note",
    },
    {
      nodeId: "n_img",
      nodeType: "Image",
      prop: "src",
      type: { type: "asset", content: true } as Type,
      current: "content",
      content: true,
      external: true,
      suggestedName: "src",
      restricted: true,
    },
    {
      nodeId: "n_rep",
      nodeType: "Repeat",
      prop: "items",
      type: { type: "list", content: true } as unknown as Type,
      current: "data",
      content: true,
      external: true,
      suggestedName: "items",
    },
  ],
  localKeys: [
    { key: "t_title", nodeIds: ["n_title"], usedOutside: false },
    { key: "t_note", nodeIds: ["n_note"], usedOutside: true },
  ],
  slotRanges: [
    {
      parentId: "n_block",
      nodeIds: ["n_title", "n_note", "n_btn"],
      types: ["Heading", "Text", "Button"],
    },
  ],
  slotPositions: [{ parentId: "n_block", maxIndex: 3 }],
  sourceListTypes: [list],
  effectiveMode: "SYSTEM",
};

describe("STU-403/404/405/413: wizard model", () => {
  it("defaults external and content properties to inputs with unique names", () => {
    const w = initialWizard(plan);
    expect(w.inputs.map((i) => [i.name, i.enabled])).toEqual([
      ["text", true],
      ["text2", true],
      ["src", true],
      ["items", true],
    ]);
    expect(validPlan(structuredClone(plan), "n_block")).toEqual(plan);
    expect(() => validPlan({ ...plan, candidates: [{}] }, "n_block")).toThrow();
    expect(() => validPlan(plan, "n_other")).toThrow();
  });
  it("reports name, external, list type, slot and limit problems", () => {
    const w = initialWizard(plan);
    const codes = () => wizardProblems(plan, w).map((p) => p.code);
    expect(codes()).toEqual(["NAME_INVALID", "INPUT_LIST_TYPE_REQUIRED"]);
    w.name = "Showcase";
    w.inputs[3]!.declared = { type: "list", listType: 0 };
    expect(codes()).toEqual([]);
    w.inputs[2]!.enabled = false;
    w.inputs[1]!.name = "text";
    w.description = "x".repeat(501);
    w.slots = [
      { name: "footer", parentId: "n_block", nodeIds: ["n_btn", "n_title"], index: 0, max: 1 },
      { name: "footer", parentId: "n_block", nodeIds: ["n_btn"], index: 0 },
    ];
    expect(codes()).toEqual(
      expect.arrayContaining([
        "DESCRIPTION_INVALID",
        "INPUT_NAME_INVALID",
        "INPUT_EXTERNAL_REQUIRED",
        "SLOT_NAME_INVALID",
        "SLOT_OVERLAP",
        "SLOT_HAS_INPUT",
        "SLOT_MAX_INVALID",
      ]),
    );
    w.slots = Array.from({ length: 9 }, (_, i) => ({
      name: "s" + i,
      parentId: "n_block",
      nodeIds: [],
      index: 0,
    }));
    w.inputs = Array.from({ length: 33 }, (_, i) => ({
      id: "new:" + i,
      name: "i" + i,
      enabled: true,
      required: false,
      declared: { type: "text" },
    }));
    expect(codes()).toEqual(expect.arrayContaining(["SLOT_LIMIT", "INPUT_LIMIT"]));
  });
  it("rejects a required input without a value on the instance", () => {
    const empty: ExtractPlan = {
      ...plan,
      candidates: [{ ...plan.candidates[0]!, current: "none", content: false }],
    };
    const w = initialWizard(empty);
    w.name = "X";
    w.inputs[0]!.enabled = true;
    w.inputs[0]!.required = true;
    expect(wizardProblems(empty, w).map((p) => p.code)).toEqual(["INPUT_REQUIRED_EMPTY"]);
  });
  it("builds the payload: list item type, declared inputs, slots and Q-4 decisions", () => {
    const w = initialWizard(plan);
    w.name = " Showcase ";
    w.description = " Витрина ";
    w.inputs[1]!.enabled = false; // t_note stays shared text
    w.inputs[0]!.enabled = false; // t_title too
    w.inputs[3]!.declared = { type: "list", listType: 0 };
    w.inputs.push(
      {
        id: "new:a",
        name: "products",
        enabled: true,
        required: true,
        declared: { type: "list", listType: 0 },
      },
      { id: "new:b", name: "cover", enabled: true, required: false, declared: { type: "image" } },
      { id: "new:c", name: "label", enabled: true, required: false, declared: { type: "text" } },
    );
    w.slots = [
      { name: "footer", parentId: "n_block", nodeIds: ["n_btn"], index: 0, max: 2 },
      { name: "badge", parentId: "n_block", nodeIds: [], index: 1 },
    ];
    expect(localChoices(plan, w)).toEqual([
      { key: "t_title", movable: true, shared: true },
      { key: "t_note", movable: false, shared: true },
    ]);
    w.local = { t_title: "move", t_note: "move" };
    const op = extractOperation("page", plan, w);
    expect(op).toEqual({
      type: "component.extract",
      target: "page",
      payload: {
        nodeId: "n_block",
        name: "Showcase",
        description: "Витрина",
        inputs: {
          src: { from: [{ nodeId: "n_img", prop: "src" }] },
          items: {
            from: [{ nodeId: "n_rep", prop: "items" }],
            def: { of: (list as { of: unknown }).of },
          },
          products: { def: list, required: true },
          cover: { def: { type: "asset", assetKind: "image" } },
          label: { def: { type: "text" } },
        },
        slots: {
          footer: { nodeIds: ["n_btn"], max: 2 },
          badge: { nodeIds: [], at: { parentId: "n_block", index: 1 }, min: 0 },
        },
        // Only the key the page no longer needs is moved (Q-4: copy by default).
        localContent: { t_title: "move" },
      },
    });
    // The optimistic preview leaves the document to the server.
    const doc = fixture().record.body;
    expect(applyOperations(doc, [op])).toEqual(doc);
  });
  it("a property that becomes an input keeps its $local key on the page", () => {
    const w = initialWizard(plan);
    expect(localChoices(plan, w)).toEqual([]);
    w.slots = [{ name: "footer", parentId: "n_block", nodeIds: ["n_title"], index: 0 }];
    w.inputs[0]!.enabled = false;
    expect(localChoices(plan, w)).toEqual([]);
  });
  it("previews the future component structure with slots", () => {
    const doc: IrDocument = {
      irVersion: "1.0",
      kind: "page",
      root: "n_root",
      nodes: {
        n_root: { id: "n_root", type: "Box", children: ["n_block"] },
        n_block: { id: "n_block", type: "Stack", children: ["n_title", "n_btn"] },
        n_title: { id: "n_title", type: "Heading" },
        n_btn: { id: "n_btn", type: "Button" },
      },
    };
    const w = initialWizard(plan);
    w.slots = [
      { name: "footer", parentId: "n_block", nodeIds: ["n_btn"], index: 0 },
      { name: "badge", parentId: "n_block", nodeIds: [], index: 0 },
    ];
    expect(previewTree(doc, plan, w)).toEqual([
      { depth: 0, label: "Stack" },
      { depth: 1, label: "Slot · badge" },
      { depth: 1, label: "Heading" },
      { depth: 1, label: "Slot · footer" },
    ]);
  });
});

describe("STU-414/415: Composed contracts and $props", () => {
  const component: IrDocument = {
    irVersion: "1.0",
    kind: "component",
    root: "n_r",
    meta: { title: "Card" },
    inputs: { title: text, products: list } as NonNullable<IrDocument["inputs"]>,
    slotDefs: { footer: { max: 1 } },
    nodes: {
      n_r: { id: "n_r", type: "Stack", children: ["n_rep"] },
      n_rep: {
        id: "n_rep",
        type: "Repeat",
        bindings: { items: "$props.products" },
        children: ["n_t"],
      },
      n_t: { id: "n_t", type: "Text" },
    },
  };
  it("instances expose inputs; titles and refs come from resolved components", () => {
    const id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c33";
    registerComponents({ [id]: component, other: { ...component, kind: "page" } });
    const session = fixture();
    const instance: Node = {
      id: "n_i",
      type: "Composed",
      ref: { component: id, version: "live" },
      props: { title: "x" },
    };
    const page: IrDocument = structuredClone(session.record.body);
    page.nodes.n_i = instance;
    page.nodes.n_root!.children = [...page.nodes.n_root!.children!, "n_i"];
    expect(Object.keys(nodeContract(instance, session.manifest)!.props!)).toEqual([
      "title",
      "products",
    ]);
    expect(nodeContract(instance, session.manifest)!.slots).toEqual({ footer: { max: 1 } });
    expect(nodeContract(undefined, session.manifest)).toBeUndefined();
    expect(componentTitle(instance)).toBe("Card");
    expect(layers(page).find((l) => l.id === "n_i")!.label).toBe("Card");
    expect(fields(page, instance, session.manifest).map((f) => f.name)).toEqual([
      "title",
      "products",
    ]);
    expect(componentRefs(page)).toEqual([id]);
    page.nodes.n_i = { ...instance, ref: { component: id, version: 2 } };
    expect(componentRefs(page)).toEqual([]);
    registerComponents({});
    expect(componentTitle(instance)).toBeUndefined();
  });
  it("component inputs are typed data paths and lists drive Repeat items", () => {
    const manifest = fixture().manifest;
    expect(resultFields(component, manifest).fields.map((f) => f.path)).toEqual(
      expect.arrayContaining(["$props.title", "$props.products"]),
    );
    expect(itemScope(component, manifest, "n_t")).toMatchObject({ repeat: "n_rep" });
    expect(bindingFields(component, manifest, "n_t").fields.map((f) => f.path)).toContain(
      "$item.title",
    );
    expect(directPath.test("$props.products")).toBe(true);
    expect(directPath.test("$props")).toBe(false);
  });
});
