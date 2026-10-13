import { expect, it, vi } from "vitest";
import { createProxy } from "../src/proxy.js";
const env = {
  CMS_MCP_URL: "https://cms.test/mcp",
  CMS_PROJECT: "store",
  CMS_AGENT_TOKEN: "cms_agt_test",
};
it("AGT-010/011/023: stdio forwards only to configured CMS with a scoped token", async () => {
  const fetcher = vi.fn<typeof fetch>(async () =>
    Response.json({ jsonrpc: "2.0", id: 1, result: { tools: [] } }),
  );
  const proxy = createProxy(env, fetcher);
  const request = JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/list" });
  expect(JSON.parse((await proxy(request))!)).toMatchObject({ result: { tools: [] } });
  expect(fetcher.mock.calls[0]![1]).toMatchObject({
    redirect: "error",
    headers: {
      Authorization: "Bearer cms_agt_test",
      "X-CMS-Project": "store",
      "MCP-Protocol-Version": "2025-06-18",
    },
    body: request,
  });
  expect(
    await createProxy(
      { ...env, CMS_MCP_URL: "http://localhost/mcp" },
      async () => new Response(null, { status: 202 }),
    )(JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" })),
  ).toBeUndefined();
});
it.each([
  "http://remote/mcp",
  "https://cms.test/raw",
  "https://user:pass@cms.test/mcp",
  "https://cms.test/mcp?token=x",
  "https://cms.test/mcp#token",
])("AGT-052: rejects arbitrary endpoints %s", (url) =>
  expect(() => createProxy({ ...env, CMS_MCP_URL: url })).toThrow("MCP_CONFIG_INVALID"),
);
it("AGT-023: rejects service tokens and malformed project config", () => {
  expect(() => createProxy({ ...env, CMS_AGENT_TOKEN: "cms_svc_test" })).toThrow();
  expect(() => createProxy({ ...env, CMS_PROJECT: "../project" })).toThrow();
  expect(() => createProxy({})).toThrow();
});
it("AGT-011: hides upstream errors and checks framing and response ids", async () => {
  const broken = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.error(new Error("private provider body"));
    },
  });
  expect(
    await createProxy(
      env,
      async () => new Response(broken, { headers: { "Content-Type": "application/json" } }),
    )('{"jsonrpc":"2.0","id":1,"method":"ping"}'),
  ).toContain("CMS MCP request failed");
  for (const body of [
    "bad",
    JSON.stringify({ jsonrpc: "1.0", id: 1 }),
    JSON.stringify({ jsonrpc: "2.0", id: 2 }),
    "x".repeat(1024 * 1024 + 1),
  ]) {
    const value = await createProxy(
      env,
      async () => new Response(body, { headers: { "Content-Type": "application/json" } }),
    )('{"jsonrpc":"2.0","id":1,"method":"ping"}');
    expect(value).toContain("CMS MCP request failed");
    expect(value).not.toContain(env.CMS_AGENT_TOKEN);
  }
  for (const response of [
    new Response("secret", { status: 401 }),
    new Response("secret"),
    new Response(null, { headers: { "Content-Type": "application/json" } }),
  ])
    expect(
      await createProxy(env, async () => response)('{"jsonrpc":"2.0","id":1,"method":"ping"}'),
    ).toContain("CMS MCP request failed");
  expect(
    await createProxy(env, async () => {
      throw new Error(env.CMS_AGENT_TOKEN);
    })('{"jsonrpc":"2.0","id":1,"method":"ping"}'),
  ).not.toContain(env.CMS_AGENT_TOKEN);
  const proxy = createProxy(env);
  for (const input of ["bad", "{}", "null", "x".repeat(1024 * 1024 + 1)])
    expect(await proxy(input)).toContain("CMS MCP request failed");
});
