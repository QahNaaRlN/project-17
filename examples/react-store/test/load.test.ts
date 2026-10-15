import { afterEach, describe, it, expect, vi } from "vitest";
import { loadStore } from "../src/load.js";
import { cms, id, page, image, now } from "./fixtures.js";
import { catalog } from "../scripts/catalog.js";
afterEach(() => vi.restoreAllMocks());
describe("STORE-001/005/006/007: чтение CMS", () => {
  it("STU-141: SSR разрешает дизайн-токены приложения", async () => {
    const c = cms(),
      data = page(catalog);
    data.document.nodes.n_about!.design = { padding: "lg" };
    vi.mocked(c.getPage).mockResolvedValue(data);
    const result = await loadStore(c, "/store");
    expect(result.status).toBe("ready");
    expect(JSON.stringify(result.plan)).toContain("--cms-spacing-lg:24px");
  });
  it("STU-102/104: данные контента страницы доступны RichText и preview reload", async () => {
    const c = cms(),
      data = page(catalog);
    data.document.nodes.n_store!.children!.push("n_rich");
    data.document.nodes.n_rich = {
      id: "n_rich",
      type: "RichText",
      bindings: { value: "$data.article.body" },
    };
    data.data = {
      article: {
        body: {
          type: "doc",
          content: [{ type: "paragraph", content: [{ type: "text", text: "CMS article" }] }],
        },
      },
    };
    vi.mocked(c.getPage).mockResolvedValue(data);
    const result = await loadStore(
      c,
      "/store",
      { preview: { token: "test", changesetId: id } },
      { studioOrigin: "https://studio.test", manifestHash: "sha256:" + "a".repeat(64) },
    );
    expect(result.status).toBe("ready");
    expect(JSON.stringify(result.plan)).toContain("CMS article");
    expect(result.bridge!.data).toMatchObject(data.data);
    expect(result.bridge!.data).toHaveProperty("featured");
  });
  it("SDK-032/STU-033: рендерит локальный перевод в запрошенной локали", async () => {
    const c = cms(),
      data = page(catalog);
    data.document.localContent = {
      t_title: { type: "text", value: { ru: "Русский", en: "English" } },
    };
    data.document.nodes.n_store!.children!.push("n_title");
    data.document.nodes.n_title = {
      id: "n_title",
      type: "Heading",
      bindings: { text: "$local.t_title" },
    };
    vi.mocked(c.getPage).mockResolvedValue(data);
    const result = await loadStore(c, "/store", { locale: "en" });
    expect(result.locale).toBe("en");
    expect(JSON.stringify(result.plan)).toContain("English");
    expect(JSON.stringify(result.plan)).not.toContain("Русский");
  });
  it("читает страницу без preview", async () => {
    const c = cms();
    const result = await loadStore(c, "/store");
    expect(result.catalog).toEqual(catalog);
    expect(result.preview).toBe(false);
    expect(c.getPage).toHaveBeenCalledWith("/store", { locale: "ru" });
  });
  it("передаёт preview и signal для страницы и изображений, возвращает только публичную модель", async () => {
    vi.spyOn(Date, "now").mockReturnValue(now);
    const c = cms();
    const data = structuredClone(catalog);
    data.heroAsset = { assetId: id };
    data.products[0]!.asset = { assetId: id };
    vi.mocked(c.getPage).mockResolvedValue(page(data));
    const options = {
      preview: { token: "private-preview-token", changesetId: id },
      signal: new AbortController().signal,
    };
    const result = await loadStore(c, "/store", options);
    expect(result.preview).toBe(true);
    expect(result.images.hero?.loading).toBe("eager");
    expect(result.images["product:vase"]?.loading).toBe("lazy");
    expect(result.images["product:vase"]?.sizes).toContain("112px");
    expect(result.refreshAt).toBe(now + 270_000);
    expect(c.getImage).toHaveBeenCalledWith(id, expect.objectContaining(options));
    expect(JSON.stringify(result)).not.toContain("private-preview-token");
  });
  it("выбирает самое раннее время обновления изображений", async () => {
    vi.spyOn(Date, "now").mockReturnValue(now);
    const c = cms();
    const data = structuredClone(catalog);
    data.heroAsset = { assetId: id };
    data.products[0]!.asset = { assetId: id };
    vi.mocked(c.getPage).mockResolvedValue(page(data));
    vi.mocked(c.getImage)
      .mockResolvedValueOnce(image())
      .mockResolvedValueOnce({ ...image(), expiresAt: new Date(now + 60_000).toISOString() });
    expect((await loadStore(c, "/store")).refreshAt).toBe(now + 30_000);
  });
  it.each(["missing", "error", "invalid"])("возвращает %s без утечки ошибки", async (mode) => {
    const c = cms();
    if (mode === "missing") vi.mocked(c.getPage).mockResolvedValue(null);
    if (mode === "error") vi.mocked(c.getPage).mockRejectedValue(new Error("SECRET"));
    if (mode === "invalid") vi.mocked(c.getPage).mockResolvedValue(page({}));
    const result = await loadStore(c, "/store");
    expect(result.status).toBe(mode === "missing" ? "missing" : "error");
    expect(JSON.stringify(result)).not.toContain("SECRET");
    expect(result.catalog).toBeUndefined();
  });
  it.each(["missing", "failed", "expired"])(
    "ошибка одного изображения %s сохраняет контент",
    async (mode) => {
      vi.spyOn(Date, "now").mockReturnValue(now);
      const c = cms();
      const data = structuredClone(catalog);
      data.products[0]!.asset = { assetId: id };
      vi.mocked(c.getPage).mockResolvedValue(page(data));
      if (mode === "missing") vi.mocked(c.getImage).mockResolvedValue(null);
      if (mode === "failed") vi.mocked(c.getImage).mockRejectedValue(new Error("SECRET"));
      if (mode === "expired")
        vi.mocked(c.getImage).mockResolvedValue({
          ...image(),
          expiresAt: new Date(now).toISOString(),
        });
      const result = await loadStore(c, "/store");
      expect(result.status).toBe("ready");
      expect(result.images["product:vase"]).toBeNull();
      expect(result.refreshAt).toBeUndefined();
    },
  );
  it("ключи предметов hero/__proto__ не заменяют главное изображение или прототип", async () => {
    vi.spyOn(Date, "now").mockReturnValue(now);
    const c = cms();
    const data = structuredClone(catalog);
    data.heroAsset = { assetId: id };
    data.products[0]!.id = "hero";
    data.products[1]!.id = "__proto__";
    data.products[0]!.asset = data.products[1]!.asset = { assetId: id };
    vi.mocked(c.getPage).mockResolvedValue(page(data));
    const result = await loadStore(c, "/store");
    expect(result.images.hero?.loading).toBe("eager");
    expect(result.images["product:hero"]?.loading).toBe("lazy");
    expect(result.images["product:__proto__"]?.loading).toBe("lazy");
    expect(Object.getPrototypeOf(result.images)).toBe(Object.prototype);
  });
});
