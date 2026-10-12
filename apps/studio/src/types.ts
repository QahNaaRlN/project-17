import type { IrDocument } from "@cms/ir";
import type { Manifest } from "@cms/manifest";
export interface Config {
  project: string;
  environment: string;
  appOrigin: string;
}
export interface Changeset {
  id: string;
  title: string;
  seq: number;
  state: string;
  kind: string;
  targets: string[];
}
export interface DocumentRecord {
  id: string;
  kind: string;
  path: string | null;
  body: IrDocument;
}
export interface Session {
  config: Config;
  changeset: Changeset;
  record: DocumentRecord;
  manifest: Manifest;
  manifestHash: string;
}
export interface Operation {
  type: "node.setProps" | "node.setDesign" | "node.setBinding" | "localContent.set";
  target: string;
  payload: Record<string, unknown>;
}
export interface Api {
  config(signal?: AbortSignal): Promise<Config>;
  open(documentId: string, changesetId: string, signal: AbortSignal): Promise<Session>;
  apply(
    session: Session,
    operations: Operation[],
    idempotencyKey: string,
    signal: AbortSignal,
  ): Promise<void>;
  preview(session: Session, signal: AbortSignal): Promise<{ token: string; expiresAt: string }>;
}
export class StudioError extends Error {
  constructor(
    public code: string,
    public uncertain = false,
  ) {
    super(code);
  }
}
export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
  instance?: string;
}
