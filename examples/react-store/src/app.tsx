import { useEffect, useRef, useState } from "react";
import { flushSync } from "react-dom";
import { Menu, RefreshCw, X } from "lucide-react";
import type { StoreModel } from "./model.js";
import { CmsPage } from "@cms/react";
import { StoreRenderContext } from "./sections.js";
import { storeRegistry, registry } from "./registry.js";
import { attachPreviewBridge } from "@cms/react/preview";
import { preparePage } from "@cms/runtime";
import { parseCatalog } from "./model.js";
import { definitionsFor, storeComposition } from "./composition.js";
export { CatalogImage, ProductCard } from "./cards.js";
import { Button } from "./ui/button.js";

export interface AppProps {
  initialModel: StoreModel;
}
export function StoreApp({ initialModel }: AppProps) {
  const [model, setModel] = useState(initialModel);
  const [busy, setBusy] = useState(false);
  const [menu, setMenu] = useState(false);
  const [interact, setInteract] = useState(!initialModel.preview);
  const [favorites, setFavorites] = useState<string[]>([]);
  const [actionNotice, setActionNotice] = useState("");
  const request = useRef<AbortController | null>(null);
  const alive = useRef(true);
  const latest = useRef(model);
  const previewBridge = useRef<Awaited<ReturnType<typeof attachPreviewBridge>> | undefined>(
    undefined,
  );
  latest.current = model;
  useEffect(() => {
    const initial = latest.current.bridge;
    const root = document.getElementById("home");
    if (!initial || !root) return;
    let stopped = false,
      bridge: Awaited<ReturnType<typeof attachPreviewBridge>> | undefined,
      fresh: StoreModel | undefined;
    setInteract(false);
    void attachPreviewBridge({
      root,
      studioOrigin: initial.studioOrigin,
      initial,
      manifestHash: initial.manifestHash,
      locale: latest.current.locale ?? "ru",
      ...(initial.changesetId ? { changesetId: initial.changesetId } : {}),
      onMode: (mode) => {
        if (!stopped) setInteract(mode === "interact");
      },
      reload: async (signal, locale) => {
        const response = await fetch("/api/store?locale=" + encodeURIComponent(locale), {
          signal,
          cache: "no-store",
        });
        if (!response.ok) throw new Error("STORE_PREVIEW_UNAVAILABLE");
        const next = (await response.json()) as StoreModel;
        if (!next.bridge || !next.preview || stopped) throw new Error("STORE_PREVIEW_UNAVAILABLE");
        fresh = next;
        return next.bridge;
      },
      render: async (snapshot) => {
        const current = fresh ?? latest.current;
        fresh = undefined;
        const catalog = parseCatalog({ document: snapshot.document } as Parameters<
          typeof parseCatalog
        >[0]);
        const plan = preparePage(storeComposition(snapshot.document, catalog), {
          natives: definitionsFor(catalog),
          actions: registry.actions,
          breakpoints: registry.breakpoints,
          scope: {
            context: { locale: current.locale ?? "ru", store: catalog },
            data: current.bridge!.data,
          },
        });
        bridge?.controller.notify("diagnostics", { items: plan.diagnostics });
        if (plan.diagnostics.length) throw new Error("STORE_PREVIEW_INVALID");
        plan.preview = true;
        plan.interactionMode = "design";
        if (!stopped) {
          const next = { ...current, catalog, plan, bridge: { ...current.bridge!, ...snapshot } };
          latest.current = next;
          flushSync(() => setModel(next));
        }
      },
      navigate: (path) => {
        window.location.assign(path);
      },
    })
      .then((value) => {
        if (stopped) value.dispose();
        else {
          bridge = value;
          previewBridge.current = value;
        }
      })
      .catch(() => {
        if (!stopped) setActionNotice("Предпросмотр недоступен. Обновите страницу.");
      });
    return () => {
      stopped = true;
      bridge?.dispose();
      if (previewBridge.current === bridge) previewBridge.current = undefined;
    };
  }, [model.bridge?.studioOrigin, model.bridge?.documentId, model.bridge?.changesetId]);
  async function refresh(showLoading = true) {
    if (previewBridge.current) {
      await previewBridge.current.controller.refresh();
      return;
    }
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    if (showLoading) setBusy(true);
    try {
      const response = await fetch("/api/store", { signal: controller.signal, cache: "no-store" });
      if (!response.ok && response.status !== 404) throw new Error("STORE_READ_FAILED");
      const next = (await response.json()) as StoreModel;
      if (!controller.signal.aborted && alive.current) setModel(next);
    } catch {
      if (!controller.signal.aborted && alive.current)
        setModel({ status: "error", preview: initialModel.preview, images: {} });
    } finally {
      if (!controller.signal.aborted && alive.current) setBusy(false);
    }
  }
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      request.current?.abort();
    };
  }, []);
  useEffect(() => {
    if (model.refreshAt === undefined || model.status !== "ready") return;
    const timer = setTimeout(
      () => {
        void refresh(false);
      },
      Math.max(5000, Math.min(model.refreshAt - Date.now(), 2_147_483_647)),
    );
    return () => clearTimeout(timer);
    // Refresh reads no state except the invariant initial preview flag.
  }, [model.refreshAt, model.status]);
  const c = model.catalog;
  return (
    <div className="store-shell">
      <div className="demo-strip">
        <span>Project 17 / Демонстрационная витрина</span>
        <div>
          <span className="mode-label">{model.preview ? "Черновик" : "Опубликовано"}</span>
          {model.preview && !model.bridge && (
            <Button
              className="button-ghost interaction-button"
              aria-pressed={interact}
              onClick={() => setInteract(!interact)}
            >
              {interact ? "Режим дизайна" : "Включить взаимодействие"}
            </Button>
          )}
          <Button
            className="button-ghost refresh-button"
            disabled={busy}
            onClick={() => void refresh()}
            aria-label="Обновить контент"
          >
            <RefreshCw size={14} aria-hidden="true" />
            <span>{busy ? "Загрузка…" : "Обновить"}</span>
          </Button>
        </div>
      </div>
      <header className="site-header">
        <a className="brand" href="#home">
          {c?.brand ?? "Тихая форма"}
          <span aria-hidden="true">.</span>
        </a>
        <nav className="desktop-nav" aria-label="Основная навигация">
          <a href="#collection">Коллекция</a>
          <a href="#about">О вещах</a>
        </nav>
        <Button
          className="button-ghost menu-button"
          aria-label={menu ? "Закрыть меню" : "Открыть меню"}
          aria-expanded={menu}
          aria-controls="mobile-nav"
          onClick={() => setMenu(!menu)}
        >
          {menu ? <X size={18} aria-hidden="true" /> : <Menu size={18} aria-hidden="true" />}
          <span>Меню</span>
        </Button>
      </header>
      {menu && (
        <nav className="mobile-nav" id="mobile-nav" aria-label="Навигация телефона">
          <a href="#collection" onClick={() => setMenu(false)}>
            Коллекция
          </a>
          <a href="#about" onClick={() => setMenu(false)}>
            О вещах
          </a>
        </nav>
      )}
      <main id="home" aria-busy={busy}>
        {actionNotice && (
          <p className="action-notice" role="status">
            {actionNotice}
          </p>
        )}
        {busy ? (
          <section className="loading-view" role="status" aria-label="Загрузка контента">
            <span className="eyebrow">Загружаем страницу</span>
            <div className="hero-skeleton" />
            <div className="skeleton-grid">
              <div />
              <div />
              <div />
            </div>
          </section>
        ) : model.status === "ready" && c ? (
          <StoreRenderContext.Provider value={model}>
            {model.plan && (
              <CmsPage
                plan={model.plan}
                registry={storeRegistry}
                actions={registry.actions}
                preview={model.preview}
                mode={interact ? "interact" : "design"}
                app={{
                  favorite(id: string, title: string) {
                    const already = favorites.includes(id);
                    setFavorites((items) => (items.includes(id) ? items : [...items, id]));
                    setActionNotice(
                      already ? `«${title}» уже в избранном` : `«${title}» добавлен в избранное`,
                    );
                  },
                }}
                onActionError={() =>
                  setActionNotice("Не получилось выполнить действие. Попробуйте снова.")
                }
              />
            )}
          </StoreRenderContext.Provider>
        ) : (
          <section className="error-state" role={model.status === "error" ? "alert" : "status"}>
            <span className="eyebrow">Демонстрационная витрина</span>
            <h1>
              {model.status === "missing"
                ? "Страница ещё не создана"
                : "Не получилось загрузить страницу"}
            </h1>
            <p>
              {model.status === "missing"
                ? "Создайте документ витрины в CMS и обновите страницу."
                : "Проверьте доступность CMS и попробуйте снова."}
            </p>
            <Button onClick={() => void refresh()}>Попробовать снова</Button>
          </section>
        )}
      </main>
      <footer className="site-footer">
        <span>{c?.brand ?? "Тихая форма"}</span>
        <span>Демонстрационные товары · без оформления заказа</span>
      </footer>
    </div>
  );
}
