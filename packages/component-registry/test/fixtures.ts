import { mkdtemp, mkdir, writeFile, symlink, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach } from "vitest";
import type { BuildOptions } from "../src/cli.js";
const roots: string[] = [];
export const packageRoot = fileURLToPath(new URL("../", import.meta.url));
export const commit = "a".repeat(40);
export async function fixture(files: Record<string, string> = {}): Promise<BuildOptions> {
  const root = await mkdtemp(join(tmpdir(), "cms-registry-test-"));
  roots.push(root);
  await mkdir(join(root, "node_modules/@cms"), { recursive: true });
  await symlink(packageRoot, join(root, "node_modules/@cms/component-registry"), "junction");
  const source = {
    "package.json": JSON.stringify({ type: "module" }),
    "tsconfig.json": JSON.stringify({
      compilerOptions: {
        module: "NodeNext",
        target: "ES2022",
        strict: true,
        skipLibCheck: true,
        types: [],
      },
      include: ["**/*.ts"],
    }),
    "registry.ts": `import {createRegistry, defineComponent as define, t} from '@cms/component-registry';
import {Card as Render} from './components/card.js';
export default createRegistry({components: [define({name: 'Card', component: Render, props: {title: t.string().required()}})]});`,
    "components/card.ts": `import {label} from './label.js';
export function Card(p: {title: string}) {return label(p.title);}`,
    "components/label.ts": `import {prefix} from './prefix.js'; export const label = (s: string) => prefix + s;`,
    "components/prefix.ts": `import {last} from './last.js'; export const prefix = last;`,
    "components/last.ts": `export const last = 'hello';`,
    "components/card.test.ts": `import {Card} from './card.js'; export const example = Card({title: 'test'});`,
    ...files,
  };
  for (const [name, content] of Object.entries(source)) {
    await mkdir(resolve(root, name, ".."), { recursive: true });
    await writeFile(join(root, name), content);
  }
  return {
    root,
    registryFile: join(root, "registry.ts"),
    configFile: join(root, "tsconfig.json"),
    output: join(root, ".cms"),
    commit,
    app: { id: "fixture", version: "1.0.0", framework: "other" },
  };
}
afterEach(async () => {
  for (const root of roots.splice(0)) {
    if (!resolve(root).startsWith(resolve(tmpdir(), "cms-registry-test-")))
      throw new Error("Unexpected fixture path");
    await rm(root, { recursive: true, force: true });
  }
});
