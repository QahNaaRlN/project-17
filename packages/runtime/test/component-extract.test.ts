import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import type { IrDocument } from "@cms/ir";
import { preparePage, type PlanNode } from "../src/plan.js";

interface Fixture {
  before: IrDocument;
  page: IrDocument;
  component: IrDocument;
  payload: { componentId: string };
}
const fixture = JSON.parse(
  readFileSync(new URL("../../ir/fixtures/component-extract.json", import.meta.url), "utf8"),
) as Fixture;

/** Plan without instance-scoped keys: extraction must not change what is rendered. */
function shape(node: PlanNode): unknown {
  return {
    id: node.id,
    type: node.type,
    props: node.props,
    className: node.className,
    children: node.children.map(shape),
  };
}

describe("STU-412: component.extract render equivalence", () => {
  const scope = {
    context: { locale: "ru" },
    content: { cover: { assetId: "0192f1c5-0000-7000-8000-0000000000aa" } },
  };
  it("renders the same tree before and after extraction", () => {
    const tokens = { spacing: { md: "16px" } };
    const before = preparePage(fixture.before, { scope, tokens });
    const after = preparePage(fixture.page, {
      scope,
      tokens,
      components: { [fixture.payload.componentId]: fixture.component },
    });
    expect(before.diagnostics).toEqual([]);
    expect(after.diagnostics).toEqual([]);
    expect(after.roots.map(shape)).toEqual(before.roots.map(shape));
    expect(after.css).toBe(before.css);
    // Ключи экземпляра отличаются только префиксом экземпляра.
    expect(after.roots[0]!.children[1]!.key).not.toBe(before.roots[0]!.children[1]!.key);
  });
  it("does not rely on the page scope inherited by the component", () => {
    const nodes = Object.values(fixture.component.nodes);
    const text = JSON.stringify(nodes);
    expect(text).not.toMatch(/\$(content|data|item|index|each)\b/);
    expect(fixture.component.kind).toBe("component");
    expect(fixture.page.nodes).not.toHaveProperty(fixture.component.root);
  });
});
