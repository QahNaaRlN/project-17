import type { IrDocument } from "@cms/ir";

export interface DeliveryDiagnostic {
  nodeId?: string;
  params?: Record<string, unknown>;
  code: string;
  severity: "error" | "warning";
  pointer: string;
  message: string;
}
export type ContentValueDiagnostic = DeliveryDiagnostic & {
  code: "BINDING_VALUE_MISSING";
  params: { path: string };
};

export interface Preview {
  token: string;
  changesetId?: string;
}
export interface ReadOptions {
  locale?: string;
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
  /**
   * OPS-023, STU-605: sampled runtime diagnostics sent with the delivery key; `false`
   * or `sampleRate: 0` turns it off. Defaults: 10 % of reports, at most every 30 s.
   */
  telemetry?: false | { sampleRate?: number; intervalMs?: number; random?: () => number };
}
/** A runtime diagnostic for telemetry: its code and where it came from. */
export interface TelemetryDiagnostic {
  code: string;
  source?: "render" | "source" | "action";
}
export interface DocumentResponse {
  objectId: string;
  versionId: string;
  path: string | null;
  document: IrDocument;
  diagnostics?: DeliveryDiagnostic[];
}
export interface PageResponse {
  contentContext?: { locale: string; versions: Record<string, string> };
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
  /** Counts runtime diagnostics for telemetry; never throws and never blocks rendering. */
  report(diagnostics: readonly TelemetryDiagnostic[]): void;
  /** Sends the pending diagnostics now (e.g. before the process exits). */
  flushTelemetry(): Promise<void>;
}
