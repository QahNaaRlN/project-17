import { afterEach, expect, it, vi } from "vitest";
import { createCmsClient, CmsError } from "../src/index.js";

const key = "cms_pub_" + "a".repeat(40);
function client(extra: Record<string, unknown> = {}) {
  const fetch = vi.fn(async (..._args: unknown[]) => new Response(null, { status: 204 }));
  const cms = createCmsClient({
    baseUrl: "https://cms.example/",
    project: "store",
    environment: "production",
    deliveryKey: key,
    fetch: fetch as unknown as typeof globalThis.fetch,
    telemetry: { sampleRate: 1, intervalMs: 1000, ...extra },
  });
  return { cms, fetch };
}
afterEach(() => vi.useRealTimers());

it("OPS-023/STU-605: diagnostics are aggregated by code and source and sent with the key", async () => {
  vi.useFakeTimers();
  const { cms, fetch } = client();
  cms.report([{ code: "CMS_SOURCE_TIMEOUT", source: "source" }, { code: "CMS_NODE_UNKNOWN" }]);
  cms.report([
    { code: "CMS_SOURCE_TIMEOUT", source: "source" },
    { code: "bad code" },
    { code: "CMS_X", source: "browser" as "render" },
    null as unknown as { code: string },
  ]);
  expect(fetch).not.toHaveBeenCalled();
  await vi.advanceTimersByTimeAsync(1000);
  expect(fetch).toHaveBeenCalledTimes(1);
  const [url, init] = fetch.mock.calls[0] as [string, RequestInit];
  expect(url).toBe("https://cms.example/delivery/v1/store/production/telemetry");
  expect(init.method).toBe("POST");
  expect((init.headers as Record<string, string>).Authorization).toBe("Bearer " + key);
  expect(JSON.parse(String(init.body))).toEqual({
    diagnostics: [
      { code: "CMS_SOURCE_TIMEOUT", source: "source", count: 2 },
      { code: "CMS_NODE_UNKNOWN", source: "render", count: 1 },
      { code: "CMS_X", source: "render", count: 1 },
    ],
  });
  await cms.flushTelemetry();
  expect(fetch).toHaveBeenCalledTimes(1);
});

it("STU-605: sampling weighs reports; failures and missing keys never throw", async () => {
  const sampled = client({ sampleRate: 0.25, random: () => 0.1 });
  sampled.cms.report([{ code: "CMS_NODE_UNKNOWN" }]);
  await sampled.cms.flushTelemetry();
  expect(
    JSON.parse(String((sampled.fetch.mock.calls[0] as [string, RequestInit])[1].body))
      .diagnostics[0].count,
  ).toBe(4);
  const skipped = client({ sampleRate: 0.25, random: () => 0.9 });
  skipped.cms.report([{ code: "CMS_NODE_UNKNOWN" }]);
  await skipped.cms.flushTelemetry();
  expect(skipped.fetch).not.toHaveBeenCalled();
  const failing = client();
  failing.fetch.mockRejectedValue(new Error("down"));
  failing.cms.report([{ code: "CMS_NODE_UNKNOWN" }]);
  await expect(failing.cms.flushTelemetry()).resolves.toBeUndefined();
  for (const off of [{ sampleRate: 0 }]) {
    const c = client(off);
    c.cms.report([{ code: "CMS_NODE_UNKNOWN" }]);
    await c.cms.flushTelemetry();
    expect(c.fetch).not.toHaveBeenCalled();
  }
  const disabled = createCmsClient({
    baseUrl: "https://cms.example/",
    project: "store",
    environment: "production",
    deliveryKey: key,
    telemetry: false,
  });
  disabled.report([{ code: "CMS_NODE_UNKNOWN" }]);
  await disabled.flushTelemetry();
  const keyless = createCmsClient({
    baseUrl: "https://cms.example/",
    project: "store",
    environment: "production",
  });
  keyless.report([{ code: "CMS_NODE_UNKNOWN" }]);
  await keyless.flushTelemetry();
  keyless.report("x" as never);
  expect(() => client({ sampleRate: 2 })).toThrow(CmsError);
  expect(() => client({ intervalMs: 0 })).toThrow(CmsError);
});
