import { expect, it, vi } from "vitest";
import type { CmsClient, PageResponse, ReadOptions } from "@cms/core";
import type { IrDocument } from "@cms/ir";
import { loadPage } from "../src/server.js";
import { createRegistry, defineAction, defineDataSource, t } from "@cms/component-registry";
const id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11";
function component(): IrDocument {
  return {
    irVersion: "1.0",
    kind: "component",
    root: "text",
    nodes: { text: { id: "text", type: "Text", props: { text: "Loaded" } } },
  };
}
function page(): PageResponse {
  return {
    page: { objectId: id, versionId: id, path: "/a", params: {} },
    document: {
      irVersion: "1.0",
      kind: "page",
      root: "root",
      nodes: { root: { id: "root", type: "Composed", ref: { component: id }, props: {} } },
    },
    components: {},
    data: {},
    dataSources: {},
  };
}
function cms(): CmsClient {
  return {
    getPage: vi.fn().mockResolvedValue(page()),
    getDocument: vi.fn().mockResolvedValue({ document: component() }),
    getEntity: vi.fn(),
    getAsset: vi.fn(),
    getRoutes: vi.fn(),
    getImage: vi.fn(),
    clearCache: vi.fn(),
    report: vi.fn(),
    flushTelemetry: vi.fn(),
  };
}
it("MF-010/SDK-014/033: loadPage загружает данные до SSR и передаёт режим preview без секретов", async () => {
  const client = cms(),
    response = page();
  const loader = vi.fn(() => ["one", "two"]),
    handler = vi.fn();
  const registry = createRegistry({
    components: [],
    actions: [defineAction({ name: "test.select", args: { id: t.string().required() }, handler })],
    dataSources: [
      defineDataSource({ name: "test.items", params: {}, result: t.list(t.string()), loader }),
    ],
    breakpoints: { sm: 640 },
  });
  response.document = {
    irVersion: "1.0",
    kind: "page",
    root: "repeat",
    dataSources: { featured: { source: "test.items" } },
    nodes: {
      repeat: {
        id: "repeat",
        type: "Repeat",
        bindings: { items: "$data.featured" },
        children: ["button"],
      },
      button: {
        id: "button",
        type: "Button",
        bindings: { label: "$item" },
        on: { click: { action: "test.select", args: { id: "$item" } } },
      },
    },
  };
  vi.mocked(client.getPage).mockResolvedValue(response);
  const read = {
    preview: { token: "private-preview", changesetId: id },
    signal: new AbortController().signal,
  };
  const plan = await loadPage(client, "/a", {
    registry,
    read,
    sourceOptions: { app: { secret: "private-loader-key" } },
  });
  expect(plan!.roots.map((node) => node.props.label)).toEqual(["one", "two"]);
  expect(plan!.roots.map((node) => node.events!.click![0]!.args.id)).toEqual(["one", "two"]);
  expect(plan!.interactionMode).toBe("design");
  expect(plan!.diagnostics).toEqual([]);
  expect(loader).toHaveBeenCalledOnce();
  expect(loader).toHaveBeenCalledWith(
    {},
    expect.objectContaining({
      app: { secret: "private-loader-key" },
      signal: expect.any(AbortSignal),
    }),
  );
  expect(handler).not.toHaveBeenCalled();
  expect(JSON.stringify(plan)).not.toMatch(/private-preview|private-loader-key|loader|handler/);
  expect(
    (await loadPage(client, "/a", { registry, read, mode: "interact" }))!.interactionMode,
  ).toBe("interact");
});
it("loads live documents with the same preview scope without serializing credentials", async () => {
  const client = cms();
  const read: ReadOptions = {
    preview: { token: "server-only-token", changesetId: id },
    signal: new AbortController().signal,
  };
  const plan = await loadPage(client, "/a", { read });
  expect(client.getPage).toHaveBeenCalledWith("/a", read);
  expect(client.getDocument).toHaveBeenCalledWith(id, read);
  expect(plan!.roots[0]!.props.text).toBe("Loaded");
  expect(JSON.stringify(plan)).not.toContain("server-only-token");
});
it("returns null for absent page and propagates page failures for host handling", async () => {
  const client = cms();
  vi.mocked(client.getPage).mockResolvedValue(null);
  expect(await loadPage(client, "/missing")).toBeNull();
  vi.mocked(client.getPage).mockRejectedValue(new Error("page failure"));
  await expect(loadPage(client, "/a")).rejects.toThrow("page failure");
});
it("propagates canceled component reads", async () => {
  const client = cms();
  const controller = new AbortController();
  controller.abort();
  vi.mocked(client.getDocument).mockRejectedValue(new Error("Canceled"));
  await expect(loadPage(client, "/a", { read: { signal: controller.signal } })).rejects.toThrow(
    "Canceled",
  );
});
it("uses delivered/explicit components and data without refetching", async () => {
  const client = cms();
  const response = page();
  response.components[id] = { versionId: id, document: component() };
  response.data = { title: "From data" };
  response.components[id]!.document.nodes.text!.props = {};
  response.components[id]!.document.nodes.text!.bindings = { text: "$data.title" };
  vi.mocked(client.getPage).mockResolvedValue(response);
  expect((await loadPage(client, "/a"))!.roots[0]!.props.text).toBe("From data");
  expect(client.getDocument).not.toHaveBeenCalled();
  expect(
    (await loadPage(client, "/a", { components: { [id]: component() } }))!.roots[0]!.props.text,
  ).toBe("Loaded");
});
it.each([null, new Error("private-token-value")])(
  "reports missing/unreadable components safely",
  async (result) => {
    const client = cms();
    if (result instanceof Error) vi.mocked(client.getDocument).mockRejectedValue(result);
    else vi.mocked(client.getDocument).mockResolvedValue(null);
    const plan = await loadPage(client, "/a");
    expect(plan!.diagnostics[0]!.code).toBe("CMS_COMPONENT_MISSING");
    expect(JSON.stringify(plan)).not.toContain("private-token-value");
    // OPS-023: a published render reports its diagnostics for telemetry.
    expect(client.report).toHaveBeenCalledWith([
      { code: "CMS_COMPONENT_MISSING", source: "render" },
    ]);
  },
);
it("does not fetch pinned/invalid refs and ignores non-composed nodes", async () => {
  const client = cms();
  const response = page();
  response.document.nodes.root!.ref!.version = 2;
  response.document.nodes.unused = { id: "unused", type: "Composed" };
  response.document.nodes.text = { id: "text", type: "Text", props: { text: "Hello" } };
  vi.mocked(client.getPage).mockResolvedValue(response);
  expect((await loadPage(client, "/a"))!.diagnostics[0]!.code).toBe(
    "CMS_COMPONENT_VERSION_UNSUPPORTED",
  );
  expect(client.getDocument).not.toHaveBeenCalled();
});
it("fetches shared refs once and diagnoses recursion", async () => {
  const client = cms();
  const response = page();
  response.document.nodes.other = { ...response.document.nodes.root!, id: "other" };
  const recursive = component();
  recursive.nodes.text = { id: "text", type: "Composed", ref: { component: id } };
  vi.mocked(client.getPage).mockResolvedValue(response);
  vi.mocked(client.getDocument).mockResolvedValue({
    objectId: id,
    versionId: id,
    path: null,
    document: recursive,
  });
  expect((await loadPage(client, "/a"))!.diagnostics[0]!.code).toBe("CMS_COMPONENT_CYCLE");
  expect(client.getDocument).toHaveBeenCalledTimes(1);
});
it("loads nested explicit live references and ignores refs on ordinary primitives", async () => {
  const client = cms();
  const next = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c12";
  const response = page();
  response.document.nodes.root!.ref!.version = "live";
  response.document.nodes.extra = {
    id: "extra",
    type: "Text",
    props: { text: "Plain" },
    ref: { component: "unused" },
  };
  const outer = component();
  outer.nodes.text = { id: "text", type: "Composed", ref: { component: next, version: "live" } };
  vi.mocked(client.getPage).mockResolvedValue(response);
  vi.mocked(client.getDocument)
    .mockResolvedValueOnce({ objectId: id, versionId: id, path: null, document: outer })
    .mockResolvedValueOnce({ objectId: next, versionId: next, path: null, document: component() });
  const prepared = await loadPage(client, "/a");
  expect(prepared!.roots[0]!.props.text).toBe("Loaded");
  expect(client.getDocument).toHaveBeenCalledTimes(2);
  expect(client.getDocument).toHaveBeenLastCalledWith(next, undefined);
});
it("diagnoses failed component reads with explicit read options lacking a signal", async () => {
  const client = cms();
  vi.mocked(client.getDocument).mockRejectedValue(new Error("private-detail"));
  expect((await loadPage(client, "/a", { read: {} }))!.diagnostics).toEqual([
    { code: "CMS_COMPONENT_MISSING", nodeId: "root" },
  ]);
});
it("bounds component fetch fanout", async () => {
  const client = cms();
  const response = page();
  response.document.nodes.root = { id: "root", type: "Box", props: {}, children: [] };
  for (let i = 0; i < 65; i++) {
    const key = `node${i}`;
    response.document.nodes[key] = {
      id: key,
      type: "Composed",
      ref: { component: `0192f1c7-4b1e-7c2b-9d10-${i.toString().padStart(12, "0")}` },
    };
    response.document.nodes.root.children!.push(key);
  }
  vi.mocked(client.getPage).mockResolvedValue(response);
  const plan = await loadPage(client, "/a");
  expect(client.getDocument).toHaveBeenCalledTimes(64);
  expect(plan!.roots).toEqual([]);
  expect(plan!.css).toBe("");
  expect(plan!.diagnostics[0]!.code).toBe("CMS_COMPONENT_LOAD_LIMIT");
});

it("OPS-023: sources and actions are classified; preview renders report nothing", async () => {
  const client = cms();
  const response = page();
  response.diagnostics = [
    { code: "CMS_SOURCE_TIMEOUT", nodeId: "root", severity: "warning", pointer: "", message: "" },
    { code: "CMS_ACTION_FAILED", nodeId: "root", severity: "warning", pointer: "", message: "" },
  ];
  vi.mocked(client.getPage).mockResolvedValue(response);
  await loadPage(client, "/a");
  expect(vi.mocked(client.report).mock.calls[0]![0]).toEqual(
    expect.arrayContaining([
      { code: "CMS_SOURCE_TIMEOUT", source: "source" },
      { code: "CMS_ACTION_FAILED", source: "action" },
    ]),
  );
  const preview = cms();
  await loadPage(preview, "/a", { read: { preview: { token: "cms_preview_token_value_123" } } });
  expect(preview.report).not.toHaveBeenCalled();
});
