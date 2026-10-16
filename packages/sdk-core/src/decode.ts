import { validateDocument, type IrDocument } from "@cms/ir";
import { CmsError } from "./errors.js";
import type {
  ContentResponse,
  DocumentResponse,
  ImageModel,
  PageResponse,
  Route,
} from "./types.js";

export const widths = [320, 480, 640, 768, 1024, 1280, 1600, 1920, 2560] as const;
export const formats = ["avif", "webp", "jpeg", "png"] as const;
export function record(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
export function uuid(value: unknown): value is string {
  return (
    typeof value === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value)
  );
}
function valid(condition: unknown): asserts condition {
  if (!condition) throw new CmsError("INVALID_RESPONSE");
}
function doc(value: unknown): IrDocument {
  const result = validateDocument(value);
  if (!result.valid) throw new CmsError("IR_INVALID", 0, result.diagnostics);
  return value as IrDocument;
}
function diagnostics(value: Record<string, unknown>) {
  if (value.diagnostics !== undefined) {
    valid(
      Array.isArray(value.diagnostics) &&
        value.diagnostics.every(
          (d) =>
            record(d) &&
            typeof d.code === "string" &&
            typeof d.pointer === "string" &&
            ["error", "warning"].includes(d.severity as string) &&
            typeof d.message === "string" &&
            (d.nodeId === undefined || typeof d.nodeId === "string") &&
            (d.params === undefined || record(d.params)),
        ),
    );
  }
}
export function page(value: unknown): PageResponse {
  valid(record(value) && record(value.page));
  const p = value.page;
  valid(
    uuid(p.objectId) &&
      uuid(p.versionId) &&
      typeof p.path === "string" &&
      p.path.startsWith("/") &&
      record(p.params) &&
      Object.values(p.params).every((v) => typeof v === "string"),
  );
  valid(doc(value.document).kind === "page");
  valid(record(value.components) && record(value.data) && record(value.dataSources));
  if (value.data.content !== undefined) valid(record(value.data.content));
  if (value.contentContext !== undefined) {
    valid(
      record(value.contentContext) &&
        typeof value.contentContext.locale === "string" &&
        record(value.contentContext.versions),
    );
    valid(
      Object.entries(value.contentContext.versions).every(
        ([id, version]) => uuid(id) && uuid(version),
      ),
    );
  }
  for (const [key, component] of Object.entries(value.components)) {
    valid(uuid(key) && record(component) && uuid(component.versionId));
    valid(doc(component.document).kind === "component");
  }
  diagnostics(value);
  return value as unknown as PageResponse;
}
export function document(value: unknown): DocumentResponse {
  valid(
    record(value) &&
      uuid(value.objectId) &&
      uuid(value.versionId) &&
      (value.path === null || (typeof value.path === "string" && value.path.startsWith("/"))),
  );
  doc(value.document);
  diagnostics(value);
  return value as unknown as DocumentResponse;
}
export function content(value: unknown, kind: "entity" | "asset"): ContentResponse {
  valid(
    record(value) &&
      uuid(value.id) &&
      uuid(value.versionId) &&
      value.kind === kind &&
      typeof value.deleted === "boolean" &&
      record(value.data),
  );
  valid(value.schema === undefined || typeof value.schema === "string");
  valid(
    value.schemaVersion === undefined ||
      (Number.isInteger(value.schemaVersion) && (value.schemaVersion as number) > 0),
  );
  return value as unknown as ContentResponse;
}
export function routes(value: unknown): { items: Route[] } {
  valid(
    record(value) &&
      Array.isArray(value.items) &&
      value.items.every(
        (r) =>
          record(r) &&
          uuid(r.objectId) &&
          uuid(r.versionId) &&
          typeof r.path === "string" &&
          r.path.startsWith("/"),
      ),
  );
  return value as unknown as { items: Route[] };
}
function size(value: unknown): value is number {
  return Number.isInteger(value) && (value as number) > 0;
}
export function image(value: unknown, now: number): ImageModel {
  valid(
    record(value) &&
      uuid(value.assetId) &&
      uuid(value.versionId) &&
      size(value.width) &&
      size(value.height) &&
      value.width * value.height <= 40_000_000,
  );
  valid(
    ["contain", "cover"].includes(value.fit as string) &&
      formats.includes(value.format as ImageModel["format"]),
  );
  valid(
    record(value.focalPoint) &&
      [value.focalPoint.x, value.focalPoint.y].every(
        (n) => typeof n === "number" && n >= 0 && n <= 1,
      ),
  );
  valid(typeof value.expiresAt === "string" && Date.parse(value.expiresAt) > now);
  valid(Array.isArray(value.variants) && value.variants.length > 0);
  const seen = new Set<number>();
  for (const v of value.variants) {
    valid(
      record(v) &&
        widths.includes(v.width as (typeof widths)[number]) &&
        !seen.has(v.width as number) &&
        typeof v.url === "string" &&
        !/[\s,]/.test(v.url),
    );
    let url: URL;
    try {
      url = new URL(v.url);
    } catch {
      throw new CmsError("INVALID_RESPONSE");
    }
    valid(
      ["http:", "https:"].includes(url.protocol) &&
        !url.username &&
        !url.password &&
        !url.hash &&
        /^\/assets\/v1\/[^/]+\/[0-9a-f-]+\/[0-9a-f]{64}$/.test(url.pathname),
    );
    seen.add(v.width as number);
  }
  return value as unknown as ImageModel;
}
