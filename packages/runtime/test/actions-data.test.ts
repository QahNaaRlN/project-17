import { expect, it, vi, afterEach } from "vitest";
import type { IrDocument } from "@cms/ir";
import { executeActions, planEvents, operands, type RuntimeAction } from "../src/actions.js";
import {
  loadSource,
  loadSources,
  sourceResult,
  appendPage,
  type RuntimeSource,
  type LoaderContext,
} from "../src/sources.js";
import { preparePageWithData } from "../src/data-plan.js";
import { preparePage } from "../src/plan.js";
const source = (loader: RuntimeSource["loader"] = () => ["one"]): RuntimeSource => ({
  name: "test.items",
  params: { limit: { type: "number", integer: true, min: 1, max: 5, default: 2 } },
  result: { type: "list", of: { type: "string", required: true } },
  loader,
});
const action = (handler = vi.fn()): RuntimeAction => ({
  name: "test.select",
  args: {
    id: { type: "string", required: true },
    count: { type: "number", min: 1, max: 8, default: 1 },
  },
  handler,
});
const context = { app: {}, node: "root", instance: "root" };
afterEach(() => vi.useRealTimers());

it("IR-052/SDK-011: navigate к якорю сохраняет область экземпляра и проверяет цель", () => {
  const node = {
    id: "button",
    type: "Button",
    on: {
      click: { action: "navigate", args: { to: { kind: "anchor" as const, node: "target" } } },
    },
  };
  expect(
    planEvents(
      node,
      {},
      ["click"],
      [],
      { target: { id: "target", type: "Text" } },
      "repeat:one/component/",
      [],
    )!.click![0]!.args.to,
  ).toEqual({ kind: "url", url: "#repeat%3Aone%2Fcomponent%2Ftarget" });
  expect(() => planEvents(node, {}, ["click"], [], {}, "", [])).toThrow(
    "CMS_ACTION_TARGET_INVALID",
  );
});

it("SDK-014: предел страницы допускает 64 источника, 65 отклоняет до вызова загрузчиков", async () => {
  const loader = vi.fn(() => ["one"]),
    definition = source(loader);
  const doc: IrDocument = {
    irVersion: "1.0",
    kind: "page",
    root: "text",
    nodes: { text: { id: "text", type: "Text", props: { text: "ready" } } },
  };
  for (const count of [64, 65]) {
    loader.mockClear();
    doc.dataSources = Object.fromEntries(
      Array.from({ length: count }, (_, index) => ["source" + index, { source: definition.name }]),
    );
    const plan = await preparePageWithData(doc, { sources: [definition] });
    expect(loader).toHaveBeenCalledTimes(count === 64 ? 64 : 0);
    expect(plan.diagnostics).toEqual(
      count === 64 ? [] : [{ code: "CMS_SOURCE_LIMIT", nodeId: "text" }],
    );
    expect(plan.roots.length).toBe(count === 64 ? 1 : 0);
  }
});
it("SDK-014: ошибка одного источника оставляет empty и независимый успешный контент", async () => {
  const definition = source(),
    failure = {
      ...source(),
      name: "test.failure",
      loader: () => {
        throw new Error("private-message");
      },
    };
  const doc: IrDocument = {
    irVersion: "1.0",
    kind: "page",
    root: "root",
    dataSources: { good: { source: definition.name }, bad: { source: failure.name } },
    nodes: {
      root: { id: "root", type: "Stack", children: ["good", "repeat", "error"] },
      good: { id: "good", type: "Text", bindings: { text: "$data.good.0" } },
      repeat: {
        id: "repeat",
        type: "Repeat",
        bindings: { items: "$data.bad.items" },
        children: ["item"],
        slots: { empty: ["empty"] },
      },
      item: { id: "item", type: "Text", bindings: { text: "$item" } },
      empty: { id: "empty", type: "Text", props: { text: "empty" } },
      error: { id: "error", type: "Text", bindings: { text: "$data.bad.error.code" } },
    },
  };
  const plan = await preparePageWithData(doc, { sources: [definition, failure] });
  expect(plan.diagnostics).toEqual([]);
  expect(plan.roots[0]!.children.map((node) => node.props.text)).toEqual([
    "one",
    "empty",
    "CMS_SOURCE_FAILED",
  ]);
  expect(JSON.stringify(plan)).not.toContain("private-message");
});

