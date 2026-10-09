import { Ajv2020, type ErrorObject, type ValidateFunction } from "ajv/dist/2020.js";
import localizeRuModule from "ajv-i18n/localize/ru/index.js";
import { DiagnosticCode, nodeIdFromPointer, pointer, type Diagnostic } from "./diagnostics.js";
import { irSchema } from "./generated/schema.js";
import type { IrDocument, Predicate } from "./generated/types.js";
import { IR_LIMITS, SUPPORTED_IR_VERSIONS } from "./limits.js";

export interface ValidationResult {
  valid: boolean;
  diagnostics: Diagnostic[];
}

// ajv-i18n — CommonJS с `module.exports = fn`, а его .d.ts объявляет `export default`.
const localizeRu = ((localizeRuModule as unknown as { default?: unknown }).default ??
  localizeRuModule) as (errors?: ErrorObject[] | null) => void;

let compiled: ValidateFunction | undefined;

function schemaValidator(): ValidateFunction {
  // Компилируем лениво: схема нужна не всем потребителям пакета.
  compiled ??= new Ajv2020({
    allErrors: true,
    strict: true,
    strictTypes: false,
    strictRequired: false,
    discriminator: true,
  }).compile(irSchema);
  return compiled;
}

/**
 * Структурная валидация документа IR (уровень L1, 02-ir.md §11.1):
 * JSON Schema, инварианты дерева IR-010…014, IR-020 и ограничения размера.
 */
export function validateDocument(doc: unknown): ValidationResult {
  const diagnostics: Diagnostic[] = [];

  const version = isObject(doc) ? doc["irVersion"] : undefined;
  if (
    typeof version === "string" &&
    !(SUPPORTED_IR_VERSIONS as readonly string[]).includes(version)
  ) {
    diagnostics.push({
      code: DiagnosticCode.IrVersionUnsupported,
      severity: "error",
      pointer: "/irVersion",
      message: `Версия IR ${version} не поддерживается; поддерживаются: ${SUPPORTED_IR_VERSIONS.join(", ")}`,
      params: { version, supported: [...SUPPORTED_IR_VERSIONS] },
    });
    return { valid: false, diagnostics };
  }

  const bytes = new TextEncoder().encode(JSON.stringify(doc)).length;
  if (bytes > IR_LIMITS.maxBodyBytes) {
    diagnostics.push({
      code: DiagnosticCode.LimitExceeded,
      severity: "error",
      pointer: "",
      message: `Размер документа ${bytes} байт превышает ${IR_LIMITS.maxBodyBytes}`,
      params: { limit: "bodyBytes", max: IR_LIMITS.maxBodyBytes, actual: bytes },
    });
  }

  const validate = schemaValidator();
  if (!validate(doc)) {
    localizeRu(validate.errors);
    diagnostics.push(...schemaDiagnostics(validate.errors ?? []));
  }

  if (isObject(doc) && isObject(doc["nodes"])) {
    diagnostics.push(...treeDiagnostics(doc as unknown as IrDocument));
    diagnostics.push(...nodeDiagnostics(doc as unknown as IrDocument));
  }

  return { valid: !diagnostics.some((d) => d.severity === "error"), diagnostics };
}

// --- JSON Schema ------------------------------------------------------------

// Ключевые слова, которые лишь сообщают «ни одна ветка не подошла» или «не тот тип» —
// малоинформативны, если для того же места или его потомков есть более конкретные ошибки.
const VAGUE_KEYWORDS = new Set(["oneOf", "anyOf", "allOf", "if", "not", "type", "discriminator"]);
// Ошибки, характерные для «чужой» ветки oneOf: проигрывают остальным на том же месте.
const BRANCH_KEYWORDS = new Set(["additionalProperties", "required", "const"]);

function rank(error: ErrorObject): number {
  if (VAGUE_KEYWORDS.has(error.keyword)) return 2;
  if (BRANCH_KEYWORDS.has(error.keyword)) return 1;
  return 0;
}

/**
 * Ajv с allErrors сообщает об ошибке в каждой ветке oneOf. Чтобы не засыпать пользователя
 * повторами, оставляем на каждое место в документе одну, самую конкретную ошибку, и убираем
 * расплывчатые ошибки (type, oneOf …) мест, у потомков которых есть свои ошибки.
 */
