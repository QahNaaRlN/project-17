import { DiagnosticCode, pointer, type Diagnostic } from "./diagnostics.js";
import { generateNodeId, isNodeId } from "./ids.js";
import type { IrDocument, Node, NodeId } from "./generated/types.js";

/** Узел во вложенной форме (02-ir.md §10): дети и слоты — узлы, а не ID; ID необязателен. */
export type NestedNode = Omit<Node, "id" | "children" | "slots"> & {
  id?: NodeId;
  children?: NestedNode[];
  slots?: { [slot: string]: NestedNode[] };
};

/** Документ во вложенной форме: вместо `root` + `nodes` — дерево `root`. */
export type NestedDocument = Omit<IrDocument, "root" | "nodes"> & { root: NestedNode };

export class NormalizeError extends Error {
  constructor(readonly diagnostics: Diagnostic[]) {
    super(diagnostics.map((d) => d.message).join("; "));
    this.name = "NormalizeError";
  }
}

export interface NormalizeOptions {
  /** Генератор ID для узлов без ID (по умолчанию generateNodeId). */
  generateId?: (taken: ReadonlySet<string>) => string;
}

/**
 * Преобразует вложенную форму в нормализованную (02-ir.md §10).
 * Заданные ID сохраняются — на них могут ссылаться действия (`openModal`, `anchor`);
 * узлам без ID назначаются новые. Недопустимые и повторяющиеся ID — ошибка NormalizeError.
 * Результат не валидируется: вызывающий проверяет его validateDocument.
 */
export function normalize(doc: NestedDocument, options: NormalizeOptions = {}): IrDocument {
  const generateId = options.generateId ?? generateNodeId;
  const taken = new Set<string>();
  const errors: Diagnostic[] = [];

  // Первый проход: зарезервировать явно заданные ID, чтобы сгенерированные с ними не совпали.
  walk(doc.root, "/root", (node, ptr) => {
    if (node.id === undefined) return;
    if (!isNodeId(node.id)) {
      errors.push({
        code: DiagnosticCode.SchemaViolation,
        severity: "error",
        pointer: `${ptr}/id`,
        message: `Недопустимый ID узла: ${String(node.id)}`,
        params: { id: node.id },
      });
    } else if (taken.has(node.id)) {
      errors.push({
        code: DiagnosticCode.NodeIdDuplicate,
        severity: "error",
        pointer: `${ptr}/id`,
        nodeId: node.id,
        message: `ID узла ${node.id} встречается несколько раз`,
        params: { id: node.id },
      });
    } else {
      taken.add(node.id);
    }
  });
  if (errors.length > 0) throw new NormalizeError(errors);

  const nodes: Record<string, Node> = {};

  const flatten = (nested: NestedNode): NodeId => {
    const { children, slots, id: givenId, ...rest } = nested;
    let id = givenId;
    if (id === undefined) {
      id = generateId(taken);
      taken.add(id);
    }
    const node: Node = { id, ...rest };
    // Заносим узел до обхода детей, чтобы порядок ключей nodes совпадал с порядком обхода.
    nodes[id] = node;
    if (children !== undefined) node.children = children.map(flatten);
    if (slots !== undefined) {
      node.slots = Object.fromEntries(
        Object.entries(slots).map(([slot, list]) => [slot, list.map(flatten)]),
      );
    }
    return id;
  };

  const { root, ...rest } = doc;
  const rootId = flatten(root);
  return { ...rest, root: rootId, nodes } as IrDocument;
}

/**
 * Обратное преобразование в вложенную форму — для агентов и экспорта.
 * Узлы, недостижимые от корня, отбрасываются. Документ должен быть валиден.
 */
export function toNested(doc: IrDocument): NestedDocument {
  const visiting = new Set<string>();

  const build = (id: NodeId, ptr: string): NestedNode => {
    const node = doc.nodes[id];
    if (node === undefined) {
      throw new NormalizeError([
        {
          code: DiagnosticCode.NodeNotFound,
          severity: "error",
          pointer: ptr,
          message: `Узел ${id} отсутствует в nodes`,
          params: { nodeId: id },
        },
      ]);
    }
    if (visiting.has(id)) {
      throw new NormalizeError([
        {
          code: DiagnosticCode.NodeCycle,
          severity: "error",
          pointer: pointer("nodes", id),
          nodeId: id,
          message: `Узел ${id} входит в цикл`,
          params: {},
        },
      ]);
    }
    visiting.add(id);
    const { children, slots, ...rest } = node;
    const nested: NestedNode = { ...rest };
    if (children !== undefined) {
      nested.children = children.map((c, i) => build(c, pointer("nodes", id, "children", i)));
    }
    if (slots !== undefined) {
      nested.slots = Object.fromEntries(
        Object.entries(slots).map(([slot, list]) => [
          slot,
          list.map((c, i) => build(c, pointer("nodes", id, "slots", slot, i))),
        ]),
      );
    }
    visiting.delete(id);
    return nested;
  };

  const { root, nodes: _nodes, ...rest } = doc;
  return { ...rest, root: build(root, "/root") };
}

function walk(node: NestedNode, ptr: string, visit: (node: NestedNode, ptr: string) => void): void {
  visit(node, ptr);
  node.children?.forEach((child, i) => walk(child, `${ptr}/children/${i}`, visit));
  for (const [slot, list] of Object.entries(node.slots ?? {})) {
    list.forEach((child, i) => walk(child, `${ptr}/slots/${slot}/${i}`, visit));
  }
}
