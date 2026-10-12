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
  history: History;
  config: Config;
  changeset: Changeset;
  record: DocumentRecord;
  manifest: Manifest;
  manifestHash: string;
}
export interface Operation {
  type:
    | "node.setProps"
    | "node.setDesign"
    | "node.setBinding"
    | "localContent.set"
    | "node.insert"
    | "node.remove"
    | "node.move";
  target: string;
  payload: Record<string, unknown>;
}
export interface Api {
  history(session: Session, beforeSeq?: number, signal?: AbortSignal): Promise<History>;
  changeHistory(
    session: Session,
    action: "undo" | "redo",
    key: string,
    signal: AbortSignal,
  ): Promise<void>;
  pages(cursor?: string, signal?: AbortSignal): Promise<List<PageItem>>;
  drafts(documentId: string, cursor?: string, signal?: AbortSignal): Promise<List<Changeset>>;
  createDraft(title: string, key: string): Promise<Changeset>;
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
export interface HistoryAction {
  id: string;
  firstSeq: number;
  lastSeq: number;
  actorId: string;
  actorName: string;
  createdAt: string;
  kind: "edit" | "undo" | "redo";
  reason: string | null;
  operations: { type: string; target: string | null; status: string }[];
}
export interface History {
  seq: number;
  items: HistoryAction[];
  nextBeforeSeq: number | null;
  canUndo: boolean;
  canRedo: boolean;
  undoReason: string | null;
  redoReason: string | null;
}
export interface PageItem {
  id: string;
  path: string;
  title: string;
  hasHead: boolean;
}
export interface List<T> {
  items: T[];
  nextCursor: string | null;
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
