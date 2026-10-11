import { useEffect, useState } from "react";
import { ArrowUpRight, ImageOff } from "lucide-react";
import { priceLabel, type ImageProps, type Product } from "./model.js";
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
  className = "",
  onFavorite,
}: {
  product: Product;
  image?: ImageProps | null | undefined;
  className?: string;
  onFavorite?: () => Promise<boolean>;
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
    <article className={`product-card ${className}`}>
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
          {onFavorite && (
            <Button
              className="button-ghost favorite-button"
              aria-label={`В избранное: ${product.title}`}
              onClick={() => void onFavorite()}
            >
              В избранное
            </Button>
          )}
        </div>
      </div>
    </article>
  );
}