function schemaDiagnostics(errors: ErrorObject[]): Diagnostic[] {
  // Вложенные ошибки propertyNames (например, pattern) дают текст для внешней ошибки.
  const nameDetails = new Map<string, string>();
  const relevant: ErrorObject[] = [];
  for (const error of errors) {
    // Ajv помечает ошибки внутри propertyNames полем propertyName.
    if ((error as ErrorObject & { propertyName?: string }).propertyName !== undefined) {
      if (error.message) nameDetails.set(error.instancePath, error.message);
    } else {
      relevant.push(error);
    }
  }

  const byPointer = new Map<string, ErrorObject>();
  for (const error of relevant) {
    const ptr = errorPointer(error);
    const current = byPointer.get(ptr);
    if (!current || rank(error) < rank(current)) byPointer.set(ptr, error);
  }

  const pointers = [...byPointer.keys()];
  const hasDescendant = (ptr: string) => pointers.some((p) => p !== ptr && p.startsWith(ptr + "/"));

  const out: Diagnostic[] = [];
  for (const [ptr, error] of byPointer) {
    if (VAGUE_KEYWORDS.has(error.keyword) && hasDescendant(ptr)) continue;
    const nodeId = nodeIdFromPointer(ptr);
    let message = error.message ?? error.keyword;
    if (error.keyword === "propertyNames") {
      const detail = nameDetails.get(error.instancePath);
      message = `недопустимое имя свойства ${String(error.params["propertyName"])}${detail ? `: ${detail}` : ""}`;
    } else if (error.keyword === "false schema") {
      message = "поле недопустимо в этом контексте";
    }
    out.push({
      code: DiagnosticCode.SchemaViolation,
      severity: "error",
      pointer: ptr,
      ...(nodeId !== undefined && { nodeId }),
      message,
      params: { keyword: error.keyword, schemaPath: error.schemaPath, ...error.params },
    });
  }
  return out;
}

function errorPointer(error: ErrorObject): string {
  // Для лишних и недопустимых имён свойств указываем на само свойство.
  if (error.keyword === "additionalProperties") {
    return `${error.instancePath}${pointer(String(error.params["additionalProperty"]))}`;
  }
  if (error.keyword === "propertyNames") {
    return `${error.instancePath}${pointer(String(error.params["propertyName"]))}`;
  }
  return error.instancePath;
}

// --- Инварианты дерева --------------------------------------------------------

interface ChildRef {
  childId: unknown;
  ptr: string;
}

function childRefs(id: string, node: unknown): ChildRef[] {
  if (!isObject(node)) return [];
  const refs: ChildRef[] = [];
  const children = node["children"];
  if (Array.isArray(children)) {
    children.forEach((childId, i) =>
      refs.push({ childId, ptr: pointer("nodes", id, "children", i) }),
    );
  }
  const slots = node["slots"];
  if (isObject(slots)) {
    for (const [slot, ids] of Object.entries(slots)) {
      if (!Array.isArray(ids)) continue;
      ids.forEach((childId, i) =>
        refs.push({ childId, ptr: pointer("nodes", id, "slots", slot, i) }),
      );
    }
  }
  return refs;
}

