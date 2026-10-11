import { createElement, act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, it, expect, vi } from "vitest";
import { StoreApp, CatalogImage } from "../src/app.js";
import { model } from "./fixtures.js";
import type { ImageProps } from "../src/model.js";
let root: Root | undefined;
async function mount(element: ReturnType<typeof createElement>) {
  const target = document.createElement("div");
  document.body.append(target);
  root = createRoot(target);
  await act(async () => {
    root!.render(element);
  });
}
async function click(selector: string) {
  const button = document.querySelector<HTMLButtonElement>(selector);
  expect(button).not.toBeNull();
  await act(async () => button!.click());
}
afterEach(async () => {
  await act(async () => root?.unmount());
  root = undefined;
  document.body.replaceChildren();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});
it("IR-050/SDK-033: событие из CMS отключено в дизайне, взаимодействие применяет правильный товар", async () => {
  await mount(createElement(StoreApp, { initialModel: model() }));
  await click(".favorite-button");
  expect(document.querySelector(".action-notice")).toBeNull();
  await click(".interaction-button");
  expect(document.querySelector(".interaction-button")!.getAttribute("aria-pressed")).toBe("true");
  await click(".favorite-button");
  expect(document.querySelector(".action-notice")!.textContent).toBe(
    "«Ваза «Линия»» добавлен в избранное",
  );
  await click(".favorite-button");
  expect(document.querySelector(".action-notice")!.textContent).toBe(
    "«Ваза «Линия»» уже в избранном",
  );
  await click(".product-card:nth-child(2) .favorite-button");
  expect(document.querySelector(".action-notice")!.textContent).toBe(
    "«Лампа «Тепло»» добавлен в избранное",
  );
  await click(".interaction-button");
  await click(".favorite-button");
  expect(document.querySelector(".action-notice")!.textContent).toBe(
    "«Лампа «Тепло»» добавлен в избранное",
  );
});
it("IR-050: ошибка аргумента действия показывает безопасное состояние", async () => {
  const initial = model(),
    plan = initial.plan!;
  const product = plan.roots[0]!.children[1]!.children[0]!;
  product.events!.favorite![0]!.args.id = 1;
  await mount(createElement(StoreApp, { initialModel: { ...initial, preview: false, plan } }));
  await click(".favorite-button");
  expect(document.querySelector(".action-notice")!.textContent).toBe(
    "Не получилось выполнить действие. Попробуйте снова.",
  );
});
it("STORE-002/003: меню и подробности предмета работают с доступными подписями", async () => {
  await mount(createElement(StoreApp, { initialModel: model() }));
  await click(".menu-button");
  expect(document.querySelector(".menu-button")!.getAttribute("aria-expanded")).toBe("true");
  expect(document.querySelector(".mobile-nav")).not.toBeNull();
  await click(".mobile-nav a");
  expect(document.querySelector(".mobile-nav")).toBeNull();
  await click(".detail-button");
  expect(document.querySelector('[role="dialog"]')!.textContent).toContain("Ваза «Линия»");
  await click(".dialog-close");
  expect(document.querySelector('[role="dialog"]')).toBeNull();
});
it("STORE-005: реальный повтор показывает загрузку, затем новые данные", async () => {
  let resolve!: (response: Response) => void;
  const pending = new Promise<Response>((r) => {
    resolve = r;
  });
  const fetch = vi.fn().mockReturnValue(pending);
  vi.stubGlobal("fetch", fetch);
  await mount(
    createElement(StoreApp, { initialModel: { status: "error", preview: true, images: {} } }),
  );
  await click(".error-state button");
  expect(document.querySelector('[aria-label="Загрузка контента"]')).not.toBeNull();
  expect(document.querySelector(".refresh-button")!.hasAttribute("disabled")).toBe(true);
  const next = model();
  next.catalog!.title = "Новый заголовок";
  await act(async () => {
    resolve(new Response(JSON.stringify(next)));
    await pending;
  });
  expect(document.querySelector("h1")!.textContent).toContain("Новый заголовок");
  expect(fetch).toHaveBeenCalledWith(
    "/api/store",
    expect.objectContaining({ cache: "no-store", signal: expect.any(AbortSignal) }),
  );
});
it.each(["http", "network", "json"])(
  "STORE-005: неудачный повтор %s не подменяется успехом",
  async (kind) => {
    const fetch =
      kind === "network"
        ? vi.fn().mockRejectedValue(new Error("SECRET"))
        : vi.fn().mockResolvedValue(
            new Response(kind === "json" ? "bad json" : "{}", {
              status: kind === "http" ? 503 : 200,
            }),
          );
    vi.stubGlobal("fetch", fetch);
    await mount(createElement(StoreApp, { initialModel: model() }));
    await click(".refresh-button");
    expect(document.querySelector('[role="alert"]')!.textContent).toContain("Не получилось");
    expect(document.body.textContent).not.toContain("SECRET");
    expect(document.querySelector(".refresh-button")!.hasAttribute("disabled")).toBe(false);
  },
);
it("STORE-005: повтор с HTTP 404 сохраняет состояние отсутствующей страницы", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ status: "missing", preview: true, images: {} }), {
        status: 404,
      }),
    ),
  );
  await mount(createElement(StoreApp, { initialModel: model() }));
  await click(".refresh-button");
  expect(document.querySelector("h1")!.textContent).toBe("Страница ещё не создана");
});
it("STORE-005: при размонтировании запрос отменяется без обновления состояния", async () => {
  let reject!: (error: Error) => void;
  const pending = new Promise<Response>((_, r) => {
    reject = r;
  });
  const fetch = vi.fn().mockReturnValue(pending);
  vi.stubGlobal("fetch", fetch);
  await mount(createElement(StoreApp, { initialModel: model() }));
  await click(".refresh-button");
  const signal = fetch.mock.calls[0]![1].signal as AbortSignal;
  await act(async () => root!.unmount());
  root = undefined;
  expect(signal.aborted).toBe(true);
  await act(async () => {
    reject(new Error("aborted"));
    await pending.catch(() => {});
  });
});
it("STORE-006: обновляет модель до истечения подписанных URL", async () => {
  vi.useFakeTimers();
  vi.setSystemTime(0);
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(model()))));
  const initial = model();
  initial.refreshAt = 1000;
  await mount(createElement(StoreApp, { initialModel: initial }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(4999);
  });
  expect(fetch).not.toHaveBeenCalled();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1);
  });
  expect(fetch).toHaveBeenCalledOnce();
});
it("STORE-006: долгий срок URL не переполняет таймер", async () => {
  vi.useFakeTimers();
  vi.setSystemTime(0);
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(model()))));
  const initial = model();
  initial.refreshAt = 2_147_483_648;
  await mount(createElement(StoreApp, { initialModel: initial }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(fetch).not.toHaveBeenCalled();
});
it("STORE-003/006: фоновое обновление изображений сохраняет открытые подробности", async () => {
  vi.useFakeTimers();
  vi.setSystemTime(0);
  let resolve!: (response: Response) => void;
  const pending = new Promise<Response>((r) => {
    resolve = r;
  });
  vi.stubGlobal("fetch", vi.fn().mockReturnValue(pending));
  const initial = model();
  initial.refreshAt = 1000;
  await mount(createElement(StoreApp, { initialModel: initial }));
  await click(".detail-button");
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(document.querySelector('[role="dialog"]')).not.toBeNull();
  expect(document.querySelector('[aria-label="Загрузка контента"]')).toBeNull();
  const next = model();
  next.catalog!.products[0]!.description = "Описание обновлено в CMS";
  await act(async () => {
    resolve(new Response(JSON.stringify(next)));
    await pending;
  });
  expect(document.querySelector('[role="dialog"]')!.textContent).toContain(
    "Описание обновлено в CMS",
  );
});
it("STORE-005/006: пустая подборка и неизвестная цена отображаются явно", async () => {
  const empty = model();
  empty.catalog!.products = [];
  await mount(createElement(StoreApp, { initialModel: empty }));
  expect(document.body.textContent).toContain("Сейчас в этой коллекции нет предметов");
  await act(async () => root!.unmount());
  root = undefined;
  const unknown = model();
  delete unknown.catalog!.products[0]!.priceRub;
  await mount(createElement(StoreApp, { initialModel: unknown }));
  expect(document.body.textContent).toContain("Цена не указана");
});
it("STORE-005: отсутствие страницы отличается от ошибки и не создаёт вымышленные карточки", async () => {
  await mount(
    createElement(StoreApp, { initialModel: { status: "missing", preview: false, images: {} } }),
  );
  expect(document.body.textContent).toContain("Страница ещё не создана");
  expect(document.body.textContent).toContain("Опубликовано");
  expect(document.querySelector(".product-card")).toBeNull();
});
it("STORE-006: ошибка изображения даёт fallback, новая ссылка восстанавливает изображение", async () => {
  const image: ImageProps = {
    src: "https://example.test/assets/v1/image?sig=one",
    srcSet: "https://example.test/assets/v1/image?sig=one 320w",
    sizes: "112px",
    alt: "image",
    width: 320,
    height: 200,
    loading: "lazy",
    decoding: "async",
    style: { objectFit: "contain", objectPosition: "50% 50%" },
  };
  await mount(createElement(CatalogImage, { image, alt: "Ваза" }));
  await act(async () => document.querySelector("img")!.dispatchEvent(new Event("error")));
  expect(document.querySelector("img")).toBeNull();
  expect(document.body.textContent).toContain("Изображение недоступно");
  await act(async () =>
    root!.render(
      createElement(CatalogImage, {
        image: { ...image, src: "https://example.test/assets/v1/image?sig=two" },
        alt: "Ваза",
      }),
    ),
  );
  expect(document.querySelector("img")!.getAttribute("alt")).toBe("Ваза");
});
