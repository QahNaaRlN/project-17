// Контракт диагностик и вспомогательных функций: то, на что опираются Studio, агент и сервер.
import { describe, expect, it } from "vitest";
import {
  DiagnosticCode,
  IR_LIMITS,
  isNodeId,
  nodeIdFromPointer,
  normalize,
  NormalizeError,
  pointer,
  toNested,
  validateDocument,
  type IrDocument,
} from "../src/index.js";

const doc = (nodes: Record<string, unknown>, extra: Record<string, unknown> = {}) => ({
  irVersion: "1.0",
  kind: "page",
  root: "n_root",
  nodes,
  ...extra,
});
const box = (id: string, children?: string[]) => ({
  id,
  type: "Box",
  ...(children && { children }),
});

function only(result: ReturnType<typeof validateDocument>, code: string) {
  const found = result.diagnostics.filter((d) => d.code === code);
  expect(found).toHaveLength(1);
  return found[0]!;
}

describe("params диагностик", () => {
  it("IR_VERSION_UNSUPPORTED", () => {
    const d = only(
      validateDocument(doc({}, { irVersion: "9.0" })),
      DiagnosticCode.IrVersionUnsupported,
    );
    expect(d.params).toEqual({ version: "9.0", supported: ["1.0"] });
  });

  it("NODE_NOT_FOUND для корня и ребёнка", () => {
    expect(
      only(validateDocument(doc({ n_aaaa: box("n_aaaa") })), DiagnosticCode.NodeNotFound).params,
    ).toEqual({
      nodeId: "n_root",
    });
    const missing = only(
      validateDocument(doc({ n_root: box("n_root", ["n_gone"]) })),
      DiagnosticCode.NodeNotFound,
    );
    expect(missing).toMatchObject({ nodeId: "n_root", params: { childId: "n_gone" } });
  });

  it("NODE_ID_MISMATCH", () => {
    const d = only(
      validateDocument(doc({ n_root: { ...box("n_root"), id: "n_other" } })),
      DiagnosticCode.NodeIdMismatch,
    );
    expect(d.params).toEqual({ key: "n_root", id: "n_other" });
  });

  it("NODE_MULTIPLE_PARENTS и NODE_CYCLE для корня", () => {
    const multi = only(
      validateDocument(
        doc({
          n_root: box("n_root", ["n_aaaa", "n_bbbb"]),
          n_aaaa: box("n_aaaa", ["n_cccc"]),
          n_bbbb: box("n_bbbb", ["n_cccc"]),
          n_cccc: box("n_cccc"),
        }),
      ),
      DiagnosticCode.NodeMultipleParents,
    );
    expect(multi).toMatchObject({ nodeId: "n_cccc", params: { parents: ["n_aaaa", "n_bbbb"] } });
    const rootCycle = only(
      validateDocument(
        doc({ n_root: box("n_root", ["n_aaaa"]), n_aaaa: box("n_aaaa", ["n_root"]) }),
      ),
      DiagnosticCode.NodeCycle,
    );
    expect(rootCycle.params).toEqual({ cycle: ["n_root"] });
  });

  it("NODE_CYCLE сообщается от наименьшего ID и перечисляет цикл", () => {
    const d = only(
      validateDocument(
        doc({
          n_root: box("n_root"),
          n_zzzz: box("n_zzzz", ["n_mmmm"]),
          n_mmmm: box("n_mmmm", ["n_aaaa"]),
          n_aaaa: box("n_aaaa", ["n_zzzz"]),
        }),
      ),
      DiagnosticCode.NodeCycle,
    );
    expect(d.nodeId).toBe("n_aaaa");
    expect([...(d.params!["cycle"] as string[])].sort()).toEqual(["n_aaaa", "n_mmmm", "n_zzzz"]);
  });

  it("LIMIT_EXCEEDED по глубине — один раз, даже при большом превышении", () => {
    const nodes: Record<string, unknown> = {};
    const n = IR_LIMITS.maxDepth + 10;
    for (let i = 0; i < n; i++) {
      const id = i === 0 ? "n_root" : `n_${String(i).padStart(4, "0")}`;
      const next = `n_${String(i + 1).padStart(4, "0")}`;
      nodes[id] = box(id, i + 1 < n ? [next] : undefined);
    }
    const d = only(validateDocument(doc(nodes)), DiagnosticCode.LimitExceeded);
    expect(d.params).toEqual({ limit: "depth", max: IR_LIMITS.maxDepth });
  });

  it("PROP_BOTH_STATIC_AND_BOUND", () => {
    const d = only(
      validateDocument(
        doc({
          n_root: {
            id: "n_root",
            type: "Text",
            props: { text: "x", as: "p" },
            bindings: { text: "$content.a" },
          },
        }),
      ),
      DiagnosticCode.PropBothStaticAndBound,
    );
    expect(d.params).toEqual({ prop: "text" });
  });

  it("диагностика схемы содержит nodeId, keyword и русский текст", () => {
    const d = only(
      validateDocument(
        doc({ n_root: { id: "n_root", type: "Box", zone: { id: "z", mode: "SYSTEM", extra: 1 } } }),
      ),
      DiagnosticCode.SchemaViolation,
    );
    expect(d).toMatchObject({ nodeId: "n_root", pointer: "/nodes/n_root/zone/extra" });
    expect(d.params?.["keyword"]).toBe("additionalProperties");
    expect(d.message).toBe("не должно иметь дополнительных полей");
  });

  it("недопустимое имя свойства и запрещённое поле объясняются", () => {
    const names = validateDocument(
      doc(
        { n_root: box("n_root") },
        { localContent: { bad: { type: "text", value: { ru: "x" } } } },
      ),
    );
    expect(names.diagnostics[0]?.message).toMatch(
      /^недопустимое имя свойства bad: должно соответствовать образцу/,
    );
    const forbidden = validateDocument(doc({ n_root: box("n_root") }, { inputs: {} }));
    expect(forbidden.diagnostics[0]?.message).toBe("поле недопустимо в этом контексте");
  });
});

