import { createElement } from "react";
import { renderToString } from "react-dom/server";
import { expect, it } from "vitest";
import { createRegistry, nativeDefinitions, t } from "@cms/component-registry";
import {
  CmsPage,
  createReactRegistry,
  defineReactComponent,
  type NativeProps,
} from "../src/index.js";
const card = defineReactComponent({
  name: "Card",
  props: { label: { type: "string", required: true }, count: { type: "number" } },
  component: ({ values, className, children }) =>
    createElement(
      "article",
      { className },
      values.label.toUpperCase(),
      values.count ?? "unknown",
      children,
    ),
  validate: (p) => typeof p.label === "string",
});
it("MF-010/MF-012: renders the implementation selected by the shared registry", () => {
  const registry = createRegistry({ components: [card] });
  const react = createReactRegistry(registry);
  expect(react.Card).toBe(card.component);
  expect(nativeDefinitions(registry).Card!.validate!({ label: "same component" })).toBe(true);
  const html = renderToString(
    createElement(CmsPage, {
      registry: react,
      plan: {
        roots: [
          {
            id: "n_card",
            key: "n_card",
            type: "Card",
            props: { label: "same component" },
            className: "card",
            children: [],
          },
        ],
        css: "",
        diagnostics: [],
      },
    }),
  );
  expect(html).toContain('class="card"');
  expect(html).toContain("SAME COMPONENT");
  expect(html).toContain("unknown");
});
it("MF-010: empty registry yields no React registrations", () => {
  expect(createReactRegistry(createRegistry({ components: [] }))).toEqual({});
});
// Compile-time assertions are checked by pnpm typecheck; they never execute.
function contractTypeChecks() {
  defineReactComponent({
    name: "DslCard",
    props: { title: t.text().required(), choices: t.list(t.enum(["a", "b"])).required() },
    component: ({ values }) => {
      const title: string = values.title;
      const choice: "a" | "b" = values.choices[0]!;
      return title + choice;
    },
  });
  defineReactComponent({
    name: "BadDslCard",
    props: { title: t.text().required() },
    // @ts-expect-error MF-012: React values must match the DSL declaration.
    component: (_p: NativeProps<{ title: number }>) => null,
  });
  defineReactComponent({
    name: "BadCard",
    props: { label: { type: "string", required: true } },
    // @ts-expect-error MF-012: a number implementation cannot consume a declared string.
    component: (_props: NativeProps<{ label: number }>) => null,
  });
  defineReactComponent({
    name: "TypedCard",
    props: {
      choice: { type: "enum", values: ["one", "two"], required: true },
      items: {
        type: "list",
        of: { type: "object", fields: { id: { type: "string", required: true } } },
        required: true,
      },
      image: { type: "asset" },
      visible: { type: "boolean" },
      ref: { type: "reference", schema: "Thing" },
    },
    component: ({ values }) => {
      const choice: "one" | "two" = values.choice;
      const id: string = values.items[0]!.id;
      // @ts-expect-error MF-012: an inferred enum excludes undeclared values.
      const invalid: "three" = values.choice;
      return choice + id + values.image?.assetId + values.ref?.entityId + values.visible + invalid;
    },
  });
}
void contractTypeChecks;
