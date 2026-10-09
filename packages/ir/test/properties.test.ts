// Свойства, которые должны выполняться для любых входных данных (docs/testing.md §3.3).
import fc from "fast-check";
import { describe, expect, it } from "vitest";
import {
  DiagnosticCode,
  normalize,
  toNested,
  validateDocument,
  type IrDocument,
  type NestedDocument,
  type NestedNode,
} from "../src/index.js";

// Небольшой набор типов и свойств: цель — форма дерева, а не полнота каталога примитивов.
const leafNode: fc.Arbitrary<NestedNode> = fc.record(
  {
    type: fc.constantFrom("Text", "Heading", "Image", "Button"),
    name: fc.string({ maxLength: 20 }),
    bindings: fc.constantFrom(
      { text: "$content.title" },
      { text: { expr: "$item.price", format: { fn: "currency" } } },
      { src: "$props.image" },
    ),
    design: fc.constantFrom(
      { color: "text" },
      { padding: { raw: "12px" } },
      { gap: { base: "md", lg: "xl" } },
    ),
  },
  { requiredKeys: ["type"] },
);

const { tree } = fc.letrec<{ tree: NestedNode }>((tie) => ({
  tree: fc.oneof(
    { depthSize: "small", withCrossShrink: true },
    leafNode,
    fc.record(
      {
        type: fc.constantFrom("Box", "Stack", "Grid"),
        children: fc.array(tie("tree"), { maxLength: 4 }),
      },
      { requiredKeys: ["type", "children"] },
    ),
    fc.record({
      type: fc.constant("ProductCard"),
      slots: fc.dictionary(
        fc.constantFrom("footer", "media"),
        fc.array(tie("tree"), { maxLength: 2 }),
        {
          maxKeys: 2,
        },
      ),
    }),
  ),
}));

const nestedDocument: fc.Arbitrary<NestedDocument> = tree.map((root) => ({
  irVersion: "1.0",
  kind: "page",
  root,
}));

/** Все ссылки на детей документа: [родитель, путь к массиву, индекс]. */
function childLinks(doc: IrDocument): { parent: string; list: string[]; index: number }[] {
  const links: { parent: string; list: string[]; index: number }[] = [];
  for (const [id, node] of Object.entries(doc.nodes)) {
    node.children?.forEach((_, index) => links.push({ parent: id, list: node.children!, index }));
    for (const list of Object.values(node.slots ?? {})) {
      list.forEach((_, index) => links.push({ parent: id, list, index }));
    }
  }
  return links;
}

describe("свойства", () => {
  it("validateDocument не бросает исключений на любом JSON", () => {
    fc.assert(
      fc.property(fc.jsonValue(), (value) => {
        const result = validateDocument(value);
        expect(typeof result.valid).toBe("boolean");
      }),
      { numRuns: 500 },
    );
  });

  it("validateDocument не бросает исключений на документах со случайными узлами", () => {
    const nodes = fc.dictionary(fc.string({ minLength: 1, maxLength: 8 }), fc.jsonValue(), {
      maxKeys: 10,
    });
    fc.assert(
      fc.property(nodes, fc.string(), (n, root) => {
        validateDocument({ irVersion: "1.0", kind: "page", root, nodes: n });
      }),
      { numRuns: 500 },
    );
  });

  it("normalize даёт валидный документ с уникальными ID для любого дерева", () => {
    fc.assert(
      fc.property(nestedDocument, (nested) => {
        const doc = normalize(nested);
        expect(validateDocument(doc).diagnostics).toEqual([]);
      }),
    );
  });

  it("normalize(toNested(doc)) == doc", () => {
    fc.assert(
      fc.property(nestedDocument, (nested) => {
        const doc = normalize(nested);
        expect(normalize(toNested(doc))).toEqual(doc);
      }),
    );
  });

  it("удаление любой ссылки на ребёнка делает его сиротой", () => {
    fc.assert(
      fc.property(nestedDocument, fc.nat(), (nested, pick) => {
        const doc = normalize(nested);
        const links = childLinks(doc);
        fc.pre(links.length > 0);
        const link = links[pick % links.length]!;
        const [orphan] = link.list.splice(link.index, 1);
        const result = validateDocument(doc);
        expect(result.diagnostics).toContainEqual(
          expect.objectContaining({ code: DiagnosticCode.NodeOrphan, nodeId: orphan }),
        );
      }),
    );
  });

  it("вложение узла во второго родителя обнаруживается", () => {
    fc.assert(
      fc.property(nestedDocument, fc.nat(), fc.nat(), (nested, pickChild, pickParent) => {
        const doc = normalize(nested);
        const nonRoot = Object.keys(doc.nodes).filter((id) => id !== doc.root);
        const containers = Object.values(doc.nodes).filter((n) => n.children !== undefined);
        fc.pre(nonRoot.length > 0 && containers.length > 0);
        const child = nonRoot[pickChild % nonRoot.length]!;
        const parent = containers[pickParent % containers.length]!;
        parent.children!.push(child);
        const codes = validateDocument(doc).diagnostics.map((d) => d.code);
        // Если новый родитель — потомок ребёнка, получается цикл, а не второй родитель.
        expect(
          codes.includes(DiagnosticCode.NodeMultipleParents) ||
            codes.includes(DiagnosticCode.NodeCycle),
        ).toBe(true);
      }),
    );
  });
});