describe("глубина условия", () => {
  const nest = (op: "and" | "or" | "not", depth: number): unknown => {
    let p: unknown = { op: "exists", arg: "$content.a" };
    for (let i = 1; i < depth; i++)
      p = op === "not" ? { op, arg: p } : { op, args: [{ op: "exists", arg: "$content.b" }, p] };
    return p;
  };
  it.each(["and", "or", "not"] as const)("%s: 8 допустимо, 9 — LIMIT_EXCEEDED", (op) => {
    const at = (depth: number) =>
      validateDocument(
        doc({ n_root: { ...box("n_root"), when: nest(op, depth) } }),
      ).diagnostics.map((d) => d.code);
    expect(at(IR_LIMITS.maxPredicateDepth)).toEqual([]);
    expect(at(IR_LIMITS.maxPredicateDepth + 1)).toEqual([DiagnosticCode.LimitExceeded]);
  });
});

describe("normalize: ID", () => {
  it("находит дубликат ID внутри слотов", () => {
    expect(() =>
      normalize({
        irVersion: "1.0",
        kind: "page",
        root: {
          id: "n_card",
          type: "ProductCard",
          slots: { footer: [{ id: "n_card", type: "Button" }] },
        },
      }),
    ).toThrow(NormalizeError);
  });

  it("передаёт генератору уже выданные ID", () => {
    const seen: number[] = [];
    let i = 0;
    const result = normalize(
      {
        irVersion: "1.0",
        kind: "page",
        root: { type: "Box", children: [{ type: "Box" }, { type: "Box" }] },
      },
      {
        generateId: (taken) => {
          seen.push(taken.size);
          return `n_gen${i++}`;
        },
      },
    );
    expect(seen).toEqual([0, 1, 2]);
    expect(Object.keys(result.nodes)).toEqual(["n_gen0", "n_gen1", "n_gen2"]);
  });

  it("ошибка NormalizeError содержит текст всех диагностик", () => {
    try {
      normalize({ irVersion: "1.0", kind: "page", root: { id: "bad", type: "Box" } });
      expect.unreachable();
    } catch (e) {
      expect((e as Error).message).toBe("Недопустимый ID узла: bad");
    }
  });

  it("toNested сообщает об отсутствующем узле", () => {
    const broken = doc({ n_root: box("n_root", ["n_gone"]) }) as IrDocument;
    expect(() => toNested(broken)).toThrow(/Узел n_gone отсутствует/);
  });

  it("toNested разрешает один узел в разных ветвях только при повторном обходе", () => {
    // Узел, встречающийся дважды, — не цикл: toNested дублирует его, а не падает.
    const shared = doc({
      n_root: box("n_root", ["n_aaaa", "n_bbbb"]),
      n_aaaa: box("n_aaaa", ["n_cccc"]),
      n_bbbb: box("n_bbbb", ["n_cccc"]),
      n_cccc: box("n_cccc"),
    }) as IrDocument;
    expect(toNested(shared).root.children?.map((c) => c.children?.[0]?.id)).toEqual([
      "n_cccc",
      "n_cccc",
    ]);
  });
});

describe("вспомогательные функции", () => {
  it("isNodeId отклоняет не-строки, даже похожие на ID", () => {
    expect(isNodeId(12345)).toBe(false);
  });

  it("pointer экранирует ~ и /", () => {
    expect(pointer("nodes", "a/b", "c~d", 0)).toBe("/nodes/a~1b/c~0d/0");
  });

  it("nodeIdFromPointer разбирает только указатели от корня", () => {
    expect(nodeIdFromPointer("/nodes/a~1b/props")).toBe("a/b");
    expect(nodeIdFromPointer("/nodes/n_x")).toBe("n_x");
    expect(nodeIdFromPointer("/meta/nodes/n_x")).toBeUndefined();
    expect(nodeIdFromPointer("/nodes")).toBeUndefined();
  });
});
