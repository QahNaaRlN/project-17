import { describe, expect, it } from "vitest";
import { DiagnosticCode, IR_LIMITS, validateDocument } from "../src/index.js";

const doc = (nodes: Record<string, unknown>, root = "n_root") => ({
  irVersion: "1.0",
  kind: "page",
  root,
  nodes,
});

describe("validateDocument", () => {
  it("отклоняет не-объект без исключений", () => {
    for (const value of [null, 42, "x", []]) {
      const result = validateDocument(value);
      expect(result.valid).toBe(false);
      expect(result.diagnostics[0]?.code).toBe(DiagnosticCode.SchemaViolation);
    }
  });

  it("сообщает одну диагностику на место в документе, а не на каждую ветку oneOf", () => {
    const result = validateDocument(
      doc({ n_root: { id: "n_root", type: "Text", bindings: { text: "$bad scope" } } }),
    );
    const at = result.diagnostics.filter((d) => d.pointer === "/nodes/n_root/bindings/text");
    expect(at).toHaveLength(1);
    expect(at[0]?.nodeId).toBe("n_root");
  });

  it("указывает на лишнее свойство, а не на объект", () => {
    const result = validateDocument({
      ...doc({ n_root: { id: "n_root", type: "Box" } }),
      policy: { foo: 1 },
    });
    expect(result.diagnostics.map((d) => d.pointer)).toContain("/policy/foo");
  });

  it("находит сироту только по вершине поддерева", () => {
    const result = validateDocument(
      doc({
        n_root: { id: "n_root", type: "Box" },
        n_top1: { id: "n_top1", type: "Box", children: ["n_kid1"] },
        n_kid1: { id: "n_kid1", type: "Box" },
      }),
    );
    const orphans = result.diagnostics.filter((d) => d.code === DiagnosticCode.NodeOrphan);
    expect(orphans.map((d) => d.nodeId)).toEqual(["n_top1"]);
  });

  it("сообщает о цикле один раз и не считает его потомков сиротами", () => {
    const result = validateDocument(
      doc({
        n_root: { id: "n_root", type: "Box" },
        n_cyc1: { id: "n_cyc1", type: "Box", children: ["n_cyc2"] },
        n_cyc2: { id: "n_cyc2", type: "Box", children: ["n_cyc1", "n_leaf"] },
        n_leaf: { id: "n_leaf", type: "Box" },
      }),
    );
    expect(result.diagnostics.map((d) => d.code)).toEqual([DiagnosticCode.NodeCycle]);
    expect(result.diagnostics[0]?.params?.["cycle"]).toHaveLength(2);
  });

  it("ограничивает размер документа", () => {
    const big = "x".repeat(IR_LIMITS.maxBodyBytes);
    const result = validateDocument({
      ...doc({ n_root: { id: "n_root", type: "Box" } }),
      meta: { description: big },
    });
    expect(result.diagnostics.some((d) => d.params?.["limit"] === "bodyBytes")).toBe(true);
  });

  it("ограничивает число узлов", () => {
    const nodes: Record<string, unknown> = {
      n_root: { id: "n_root", type: "Box", children: [] as string[] },
    };
    for (let i = 0; i < IR_LIMITS.maxNodes; i++) {
      const id = `n_${i.toString().padStart(5, "0")}`;
      nodes[id] = { id, type: "Box" };
      (nodes["n_root"] as { children: string[] }).children.push(id);
    }
    const result = validateDocument(doc(nodes));
    expect(
      result.diagnostics.some(
        (d) => d.pointer === "/nodes" && d.params?.["keyword"] === "maxProperties",
      ),
    ).toBe(true);
  });
});
