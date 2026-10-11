import { expect, it, vi } from "vitest";
import { createCmsClient } from "../src/index.js";
import { config, page, json } from "./fixtures.js";

// SDK-020: concurrent 304 may finish after its entry was evicted; capacity must stay bounded.
it("завершение 304 после вытеснения записи сохраняет лимит LRU", async () => {
  let now = 0;
  const fetch = vi
    .fn<typeof globalThis.fetch>()
    .mockImplementation(async () => json(page, { ETag: '"v"' }));
  const client = createCmsClient({
    ...config,
    fetch,
    now: () => now,
    cache: { capacity: 1, ttlMs: 1 },
  });
  await client.getPage("/a");
  now = 1;
  let resolve!: (r: Response) => void;
  fetch.mockReturnValueOnce(
    new Promise((r) => {
      resolve = r;
    }),
  );
  const request = client.getPage("/a");
  await client.getPage("/b");
  resolve(new Response(null, { status: 304 }));
  await request;
  await client.getPage("/b");
  expect(fetch).toHaveBeenCalledTimes(4);
});
// SDK-020: protocol failures must not retain ETags referring to a previously valid body.
it("повреждённый revalidation удаляет старый ETag", async () => {
  let now = 0;
  const fetch = vi
    .fn<typeof globalThis.fetch>()
    .mockResolvedValueOnce(json(page, { ETag: '"v"' }))
    .mockResolvedValueOnce(json({ invalid: true }))
    .mockResolvedValueOnce(json(page));
  const client = createCmsClient({ ...config, fetch, now: () => now, cache: { ttlMs: 1 } });
  await client.getPage("/");
  now = 1;
  await expect(client.getPage("/")).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  await client.getPage("/");
  expect(new Headers(fetch.mock.calls[2]![1]!.headers).get("If-None-Match")).toBeNull();
});
