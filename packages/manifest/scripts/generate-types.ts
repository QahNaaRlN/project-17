// Генерирует src/generated/* из schema/manifest-1.0.schema.json — единственного источника истины
// формата manifest (MF-001). Запуск: pnpm --filter @cms/manifest gen. CI проверяет актуальность.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { compile, type JSONSchema } from "json-schema-to-typescript";

const root = fileURLToPath(new URL("..", import.meta.url));
const schemaPath = `${root}schema/manifest-1.0.schema.json`;
const outDir = `${root}src/generated`;

const header =
  "// Сгенерировано scripts/generate-types.ts из schema/manifest-1.0.schema.json. Не редактировать.\n";

const schema = JSON.parse(await readFile(schemaPath, "utf8")) as JSONSchema;

const types = await compile(schema, "Manifest", {
  bannerComment: header,
  additionalProperties: false,
  unreachableDefinitions: true,
  strictIndexSignatures: false,
  maxItems: -1,
  format: true,
  style: { printWidth: 100 },
  customName: (s) => {
    const id = (s as { $id?: string }).$id;
    return id?.endsWith("/manifest.json") ? "Manifest" : undefined;
  },
});

await mkdir(outDir, { recursive: true });
await writeFile(`${outDir}/types.ts`, types);
await writeFile(
  `${outDir}/schema.ts`,
  `${header}\nexport const manifestSchema = ${JSON.stringify(schema, null, 2)} as const;\n`,
);
const catalogue = JSON.parse(await readFile(`${root}schema/builtin-catalogue.json`, "utf8"));
await writeFile(
  `${outDir}/builtins.ts`,
  `// Сгенерировано из schema/builtin-catalogue.json. Не редактировать.\nexport const builtinCatalogue = ${JSON.stringify(catalogue, null, 2)} as const;\n`,
);
console.log("generated:", `${outDir}/types.ts`, `${outDir}/schema.ts`);
