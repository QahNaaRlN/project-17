import { useEffect, useRef, useState } from "react";
import { ArrowUpRight, ImageOff, Menu, RefreshCw, X } from "lucide-react";
import { priceLabel, type ImageProps, type Product, type StoreModel } from "./model.js";
import { Button } from "./ui/button.js";
import { ProductDialog } from "./ui/dialog.js";

export function CatalogImage({
  image,
  alt,
  className = "",
}: {
  image?: ImageProps | null | undefined;
  alt: string;
  className?: string;
}) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [image?.src]);
  return (
    <div className={`catalog-image ${className}`}>
      {image && !failed ? (
        <img {...image} alt={alt} onError={() => setFailed(true)} />
      ) : (
        <div className="image-fallback">
          <ImageOff size={23} aria-hidden="true" />
          <span>Изображение недоступно</span>
        </div>
      )}
    </div>
  );
}
export function ProductCard({
  product,
  image,
}: {
  product: Product;
  image?: ImageProps | null | undefined;
}) {
  const details = (
    <>
      <CatalogImage image={image} alt={product.title} />
      <div className="dialog-bottom">
        <span>{priceLabel(product.priceRub)}</span>
        <span className="muted">Демонстрационный товар</span>
      </div>
    </>
  );
  return (
    <article className="product-card">
      <div className={`product-art tone-${product.tone}`}>
        <CatalogImage image={image} alt={product.title} />
        <span className="product-tag">{product.category}</span>
      </div>
      <div className="product-body">
        <span className="material">{product.material}</span>
        <h3>{product.title}</h3>
        <div className="product-bottom">
          <span className="price">{priceLabel(product.priceRub)}</span>
          <ProductDialog
            title={product.title}
            description={product.description}
            trigger={
              <Button
                className="button-ghost detail-button"
                aria-label={`Подробнее: ${product.title}`}
              >
                Подробнее <ArrowUpRight size={15} aria-hidden="true" />
              </Button>
            }
          >
            {details}
          </ProductDialog>
        </div>
      </div>
    </article>
  );
}
export interface AppProps {
  initialModel: StoreModel;
}
export function StoreApp({ initialModel }: AppProps) {
  const [model, setModel] = useState(initialModel);
  const [busy, setBusy] = useState(false);
  const [menu, setMenu] = useState(false);
  const request = useRef<AbortController | null>(null);
  const alive = useRef(true);
  async function refresh(showLoading = true) {
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
          <>
            <section className="hero">
              <div className="hero-copy">
                <p className="eyebrow">Коллекция / Предметы для дома</p>
                <h1>
                  {c.title}
                  <em>{c.subtitle}</em>
                </h1>
                <p className="intro">{c.description}</p>
                <a className="button button-primary hero-cta" href="#collection">
                  {c.ctaLabel}
                  <ArrowUpRight size={17} aria-hidden="true" />
                </a>
              </div>
              <div className="hero-art">
                <CatalogImage image={model.images.hero} alt="Предметы коллекции" />
              </div>
            </section>
            <section className="collection" id="collection" aria-label="Коллекция">
              <div className="section-heading">
                <div>
                  <p className="eyebrow">Небольшая подборка</p>
                  <h2>{c.collectionTitle}</h2>
                </div>
                <span className="item-count">
                  {c.products.length === 0 ? "Нет предметов" : `${c.products.length} в коллекции`}
                </span>
              </div>
              {c.products.length ? (
                <div className="product-grid">
                  {c.products.map((p) => (
                    <ProductCard key={p.id} product={p} image={model.images[`product:${p.id}`]} />
                  ))}
                </div>
              ) : (
                <div className="empty-state">
                  <h3>Новые вещи скоро появятся</h3>
                  <p>Сейчас в этой коллекции нет предметов.</p>
                </div>
              )}
            </section>
            <section className="about" id="about">
              <span className="about-mark" aria-hidden="true">
                тф.
              </span>
              <div>
                <p className="eyebrow">Наш подход</p>
                <h2>{c.aboutTitle}</h2>
                <p>{c.aboutText}</p>
              </div>
            </section>
          </>
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
