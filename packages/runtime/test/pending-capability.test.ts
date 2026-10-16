import { expect, it, vi } from "vitest";
import type { IrDocument } from "@cms/ir";
import { preparePage } from "../src/plan.js";
import { executeActions, planEvents } from "../src/actions.js";

it("STU-269: pending disables the whole event while keeping the node and unrelated events", async () => {
  const handler = vi.fn();
  const actions = [{ name: "store.favorite", args: {}, handler }];
  const doc: IrDocument = {
    irVersion: "1.0",
    kind: "page",
    root: "n_button",
    nodes: {
      n_button: {
        id: "n_button",
        type: "Button",
        props: { label: "Favorite" },
        on: {
          click: [
            { action: "store.favorite" },
            { action: "pending:00000000-0000-0000-0000-000000000001" },
            { action: "store.favorite" },
          ],
        },
      },
    },
  };
  const plan = preparePage(doc, { actions });
  expect(plan.roots[0]?.type).toBe("Button");
  expect(plan.roots[0]?.props.label).toBe("Favorite");
  expect(plan.roots[0]?.events?.click).toBeUndefined();
  expect(plan.diagnostics).toEqual([{ code: "CMS_ACTION_PENDING_CAPABILITY", nodeId: "n_button" }]);
  expect(JSON.stringify(plan)).not.toContain("00000000-0000-0000-0000-000000000001");
  const legacy = structuredClone(doc);
  legacy.nodes.n_button!.type = "Text";
  legacy.nodes.n_button!.props = { text: "Preserved legacy" };
  expect(preparePage(legacy).roots[0]?.type).toBe("Text");
  const node = {
    ...doc.nodes.n_button!,
    on: { click: { action: "pending:legacy" }, focus: { action: "store.favorite" } },
  };
  const diagnostic = vi.fn();
  expect(planEvents(node, {}, ["click", "focus"], actions, doc.nodes, "", [], diagnostic)).toEqual({
    focus: [{ action: "store.favorite", args: {} }],
  });
  expect(diagnostic).toHaveBeenCalledTimes(1);
  const error = vi.fn();
  const success = await executeActions(
    [
      { action: "store.favorite", args: {} },
      { action: "pending:legacy", args: {} },
    ],
    actions,
    { app: {}, node: "n_button", instance: "n_button" },
    { onError: error },
  );
  expect(success).toBe(false);
  expect(handler).not.toHaveBeenCalled();
  expect(error).toHaveBeenCalledWith({
    code: "CMS_ACTION_PENDING_CAPABILITY",
    action: "pending",
    node: "n_button",
    instance: "n_button",
  });
});
