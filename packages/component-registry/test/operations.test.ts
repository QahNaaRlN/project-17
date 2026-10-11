import { expect, it } from "vitest";
import {
  createRegistry,
  defineAction,
  defineDataSource,
  t,
  type ActionContext,
  type LoaderContext,
} from "../src/index.js";
import { buildManifest } from "../src/manifest.js";
import { buildProject } from "../src/cli.js";
import { fixture } from "./fixtures.js";
const app = { id: "operations", version: "1.0.0", framework: "other" } as const;
it("MF-010/012: действия и источники сохраняют снимок контракта и runtime-функции", () => {
  const handler = (args: { id: string }, ctx: ActionContext<{ select: (id: string) => void }>) =>
    ctx.app.select(args.id);
  const loader = (params: { limit?: number | null }, ctx: LoaderContext) => {
    void ctx;
    return [String(params.limit ?? 1)];
  };
  const action = defineAction({
    name: "test.select",
    description: "select",
    args: { id: t.string().required() },
    capabilities: ["test.select"],
    deprecated: { since: "1.0.0", message: "old" },
    handler,
  });
  const source = defineDataSource({
    name: "test.items",
    description: "items",
    params: { limit: t.number().default(1) },
    result: t.list(t.string()),
    cache: { ttlSeconds: 60 },
    capabilities: ["test.select"],
    deprecated: null,
    loader,
  });
  const registry = createRegistry({
    components: [],
    actions: [action],
    dataSources: [source],
    capabilities: { "test.select": { description: "select" } },
  });
  const manifest = buildManifest(registry, { app });
  expect(manifest.actions![action.name]).toEqual({
    description: "select",
    args: { id: { type: "string", required: true } },
    capabilities: ["test.select"],
    deprecated: { since: "1.0.0", message: "old" },
  });
  expect(manifest.dataSources![source.name]).toEqual({
    description: "items",
    params: { limit: { type: "number", default: 1 } },
    result: { type: "list", of: { type: "string" } },
    cache: { ttlSeconds: 60 },
    capabilities: ["test.select"],
    deprecated: null,
  });
  expect(registry.actions[0]!.handler).toBe(handler);
  expect(registry.dataSources[0]!.loader).toBe(loader);
  expect(Object.isFrozen(registry.actions[0]!.args)).toBe(true);
  expect(Object.isFrozen(registry.dataSources[0]!.result)).toBe(true);
  expect(JSON.stringify(manifest)).not.toContain("handler");
  expect(JSON.stringify(manifest)).not.toContain("loader");
  const paginated = defineDataSource({
    name: "test.pages",
    params: {},
    result: t.list(t.string()),
    paginated: true,
    loader: () => ({ items: ["one"], hasMore: false }),
  });
  expect(
    buildManifest(createRegistry({ components: [], dataSources: [paginated] }), { app })
      .dataSources![paginated.name]!.paginated,
  ).toBe(true);
});
it("MF-010: отклоняет дубликаты, отсутствие функций, неверную пагинацию и getters метаданных", () => {
  const action = defineAction({ name: "test.select", args: {}, handler: () => null });
  const source = defineDataSource({
    name: "test.items",
    params: {},
    result: { type: "string" },
    loader: () => "one",
  });
  expect(() => createRegistry({ components: [], actions: [action, action] })).toThrow(
    "CMS_REGISTRY_NAME_DUPLICATE",
  );
  expect(() => createRegistry({ components: [], dataSources: [source, source] })).toThrow(
    "CMS_REGISTRY_NAME_DUPLICATE",
  );
  expect(() => defineAction({ ...action, name: "bad" })).toThrow("CMS_REGISTRY_NAME_INVALID");
  expect(() => defineDataSource({ ...source, name: "bad" })).toThrow("CMS_REGISTRY_NAME_INVALID");
  expect(() => defineAction({ ...action, handler: null } as never)).toThrow(
    "CMS_REGISTRY_HANDLER_INVALID",
  );
  expect(() => defineDataSource({ ...source, loader: null } as never)).toThrow(
    "CMS_REGISTRY_LOADER_INVALID",
  );
  expect(() => defineDataSource({ ...source, paginated: true } as never)).toThrow(
    "CMS_REGISTRY_PAGINATION_INVALID",
  );
  let reads = 0;
  const getter = {
    enumerable: true,
    get() {
      reads++;
      return "bad";
    },
  };
  expect(() => defineAction(Object.defineProperty({ ...action }, "description", getter))).toThrow(
    "CMS_REGISTRY_METADATA_INVALID",
  );
  expect(() => defineDataSource(Object.defineProperty({ ...source }, "result", getter))).toThrow(
    "CMS_REGISTRY_METADATA_INVALID",
  );
  expect(reads).toBe(0);
});
it("MF-011: CLI индексирует экспортированные действия и источники вместе с manifest", async () => {
  const options = await fixture({
    "registry.ts": `import {createRegistry, defineAction, defineDataSource, t} from '@cms/component-registry'; import {handler, loader} from './operations.js';
export default createRegistry({components: [], actions: [defineAction({name:'test.select', args: {id:t.string().required()}, handler})], dataSources: [defineDataSource({name:'test.items', params:{}, result:t.list(t.string()), loader})]});`,
    "operations.ts": `export function handler(args: {id:string}) {return args.id;} export function loader() {return ['one'];}`,
  });
  const artifact = await buildProject(options);
  expect(artifact.manifest.actions!["test.select"]!.sourceRef).toMatchObject({
    file: "operations.ts",
    export: "handler",
  });
  expect(artifact.manifest.dataSources!["test.items"]!.sourceRef).toMatchObject({
    file: "operations.ts",
    export: "loader",
  });
  expect(Object.keys(artifact.index.elements)).toEqual([
    "action:test.select",
    "dataSource:test.items",
  ]);
});
// MF-012: implementation types are checked against the metadata.
function compileContracts(): void {
  defineAction({
    name: "test.valid",
    args: { id: t.string().required() },
    handler: (args) => args.id.toUpperCase(),
  });
  defineDataSource({
    name: "test.valid",
    params: { count: t.number().required() },
    result: t.string(),
    loader: (params) => String(params.count),
  });
  defineAction({
    name: "test.invalid",
    args: { id: t.string().required() },
    // @ts-expect-error MF-012: string args cannot be consumed as number.
    handler: (args: { id: number }) => args.id,
  });
  // @ts-expect-error MF-012: source result is string.
  defineDataSource({ name: "test.invalid", params: {}, result: t.string(), loader: () => 1 });
  defineDataSource({
    name: "test.invalid",
    params: {},
    result: t.list(t.string()),
    paginated: true,
    // @ts-expect-error MF-012: paginated list returns a page envelope.
    loader: () => ["one"],
  });
}
void compileContracts;
