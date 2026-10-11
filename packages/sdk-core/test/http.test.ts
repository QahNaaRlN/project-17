import { createServer } from "node:http";
import { once } from "node:events";
import { expect, it } from "vitest";
import { createCmsClient } from "../src/index.js";
import { config, page } from "./fixtures.js";

// API-033 / SDK-020: exercise native Fetch, Headers, byte streaming and actual 304 over HTTP.
it("настоящий HTTP транспорт повторно проверяет ETag и не следует redirects", async () => {
  const requests: {
    url: string;
    auth: string | undefined;
    etag: string | undefined;
    cookie: string | undefined;
  }[] = [];
  const server = createServer((req, res) => {
    requests.push({
      url: req.url!,
      auth: req.headers.authorization,
      etag: req.headers["if-none-match"],
      cookie: req.headers.cookie,
    });
    if (req.url?.includes("redirect")) {
      res.writeHead(302, { Location: "/leaked" });
      res.end();
      return;
    }
    if (req.headers["if-none-match"] === '"http-v1"') {
      res.writeHead(304, { ETag: '"http-v1"' });
      res.end();
      return;
    }
    res.writeHead(200, { "Content-Type": "application/json", ETag: '"http-v1"' });
    const text = JSON.stringify(page);
    res.write(text.slice(0, 30));
    res.end(text.slice(30));
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Missing address");
  let now = 0;
  const client = createCmsClient({
    ...config,
    baseUrl: `http://127.0.0.1:${address.port}`,
    now: () => now,
    cache: { ttlMs: 1 },
  });
  try {
    expect(await client.getPage("/")).toEqual(page);
    now = 1;
    expect(await client.getPage("/")).toEqual(page);
    expect(requests[1]).toMatchObject({
      auth: "Bearer cms_pub_secret",
      etag: '"http-v1"',
      cookie: undefined,
    });
    await expect(client.getPage("/redirect")).rejects.toMatchObject({ code: "NETWORK_ERROR" });
    expect(requests).toHaveLength(3);
    expect(requests.some((r) => r.url === "/leaked")).toBe(false);
  } finally {
    server.closeAllConnections();
    server.close();
    await once(server, "close");
  }
});

// API-040: in-flight native body reading must obey AbortSignal and remove listeners.
it("настоящий Fetch прерывает чтение незавершённого тела по AbortSignal", async () => {
  let reached!: () => void;
  const headersWritten = new Promise<void>((resolve) => {
    reached = resolve;
  });
  const server = createServer((_req, res) => {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.write("{");
    reached();
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Missing address");
  const controller = new AbortController();
  try {
    const client = createCmsClient({ ...config, baseUrl: `http://127.0.0.1:${address.port}` });
    const promise = client.getPage("/", { signal: controller.signal });
    const assertion = expect(promise).rejects.toMatchObject({ code: "ABORTED" });
    await headersWritten;
    controller.abort();
    await assertion;
  } finally {
    server.closeAllConnections();
    server.close();
    await once(server, "close");
  }
});
