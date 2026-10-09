// Генерирует src/generated/* из schema/ir-1.0.schema.json — единственного источника истины IR (IR-005).
// Запуск: pnpm --filter @cms/ir gen. CI проверяет, что сгенерированные файлы актуальны.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { compile, type JSONSchema } from "json-schema-to-typescript";

const root = fileURLToPath(new URL("..", import.meta.url));
const schemaPath = `${root}schema/ir-1.0.schema.json`;
const outDir = `${root}src/generated`;

const header =
  "// Сгенерировано scripts/generate-types.ts из schema/ir-1.0.schema.json. Не редактировать.\n";

const schema = JSON.parse(await readFile(schemaPath, "utf8")) as JSONSchema;

// Условные ограничения (allOf из if/then/else) в типах TypeScript не выражаются, а генератор
// превращает их в индексные сигнатуры `[k: string]: unknown`, скрывающие опечатки в полях.
// Для генерации типов их убираем; валидатор проверяет полную схему.
// Ветвление по форме значения (if/then/else) для типов эквивалентно объединению then | else.
function stripConditionals(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(stripConditionals);
  if (value === null || typeof value !== "object") return value;
  const obj = value as Record<string, unknown>;
  if ("if" in obj && "then" in obj && "else" in obj) {
    const { if: _if, then, else: otherwise, ...rest } = obj;
    return stripConditionals({ ...rest, anyOf: [then, otherwise] });
  }
  const out: Record<string, unknown> = {};
  for (const [key, child] of Object.entries(value)) {
    const conditionalOnly =
      key === "allOf" &&
      Array.isArray(child) &&
      child.every((c) => typeof c === "object" && c !== null && "if" in c);
    if (!conditionalOnly) out[key] = stripConditionals(child);
  }
  return out;
}

const types = await compile(stripConditionals(schema) as JSONSchema, "IrDocument", {
  bannerComment: header,
  additionalProperties: false,
  unreachableDefinitions: true,
  strictIndexSignatures: false,
  maxItems: -1,
  format: true,
  style: { printWidth: 100 },
  customName: (s) => {
    const id = (s as { $id?: string }).$id;
    return id?.endsWith("/document.json") ? "IrDocument" : undefined;
  },
});

// В Design есть именованное поле `states` и индексная сигнатура для дизайн-свойств. TypeScript
// требует, чтобы тип именованного поля входил в тип индексной сигнатуры, поэтому расширяем её.
const designIndex = /(export interface Design \{[^}]*?\n {2})\[k: string\]: DesignValue;/;
if (!designIndex.test(types))
  throw new Error("Design index signature not found in generated types");
const fixedTypes = types.replace(
  designIndex,
  "$1[k: string]: DesignValue | DesignStates | undefined;",
);

await mkdir(outDir, { recursive: true });
await writeFile(`${outDir}/types.ts`, fixedTypes);
await writeFile(
  `${outDir}/schema.ts`,
  `${header}\nexport const irSchema = ${JSON.stringify(schema, null, 2)} as const;\n`,
);
console.log("generated:", `${outDir}/types.ts`, `${outDir}/schema.ts`);
