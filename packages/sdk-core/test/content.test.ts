import { expect, it, vi } from "vitest";
import { createCmsClient } from "../src/index.js";
import { page as decodePage } from "../src/decode.js";
import { config, page, json, ID, VERSION, model } from "./fixtures.js";
it("STU-210/211/215: locale изолирует кеш и передаётся в Delivery; preview обходит кеш", async () => {
  const fetch = vi.fn<typeof globalThis.fetch>().mockImplementation(async (url) =>
    json({
      ...page,
      data: { content: { title: String(url).includes("locale=en") ? "Title" : "Заголовок" } },
      contentContext: { locale: "ru", versions: { [ID]: VERSION } },
    }),
  );
  const client = createCmsClient({ ...config, fetch });
  expect((await client.getPage("/", { locale: "ru" }))!.data.content).toEqual({
    title: "Заголовок",
  });
  expect((await client.getPage("/", { locale: "en" }))!.data.content).toEqual({ title: "Title" });
  await client.getPage("/", { locale: "ru" });
  expect(fetch).toHaveBeenCalledTimes(2);
  await client.getPage("/", { locale: "ru", preview: { token: "test-preview" } });
  await client.getPage("/", { locale: "ru", preview: { token: "test-preview" } });
  expect(fetch).toHaveBeenCalledTimes(4);
  await expect(client.getPage("/", { locale: "../bad" })).rejects.toThrow();
});
it("STU-210: decoder проверяет проекцию и отдельные версии зависимостей", () => {
  for (const data of [{ content: null }, { content: [] }, { content: "private" }])
    expect(() => decodePage({ ...page, data })).toThrow();
  for (const contentContext of [
    null,
    { locale: 1, versions: {} },
    { locale: "ru", versions: { [ID]: "bad" } },
    { locale: "ru", versions: [] },
  ])
    expect(() => decodePage({ ...page, contentContext })).toThrow();
  expect(decodePage(page)).toEqual(page);
});
it("STU-212: locale страницы не добавляется к строгому image API", async () => {
  const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(json(model));
  const client = createCmsClient({ ...config, fetch });
  await client.getImage(ID, { locale: "ru" });
  expect(new URL(String(fetch.mock.calls[0]![0])).searchParams.has("locale")).toBe(false);
});
