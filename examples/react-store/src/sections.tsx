import { createContext, useContext } from "react";
import { ArrowUpRight } from "lucide-react";
import type { NativeProps } from "@cms/react";
import type { Product, StoreModel } from "./model.js";
import { CatalogImage, ProductCard } from "./cards.js";
export const StoreRenderContext = createContext<Pick<StoreModel, "images">>({ images: {} });
export function Storefront({ children, className, instanceId, previewAttributes }: NativeProps) {
  return className || previewAttributes ? (
    <div id={instanceId} className={className || undefined} {...previewAttributes}>
      {children}
    </div>
  ) : (
    children
  );
}
export function Hero({ values: p, className, previewAttributes }: NativeProps) {
  const { images } = useContext(StoreRenderContext);
  return (
    <section className={`hero ${className}`} {...previewAttributes}>
      <div className="hero-copy">
        <p className="eyebrow">Коллекция / Предметы для дома</p>
        <h1>
          {String(p.title)}
          <em>{String(p.subtitle)}</em>
        </h1>
        <p className="intro">{String(p.description)}</p>
        <a className="button button-primary hero-cta" href="#collection">
          {String(p.ctaLabel)}
          <ArrowUpRight size={17} aria-hidden="true" />
        </a>
      </div>
      <div className="hero-art">
        <CatalogImage image={images.hero} alt="Предметы коллекции" />
      </div>
    </section>
  );
}
export function Collection({ values: p, children, className, previewAttributes }: NativeProps) {
  const count = (p.products as Product[] | undefined)?.length ?? 0;
  return (
    <section
      className={`collection ${className}`}
      id="collection"
      aria-label="Коллекция"
      {...previewAttributes}
    >
      <div className="section-heading">
        <div>
          <p className="eyebrow">Небольшая подборка</p>
          <h2>{String(p.title)}</h2>
        </div>
        <span className="item-count">{count === 0 ? "Нет предметов" : `${count} в коллекции`}</span>
      </div>
      {count ? <div className="product-grid">{children}</div> : children}
    </section>
  );
}
export function Item({ values: p, className, events, previewAttributes }: NativeProps) {
  const { images } = useContext(StoreRenderContext);
  const product = p.product as Product;
  return (
    <ProductCard
      product={product}
      image={images[`product:${product.id}`]}
      className={className}
      {...(previewAttributes ? { previewAttributes } : {})}
      {...(events?.favorite ? { onFavorite: events.favorite } : {})}
    />
  );
}
export function Empty({ className, previewAttributes }: NativeProps) {
  return (
    <div className={`empty-state ${className}`} {...previewAttributes}>
      <h3>Новые вещи скоро появятся</h3>
      <p>Сейчас в этой коллекции нет предметов.</p>
    </div>
  );
}
export function About({ values: p, className, previewAttributes }: NativeProps) {
  return (
    <section className={`about ${className}`} id="about" {...previewAttributes}>
      <span className="about-mark" aria-hidden="true">
        тф.
      </span>
      <div>
        <p className="eyebrow">Наш подход</p>
        <h2>{String(p.title)}</h2>
        <p>{String(p.text)}</p>
      </div>
    </section>
  );
}
