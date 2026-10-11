import { expect, it } from "vitest";
import { createRegistry, defineComponent } from "../src/index.js";
import { projectProgram, sourceIndex } from "../src/source-index.js";
import { fixture } from "./fixtures.js";
import { join } from "node:path";
const registry = createRegistry({
  components: [defineComponent({ name: "Card", props: {}, component: () => null })],
});
it("MF-011: ignores unrelated application functions with the same factory name", async () => {
  const options = await fixture({
    "registry.ts": `import {createRegistry, defineComponent as define} from '@cms/component-registry';
import {Card} from './components/card.js';
function defineComponent(input: string) {return input;}
defineComponent('unrelated');
export default createRegistry({components: [define({name: 'Card', component: Card, props: {title: {type: 'string', required: true}}})]});`,
  });
  const result = sourceIndex(
    projectProgram(options.configFile),
    options.registryFile,
    registry,
    options.root,
    options.commit,
  );
  expect(result.refs.Card!.file).toBe("components/card.ts");
});
it("MF-011: resolves namespace imports, reexports, quoted fields and shorthand implementation", async () => {
  const options = await fixture({
    "components/card.ts": `// public implementation\nexport function Card(..._p: unknown[]) {return null;}`,
    "registry.ts": `import * as cms from '@cms/component-registry'; import {Render as component} from './barrel.js';
export default cms.createRegistry({components: [cms.defineComponent({'name': 'Card', component, props: {}})]});`,
    "barrel.ts": `export {Card as Render} from './components/card.js';`,
  });
  const result = sourceIndex(
    projectProgram(options.configFile),
    options.registryFile,
    registry,
    options.root,
    options.commit,
  );
  expect(result.refs.Card).toEqual({ file: "components/card.ts", export: "Card", line: 2 });
});
it("MF-011: resolves default exports and records cyclic/dynamic imports without source text", async () => {
  const options = await fixture({
    "registry.ts": `import {defineComponent, createRegistry} from '@cms/component-registry'; import Card from './components/card.js';
export default createRegistry({components: [defineComponent({name: \`Card\`, component: Card, props: {}})]});`,
    "components/card.ts": `import './loop.js'; import './generated/skip.js';
export default function Card() {return import('./lazy.js');}`,
    "components/loop.ts": `import './card.js';`,
    "components/lazy.ts": `export const lazy = true;`,
    "components/generated/skip.ts": `export const generated = true;`,
    "components/card.test.ts": `import './card.js';`,
  });
  const result = sourceIndex(
    projectProgram(options.configFile),
    options.registryFile,
    registry,
    options.root,
    options.commit,
  );
  expect(result.refs.Card!.export).toBe("default");
  expect(result.index.elements["component:Card"]!.dependencies).toEqual([
    "components/lazy.ts",
    "components/loop.ts",
  ]);
});
it("MF-001: invalid TypeScript configuration cannot generate an artifact", async () => {
  const options = await fixture({ "tsconfig.json": '{"compilerOptions":{"notAnOption":true}}' });
  expect(() => projectProgram(options.configFile)).toThrow("CMS_TSCONFIG_INVALID");
  expect(() => projectProgram(join(options.root, "missing.json"))).toThrow("CMS_TSCONFIG_INVALID");
});
it("MF-011: missing registry sources are rejected", async () => {
  const options = await fixture();
  const program = projectProgram(options.configFile);
  expect(() =>
    sourceIndex(program, join(options.root, "missing.ts"), registry, options.root, options.commit),
  ).toThrow("CMS_REGISTRY_SOURCE_MISSING");
});
it.each([
  [
    "opaque",
    `const def = {name: 'Card', component: Card, props: {}}; export default createRegistry({components: [defineComponent(def)]});`,
    "CMS_SOURCE_DEFINITION_UNSUPPORTED",
  ],
  [
    "computed name",
    `export default createRegistry({components: [defineComponent({name: String('Card'), component: Card, props: {}})]});`,
    "CMS_SOURCE_DEFINITION_UNSUPPORTED",
  ],
  [
    "inline implementation",
    `export default createRegistry({components: [defineComponent({name: 'Card', component: () => null, props: {}})]});`,
    "CMS_SOURCE_IMPLEMENTATION_UNRESOLVED",
  ],
  [
    "unregistered",
    `export default createRegistry({components: []});`,
    "CMS_SOURCE_DEFINITION_AMBIGUOUS",
  ],
  [
    "ambiguous",
    `const a = defineComponent({name: 'Card', component: Card, props: {}}); const b = defineComponent({name: 'Card', component: Card, props: {}}); export default createRegistry({components: [a]});`,
    "CMS_SOURCE_DEFINITION_AMBIGUOUS",
  ],
])("MF-011: rejects %s source definitions", async (_label, body, code) => {
  const options = await fixture({
    "registry.ts":
      `import {createRegistry, defineComponent} from '@cms/component-registry'; import {Card} from './components/card.js';\n` +
      body,
    "components/card.ts": `export function Card(..._p: unknown[]) {return null;}`,
  });
  expect(() =>
    sourceIndex(
      projectProgram(options.configFile),
      options.registryFile,
      registry,
      options.root,
      options.commit,
    ),
  ).toThrow(code);
});
it("MF-011: implementations must be exported from an application source file", async () => {
  const options = await fixture({
    "registry.ts": `import {createRegistry, defineComponent} from '@cms/component-registry';
function Card() {return 'private';}
export default createRegistry({components: [defineComponent({name: 'Card', component: Card, props: {}})]});`,
  });
  expect(() =>
    sourceIndex(
      projectProgram(options.configFile),
      options.registryFile,
      registry,
      options.root,
      options.commit,
    ),
  ).toThrow("CMS_SOURCE_IMPLEMENTATION_NOT_EXPORTED");
  const external = await fixture({
    "registry.ts": `import {createRegistry, defineComponent} from '@cms/component-registry'; import {Card} from './node_modules/vendor/index.js';
export default createRegistry({components: [defineComponent({name: 'Card', component: Card, props: {}})]});`,
    "node_modules/vendor/package.json": '{"type":"module"}',
    "node_modules/vendor/index.ts": "export function Card() {return null;}",
  });
  expect(() =>
    sourceIndex(
      projectProgram(external.configFile),
      external.registryFile,
      registry,
      external.root,
      external.commit,
    ),
  ).toThrow("CMS_SOURCE_IMPLEMENTATION_EXTERNAL");
});