function treeDiagnostics(doc: IrDocument): Diagnostic[] {
  const out: Diagnostic[] = [];
  const nodes = doc.nodes as Record<string, unknown>;
  const root = doc.root;

  if (typeof root !== "string" || !Object.hasOwn(nodes, root)) {
    out.push({
      code: DiagnosticCode.NodeNotFound,
      severity: "error",
      pointer: "/root",
      message: `Корневой узел ${String(root)} отсутствует в nodes`,
      params: { nodeId: root },
    });
  }

  const parentOf = new Map<string, string>();
  const childrenOf = new Map<string, string[]>();

  for (const [id, node] of Object.entries(nodes)) {
    if (isObject(node) && node["id"] !== id) {
      out.push({
        code: DiagnosticCode.NodeIdMismatch,
        severity: "error",
        pointer: pointer("nodes", id, "id"),
        nodeId: id,
        message: `Поле id узла (${String(node["id"])}) не совпадает с ключом ${id}`,
        params: { key: id, id: node["id"] },
      });
    }

    const kids: string[] = [];
    for (const { childId, ptr } of childRefs(id, node)) {
      if (typeof childId !== "string") continue; // нарушение типа уже сообщено схемой
      if (!Object.hasOwn(nodes, childId)) {
        out.push({
          code: DiagnosticCode.NodeNotFound,
          severity: "error",
          pointer: ptr,
          nodeId: id,
          message: `Дочерний узел ${childId} отсутствует в nodes`,
          params: { childId },
        });
        continue;
      }
      if (childId === root) {
        out.push({
          code: DiagnosticCode.NodeCycle,
          severity: "error",
          pointer: ptr,
          nodeId: id,
          message: `Корневой узел ${childId} указан как дочерний узла ${id}`,
          params: { cycle: [childId] },
        });
        continue;
      }
      const existingParent = parentOf.get(childId);
      if (existingParent !== undefined) {
        out.push({
          code: DiagnosticCode.NodeMultipleParents,
          severity: "error",
          pointer: ptr,
          nodeId: childId,
          message: `Узел ${childId} уже вложен в ${existingParent}; узел может иметь только одного родителя`,
          params: { parents: [existingParent, id] },
        });
        continue;
      }
      parentOf.set(childId, id);
      kids.push(childId);
    }
    childrenOf.set(id, kids);
  }

  // Обход от корня: достижимость и глубина.
  const reachable = new Set<string>();
  if (typeof root === "string" && Object.hasOwn(nodes, root)) {
    const stack: [string, number][] = [[root, 1]];
    let depthReported = false;
    while (stack.length > 0) {
      const [id, depth] = stack.pop()!;
      if (reachable.has(id)) continue;
      reachable.add(id);
      if (depth > IR_LIMITS.maxDepth && !depthReported) {
        depthReported = true;
        out.push({
          code: DiagnosticCode.LimitExceeded,
          severity: "error",
          pointer: pointer("nodes", id),
          nodeId: id,
          message: `Глубина вложенности превышает ${IR_LIMITS.maxDepth}`,
          params: { limit: "depth", max: IR_LIMITS.maxDepth },
        });
      }
      for (const child of childrenOf.get(id) ?? []) stack.push([child, depth + 1]);
    }
  }

  // Без корня достижимость не определена: каждый узел оказался бы «сиротой».
  if (reachable.size === 0) return out;

  // Недостижимые узлы: либо вершина «сиротского» поддерева, либо часть цикла.
  const settled = new Set<string>(reachable);
  for (const id of Object.keys(nodes)) {
    if (settled.has(id)) continue;
    const chain: string[] = [];
    const inChain = new Set<string>();
    let current: string | undefined = id;
    while (current !== undefined && !settled.has(current) && !inChain.has(current)) {
      chain.push(current);
      inChain.add(current);
      current = parentOf.get(current);
    }
    if (current !== undefined && inChain.has(current)) {
      const cycle = chain.slice(chain.indexOf(current));
      const first = [...cycle].sort()[0]!;
      out.push({
        code: DiagnosticCode.NodeCycle,
        severity: "error",
        pointer: pointer("nodes", first),
        nodeId: first,
        message: `Узлы образуют цикл: ${cycle.join(" → ")}`,
        params: { cycle },
      });
    } else if (current === undefined) {
      const top = chain[chain.length - 1]!;
      out.push({
        code: DiagnosticCode.NodeOrphan,
        severity: "error",
        pointer: pointer("nodes", top),
        nodeId: top,
        message: `Узел ${top} не связан с корнем документа`,
        params: {},
      });
    }
    for (const n of chain) settled.add(n);
  }

  return out;
}

// --- Проверки отдельных узлов ---------------------------------------------------

function nodeDiagnostics(doc: IrDocument): Diagnostic[] {
  const out: Diagnostic[] = [];
  for (const [id, node] of Object.entries(doc.nodes as Record<string, unknown>)) {
    if (!isObject(node)) continue;

    const props = node["props"];
    const bindings = node["bindings"];
    if (isObject(props) && isObject(bindings)) {
      for (const key of Object.keys(props)) {
        if (!Object.hasOwn(bindings, key)) continue;
        out.push({
          code: DiagnosticCode.PropBothStaticAndBound,
          severity: "error",
          pointer: pointer("nodes", id, "props", key),
          nodeId: id,
          message: `Свойство ${key} задано и в props, и в bindings`,
          params: { prop: key },
        });
      }
    }

    const when = node["when"];
    if (isObject(when)) {
      const depth = predicateDepth(when as unknown as Predicate);
      if (depth > IR_LIMITS.maxPredicateDepth) {
        out.push({
          code: DiagnosticCode.LimitExceeded,
          severity: "error",
          pointer: pointer("nodes", id, "when"),
          nodeId: id,
          message: `Глубина условия ${depth} превышает ${IR_LIMITS.maxPredicateDepth}`,
          params: { limit: "predicateDepth", max: IR_LIMITS.maxPredicateDepth, actual: depth },
        });
      }
    }
  }
  return out;
}

function predicateDepth(p: unknown): number {
  if (!isObject(p)) return 0;
  const nested: unknown[] = [];
  if (p["op"] === "not") nested.push(p["arg"]);
  if ((p["op"] === "and" || p["op"] === "or") && Array.isArray(p["args"]))
    nested.push(...p["args"]);
  return 1 + Math.max(0, ...nested.map(predicateDepth));
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
