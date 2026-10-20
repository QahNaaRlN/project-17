import { expect, it, vi } from "vitest";
import { loadSource } from "../src/sources.js";

it("STU-387: a data source placeholder is never loaded and reports CMS_SOURCE_PENDING", async () => {
  const loader = vi.fn(async () => ({ items: [] }));
  const result = await loadSource(
    { source: "store.recommendations", pending: "0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11" },
    [
      {
        name: "store.recommendations",
        params: {},
        result: { type: "object", fields: {} },
        loader,
      } as never,
    ],
    {},
  );
  expect(loader).not.toHaveBeenCalled();
  expect(result).toMatchObject({ error: { code: "CMS_SOURCE_PENDING" } });
});
