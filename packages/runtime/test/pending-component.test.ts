import { expect, it } from "vitest";
import type { IrDocument } from "@cms/ir";
import { preparePage } from "../src/plan.js";

const request = "0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11";
const doc: IrDocument = {
  irVersion: "1.0",
  kind: "page",
  root: "n_root",
  nodes: {
    n_root: { id: "n_root", type: "Box", children: ["n_promo"] },
    n_promo: {
      id: "n_promo",
      type: "PromoBanner",
      pending: request,
      props: { title: "Sale" },
      children: ["n_btn"],
    },
    n_btn: { id: "n_btn", type: "Button", props: { label: "Buy" } },
  },
};

it("STU-366: a component placeholder plans no application code and keeps its children", () => {
  let called = false;
  const plan = preparePage(doc, {
    natives: {
      PromoBanner: {
        props: { title: {} },
        validate: () => {
          called = true;
          return true;
        },
      },
    },
  });
  const promo = plan.roots[0]?.children[0];
  expect(promo).toMatchObject({ id: "n_promo", type: "PromoBanner", pending: request, props: {} });
  expect(promo?.children.map((c) => c.type)).toEqual(["Button"]);
  expect(called).toBe(false);
  expect(plan.diagnostics).toContainEqual({ code: "CMS_COMPONENT_PENDING_CAPABILITY", nodeId: "n_promo" });
});

it("STU-366: slots of a placeholder are not supported, like native slots", () => {
  const withSlot = structuredClone(doc);
  withSlot.nodes.n_promo!.children = [];
  withSlot.nodes.n_promo!.slots = { actions: ["n_btn"] };
  const plan = preparePage(withSlot);
  expect(plan.roots[0]?.children).toEqual([]);
  expect(plan.diagnostics.map((d) => d.code)).toContain("CMS_SLOT_UNSUPPORTED");
});