it("IR-050/052: применяет defaults, ожидает действия по порядку, ошибка прерывает цепочку", async () => {
  let finish!: () => void;
  const calls: string[] = [];
  const first = action(
    vi.fn(
      () =>
        new Promise<void>((done) => {
          calls.push("first");
          finish = done;
        }),
    ),
  );
  const second = {
    name: "test.next",
    args: {},
    handler: vi.fn(() => {
      calls.push("second");
    }),
  };
  const pending = executeActions(
    [
      { action: first.name, args: { id: "one" } },
      { action: second.name, args: {} },
    ],
    [first, second],
    context,
  );
  expect(calls).toEqual(["first"]);
  expect(first.handler).toHaveBeenCalledWith({ id: "one", count: 1 }, context);
  finish();
  expect(await pending).toBe(true);
  expect(calls).toEqual(["first", "second"]);
  const error = vi.fn();
  first.handler = vi.fn(() => {
    throw new Error("SECRET ARGUMENT");
  });
  expect(
    await executeActions(
      [
        { action: first.name, args: { id: "one" } },
        { action: second.name, args: {} },
      ],
      [first, second],
      context,
      { onError: error },
    ),
  ).toBe(false);
  expect(second.handler).toHaveBeenCalledTimes(1);
  expect(error).toHaveBeenCalledWith({
    code: "CMS_ACTION_FAILED",
    action: first.name,
    node: "root",
    instance: "root",
  });
});
it("SDK-033/013: дизайн отключает действия, некорректные обработчики дают безопасную диагностику", async () => {
  const handler = vi.fn(),
    error = vi.fn();
  expect(
    await executeActions(
      [{ action: "test.select", args: { id: "one" } }],
      [action(handler)],
      context,
      { mode: "design" },
    ),
  ).toBe(false);
  expect(handler).not.toHaveBeenCalled();
  for (const [name, args, code] of [
    ["unknown.call", {}, "CMS_ACTION_UNKNOWN"],
    ["test.select", { id: 1 }, "CMS_VALUE_INVALID"],
    ["track", { event: "view" }, "CMS_ACTION_HANDLER_MISSING"],
  ] as const) {
    expect(
      await executeActions([{ action: name, args }], [action()], context, { onError: error }),
    ).toBe(false);
    expect(error.mock.calls.at(-1)![0].code).toBe(code);
  }
  expect(
    await executeActions([{ action: "unknown.call", args: {} }], [], context, {
      onError: () => {
        throw new Error("reporting");
      },
    }),
  ).toBe(false);
});
it("IR-050/052: встроенные сервисы получают только проверенные аргументы", async () => {
  const navigate = vi.fn(),
    openModal = vi.fn(),
    closeModal = vi.fn(),
    scrollTo = vi.fn(),
    track = vi.fn(),
    loadMore = vi.fn();
  const services = { ...context, navigate, openModal, closeModal, scrollTo, track, loadMore };
  const chain = [
    { action: "navigate", args: { to: { kind: "url", url: "/safe" } } },
    { action: "openModal", args: { target: "instance/modal" } },
    { action: "closeModal", args: { target: "instance/modal" } },
    { action: "scrollTo", args: { target: "instance/target" } },
    { action: "track", args: { event: "view", props: { id: "one" } } },
    { action: "data.loadMore", args: { source: "featured" } },
  ];
  expect(await executeActions(chain, [], services)).toBe(true);
  expect(navigate).toHaveBeenCalledWith({ kind: "url", url: "/safe" });
  expect(openModal).toHaveBeenCalledWith("instance/modal");
  expect(closeModal).toHaveBeenCalledWith("instance/modal");
  expect(scrollTo).toHaveBeenCalledWith("instance/target");
  expect(track).toHaveBeenCalledWith("view", { id: "one" });
  expect(loadMore).toHaveBeenCalledWith("featured");
  expect(await executeActions([{ action: "track", args: { event: "view" } }], [], services)).toBe(
    true,
  );
  expect(track).toHaveBeenLastCalledWith("view", {});
  for (const item of chain) expect(await executeActions([item], [], context)).toBe(false);
  for (const args of [{ target: 2 }, { target: "" }, { target: "node", other: true }])
    expect(await executeActions([{ action: "scrollTo", args }], [], services)).toBe(false);
  expect(
    await executeActions(
      [{ action: "navigate", args: { to: { kind: "url", url: "javascript:bad" } } }],
      [],
      services,
    ),
  ).toBe(false);
  expect(
    await executeActions([{ action: "track", args: { event: "view", props: [] } }], [], services),
  ).toBe(false);
});
it("IR-051/052: проверяет события, размер цепочки и цели DOM/данных до рендеринга", () => {
  const node = {
    id: "root",
    type: "Button",
    on: { click: { action: "scrollTo", args: { target: { lit: "target" } } } },
  };
  const nodes = { target: { id: "target", type: "Text" }, modal: { id: "modal", type: "Modal" } };
  expect(planEvents(node, {}, ["click"], [], nodes, "instance/", [])!.click).toEqual([
    { action: "scrollTo", args: { target: "instance/target" } },
  ]);
  expect(planEvents({ ...node, on: {} }, {}, [], [], nodes, "", [])).toBeUndefined();
  expect(() => planEvents(node, {}, ["submit"], [], nodes, "", [])).toThrow("CMS_EVENT_UNKNOWN");
  for (const chain of [[], Array(9).fill({ action: "track", args: { event: { lit: "view" } } })])
    expect(() =>
      planEvents({ ...node, on: { click: chain as never } }, {}, ["click"], [], nodes, "", []),
    ).toThrow("CMS_ACTION_CHAIN_INVALID");
  for (const [name, target] of [
    ["openModal", "target"],
    ["scrollTo", "missing"],
    ["closeModal", "target"],
  ])
    expect(() =>
      planEvents(
        { ...node, on: { click: { action: name!, args: { target: { lit: target } } } } },
        {},
        ["click"],
        [],
        nodes,
        "",
        [],
      ),
    ).toThrow("CMS_ACTION_TARGET_INVALID");
  expect(
    planEvents(
      { ...node, on: { click: { action: "openModal", args: { target: { lit: "modal" } } } } },
      {},
      ["click"],
      [],
      nodes,
      "",
      [],
    )!.click![0]!.args.target,
  ).toBe("modal");
  expect(() =>
    planEvents(
      { ...node, on: { click: { action: "data.loadMore", args: { source: { lit: "missing" } } } } },
      {},
      ["click"],
      [],
      nodes,
      "",
      ["featured"],
    ),
  ).toThrow("CMS_ACTION_TARGET_INVALID");
  expect(
    planEvents(
      {
        ...node,
        on: { click: { action: "data.loadMore", args: { source: { lit: "featured" } } } },
      },
      {},
      ["click"],
      [],
      nodes,
      "",
      ["featured"],
    ),
  ).toBeDefined();
});
it("IR-040/052: операнды разрешают собственные пути и параметры маршрута страницы", () => {
  expect(
    operands(
      {
        id: "$item.id",
        page: { kind: "page", page: "id", params: { slug: "$item.slug" } },
        url: { kind: "url", url: "/safe" },
      },
      { item: { id: "one", slug: "slug" } },
    ),
  ).toEqual({
    id: "one",
    page: { kind: "page", page: "id", params: { slug: "slug" } },
    url: { kind: "url", url: "/safe" },
  });
  expect(() => operands({ x: { lit: 1, extra: true } as never }, {})).toThrow();
  expect(() => operands({ x: "$data.one" }, {}, true)).toThrow("CMS_SOURCE_PARAMS_INVALID");
});
it("SDK-014: параметры источника проверяются до вызова загрузчика приложения", async () => {
  const loader = vi.fn(() => ["one"]),
    definition = source(loader);
  const signal = new AbortController().signal;
  expect(
    await loadSource(
      { source: definition.name },
      [definition],
      {},
      { app: { private: "server" }, signal },
    ),
  ).toEqual(["one"]);
  expect(loader).toHaveBeenCalledWith(
    { limit: 2 },
    expect.objectContaining({ app: { private: "server" }, signal: expect.any(AbortSignal) }),
  );
  expect(
    await loadSource(
      { source: definition.name, params: { limit: "$context.limit" } },
      [definition],
      { context: { limit: 3 } },
    ),
  ).toEqual(["one"]);
  expect(
    await loadSource({ source: definition.name, params: { limit: { lit: 0 } } }, [definition], {}),
  ).toEqual({ error: { code: "CMS_SOURCE_PARAMS_INVALID" } });
  expect(loader).toHaveBeenCalledTimes(2);
  expect(await loadSource({ source: "missing" }, [], {})).toEqual({
    error: { code: "CMS_SOURCE_UNKNOWN" },
  });
  expect(await loadSource({ source: definition.name }, [definition], {}, { timeoutMs: 0 })).toEqual(
    { error: { code: "CMS_SOURCE_TIMEOUT_INVALID" } },
  );
  expect(await loadSource({ source: definition.name }, [definition], {}, { cursor: "x" })).toEqual({
    error: { code: "CMS_SOURCE_CURSOR_INVALID" },
  });
  expect(
    await loadSource(
      { source: definition.name },
      [
        source(() => {
          throw new Error("SECRET");
        }),
      ],
      {},
    ),
  ).toEqual({ error: { code: "CMS_SOURCE_FAILED" } });
  expect(await loadSource({ source: definition.name }, [source(() => [1])], {})).toEqual({
    error: { code: "CMS_SOURCE_RESULT_INVALID" },
  });
});
it("SDK-014: независимые источники запускаются параллельно, отмена и таймаут прерывают сигнал", async () => {
  vi.useFakeTimers();
  const signals: AbortSignal[] = [];
  const loader = vi.fn((_args: never, ctx: never) => {
    signals.push((ctx as LoaderContext).signal);
    return new Promise(() => {});
  });
  const definition = source(loader);
  const pending = loadSources(
    { one: { source: definition.name }, two: { source: definition.name } },
    [definition],
    {},
    { timeoutMs: 3 },
  );
  await Promise.resolve();
  expect(loader).toHaveBeenCalledTimes(2);
  await vi.advanceTimersByTimeAsync(3);
  expect(await pending).toEqual({
    one: { error: { code: "CMS_SOURCE_TIMEOUT" } },
    two: { error: { code: "CMS_SOURCE_TIMEOUT" } },
  });
  expect(signals.every((signal) => signal.aborted)).toBe(true);
  const controller = new AbortController();
  const canceled = loadSource(
    { source: definition.name },
    [definition],
    {},
    { signal: controller.signal },
  );
  await Promise.resolve();
  controller.abort();
  expect(await canceled).toEqual({ error: { code: "CMS_SOURCE_ABORTED" } });
  expect(
    await loadSource({ source: definition.name }, [definition], {}, { signal: controller.signal }),
  ).toEqual({ error: { code: "CMS_SOURCE_ABORTED" } });
  expect(
    await loadSources(
      Object.fromEntries(
        Array.from({ length: 65 }, (_, i) => [String(i), { source: definition.name }]),
      ),
      [definition],
      {},
    ),
  ).toEqual({ error: { code: "CMS_SOURCE_LIMIT" } });
});
it("IR-6.6: пагинация требует cursor, проверяет каждую порцию и ограничивает накопление", async () => {
  const definition = { ...source(), paginated: true };
  const first = { items: ["one"], hasMore: true, cursor: "next" };
  expect(sourceResult(definition, first)).toEqual(first);
  expect(appendPage(definition, first, { items: ["two"], hasMore: false })).toEqual({
    items: ["one", "two"],
    hasMore: false,
  });
  for (const invalid of [
    null,
    { items: [1], hasMore: false },
    { items: [], hasMore: true },
    { items: [], hasMore: true, cursor: "" },
    { items: [], hasMore: false, cursor: 1 },
    { items: [], hasMore: false, cursor: "a".repeat(2049) },
    { items: [], hasMore: false, extra: true },
    { items: [], hasMore: 1 },
  ])
    expect(() => sourceResult(definition, invalid)).toThrow();
  expect(() => sourceResult({ ...definition, result: { type: "string" } }, first)).toThrow();
  expect(() => appendPage(source(), first, first)).toThrow();
  expect(() => appendPage(definition, { ...first, hasMore: false }, first)).toThrow();
  expect(() => appendPage(definition, first, first)).toThrow();
  expect(() =>
    appendPage(
      definition,
      { ...first, items: Array(2000).fill("one") },
      { items: ["two"], hasMore: false },
    ),
  ).toThrow();
  const loader = vi.fn(() => ({ items: ["two"], hasMore: false }));
  expect(
    await loadSource(
      { source: definition.name },
      [{ ...definition, loader }],
      {},
      { cursor: "next" },
    ),
  ).toEqual({ items: ["two"], hasMore: false });
  expect(loader).toHaveBeenCalledWith({ limit: 2 }, expect.objectContaining({ cursor: "next" }));
  for (const cursor of ["", "a".repeat(2049)])
    expect(await loadSource({ source: definition.name }, [definition], {}, { cursor })).toEqual({
      error: { code: "CMS_SOURCE_CURSOR_INVALID" },
    });
});
it("SDK-014/IR-050: SSR загружает экземпляры с разрешёнными props и областью Repeat", async () => {
  const definition = {
    name: "test.title",
    params: { title: { type: "string" as const, required: true } },
    result: { type: "string" as const },
    loader: vi.fn((args: never) => (args as { title: string }).title),
  };
  const component: IrDocument = {
    irVersion: "1.0",
    kind: "component",
    root: "button",
    inputs: { title: { type: "string" } },
    dataSources: { title: { source: definition.name, params: { title: "$props.title" } } },
    nodes: {
      button: {
        id: "button",
        type: "Button",
        bindings: { label: "$data.title" },
        on: { click: { action: "test.select", args: { id: "$props.title" } } },
      },
    },
  };
  const doc: IrDocument = {
    irVersion: "1.0",
    kind: "page",
    root: "repeat",
    nodes: {
      repeat: {
        id: "repeat",
        type: "Repeat",
        props: { items: ["one", "two"] },
        children: ["comp"],
      },
      comp: {
        id: "comp",
        type: "Composed",
        ref: { component: "id" },
        bindings: { title: "$item" },
      },
    },
  };
  const result = await preparePageWithData(doc, {
    components: { id: component },
    sources: [definition],
    actions: [action()],
  });
  expect(result.diagnostics).toEqual([]);
  expect(result.roots.map((node) => node.props.label)).toEqual(["one", "two"]);
  expect(result.roots.map((node) => node.events!.click![0]!.args.id)).toEqual(["one", "two"]);
  expect(definition.loader).toHaveBeenCalledTimes(2);
  const plain = {
    ...doc,
    nodes: { repeat: { id: "repeat", type: "Text", props: { text: "plain" } } },
  } as IrDocument;
  expect((await preparePageWithData(plain)).roots[0]!.props.text).toBe("plain");
  const unknown = { ...component, kind: "page" as const };
  expect(preparePage(unknown).diagnostics[0]!.code).toBe("CMS_ACTION_UNKNOWN");
});
