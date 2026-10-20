// Build a temporary application fixture without store.favorite and PromoBanner, then restore the
// original source immediately. The resulting dist/.cms files form the initial
// deployment for studio-live-capabilities.mjs; no fixture source is committed.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
import { fileURLToPath, URL } from "node:url";
if (!process.env.CMS_PNPM_CLI || !process.env.STUDIO_REVIEW_OUTPUT)
  throw Error("CMS_PNPM_CLI and isolated output required");
const repo = fileURLToPath(new URL("..", import.meta.url));
const registry = resolve(repo, "examples/react-store/src/registry.ts");
const original = await readFile(registry, "utf8");
const block = original.match(/ {2}actions: \[[\s\S]*?\n {2}\],\n {2}dataSources:/);
if (!block || !block[0].includes('name: "store.favorite"'))
  throw Error("Unexpected fixture registry");
// STU-374: the requested component is absent from the baseline too.
const promo = original.match(/ {4}defineReactComponent\(\{\n {6}name: "PromoBanner",[\s\S]*?\n {4}\}\),\n/);
if (!promo) throw Error("Unexpected fixture registry");
const fixture = original
  .replace(block[0], "  actions: [],\n  dataSources:")
  .replace(promo[0], "")
  .replace("About, Promo }", "About }")
  .replace("  defineAction,\n", "")
  .replace(
    "{ favoriteProduct, loadProducts, loadCollectionProducts }",
    "{ loadProducts, loadCollectionProducts }",
  );
await mkdir(process.env.STUDIO_REVIEW_OUTPUT, { recursive: true });
await writeFile(
  resolve(process.env.STUDIO_REVIEW_OUTPUT, "registry-original.private.txt"),
  original,
);
try {
  await writeFile(registry, fixture);
  execFileSync(
    process.execPath,
    [process.env.CMS_PNPM_CLI, "--filter", "@cms/store-example", "build"],
    { cwd: repo, stdio: "pipe", windowsHide: true },
  );
  console.log("Baseline application built without the requested action; source restored next.");
} finally {
  await writeFile(registry, original);
}
