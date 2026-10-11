import { describe, it, expect } from "vitest";
import fc from "fast-check";
import { Image, imageSizes } from "../src/image.js";
import type { ImageModel, Width } from "../src/image.js";

const variant = (width: number) => ({
  width,
  url: `https://cdn.example.test/assets/v1/staging/asset/hash?w=${width}&sig=fixture`,
});
const model: ImageModel = {
  width: 640,
  height: 480,
  fit: "cover",
  format: "webp",
  focalPoint: { x: 0.2, y: 0.8 },
  expiresAt: "2099-01-01T00:00:00Z",
  variants: [variant(1280), variant(320), variant(640)],
};
const options = {
  alt: "Picture",
  width: {
    base: "100vw" as const,
    breakpoints: [
      { minWidth: 1024, width: 640 },
      { minWidth: 768, width: 480 },
    ],
  },
  now: 0,
};
describe("CNT-052/SDK-001 adaptive Image", () => {
  it("covers adaptive boundaries, exact pixel bound and explicit clock", () => {
    expect(imageSizes({ base: "1vw" })).toBe("1vw");
    expect(
      imageSizes({
        base: "50.25vw",
        breakpoints: [
          { minWidth: 320, width: 200 },
          { minWidth: 1024, width: 640 },
        ],
      }),
    ).toBe("(min-width: 1024px) 640px, (min-width: 320px) 200px, 50.25vw");
    expect(imageSizes({ base: 10000 })).toBe("10000px");
    for (const base of ["prefix50vw", "50vwsuffix", "1junk50vw"])
      expect(() => imageSizes({ base: base as Width })).toThrow();
    for (const focalPoint of [
      { x: 0, y: 0 },
      { x: 1, y: 1 },
    ])
      expect(Image({ ...model, width: 8000, height: 5000, focalPoint }, options).props.width).toBe(
        8000,
      );
    for (const focalPoint of [
      { x: 1.1, y: 0 },
      { x: 0, y: -0.1 },
    ])
      expect(() => Image({ ...model, focalPoint }, options)).toThrow();
    expect(() =>
      Image(
        { ...model, expiresAt: "2030-01-01T00:00:00Z" },
        { ...options, now: Date.parse("2040-01-01T00:00:00Z") },
      ),
    ).toThrow();
    const local = variant(320);
    local.url = local.url.replace("https:", "http:");
    expect(Image({ ...model, variants: [local] }, options).props.src).toBe(local.url);
  });
  it("uses signed variants and focal point without modifying URLs or model", () => {
    const before = JSON.stringify(model);
    const image = Image(model, options);
    expect(image).toEqual({
      tag: "img",
      props: {
        src: variant(640).url,
        srcSet: `${variant(320).url} 320w, ${variant(640).url} 640w, ${variant(1280).url} 1280w`,
        sizes: "(min-width: 1024px) 640px, (min-width: 768px) 480px, 100vw",
        alt: "Picture",
        width: 640,
        height: 480,
        loading: "lazy",
        decoding: "async",
        style: { objectFit: "cover", objectPosition: "20% 80%" },
      },
    });
    expect(JSON.stringify(model)).toBe(before);
  });
  it("falls back to the largest available width and accepts eager loading", () => {
    expect(
      Image(
        { ...model, fit: "contain", variants: [variant(320)] },
        { ...options, preferredWidth: 2560, loading: "eager" },
      ).props,
    ).toMatchObject({ src: variant(320).url, loading: "eager", style: { objectFit: "contain" } });
    expect(Image(model, { ...options, preferredWidth: 320 }).props.src).toBe(variant(320).url);
    expect(imageSizes({ base: 400 })).toBe("400px");
    expect(imageSizes({ base: "50.5vw" })).toBe("50.5vw");
  });
  it.each(["bad", "1970-01-01T00:00:00Z"])("rejects expired/invalid URL models %s", (expiresAt) => {
    expect(() => Image({ ...model, expiresAt }, options)).toThrow(/expired/);
  });
  it("uses the current time when not supplied", () => {
    const { now: _, ...current } = options;
    expect(Image(model, current).tag).toBe("img");
  });
  it.each([0, -1, 10001, NaN, Infinity, "0vw", "101vw", "100.1vw", "20px", "calc(100vw)"])(
    "rejects invalid sizes %s",
    (width) => {
      expect(() => imageSizes({ base: width as Width })).toThrow(/width/);
    },
  );
  it.each([0, -1, 1.5, 10001])("rejects invalid breakpoint %s", (minWidth) => {
    expect(() => imageSizes({ base: 100, breakpoints: [{ minWidth, width: 100 }] })).toThrow(
      /breakpoint/,
    );
  });
  it("rejects duplicate breakpoints", () => {
    expect(() =>
      imageSizes({
        base: 100,
        breakpoints: [
          { minWidth: 320, width: 100 },
          { minWidth: 320, width: 200 },
        ],
      }),
    ).toThrow(/breakpoint/);
  });
  it.each([
    { width: 0 },
    { height: 0 },
    { width: 1.5 },
    { height: -1 },
    { width: 10000, height: 10000 },
    { fit: "bad" },
    { format: "bad" },
  ])("rejects malformed dimensions/format/fit %j", (change) => {
    expect(() => Image({ ...model, ...change } as ImageModel, options)).toThrow(/model/);
  });
  it.each([
    { x: -0.1, y: 0 },
    { x: 0, y: 1.1 },
    { x: NaN, y: 0 },
    { x: 0, y: Infinity },
  ])("rejects focal point %j", (focalPoint) => {
    expect(() => Image({ ...model, focalPoint }, options)).toThrow(/focal/);
  });
  it.each([
    "javascript:alert(1)",
    "https://user:pass@x/assets/v1/a?sig=s",
    "https://x/other?sig=s",
    "https://x/assets/v1/a",
    "https://x/assets/v1/a?sig=s#x",
    "https://x/assets/v1/a?sig=s,evil",
    "invalid",
  ])("rejects unsafe variant URL %s", (url) => {
    expect(() => Image({ ...model, variants: [{ width: 320, url }] }, options)).toThrow();
  });
  it("rejects missing, duplicate, unsupported variants and preferred widths", () => {
    for (const variants of [[], [variant(1)], [variant(320), variant(320)]])
      expect(() => Image({ ...model, variants }, options)).toThrow();
    expect(() => Image(model, { ...options, preferredWidth: 400 })).toThrow(/preferred/);
  });
  it("handles every allowed format/width", () => {
    for (const format of ["avif", "webp", "jpeg", "png"] as const) {
      const widths = [320, 480, 640, 768, 1024, 1280, 1600, 1920, 2560];
      const result = Image({ ...model, format, variants: widths.map(variant) }, options);
      expect(result.props.srcSet.split(", ")).toHaveLength(9);
    }
  });
  it("properties: responsive widths stay serializable and widths sorted", () => {
    fc.assert(
      fc.property(
        fc.integer({ min: 1, max: 10000 }),
        fc.integer({ min: 1, max: 10000 }),
        (width, breakpoint) => {
          const result = Image(model, {
            ...options,
            width: { base: width, breakpoints: [{ minWidth: breakpoint, width: "100vw" }] },
          });
          expect(result.props.sizes).toBe(`(min-width: ${breakpoint}px) 100vw, ${width}px`);
          expect(JSON.parse(JSON.stringify(result))).toEqual(result);
        },
      ),
    );
  });
});
