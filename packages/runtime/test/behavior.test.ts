import { expect, it, vi } from "vitest";
import type { IrDocument } from "@cms/ir";
import { preparePage } from "../src/plan.js";
import { preparePageWithData } from "../src/data-plan.js";
import { loadSources } from "../src/sources.js";
import { sourceFailures } from "../src/source-error.js";
import { sourceError } from "../src/source-error.js";
import { actionTarget, executeActions, planEvents, type RuntimeAction } from "../src/actions.js";
it("STU-247/250: SourceError never becomes empty/exists truth and independent sections survive", () => {
  const doc: IrDocument = {
    irVersion: "1.0",
    kind: "page",
    root: "root",
    nodes: {
      root: { id: "root", type: "Stack", children: ["safe", "conditional", "action"] },
      safe: { id: "safe", type: "Text", props: { text: "independent" } },
      conditional: {
        id: "conditional",
        type: "Text",
        props: { text: "hidden" },
        when: { op: "empty", arg: "$data.bad.items" },
      },
      action: {
        id: "action",
        type: "Button",
        props: { label: "test" },
        on: { click: { action: "test.use", args: { value: "$data.bad.items" } } },
      },
    },
  };
  const definition: RuntimeAction = {
    name: "test.use",
    args: { value: { type: "string" } },
    handler: vi.fn(),
  };
  const plan = preparePage(doc, {
    actions: [definition],
    scope: { data: { bad: { error: { code: "CMS_SOURCE_TIMEOUT", secret: "private" } } } },
  });
  expect(plan.roots[0]!.children.map((n) => n.id)).toEqual(["safe"]);
  expect(plan.diagnostics).toEqual([
    { code: "CMS_SOURCE_TIMEOUT", nodeId: "conditional" },
    { code: "CMS_ACTION_SOURCE_UNAVAILABLE", nodeId: "action" },
  ]);
  expect(JSON.stringify(plan)).not.toContain("private");
  expect(definition.handler).not.toHaveBeenCalled();
  for (const op of ["exists", "empty"] as const) {
    doc.nodes.conditional!.when = { op, arg: "$data.bad" };
    expect(
      preparePage(doc, { scope: { data: { bad: { error: { code: "private" } } } } }).diagnostics[0]!
        .code,
    ).toBe("CMS_SOURCE_FAILED");
  }
});
it("STU-250: ordinary error fields and literal strings are data, source diagnostics are safe", () => {
  const scope = { data: { a: { error: { code: "private" }, items: [] } } };
  for (const value of [
    undefined,
    1,
    { lit: "$data.a" },
    { default: "$data.a" },
    "$data.a.items",
    { expr: "$data.a.items" },
  ])
    expect(sourceError(value, scope)).toBeUndefined();
  expect(
    sourceError(
      { args: ["$data.a.items"] },
      { data: { a: { error: { code: "CMS_SOURCE_FAILED" } } } },
    ),
  ).toBe("CMS_SOURCE_FAILED");
  expect(sourceError("$data.a", { data: { a: { error: null } } })).toBe("CMS_SOURCE_FAILED");
  expect(sourceError("$data.a", { data: { a: [] } })).toBeUndefined();
});
it("STU-250: loader failure identity survives JSON without confusing a legitimate sole error field", async () => {
  const sources = [
    {
      name: "normal",
      params: {},
      result: {
        type: "object" as const,
        fields: {
          error: { type: "object" as const, fields: { code: { type: "string" as const } } },
        },
      },
      loader: () => ({ error: { code: "CMS_SOURCE_TIMEOUT" } }),
    },
    {
      name: "failure",
      params: {},
      result: { type: "string" as const },
      loader: () => {
        throw Error("private");
      },
    },
  ];
  const data = await loadSources(
    { good: { source: "normal" }, bad: { source: "failure" } },
    sources,
    {},
  );
  const errors = sourceFailures(data);
  expect(errors).toEqual({ bad: "CMS_SOURCE_FAILED" });
  const scope = { data: JSON.parse(JSON.stringify(data)), sourceErrors: errors };
  expect(sourceError("$data.good.error.code", scope)).toBeUndefined();
  expect(sourceError("$data.bad", scope)).toBe("CMS_SOURCE_FAILED");
  const doc: IrDocument = {
    irVersion: "1.0",
    kind: "page",
    root: "root",
    dataSources: { good: { source: "normal" }, bad: { source: "failure" } },
    nodes: {
      root: { id: "root", type: "Stack", children: ["good", "bad"] },
      good: {
        id: "good",
        type: "Text",
        bindings: { text: "$data.good.error.code" },
        when: { op: "exists", arg: "$data.good" },
      },
      bad: { id: "bad", type: "Text", when: { op: "empty", arg: "$data.bad" } },
    },
  };
  const plan = await preparePageWithData(doc, { sources });
  expect(plan.roots[0]!.children[0]!.props.text).toBe("CMS_SOURCE_TIMEOUT");
  expect(plan.diagnostics).toEqual([{ code: "CMS_SOURCE_FAILED", nodeId: "bad" }]);
});
it("STU-239/249/250: chains run sequentially, stop on error or a new plan and never retry effects", async () => {
  const order: string[] = [],
    error = vi.fn();
  const actions: RuntimeAction[] = [
    {
      name: "test.first",
      args: {},
      handler: async () => {
        order.push("one");
        await Promise.resolve();
        order.push("two");
      },
    },
    {
      name: "test.fail",
      args: {},
      handler: () => {
        order.push("fail");
        throw new Error("private handler error");
      },
    },
    {
      name: "test.last",
      args: {},
      handler: () => {
        order.push("last");
      },
    },
  ];
  const chain = actions.map((a) => ({ action: a.name, args: {} }));
  const context = { app: {}, node: "button", instance: "repeat:one/button" };
  expect(await executeActions(chain, actions, context, { mode: "design" })).toBe(false);
  expect(order).toEqual([]);
  expect(await executeActions(chain, actions, context, { onError: error })).toBe(false);
  expect(order).toEqual(["one", "two", "fail"]);
  expect(error).toHaveBeenCalledWith({
    code: "CMS_ACTION_FAILED",
    action: "test.fail",
    node: "button",
    instance: "repeat:one/button",
  });
  order.length = 0;
  let active = true;
  actions[0]!.handler = () => {
    order.push("one");
    active = false;
  };
  expect(await executeActions(chain, actions, context, { isActive: () => active })).toBe(false);
  expect(order).toEqual(["one"]);
});
it("STU-242/248: Repeat action targets address the current item or outer document", () => {
  const doc: IrDocument = {
    irVersion: "1.0",
    kind: "page",
    root: "root",
    nodes: {
      root: { id: "root", type: "Stack", children: ["repeat", "outer"] },
      repeat: {
        id: "repeat",
        type: "Repeat",
        children: ["card"],
        slots: { empty: ["empty"] },
      },
      button: {
        id: "button",
        type: "Button",
        on: {
          click: [
            { action: "scrollTo", args: { target: { lit: "target" } } },
            { action: "navigate", args: { to: { lit: { kind: "anchor", node: "outer" } } } },
          ],
        },
      },
      card: { id: "card", type: "Stack", children: ["button", "target"] },
      target: { id: "target", type: "Text" },
      outer: { id: "outer", type: "Text" },
      empty: { id: "empty", type: "Text" },
    },
  };
  const events = planEvents(doc.nodes.button!, {}, ["click"], [], doc.nodes, "repeat:one/", [])!;
  expect(events.click![0]!.args.target).toBe("repeat:one/target");
  expect(events.click![1]!.args.to).toEqual({ kind: "url", url: "#outer" });
  expect(actionTarget("empty", "outer", doc.nodes, "repeat/empty/")).toBe("outer");
  expect(() => actionTarget("outer", "target", doc.nodes, "")).toThrow("CMS_ACTION_TARGET_INVALID");
  expect(() => actionTarget("button", "missing", doc.nodes, "repeat:one/")).toThrow(
    "CMS_ACTION_TARGET_INVALID",
  );
});
