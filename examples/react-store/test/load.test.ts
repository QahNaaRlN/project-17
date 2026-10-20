import { afterEach, describe, it, expect, vi } from "vitest";
import { loadStore } from "../src/load.js";
import { cms, id, page, image, now } from "./fixtures.js";
import { catalog } from "../scripts/catalog.js";
afterEach(() => vi.restoreAllMocks());
describe("STORE-001/005/006/007: чтение CMS", () => {
  it("STU-212/215: обложка из primary content обновляет подписанный URL до истечения", async () => {
    vi.spyOn(Date, "now").mockReturnValue(now);
    const c = cms(),
      data = page(catalog);
    data.document.nodes.n_store!.children!.push("n_primary_image");
    data.document.nodes.n_primary_image = {
      id: "n_primary_image",
      type: "Image",
      bindings: { src: "$content.cover", alt: "$content.alt" },
    };
    data.data.content = { cover: { assetId: id }, alt: "Cover" };
    vi.mocked(c.getPage).mockResolvedValue(data);
    const result = await loadStore(c, "/store");
    expect(result.status).toBe("ready");
    expect(result.plan!.images).toBeDefined();
    expect(result.refreshAt).toBe(now + 270_000);
  });
  it.each([undefined, null])(
    "STU-211/212/216: отсутствие обложки %s сохраняет страницу и preview",
    async (cover) => {
      const c = cms(),
        data = page(catalog);
      data.document.nodes.n_store!.children!.push("n_primary_title", "n_primary_image");
      data.document.nodes.n_primary_title = {
        id: "n_primary_title",
        type: "Heading",
        bindings: { text: "$content.title" },
      };
      data.document.nodes.n_primary_image = {
        id: "n_primary_image",
        type: "Image",
        bindings: { src: "$content.cover", alt: "$content.alt" },
      };
      data.data.content = {
        title: "Available title",
        alt: "Cover",
        ...(cover === null ? { cover: null } : {}),
      };
      vi.mocked(c.getPage).mockResolvedValue(data);
      const result = await loadStore(
        c,
        "/store",
        { locale: "en", preview: { token: "test", changesetId: id } },
        { studioOrigin: "https://studio.test", manifestHash: "sha256:" + "a".repeat(64) },
      );
      expect(result.status).toBe("ready");
      expect(result.bridge).toBeDefined();
      expect(JSON.stringify(result.plan)).toContain("Available title");
      expect(result.plan!.diagnostics).toContainEqual({
        code: "CMS_IMAGE_UNAVAILABLE",
        nodeId: "n_primary_image",
      });
      expect(c.getImage).not.toHaveBeenCalled();
    },
  );
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
describe("STU-412: Composed instances on the storefront", () => {
  const componentId = "0192f1c5-0000-7000-8000-000000000001";
  const component = {
    irVersion: "1.0" as const,
    kind: "component" as const,
    root: "n_card",
    inputs: { title: { type: "text" as const, content: true } },
    nodes: {
      n_card: { id: "n_card", type: "Stack", children: ["n_title"] },
      n_title: { id: "n_title", type: "Heading", bindings: { text: "$props.title" } },
    },
  };
  function withInstance() {
    const data = page(catalog);
    data.document.nodes.n_store!.children!.push("n_inst");
    data.document.nodes.n_inst = {
      id: "n_inst",
      type: "Composed",
      ref: { component: componentId, version: "live" },
      props: { title: "Витрина" },
    };
    return data;
  }
  it("loads the component with the page read and passes it to the preview bridge", async () => {
    const c = cms();
    vi.mocked(c.getPage).mockResolvedValue(withInstance());
    c.getDocument = vi.fn(async () => ({ document: component })) as unknown as typeof c.getDocument;
    const read = { preview: { token: "test", changesetId: id } };
    const result = await loadStore(c, "/store", read, {
      studioOrigin: "https://studio.test",
      manifestHash: "sha256:" + "a".repeat(64),
    });
    expect(result.status).toBe("ready");
    expect(JSON.stringify(result.plan)).toContain("Витрина");
    expect(c.getDocument).toHaveBeenCalledWith(componentId, expect.objectContaining(read));
    expect(result.bridge!.components![componentId]).toEqual(component);
  });
  it("an unresolved component is a store error, not a partial page", async () => {
    const c = cms();
    vi.mocked(c.getPage).mockResolvedValue(withInstance());
    c.getDocument = vi.fn(async () => null) as unknown as typeof c.getDocument;
    expect((await loadStore(c, "/store")).status).toBe("error");
  });
});
