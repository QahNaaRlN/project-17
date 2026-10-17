import { validateDiffIndex, validateDiffPage } from "./review-diff.js";
import {
  publicationList,
  publicationCard,
  publicationPage,
  type PublicationList,
  type PublicationCard,
  type PublicationPage,
  type PublicationEntry,
} from "./publication-history.js";
import type { DiffIndex, DiffPage } from "./types.js";
import { validateQueue, validateQueueCard } from "./work-queue.js";
import { checkCreatedWork, canRetryWork, workTitle, clearWorkAttempt } from "./work-creation.js";
import type { WorkQueueItem, WorkQueueCard, WorkContext } from "./types.js";
import { validateDocument } from "@cms/ir";
import { jsonValue } from "@cms/runtime";
import { canonicalJson } from "@cms/manifest";
import { capability, type CapabilityRequest, type CapabilityResult } from "./capabilities.js";
import {
  delivery,
  deliveryTarget,
  deliveryPreview,
  type Delivery,
  type DeliveryResult,
  type DeliveryTarget,
  type DeliveryEndpoint,
  type DeliveryPreview,
} from "./capability-delivery.js";
import {
  StudioError,
  type Api,
  type Changeset,
  type Config,
  type DocumentRecord,
  type Session,
  type List,
  type PageItem,
  type History,
  type Account,
  type WorkItem,
  type ContentRecord,
  type ContentItem,
  type ContentCatalog,
  type Upload,
  type AgentConfig,
  type AgentRun,
} from "./types.js";
import { validateReview, problemDiagnostics, safeDiagnostics } from "./review.js";
import type { Review, Publication } from "./types.js";
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i;
function checkUpload(value: Upload, uncertain = false): Upload {
  if (
    !value ||
    !uuid.test(value.assetId) ||
    !["pending", "processing", "ready", "rejected"].includes(value.status) ||
    !Number.isFinite(Date.parse(value.expiresAt)) ||
    (value.fileHash !== undefined && !/^[a-f0-9]{64}$/.test(value.fileHash)) ||
    (value.status === "ready" && !value.fileHash) ||
    (value.errorCode !== undefined && !/^[A-Z0-9_]{1,80}$/.test(value.errorCode)) ||
    (value.uploadUrl !== undefined && typeof value.uploadUrl !== "string")
  )
    throw new StudioError("RESPONSE_INVALID", uncertain);
  return value;
}
export function createApi(fetcher: typeof fetch = fetch, onSessionLost?: () => void): Api {
  let csrf: string | undefined,
    actor: string | undefined,
    human = false;
  async function request<T>(
    path: string,
    signal?: AbortSignal,
    body?: unknown,
    key?: string,
  ): Promise<T> {
    if (human && !path.startsWith("/auth/") && path !== "/config") {
      try {
        const account = await api.account(signal);
        if (!account.authenticated) {
          onSessionLost?.();
          throw new StudioError("UNAUTHENTICATED");
        }
      } catch (cause) {
        throw new StudioError(
          cause instanceof StudioError ? cause.code : "SESSION_UNAVAILABLE",
          body !== undefined,
          cause instanceof StudioError ? cause.diagnostics : [],
        );
      }
    }
    let response: Response;
    try {
      response = await fetcher("/api/studio" + path, {
        method: body === undefined ? "GET" : "POST",
        credentials: "same-origin",
        cache: "no-store",
        ...(body === undefined && human && actor ? { headers: { "X-CMS-Actor": actor } } : {}),
        ...(signal ? { signal } : {}),
        ...(body === undefined
          ? {}
          : {
              headers: {
                "Content-Type": "application/json",
                "Idempotency-Key": key!,
                ...(csrf ? { "X-CSRF-Token": csrf } : {}),
                ...(human && actor ? { "X-CMS-Actor": actor } : {}),
              },
              body: JSON.stringify(body),
            }),
      });
    } catch {
      throw new StudioError("NETWORK_UNCERTAIN", body !== undefined);
    }
    if (!response.ok) {
      let code = "CMS_REQUEST_FAILED";
      let diagnostics: StudioError["diagnostics"] = [];
      try {
        const value = await response.json();
        if (typeof value.code === "string" && /^[A-Z0-9_]{1,80}$/.test(value.code))
          code = value.code;
        diagnostics = [...problemDiagnostics(value.params), ...safeDiagnostics(value.diagnostics)];
      } catch {
        /* no server detail/secrets in UI */
      }
      if (
        human &&
        (response.status === 401 ||
          code === "SESSION_CHANGED" ||
          code === "PROJECT_ACCESS_REQUIRED")
      )
        onSessionLost?.();
      throw new StudioError(code, body !== undefined && response.status >= 500, diagnostics);
    }
    try {
      return (await response.json()) as T;
    } catch {
      throw new StudioError("RESPONSE_INVALID", body !== undefined);
    }
  }
  const api: Api = {
    async deliveryEndpoints(signal) {
      const config = await api.config(signal);
      const value = await request<{ items: DeliveryEndpoint[] }>(
        "/api/v1/capability-delivery-endpoints?" +
          new URLSearchParams({ environment: config.environment }),
        signal,
      );
      if (
        !Array.isArray(value?.items) ||
        value.items.some(
          (e) =>
            !e ||
            typeof e.endpointId !== "string" ||
            typeof e.url !== "string" ||
            !/^sha256:[a-f0-9]{64}$/.test(e.fingerprint) ||
            typeof e.ready !== "boolean",
        )
      )
        throw new StudioError("RESPONSE_INVALID");
      return value;
    },
    async deliveryTarget(signal) {
      const config = await api.config(signal);
      return deliveryTarget(
        await request<DeliveryTarget>(
          "/api/v1/capability-delivery-target?" +
            new URLSearchParams({ environment: config.environment }),
          signal,
        ),
      );
    },
    async deliveryPreview(id, signal) {
      if (!uuid.test(id)) throw new StudioError("IDENTIFIER_INVALID");
      const config = await api.config(signal);
      return deliveryPreview(
        await request<DeliveryPreview>(
          "/api/v1/capability-requests/" +
            id +
            "/delivery-preview?" +
            new URLSearchParams({ environment: config.environment }),
          signal,
        ),
      );
    },
    async deliveries(id, cursor, signal) {
      if (!uuid.test(id)) throw new StudioError("IDENTIFIER_INVALID");
      const config = await api.config(signal);
      const value = await request<List<Delivery>>(
        "/api/v1/capability-requests/" +
          id +
          "/deliveries?" +
          new URLSearchParams({
            environment: config.environment,
            limit: "25",
            ...(cursor ? { cursor } : {}),
          }),
        signal,
      );
      if (
        !Array.isArray(value?.items) ||
        (value.nextCursor != null && typeof value.nextCursor !== "string")
      )
        throw new StudioError("RESPONSE_INVALID");
      return { ...value, items: value.items.map((v) => delivery(v)) };
    },
    async delivery(id, signal) {
      if (!uuid.test(id)) throw new StudioError("IDENTIFIER_INVALID");
      const config = await api.config(signal);
      return delivery(
        await request<Delivery>(
          "/api/v1/capability-deliveries/" +
            id +
            "?" +
            new URLSearchParams({ environment: config.environment }),
          signal,
        ),
      );
    },
    async deliveryCommand(attempt) {
      if (attempt.actor) {
        const account = await api.account();
        if (!account.authenticated || account.user?.id !== attempt.actor)
          throw new StudioError("SESSION_CHANGED", true);
      }
      const value = await request<{ result: DeliveryResult | DeliveryTarget }>(
        "/api/v1/commands/" + attempt.name,
        undefined,
        { payload: attempt.payload, reason: attempt.reason },
        attempt.key,
      );
      if (attempt.name === "set-capability-delivery-target")
        return deliveryTarget(value?.result as DeliveryTarget, true);
      const result = value?.result as DeliveryResult;
      delivery(result?.delivery, true);
      if (typeof result.deduplicated !== "boolean") throw new StudioError("RESPONSE_INVALID", true);
      return result;
    },
    async capabilities(filters, signal) {
      const config = await api.config(signal);
      const query = new URLSearchParams({
        environment: config.environment,
        kind: "action",
        limit: "25",
        ...filters,
      });
      const list = await request<List<CapabilityRequest>>(
        "/api/v1/capability-requests?" + query,
        signal,
      );
      if (
        !Array.isArray(list?.items) ||
        (list.nextCursor !== null &&
          list.nextCursor !== undefined &&
          typeof list.nextCursor !== "string")
      )
        throw new StudioError("RESPONSE_INVALID");
      return { ...list, items: list.items.map((v) => capability(v)) };
    },
    async capability(id, signal) {
      if (!uuid.test(id)) throw new StudioError("IDENTIFIER_INVALID");
      const config = await api.config(signal);
      return capability(
        await request<CapabilityRequest>(
          "/api/v1/capability-requests/" +
            id +
            "?environment=" +
            encodeURIComponent(config.environment),
          signal,
        ),
      );
    },
    async capabilityContext(id, version, signal) {
      if (
        !uuid.test(id) ||
        (version !== undefined && (!Number.isSafeInteger(version) || version < 1))
      )
        throw new StudioError("IDENTIFIER_INVALID");
      const config = await api.config(signal);
      const value = await request<Record<string, unknown>>(
        "/api/v1/capability-requests/" +
          id +
          (version === undefined ? "/context-preview" : "/context-pack") +
          "?" +
          new URLSearchParams({
            environment: config.environment,
            ...(version === undefined ? {} : { version: String(version) }),
          }),
        signal,
      );
      const pack = version === undefined ? value.pack : value;
      if (
        !pack ||
        typeof pack !== "object" ||
        Array.isArray(pack) ||
        new TextEncoder().encode(canonicalJson(pack)).length > 512 * 1024
      )
        throw new StudioError("RESPONSE_INVALID");
      return {
        pack: pack as Record<string, unknown>,
        ...(typeof value.hash === "string" ? { hash: value.hash } : {}),
      };
    },
    async capabilityCommand(attempt) {
      const value = await request<{ result: CapabilityResult }>(
        "/api/v1/commands/" + attempt.name,
        undefined,
        { payload: attempt.payload, reason: attempt.reason },
        attempt.key,
      );
      capability(value?.result?.request, true);
      return value.result;
    },
    async createWork(attempt) {
      let config: Config;
      try {
        config = await api.config();
        if (config.authMode === "oidc") {
          const account = await api.account();
          if (!account.authenticated || account.user?.id !== attempt.actor)
            throw new StudioError("SESSION_CHANGED", true);
        }
      } catch (cause) {
        // A preflight failure cannot resolve a previously unconfirmed commit.
        throw new StudioError(
          cause instanceof StudioError ? cause.code : "SESSION_UNAVAILABLE",
          true,
        );
      }
      if (config.project !== attempt.project || config.environment !== attempt.environment)
        throw new StudioError("SESSION_CHANGED", true);
      if (
        !canRetryWork(attempt) ||
        attempt.body.reason !== "Studio: create content work" ||
        workTitle(attempt.body.payload.title) !== attempt.body.payload.title ||
        attempt.body.payload.targets.length !== 1 ||
        attempt.body.payload.targets[0] !== config.environment
      )
        throw new StudioError("WORK_ATTEMPT_INVALID", true);
      const response = await request<{ result: Changeset }>(
        "/api/v1/commands/create-changeset",
        undefined,
        attempt.body,
        attempt.key,
      );
      return checkCreatedWork(response?.result, config.environment);
    },
    async workQueue(filters, cursor, signal) {
      const config = await api.config(signal);
      const query = new URLSearchParams({
        environment: config.environment,
        ...filters,
        limit: "50",
        ...(cursor ? { cursor } : {}),
      });
      return validateQueue(
        await request<List<WorkQueueItem>>("/api/v1/work-queue?" + query, signal),
      );
    },
    async openWork(id, signal) {
      if (!uuid.test(id)) throw new StudioError("IDENTIFIER_INVALID");
      const config = await api.config(signal);
      const cardPath =
        "/api/v1/work-queue/" + id + "?environment=" + encodeURIComponent(config.environment);
      const work = validateQueueCard(await request<WorkQueueCard>(cardPath, signal), id);
      const [changeset, active, history, rawReview] = await Promise.all([
        request<Changeset>("/api/v1/changesets/" + id, signal),
        request<{ manifest: WorkContext["manifest"]; manifestHash: string }>(
          "/api/v1/manifest?environment=" + encodeURIComponent(config.environment),
          signal,
        ),
        readHistory(id, work.item.seq, undefined, signal),
        request<Review>("/api/v1/changesets/" + id + "/review", signal),
      ]);
      const review = validateReview(rawReview, id);
      const after = validateQueueCard(await request<WorkQueueCard>(cardPath, signal), id);
      if (
        changeset?.id !== id ||
        changeset.kind !== "standard" ||
        !changeset.targets?.includes(config.environment) ||
        changeset.seq !== work.item.seq ||
        review.changeset.seq !== changeset.seq ||
        history.seq !== changeset.seq ||
        review.changeset.state !== work.item.state ||
        after.item.seq !== work.item.seq ||
        after.item.state !== work.item.state
      )
        throw new StudioError("CHANGESET_SEQ_CONFLICT");
      if (
        !/^sha256:[a-f0-9]{64}$/.test(active?.manifestHash) ||
        active.manifest?.manifestVersion !== "1.0" ||
        !active.manifest.tokens ||
        !active.manifest.breakpoints
      )
        throw new StudioError("SESSION_INVALID");
      return { config, changeset, work, history, review, ...active };
    },
    async publications(cursor, signal) {
      const config = await api.config(signal);
      const query = new URLSearchParams({
        environment: config.environment,
        limit: "50",
        ...(cursor ? { cursor } : {}),
      });
      return publicationList(
        await request<PublicationList>("/api/v1/publication-history?" + query, signal),
        config.environment,
      );
    },
    async publication(id, preview, signal) {
      const config = await api.config(signal);
      return publicationCard(
        await request<PublicationCard>(
          "/api/v1/publication-history/" +
            encodeURIComponent(id) +
            (preview ? "/rollback-preview" : "") +
            "?environment=" +
            encodeURIComponent(config.environment),
          signal,
        ),
        id,
        config.environment,
      );
    },
    async publicationDiff(card, objectId, preview, cursor, signal) {
      const query = new URLSearchParams({
        environment: card.publication.environment,
        objectId,
        snapshot: card.snapshot,
        limit: "100",
        ...(cursor ? { cursor } : {}),
      });
      return publicationPage(
        await request<PublicationPage>(
          "/api/v1/publication-history/" +
            card.publication.id +
            (preview ? "/rollback-preview" : "") +
            "/diff?" +
            query,
          signal,
        ),
        card.publication.id,
        objectId,
        card.snapshot,
      );
    },
    async rollback(attempt) {
      const { reason, key } = attempt;
      const payload = {
        publicationId: attempt.publicationId,
        resetHead: attempt.resetHead,
        expectedRollbackSnapshot: attempt.expectedRollbackSnapshot,
      };
      const value = await request<{ result: PublicationEntry }>(
        "/api/v1/commands/rollback",
        undefined,
        { reason, payload },
        key,
      );
      const p = value?.result;
      if (
        !p ||
        !uuid.test(p.id) ||
        p.kind !== "rollback" ||
        p.sourcePublicationId !== attempt.publicationId
      )
        throw new StudioError("RESPONSE_INVALID", true);
      return p;
    },
    async agentConfig(signal) {
      const value = await request<AgentConfig>("/api/v1/agent-config", signal);
      if (
        !value ||
        value.provider !== "openrouter" ||
        typeof value.model !== "string" ||
        typeof value.configured !== "boolean" ||
        typeof value.canDelegate !== "boolean" ||
        !Number.isSafeInteger(value.monthlyTokens) ||
        !Number.isSafeInteger(value.userMonthlyTokens)
      )
        throw new StudioError("RESPONSE_INVALID");
      return value;
    },
    async agentRuns(session, signal) {
      const query = new URLSearchParams({
        documentId: session.record.id,
        changesetId: session.changeset.id,
        environment: session.config.environment,
      });
      const value = await request<{ items: AgentRun[] }>("/api/v1/agent-runs?" + query, signal);
      if (
        !Array.isArray(value?.items) ||
        value.items.length > 10 ||
        value.items.some(
          (r) =>
            !r ||
            !uuid.test(r.id) ||
            r.documentId !== session.record.id ||
            r.changesetId !== session.changeset.id ||
            r.environment !== session.config.environment ||
            !["queued", "running", "completed", "failed", "cancelled", "interrupted"].includes(
              r.state,
            ) ||
            typeof r.prompt !== "string" ||
            r.prompt.length > 16000 ||
            typeof r.model !== "string" ||
            !Number.isSafeInteger(r.tokens) ||
            r.tokens < 0 ||
            !Number.isSafeInteger(r.reserved) ||
            r.reserved < 0 ||
            typeof r.usageKnown !== "boolean" ||
            !Number.isSafeInteger(r.stepSeq) ||
            !Array.isArray(r.steps) ||
            r.steps.length > 32 ||
            r.steps.some(
              (step) =>
                !step ||
                !Number.isSafeInteger(step.seq) ||
                typeof step.kind !== "string" ||
                typeof step.data !== "object" ||
                !step.data ||
                (step.data.text !== undefined && typeof step.data.text !== "string") ||
                (step.data.name !== undefined && typeof step.data.name !== "string") ||
                (step.data.requestId !== undefined && !uuid.test(step.data.requestId)),
            ),
        )
      )
        throw new StudioError("RESPONSE_INVALID");
      return value.items;
    },
    async agentCommand(name, payload, key) {
      await request(
        "/api/v1/commands/" + name,
        undefined,
        { payload, reason: "Studio: explicit Agent task" },
        key,
      );
    },
    async events(session, after, signal) {
      if (human) {
        const account = await api.account(signal);
        if (!account.authenticated) {
          onSessionLost?.();
          throw new StudioError("UNAUTHENTICATED");
        }
      }
      const query = new URLSearchParams({
        environment: session.config.environment,
        documentId: session.record.id,
        changesetId: session.changeset.id,
        ...(after === undefined ? {} : { after }),
      });
      const response = await fetcher("/api/studio/api/v1/events?" + query, {
        credentials: "same-origin",
        cache: "no-store",
        signal,
        headers: {
          Accept: "text/event-stream",
          ...(human && actor ? { "X-CMS-Actor": actor } : {}),
        },
      });
      if (!response.ok) {
        if (response.status === 401 || response.status === 403) onSessionLost?.();
        throw new StudioError(
          response.status === 401 || response.status === 403
            ? "UNAUTHENTICATED"
            : "EVENTS_UNAVAILABLE",
        );
      }
      if (!response.headers.get("content-type")?.startsWith("text/event-stream") || !response.body)
        throw new StudioError("RESPONSE_INVALID");
      return response;
    },
    async presence(session, leaseId, leave, signal) {
      await request(
        "/api/v1/commands/presence-" + (leave ? "leave" : "heartbeat"),
        signal,
        {
          payload: {
            leaseId,
            ...(leave
              ? {}
              : {
                  environment: session.config.environment,
                  documentId: session.record.id,
                  changesetId: session.changeset.id,
                }),
          },
        },
        crypto.randomUUID(),
      );
    },
    async contentCatalog(session, kind, schema, cursor, signal, search) {
      const value = (await listing<ContentItem>(
        "/api/v1/content-catalog",
        {
          environment: session.config.environment,
          changesetId: session.changeset.id,
          kind,
          ...(schema ? { schema } : {}),
          ...(search ? { q: search } : {}),
        },
        cursor,
        signal,
      )) as ContentCatalog;
      if (
        value.items.some(
          (i) =>
            !i ||
            !uuid.test(i.id) ||
            i.kind !== kind ||
            typeof i.title !== "string" ||
            typeof i.deleted !== "boolean" ||
            (schema && i.schema !== schema),
        )
      )
        throw new StudioError("RESPONSE_INVALID");
      if (
        !Array.isArray(value.locales) ||
        value.locales.some(
          (locale) => typeof locale !== "string" || !/^[a-z]{2,3}(?:-[A-Z]{2})?$/.test(locale),
        ) ||
        typeof value.defaultLocale !== "string" ||
        !/^[a-z]{2,3}(?:-[A-Z]{2})?$/.test(value.defaultLocale) ||
        (value.locales.length && !value.locales.includes(value.defaultLocale))
      )
        throw new StudioError("RESPONSE_INVALID");
      return value;
    },
    async content(session, id, signal) {
      if (!uuid.test(id)) throw new StudioError("IDENTIFIER_INVALID");
      const params = new URLSearchParams({
        environment: session.config.environment,
        changesetId: session.changeset.id,
      });
      const value = await request<ContentRecord>(`/api/v1/content/${id}?${params}`, signal);
      if (
        !value ||
        value.id !== id ||
        !uuid.test(value.versionId) ||
        !["entity", "asset"].includes(value.kind) ||
        typeof value.deleted !== "boolean" ||
        !value.data ||
        typeof value.data !== "object" ||
        Array.isArray(value.data) ||
        (value.kind === "entity" &&
          (typeof value.schema !== "string" || !Number.isSafeInteger(value.schemaVersion)))
      )
        throw new StudioError("RESPONSE_INVALID");
      try {
        jsonValue(value.data);
        canonicalJson(value.data);
      } catch {
        throw new StudioError("RESPONSE_INVALID");
      }
      return value;
    },
    async applyContent(session, operations, key, signal) {
      if (session.changeset.targets[0] !== session.config.environment)
        throw new StudioError("CONTENT_TARGET_MISMATCH");
      const value = await request<{
        result: { changeset: Changeset; operations: { type: string; target: string }[] };
      }>(
        "/api/v1/commands/apply-operations",
        signal,
        {
          payload: {
            changesetId: session.changeset.id,
            expectedSeq: session.changeset.seq,
            operations,
            group: true,
          },
          reason: "Studio: edit content draft",
        },
        key,
      );
      if (
        value?.result?.changeset?.id !== session.changeset.id ||
        value.result.changeset.seq !== session.changeset.seq + operations.length ||
        !Array.isArray(value.result.operations) ||
        value.result.operations.length !== operations.length ||
        value.result.operations.some(
          (op, i) =>
            !uuid.test(op.target) ||
            op.type !== operations[i]?.type ||
            (operations[i]?.target && operations[i]!.target !== op.target),
        )
      )
        throw new StudioError("RESPONSE_INVALID", true);
      return value.result.operations.map((op) => op.target);
    },
    async uploadCommand(command, payload, key, signal) {
      const value = await request<{ result: Upload }>(
        `/api/v1/commands/${command}`,
        signal,
        { payload, reason: "Studio: upload asset" },
        key,
      );
      return checkUpload(value?.result, true);
    },
    async uploadStatus(id, signal) {
      if (!uuid.test(id)) throw new StudioError("IDENTIFIER_INVALID");
      const value = checkUpload(await request<Upload>(`/api/v1/asset-uploads/${id}`, signal));
      if (value.assetId !== id) throw new StudioError("RESPONSE_INVALID");
      return value;
    },
    async account(signal) {
      const value = await request<Account>("/auth/session", signal);
      if (
        !value ||
        typeof value.authenticated !== "boolean" ||
        typeof value.hasAccess !== "boolean" ||
        !Array.isArray(value.rights) ||
        value.rights.some((r) => typeof r !== "string") ||
        (value.authenticated &&
          (!value.user ||
            !uuid.test(value.user.id) ||
            typeof value.user.name !== "string" ||
            typeof value.csrfToken !== "string" ||
            !/^[a-zA-Z0-9_-]{43}$/.test(value.csrfToken) ||
            !Number.isFinite(Date.parse(value.expiresAt!))))
      )
        throw new StudioError("SESSION_INVALID");
      if (value.authenticated && actor && actor !== value.user!.id) {
        onSessionLost?.();
        throw new StudioError("SESSION_CHANGED");
      }
      if (value.authenticated) actor = value.user!.id;
      csrf = value.authenticated ? value.csrfToken : undefined;
      return value;
    },
    async logout(signal) {
      await api.account(signal);
      await request("/auth/logout", signal, {}, crypto.randomUUID());
      csrf = undefined;
      try {
        clearWorkAttempt();
      } catch {
        /* Logout must still revoke the server session. */
      }
    },
    async workItems(cursor, signal) {
      const config = await api.config(signal);
      const value = await listing<WorkItem>(
        "/api/v1/work-items",
        { environment: config.environment },
        cursor,
        signal,
      );
      if (
        value.items.some(
          (i) =>
            !i ||
            !uuid.test(i.id) ||
            !uuid.test(i.documentId) ||
            !uuid.test(i.ownerId) ||
            typeof i.title !== "string" ||
            typeof i.ownerName !== "string" ||
            typeof i.own !== "boolean" ||
            typeof i.path !== "string" ||
            !i.path.startsWith("/") ||
            i.path.startsWith("//") ||
            !["open", "failed", "changes_requested", "in_review", "approved"].includes(i.state),
        )
      )
        throw new StudioError("RESPONSE_INVALID");
      return value;
    },
    async workflow(session, action, comment, key, signal) {
      const commands = {
        submit: "submit-changeset",
        reopen: "reopen-changeset",
        approve: "approve-changeset",
        "request-changes": "request-changes",
        publish: "publish",
      };
      const result = await request<{ result: Partial<Publication> & { changeset?: Changeset } }>(
        `/api/v1/commands/${commands[action]}`,
        signal,
        {
          payload: {
            changesetId: session.changeset.id,
            expectedSeq: session.changeset.seq,
            ...(action === "publish" ? { environment: session.config.environment } : {}),
            ...(action === "request-changes" ? { comment } : {}),
            ...(session.diffSnapshot && ["approve", "request-changes", "publish"].includes(action)
              ? { expectedDiffSnapshot: session.diffSnapshot }
              : {}),
          },
          reason: `Studio: ${action} Change Set`,
        },
        key,
      );
      if (action !== "publish") {
        const cs = result?.result?.changeset;
        const states = {
          submit: ["failed", "in_review", "approved"],
          reopen: ["open"],
          approve: ["in_review", "approved"],
          "request-changes": ["changes_requested"],
        };
        if (
          !cs ||
          cs.id !== session.changeset.id ||
          cs.seq !== session.changeset.seq ||
          !states[action].includes(cs.state)
        )
          throw new StudioError("RESPONSE_INVALID", true);
        return null;
      }
      const p = result?.result;
      if (
        !p ||
        typeof p.id !== "string" ||
        !uuid.test(p.id) ||
        p.changesetId !== session.changeset.id ||
        p.environment !== session.config.environment ||
        typeof p.createdAt !== "string" ||
        !Number.isFinite(Date.parse(p.createdAt))
      )
        throw new StudioError("RESPONSE_INVALID", true);
      return {
        id: p.id,
        changesetId: p.changesetId,
        environment: p.environment,
        createdAt: p.createdAt,
      };
    },
    async diffIndex(session, signal) {
      const query = new URLSearchParams({ environment: session.config.environment });
      return validateDiffIndex(
        await request<DiffIndex>(
          `/api/v1/changesets/${session.changeset.id}/diff?${query}`,
          signal,
        ),
        session,
      );
    },
    async diffPage(session, index, objectId, baseline, cursor, signal) {
      const query = new URLSearchParams({
        environment: session.config.environment,
        baseline,
        snapshot: index.snapshot,
      });
      if (cursor) query.set("after", cursor);
      const result = validateDiffPage(
        await request<DiffPage>(
          `/api/v1/changesets/${session.changeset.id}/diff/${objectId}?${query}`,
          signal,
        ),
        index,
        objectId,
        baseline,
      );
      if (cursor && result.nextCursor === cursor) throw new StudioError("RESPONSE_INVALID");
      return result;
    },
    async history(session, beforeSeq, signal) {
      return readHistory(session.changeset.id, session.changeset.seq, beforeSeq, signal);
    },
    async changeHistory(session, action, key, signal) {
      await request(
        `/api/v1/commands/${action}`,
        signal,
        {
          payload: { changesetId: session.changeset.id, expectedSeq: session.changeset.seq },
          reason: `Studio: ${action} action`,
        },
        key,
      );
    },
    async pages(cursor, signal, query = "") {
      query = query.trim();
      if ([...query].length > 200 || query.includes("\0")) throw new StudioError("PARAM_INVALID");
      const config = await api.config(signal);
      const value = await listing<PageItem>(
        "/api/v1/pages",
        { environment: config.environment, ...(query ? { q: query } : {}) },
        cursor,
        signal,
      );
      if (
        value.items.some(
          (p) =>
            !p ||
            !uuid.test(p.id) ||
            typeof p.path !== "string" ||
            !p.path.startsWith("/") ||
            p.path.startsWith("//") ||
            typeof p.title !== "string" ||
            typeof p.hasHead !== "boolean",
        )
      )
        throw new StudioError("RESPONSE_INVALID");
      return value;
    },
    async drafts(documentId, cursor, signal) {
      if (!uuid.test(documentId)) throw new StudioError("IDENTIFIER_INVALID");
      const config = await api.config(signal);
      const value = await listing<Changeset>(
        "/api/v1/working-drafts",
        { environment: config.environment, documentId },
        cursor,
        signal,
      );
      if (
        value.items.some(
          (d) =>
            !d ||
            !uuid.test(d.id) ||
            typeof d.title !== "string" ||
            d.state !== "open" ||
            d.kind !== "standard" ||
            !Array.isArray(d.targets) ||
            !d.targets.includes(config.environment),
        )
      )
        throw new StudioError("RESPONSE_INVALID");
      return value;
    },
    async createDraft(title, key) {
      title = title.trim();
      if ([...title].length < 1 || [...title].length > 200) throw new StudioError("TITLE_INVALID");
      const config = await api.config();
      const response = await request<{ result: Changeset }>(
        "/api/v1/commands/create-changeset",
        undefined,
        { payload: { title, targets: [config.environment] }, reason: "Studio: create page draft" },
        key,
      );
      if (
        !uuid.test(response?.result?.id) ||
        response.result.kind !== "standard" ||
        response.result.state !== "open" ||
        !Array.isArray(response.result.targets) ||
        !response.result.targets.includes(config.environment)
      )
        throw new StudioError("RESPONSE_INVALID", true);
      return response.result;
    },
    async config(signal) {
      const value = await request<Config>("/config", signal);
      human = value.authMode === "oidc";
      return value;
    },
    async open(documentId, changesetId, signal) {
      if (!uuid.test(documentId) || !uuid.test(changesetId))
        throw new StudioError("IDENTIFIER_INVALID");
      const config = await api.config(signal);
      const [changeset, record, active, history, rawReview] = await Promise.all([
        request<Changeset>(`/api/v1/changesets/${changesetId}`, signal),
        request<DocumentRecord>(
          `/api/v1/documents/${documentId}?changesetId=${changesetId}`,
          signal,
        ),
        request<{ manifest: Session["manifest"]; manifestHash: string }>(
          `/api/v1/manifest?environment=${encodeURIComponent(config.environment)}`,
          signal,
        ),
        readHistory(changesetId, undefined, undefined, signal),
        request<Review>(`/api/v1/changesets/${changesetId}/review`, signal),
      ]);
      if (
        record?.id !== documentId ||
        changeset?.id !== changesetId ||
        changeset.kind !== "standard" ||
        !Array.isArray(changeset.targets) ||
        !changeset.targets.includes(config.environment) ||
        !Number.isSafeInteger(changeset.seq) ||
        changeset.seq < 0 ||
        !validateDocument(record.body).valid ||
        record.kind !== "page" ||
        typeof record.path !== "string" ||
        !record.path.startsWith("/") ||
        record.path.startsWith("//") ||
        !/^sha256:[a-f0-9]{64}$/.test(active?.manifestHash) ||
        active.manifest?.manifestVersion !== "1.0" ||
        !active.manifest.tokens ||
        !active.manifest.breakpoints
      )
        throw new StudioError("SESSION_INVALID");
      if (history.seq !== changeset.seq) throw new StudioError("CHANGESET_SEQ_CONFLICT");
      const review = validateReview(rawReview, changesetId);
      if (review.changeset.seq !== changeset.seq || review.changeset.state !== changeset.state)
        throw new StudioError("CHANGESET_SEQ_CONFLICT");
      // Query API reads are separate requests; reject a snapshot crossing a write.
      const after = await request<Changeset>(`/api/v1/changesets/${changesetId}`, signal);
      if (
        after.id !== changeset.id ||
        after.seq !== changeset.seq ||
        after.state !== changeset.state
      )
        throw new StudioError("CHANGESET_SEQ_CONFLICT");
      return { config, changeset, record, history, review, ...active };
    },
    async apply(session, operations, key, signal) {
      await request(
        "/api/v1/commands/apply-operations",
        signal,
        {
          payload: {
            changesetId: session.changeset.id,
            expectedSeq: session.changeset.seq,
            operations,
            group: true,
          },
          reason: "Studio: edit page draft",
        },
        key,
      );
    },
    async preview(session, signal) {
      const result = await request<{ result: { token: string; expiresAt: string } }>(
        "/api/v1/commands/create-preview-token",
        signal,
        {
          payload: { environment: session.config.environment, changesetId: session.changeset.id },
          reason: "Studio: preview page draft",
        },
        crypto.randomUUID(),
      );
      if (
        typeof result.result.token !== "string" ||
        !Number.isFinite(Date.parse(result.result.expiresAt))
      )
        throw new StudioError("PREVIEW_INVALID");
      return result.result;
    },
  };
  async function readHistory(
    id: string,
    expected?: number,
    before?: number,
    signal?: AbortSignal,
  ): Promise<History> {
    if (before !== undefined && (!Number.isSafeInteger(before) || before < 1))
      throw new StudioError("PARAM_INVALID");
    const data = await request<History>(
      `/api/v1/changesets/${id}/history${before === undefined ? "" : `?beforeSeq=${before}`}`,
      signal,
    );
    const reason = (value: unknown) =>
      value === null || (typeof value === "string" && /^[A-Z0-9_]{1,80}$/.test(value));
    if (
      !data ||
      !Number.isSafeInteger(data.seq) ||
      data.seq < 0 ||
      !Array.isArray(data.items) ||
      typeof data.canUndo !== "boolean" ||
      typeof data.canRedo !== "boolean" ||
      !reason(data.undoReason) ||
      !reason(data.redoReason) ||
      data.canUndo !== (data.undoReason === null) ||
      data.canRedo !== (data.redoReason === null) ||
      data.items.length > 20 ||
      !(
        data.nextBeforeSeq === null ||
        (Number.isSafeInteger(data.nextBeforeSeq) && data.nextBeforeSeq > 0)
      ) ||
      data.items.some(
        (item, index) =>
          !item ||
          typeof item.id !== "string" ||
          !uuid.test(item.id) ||
          typeof item.actorId !== "string" ||
          !uuid.test(item.actorId) ||
          !Number.isSafeInteger(item.firstSeq) ||
          item.firstSeq < 1 ||
          !Number.isSafeInteger(item.lastSeq) ||
          item.lastSeq < item.firstSeq ||
          item.lastSeq > data.seq ||
          (before !== undefined && item.lastSeq >= before) ||
          (index > 0 && item.lastSeq >= data.items[index - 1]!.firstSeq) ||
          typeof item.actorName !== "string" ||
          typeof item.createdAt !== "string" ||
          !Number.isFinite(Date.parse(item.createdAt)) ||
          !["edit", "undo", "redo"].includes(item.kind) ||
          !Array.isArray(item.operations) ||
          item.operations.length === 0 ||
          item.operations.some(
            (op) =>
              !op ||
              typeof op.type !== "string" ||
              !(op.target === null || (typeof op.target === "string" && uuid.test(op.target))) ||
              !["applied", "conflict", "dropped"].includes(op.status),
          ),
      )
    )
      throw new StudioError("HISTORY_INVALID");
    if (
      data.nextBeforeSeq !== null &&
      (data.items.length === 0 ||
        data.nextBeforeSeq !== data.items.at(-1)?.lastSeq ||
        (before !== undefined && data.nextBeforeSeq >= before))
    )
      throw new StudioError("HISTORY_INVALID");
    if (expected !== undefined && data.seq !== expected)
      throw new StudioError("CHANGESET_SEQ_CONFLICT");
    return data;
  }
  async function listing<T>(
    path: string,
    params: Record<string, string>,
    cursor?: string,
    signal?: AbortSignal,
  ): Promise<List<T>> {
    if (cursor !== undefined && !uuid.test(cursor)) throw new StudioError("IDENTIFIER_INVALID");
    const query = new URLSearchParams(params);
    if (cursor) query.set("after", cursor);
    const value = await request<List<T>>(`${path}?${query}`, signal);
    if (
      !Array.isArray(value?.items) ||
      !(
        value.nextCursor === null ||
        (typeof value.nextCursor === "string" && uuid.test(value.nextCursor))
      )
    )
      throw new StudioError("RESPONSE_INVALID");
    return value;
  }
  return api;
}
