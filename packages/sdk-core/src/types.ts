import type { IrDocument } from "@cms/ir";

export interface DeliveryDiagnostic {
  code: string;
  severity: "error" | "warning";
  pointer: string;
  message: string;
}

export interface Preview {
  token: string;
  changesetId?: string;
}
export interface ReadOptions {
  preview?: Preview;
  signal?: AbortSignal;
}
export interface ImageOptions extends ReadOptions {
  fmt?: "avif" | "webp" | "jpeg" | "png";
  fit?: "contain" | "cover";
  w?: number;
  h?: number;
}
export interface ClientOptions {
  baseUrl: string;
  project: string;
  environment: string;
  deliveryKey?: string;
  fetch?: typeof globalThis.fetch;
  cache?: false | { capacity?: number; ttlMs?: number };
  timeoutMs?: number;
  maxResponseBytes?: number;
  now?: () => number;
}
export interface DocumentResponse {
  objectId: string;
  versionId: string;
  path: string | null;
  document: IrDocument;
  diagnostics?: DeliveryDiagnostic[];
}
export interface PageResponse {
  page: { objectId: string; versionId: string; path: string; params: Record<string, string> };
  document: IrDocument;
  components: Record<string, { versionId: string; document: IrDocument }>;
  data: Record<string, unknown>;
  dataSources: Record<string, unknown>;
  diagnostics?: DeliveryDiagnostic[];
}
export interface ContentResponse {
  id: string;
  versionId: string;
  kind: "entity" | "asset";
  deleted: boolean;
  data: Record<string, unknown>;
  schema?: string;
  schemaVersion?: number;
}
export interface Route {
  path: string;
  objectId: string;
  versionId: string;
}
export interface ImageModel {
  assetId: string;
  versionId: string;
  width: number;
  height: number;
  fit: "contain" | "cover";
  format: "avif" | "webp" | "jpeg" | "png";
  focalPoint: { x: number; y: number };
  expiresAt: string;
  variants: { width: number; url: string }[];
}
export interface CmsClient {
  getPage(path: string, options?: ReadOptions): Promise<PageResponse | null>;
  getDocument(id: string, options?: ReadOptions): Promise<DocumentResponse | null>;
  getEntity(id: string, options?: ReadOptions): Promise<ContentResponse | null>;
  getAsset(id: string, options?: ReadOptions): Promise<ContentResponse | null>;
  getRoutes(options?: ReadOptions): Promise<{ items: Route[] }>;
  getImage(id: string, options?: ImageOptions): Promise<ImageModel | null>;
  clearCache(): void;
}
