import type { IrDocument } from "@cms/ir";
import type { Manifest } from "@cms/manifest";
export interface Config {
  assetUploadOrigin?: string;
  authMode?: "service" | "oidc";
  project: string;
  environment: string;
  appOrigin: string;
  /** Exact origins of PR preview deployments (STU-328). */
  previewAppOrigins?: string[];
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
export interface WorkContext {
  codeIndexAvailable?: boolean;
  work?: WorkQueueCard;
  diffSnapshot?: string;
  review: Review;
  history: History;
  config: Config;
  changeset: Changeset;
  record?: DocumentRecord;
  manifest: Manifest;
  manifestHash: string;
}
export interface Session extends WorkContext {
  record: DocumentRecord;
}
export interface WorkQueueFilters {
  q: string;
  state: string;
  ownership: string;
  kind: string;
}
export interface WorkQueueItem {
  id: string;
  title: string;
  ownerId: string;
  ownerName: string;
  own: boolean;
  state: string;
  createdAt: string;
  updatedAt: string;
  seq: number;
  objectCount: number;
  deletedCount: number;
  countsByKind: Record<"page" | "component" | "entity" | "asset", number>;
}
export interface WorkQueueObject {
  id: string;
  kind: string;
  docKind: string | null;
  title: string;
  baseVersionId: string | null;
  workingVersionId: string;
  deleted: boolean;
  basePath: string | null;
  workingPath: string | null;
}
export interface WorkQueueCard {
  item: WorkQueueItem;
  objects: WorkQueueObject[];
}
export interface Operation {
  type:
    | "document.setContent"
    | "document.setDataSource"
    | "node.setProps"
    | "node.setDesign"
    | "node.setBinding"
    | "node.rename"
    | "node.setCondition"
    | "node.setBehavior"
    | "localContent.set"
    | "localContent.remove"
    | "node.insert"
    | "node.remove"
    | "node.move"
    | "component.extract";
  target: string;
  payload: Record<string, unknown>;
}
export interface Api {
  githubConfig?(signal?: AbortSignal): Promise<import("./capability-github.js").GithubConfig>;
  github?(id: string, signal?: AbortSignal): Promise<import("./capability-github.js").GithubView>;
  githubHistory?(
    id: string,
    cursor?: string,
    signal?: AbortSignal,
  ): Promise<List<import("./capability-github.js").GithubObservation>>;
  githubCommand?(attempt: import("./capability-github.js").GithubAttempt): Promise<unknown>;
  deliveryEndpoints?(
    signal?: AbortSignal,
  ): Promise<{ items: import("./capability-delivery.js").DeliveryEndpoint[] }>;
  deliveryTarget?(signal?: AbortSignal): Promise<import("./capability-delivery.js").DeliveryTarget>;
  deliveryPreview?(
    id: string,
    signal?: AbortSignal,
  ): Promise<import("./capability-delivery.js").DeliveryPreview>;
  deliveries?(
    id: string,
    cursor?: string,
    signal?: AbortSignal,
  ): Promise<List<import("./capability-delivery.js").Delivery>>;
  delivery?(id: string, signal?: AbortSignal): Promise<import("./capability-delivery.js").Delivery>;
  deliveryCommand?(
    attempt: import("./capability-delivery.js").DeliveryAttempt,
  ): Promise<
    | import("./capability-delivery.js").DeliveryResult
    | import("./capability-delivery.js").DeliveryTarget
  >;
  capabilities?(
    filters: { status?: string; key?: string; cursor?: string },
    signal?: AbortSignal,
  ): Promise<List<import("./capabilities.js").CapabilityRequest>>;
  capability?(
    id: string,
    signal?: AbortSignal,
  ): Promise<import("./capabilities.js").CapabilityRequest>;
  capabilityContext?(
    id: string,
    version?: number,
    signal?: AbortSignal,
  ): Promise<{ hash?: string; pack: Record<string, unknown> }>;
  capabilityCommand?(
    attempt: import("./capabilities.js").CapabilityAttempt,
  ): Promise<import("./capabilities.js").CapabilityResult>;
  createWork?(attempt: import("./work-creation.js").WorkCreationAttempt): Promise<Changeset>;
  workQueue?(
    filters: WorkQueueFilters,
    cursor?: string,
    signal?: AbortSignal,
  ): Promise<List<WorkQueueItem>>;
  openWork?(id: string, signal: AbortSignal): Promise<WorkContext>;
  publications?(
    cursor?: string,
    signal?: AbortSignal,
  ): Promise<import("./publication-history.js").PublicationList>;
  publication?(
    id: string,
    preview: boolean,
    signal?: AbortSignal,
  ): Promise<import("./publication-history.js").PublicationCard>;
  publicationDiff?(
    card: import("./publication-history.js").PublicationCard,
    objectId: string,
    preview: boolean,
    cursor?: string,
    signal?: AbortSignal,
  ): Promise<import("./publication-history.js").PublicationPage>;
  rollback?(
    attempt: import("./publication-history.js").RollbackAttempt,
  ): Promise<import("./publication-history.js").PublicationEntry>;
  diffIndex?(session: WorkContext, signal?: AbortSignal): Promise<DiffIndex>;
  diffPage?(
    session: WorkContext,
    index: DiffIndex,
    objectId: string,
    baseline: DiffBaseline,
    cursor?: string,
    signal?: AbortSignal,
  ): Promise<DiffPage>;
  agentConfig?(signal?: AbortSignal): Promise<AgentConfig>;
  agentRuns?(session: Session, signal?: AbortSignal): Promise<AgentRun[]>;
  agentCommand?(
    name: "start-agent-run" | "cancel-agent-run",
    payload: Record<string, unknown>,
    key: string,
  ): Promise<void>;
  events?(session: Session, after: string | undefined, signal: AbortSignal): Promise<Response>;
  presence?(session: Session, leaseId: string, leave: boolean, signal: AbortSignal): Promise<void>;
  contentCatalog(
    session: WorkContext,
    kind: ContentRecord["kind"],
    schema?: string,
    cursor?: string,
    signal?: AbortSignal,
    search?: string,
  ): Promise<ContentCatalog>;
  content(session: WorkContext, id: string, signal?: AbortSignal): Promise<ContentRecord>;
  applyContent(
    session: WorkContext,
    operations: ContentOperation[],
    key: string,
    signal: AbortSignal,
  ): Promise<string[]>;
  uploadCommand(
    command: "create-asset-upload" | "complete-asset-upload",
    payload: Record<string, unknown>,
    key: string,
    signal: AbortSignal,
  ): Promise<Upload>;
  uploadStatus(id: string, signal: AbortSignal): Promise<Upload>;
  account(signal?: AbortSignal): Promise<Account>;
  logout(signal?: AbortSignal): Promise<void>;
  workItems(cursor?: string, signal?: AbortSignal): Promise<List<WorkItem>>;
  workflow(
    session: WorkContext,
    action: WorkflowAction,
    comment: string,
    key: string,
    signal: AbortSignal,
  ): Promise<Publication | null>;
  history(session: WorkContext, beforeSeq?: number, signal?: AbortSignal): Promise<History>;
  changeHistory(
    session: WorkContext,
    action: "undo" | "redo",
    key: string,
    signal: AbortSignal,
  ): Promise<void>;
  pages(cursor?: string, signal?: AbortSignal, query?: string): Promise<List<PageItem>>;
  drafts(documentId: string, cursor?: string, signal?: AbortSignal): Promise<List<Changeset>>;
  createDraft(title: string, key: string): Promise<Changeset>;
  config(signal?: AbortSignal): Promise<Config>;
  open(documentId: string, changesetId: string, signal: AbortSignal): Promise<Session>;
  apply(
    session: Session,
    operations: Operation[],
    idempotencyKey: string,
    signal: AbortSignal,
    reason?: string,
  ): Promise<void>;
  extractPlan?(
    session: Session,
    nodeId: string,
    signal?: AbortSignal,
  ): Promise<import("./component-extract.js").ExtractPlan>;
  components?(
    session: Session,
    ids: string[],
    signal?: AbortSignal,
  ): Promise<Record<string, IrDocument>>;
  preview(
    session: Session,
    signal: AbortSignal,
    environment?: string,
  ): Promise<{ token: string; expiresAt: string; registrationSeq?: number }>;
  capabilityPreview?(
    id: string,
    signal?: AbortSignal,
  ): Promise<import("./capability-preview.js").PreviewList>;
  previewArgs?(attempt: import("./capability-preview.js").PreviewArgsAttempt): Promise<void>;
}
export interface AgentConfig {
  provider: "openrouter";
  model: string;
  configured: boolean;
  canDelegate: boolean;
  monthlyTokens: number;
  userMonthlyTokens: number;
}
export interface AgentRun {
  id: string;
  changesetId: string;
  documentId: string;
  environment: string;
  model: string;
  prompt: string;
  state: "queued" | "running" | "completed" | "failed" | "cancelled" | "interrupted";
  stepSeq: number;
  errorCode?: string;
  steps: {
    seq: number;
    kind: string;
    data: { name?: string; text?: string; isError?: boolean; code?: string; requestId?: string };
  }[];
  tokens: number;
  reserved: number;
  usageKnown: boolean;
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
export interface Account {
  authenticated: boolean;
  user?: { id: string; name: string };
  csrfToken?: string;
  rights: string[];
  hasAccess: boolean;
  expiresAt?: string;
}
export interface WorkItem {
  id: string;
  title: string;
  documentId: string;
  path: string;
  ownerId: string;
  ownerName: string;
  own: boolean;
  state: string;
}
export interface List<T> {
  items: T[];
  nextCursor: string | null;
}
export interface ContentItem {
  id: string;
  kind: "entity" | "asset";
  schema?: string;
  title: string;
  deleted: boolean;
  assetKind?: string;
  mimeType?: string;
}
export interface ContentCatalog extends List<ContentItem> {
  locales: string[];
  defaultLocale: string;
}
export interface ContentRecord {
  id: string;
  kind: ContentItem["kind"];
  schema?: string;
  schemaVersion?: number;
  versionId: string;
  deleted: boolean;
  data: Record<string, unknown>;
}
export interface ContentOperation {
  expectedVersionId?: string;
  type:
    | "entity.create"
    | "entity.setFields"
    | "entity.delete"
    | "entity.restore"
    | "asset.create"
    | "asset.updateMeta"
    | "asset.replaceFile";
  target?: string;
  payload: Record<string, unknown>;
}
export interface Upload {
  assetId: string;
  status: "pending" | "processing" | "ready" | "rejected";
  expiresAt: string;
  fileHash?: string;
  errorCode?: string;
  uploadUrl?: string;
}
export class StudioError extends Error {
  /** Причина отказа операции apply-operations (params.operationCode) и её указатели. */
  operation?: { code: string; diagnostics: { code: string; pointer: string; nodeId?: string }[] };
  constructor(
    public code: string,
    public uncertain = false,
    public diagnostics: ReviewDiagnostic[] = [],
  ) {
    super(code);
  }
}
export const workflowActions = [
  "submit",
  "reopen",
  "approve",
  "request-changes",
  "publish",
] as const;
export type WorkflowAction = (typeof workflowActions)[number];
export interface Review {
  capabilityPolicy?: { defaultMode: string; maxMode: string };
  canEdit: boolean;
  canEditBehavior?: boolean;
  canEditCondition?: boolean;
  needsAttention: boolean;
  changeset: Changeset;
  risk: "low" | "medium" | "high" | null;
  requiredApprovals: number;
  approvals: number;
  requiredRoles?: string[];
  missingRequiredRoles?: string[];
  checks: { stage: string; status: string; blocking: boolean; details: unknown }[];
  history: {
    approverId: string;
    decision: string;
    comment: string | null;
    valid: boolean;
    createdAt: string;
  }[];
  objects: { id: string; kind: string; path: string | null; deleted: boolean }[];
  actions: Record<WorkflowAction, string | null>;
}
export interface ReviewDiagnostic {
  objectId: string;
  environment: string;
  code: string;
  severity: "error" | "warning";
  pointer: string;
  nodeId?: string;
}
export interface Publication {
  id: string;
  changesetId: string;
  environment: string;
  createdAt: string;
}
export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
  instance?: string;
}
export type DiffBaseline = "base" | "published";
export interface DiffSummary {
  effect: "added" | "modified" | "deleted" | "restored" | "metadata-only" | "unchanged";
  changeCount: number;
}
export interface DiffObject {
  id: string;
  kind: "document" | "entity" | "asset";
  docKind?: string;
  title: string;
  beforePath: string | null;
  afterPath: string | null;
  baseVersionId: string | null;
  workingVersionId: string;
  headVersionId: string | null;
  publishedVersionId: string | null;
  workingDeleted?: boolean;
  baseStale: boolean;
  summaries: Record<DiffBaseline, DiffSummary>;
}
export interface DiffIndex {
  changesetId: string;
  seq: number;
  snapshot: string;
  environment: string;
  targets: string[];
  complete: true;
  objects: DiffObject[];
}
export type DiffValue =
  | { present: false }
  | { present: true; value: unknown }
  | { present: true; rawJson: string }
  | { present: true; redacted: true; reason: "service-field" };
export interface DiffChange {
  key: string;
  category: string;
  kind: "add" | "remove" | "replace" | "move";
  pointer: string;
  nodeId?: string;
  locale?: string;
  before: DiffValue;
  after: DiffValue;
}
export interface DiffPage {
  changesetId: string;
  objectId: string;
  seq: number;
  snapshot: string;
  environment: string;
  baseline: DiffBaseline;
  totalChanges: number;
  items: DiffChange[];
  nextCursor: string | null;
}
