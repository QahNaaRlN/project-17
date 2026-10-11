const widths = new Set([320, 480, 640, 768, 1024, 1280, 1600, 1920, 2560]);
export type Width = number | `${number}vw`;
export interface AdaptiveWidth {
  base: Width;
  breakpoints?: readonly { minWidth: number; width: Width }[];
}
export interface ImageModel {
  width: number;
  height: number;
  fit: "contain" | "cover";
  format: "avif" | "webp" | "jpeg" | "png";
  focalPoint: { x: number; y: number };
  expiresAt: string;
  variants: readonly { width: number; url: string }[];
}
export interface ImageOptions {
  alt: string;
  width: AdaptiveWidth;
  preferredWidth?: number;
  loading?: "eager" | "lazy";
  now?: number;
}
function size(width: Width): string {
  if (typeof width === "number" && Number.isFinite(width) && width > 0 && width <= 10000)
    return `${width}px`;
  if (
    typeof width === "string" &&
    /^(?:[1-9]\d?|100)(?:\.\d+)?vw$/.test(width) &&
    Number.parseFloat(width) <= 100
  )
    return width;
  throw new Error("Invalid adaptive image width");
}
// CNT-052: descending min-width clauses map responsive node widths to browser sizes.
export function imageSizes(width: AdaptiveWidth): string {
  const points = [...(width.breakpoints ?? [])].sort((a, b) => b.minWidth - a.minWidth);
  const seen = new Set<number>();
  const parts = points.map((p) => {
    if (
      !Number.isInteger(p.minWidth) ||
      p.minWidth <= 0 ||
      p.minWidth > 10000 ||
      seen.has(p.minWidth)
    )
      throw new Error("Invalid image breakpoint");
    seen.add(p.minWidth);
    return `(min-width: ${p.minWidth}px) ${size(p.width)}`;
  });
  return [...parts, size(width.base)].join(", ");
}
// SDK-001/CNT-052: adapters render this serializable model; no signing keys or network calls.
export function Image(model: ImageModel, options: ImageOptions) {
  const expires = Date.parse(model.expiresAt);
  if (!Number.isFinite(expires) || expires <= (options.now ?? Date.now()))
    throw new Error("Image URLs expired; reload the image model");
  if (
    !Number.isInteger(model.width) ||
    !Number.isInteger(model.height) ||
    model.width <= 0 ||
    model.height <= 0 ||
    model.width * model.height > 40000000 ||
    !["contain", "cover"].includes(model.fit) ||
    !["avif", "webp", "jpeg", "png"].includes(model.format)
  )
    throw new Error("Invalid image model");
  const { x, y } = model.focalPoint;
  if (!Number.isFinite(x) || !Number.isFinite(y) || x < 0 || x > 1 || y < 0 || y > 1)
    throw new Error("Invalid image focal point");
  const variants = [...model.variants].sort((a, b) => a.width - b.width);
  const seen = new Set<number>();
  for (const v of variants) {
    const u = new URL(v.url);
    if (
      !widths.has(v.width) ||
      seen.has(v.width) ||
      !["https:", "http:"].includes(u.protocol) ||
      u.username ||
      u.password ||
      u.hash ||
      !u.pathname.startsWith("/assets/v1/") ||
      u.searchParams.get("sig") === null ||
      /\s|,/.test(v.url)
    )
      throw new Error("Invalid signed image variant");
    seen.add(v.width);
  }
  if (variants.length === 0) throw new Error("Image variants missing");
  const preferred = options.preferredWidth ?? 640;
  if (!widths.has(preferred)) throw new Error("Invalid preferred image width");
  const src = variants.find((v) => v.width >= preferred) ?? variants[variants.length - 1]!;
  return {
    tag: "img" as const,
    props: {
      src: src.url,
      srcSet: variants.map((v) => `${v.url} ${v.width}w`).join(", "),
      sizes: imageSizes(options.width),
      alt: options.alt,
      width: model.width,
      height: model.height,
      loading: options.loading ?? "lazy",
      decoding: "async" as const,
      style: { objectFit: model.fit, objectPosition: `${x * 100}% ${y * 100}%` },
    },
  };
}
