import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import {
  DiagnosticCode,
  normalize,
  NormalizeError,
  toNested,
  validateDocument,
  type IrDocument,
  type NestedDocument,
} from "../src/index.js";

const fixture = (name: string): IrDocument =>
  JSON.parse(
    readFileSync(fileURLToPath(new URL(`../fixtures/valid/${name}.json`, import.meta.url)), "utf8"),
  ) as IrDocument;

const nested: NestedDocument = {
  irVersion: "1.0",
  kind: "page",
  root: {
    type: "Container",
    children: [
      { type: "Heading", props: { level: 1 }, bindings: { text: "$content.title" } },
      {
        id: "n_modal1",
        type: "Modal",
        children: [{ type: "Text", bindings: { text: "$content.lead" } }],
      },
      {
        type: "Button",
        on: { click: { action: "openModal", args: { target: { lit: "n_modal1" } } } },
      },
    ],
  },
};

describe("normalize", () => {
  it("строит валидный нормализованный документ и назначает ID", () => {
    const doc = normalize(nested);
    expect(validateDocument(doc).diagnostics).toEqual([]);
    expect(Object.keys(doc.nodes)).toHaveLength(5);
    for (const id of Object.keys(doc.nodes)) expect(id).toMatch(/^(n_[a-z0-9]{8}|n_modal1)$/);
  });

  it("сохраняет заданные ID и порядок детей", () => {
    const doc = normalize(nested);
    const root = doc.nodes[doc.root]!;
    expect(root.children).toHaveLength(3);
    expect(root.children![1]).toBe("n_modal1");
    expect(doc.nodes["n_modal1"]!.type).toBe("Modal");
  });

  it("использует переданный генератор и не повторяет занятые ID", () => {
    let i = 0;
    const doc = normalize(nested, { generateId: () => `n_gen${i++}` });
    expect(Object.keys(doc.nodes).sort()).toEqual([
      "n_gen0",
      "n_gen1",
      "n_gen2",
      "n_gen3",
      "n_modal1",
    ]);
  });

  it("отклоняет повторяющиеся и недопустимые ID", () => {
    const dup: NestedDocument = {
      irVersion: "1.0",
      kind: "page",
      root: {
        id: "n_same",
        type: "Box",
        children: [
          { id: "n_same", type: "Box" },
          { id: "x", type: "Box" },
        ],
      },
    };
    try {
      normalize(dup);
      expect.unreachable();
    } catch (e) {
      expect(e).toBeInstanceOf(NormalizeError);
      const codes = (e as NormalizeError).diagnostics.map((d) => [d.code, d.pointer]);
      expect(codes).toEqual([
        [DiagnosticCode.NodeIdDuplicate, "/root/children/0/id"],
        [DiagnosticCode.SchemaViolation, "/root/children/1/id"],
      ]);
    }
  });

  it("обрабатывает слоты", () => {
    const doc = normalize({
      irVersion: "1.0",
      kind: "page",
      root: {
        type: "ProductCard",
        slots: { footer: [{ type: "Button" }, { type: "Link" }] },
      },
    });
    const root = doc.nodes[doc.root]!;
    expect(root.slots!["footer"]).toHaveLength(2);
    expect(validateDocument(doc).valid).toBe(true);
  });
});

describe("toNested", () => {
  it.each(["page-collection", "component-showcase", "minimal"])(
    "%s: normalize(toNested(doc)) == doc",
    (name) => {
      const doc = fixture(name);
      expect(normalize(toNested(doc))).toEqual(doc);
    },
  );

  it("отклоняет цикл", () => {
    const doc = {
      irVersion: "1.0",
      kind: "page",
      root: "n_root",
      nodes: {
        n_root: { id: "n_root", type: "Box", children: ["n_aaaa"] },
        n_aaaa: { id: "n_aaaa", type: "Box", children: ["n_root"] },
      },
    } as IrDocument;
    expect(() => toNested(doc)).toThrow(NormalizeError);
  });
});
