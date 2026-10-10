import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { expect, it } from "vitest";
import { validateManifest } from "../src/index.js";

const dir = fileURLToPath(new URL("../fixtures/invalid/", import.meta.url));

// Conformance-тесты проверяют лишь наличие ожидаемых диагностик; снимок фиксирует полный
// результат — тексты, params, порядок и отсутствие лишних диагностик.
it("полные диагностики invalid-фикстур совпадают со снимком", async () => {
  const result: Record<string, unknown> = {};
  for (const file of readdirSync(dir)
    .filter((f) => f.endsWith(".json"))
    .sort()) {
    const fixture = JSON.parse(readFileSync(dir + file, "utf8")) as { manifest: unknown };
    result[file] = validateManifest(fixture.manifest).diagnostics;
  }
  await expect(JSON.stringify(result, null, 2) + "\n").toMatchFileSnapshot(
    "__snapshots__/golden.json",
  );
});
