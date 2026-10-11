import { CmsError } from "./errors.js";
import * as decode from "./decode.js";
import type { ClientOptions, CmsClient, ReadOptions, ImageOptions } from "./types.js";

interface Entry {
  value: unknown;
  etag: string;
  expires: number;
}
function input(condition: unknown): asserts condition {
  if (!condition) throw new CmsError("INVALID_ARGUMENT");
}
function positive(value: number): boolean {
  return Number.isSafeInteger(value) && value > 0;
}
function id(value: string): string {
  input(decode.uuid(value));
  return value.toLowerCase();
}
function secret(value: unknown): value is string {
  return typeof value === "string" && /^[!-~]+$/.test(value);
}
function cacheable(response: Response): boolean {
  return !/(?:^|,)\s*(?:no-store|private)(?:\s|,|=|$)/i.test(
    response.headers.get("Cache-Control") ?? "",
  );
}

async function json(response: Response, limit: number): Promise<unknown> {
  const length = response.headers.get("Content-Length");
  if (length !== null && Number(length) > limit) throw new CmsError("RESPONSE_TOO_LARGE");
  const reader = response.body?.getReader();
  if (!reader) throw new CmsError("INVALID_RESPONSE");
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    while (true) {
      const chunk = await reader.read();
      if (chunk.done) break;
      size += chunk.value.byteLength;
      if (size > limit) {
        void reader.cancel().catch(() => {});
        throw new CmsError("RESPONSE_TOO_LARGE");
      }
      chunks.push(chunk.value);
    }
  } finally {
    reader.releaseLock();
  }
  const bytes = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  const mime = response.headers.get("Content-Type")?.split(";", 1)[0]?.trim().toLowerCase();
  if (mime !== "application/json" && mime !== "application/problem+json")
    throw new CmsError("INVALID_RESPONSE");
  try {
    return JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes)) as unknown;
  } catch {
    throw new CmsError("INVALID_RESPONSE");
  }
}

export function createCmsClient(options: ClientOptions): CmsClient {
  let base: URL;
  try {
    base = new URL(options.baseUrl);
  } catch {
    throw new CmsError("INVALID_ARGUMENT");
  }
  input(
    ["http:", "https:"].includes(base.protocol) &&
      !base.username &&
      !base.password &&
      !base.search &&
      !base.hash &&
      base.pathname === "/",
  );
  input(
    [options.project, options.environment].every(
      (v) => typeof v === "string" && /^[a-z0-9][a-z0-9-]{0,62}$/.test(v),
    ),
  );
  input(options.deliveryKey === undefined || secret(options.deliveryKey));
  const fetcher = options.fetch ?? globalThis.fetch;
  input(typeof fetcher === "function");
  const timeout = options.timeoutMs ?? 10_000;
  const limit = options.maxResponseBytes ?? 8 * 1024 * 1024;
  const capacity = options.cache === false ? 0 : (options.cache?.capacity ?? 500);
  const ttl = options.cache === false ? 60_000 : (options.cache?.ttlMs ?? 60_000);
  input(
    positive(timeout) &&
      timeout <= 2_147_483_647 &&
      positive(limit) &&
      Number.isSafeInteger(capacity) &&
      capacity >= 0 &&
      positive(ttl),
  );
  const now = options.now ?? Date.now;
  const project = options.project,
    environment = options.environment,
    key = options.deliveryKey;
  const prefix = `${base.origin}/delivery/v1/${project}/${environment}/`;
  const cache = new Map<string, Entry>();
  let epoch = 0;
  function store(url: string, entry: Entry) {
    cache.delete(url);
    cache.set(url, entry);
    while (cache.size > capacity) cache.delete(cache.keys().next().value!);
  }

  async function read<T>(
    endpoint: string,
    params: URLSearchParams,
    opts: ReadOptions,
    parse: (value: unknown) => T,
    nullable: boolean,
    images = false,
  ): Promise<T | null> {
    const preview = opts.preview;
    input(preview === undefined || (decode.record(preview) && secret(preview.token)));
    const token = preview?.token ?? key;
    input(secret(token));
    if (preview?.changesetId !== undefined) params.set("changesetId", id(preview.changesetId));
    if (opts.signal?.aborted) throw new CmsError("ABORTED");
    const url = prefix + endpoint + (params.size ? `?${params}` : "");
    const useCache = !preview && !images && capacity > 0;
    const cached = useCache ? cache.get(url) : undefined;
    if (cached) {
      cache.delete(url);
      cache.set(url, cached);
    }
    if (cached && cached.expires > now()) return structuredClone(cached.value) as T;
    const started = epoch;
    const controller = new AbortController();
    const abort = () => controller.abort();
    opts.signal?.addEventListener("abort", abort, { once: true });
    const timer = setTimeout(abort, timeout);
    try {
      const headers = new Headers({
        Authorization: `${preview ? "Preview" : "Bearer"} ${token}`,
        Accept: "application/json",
      });
      if (cached?.etag) headers.set("If-None-Match", cached.etag);
      const response = await fetcher(url, {
        method: "GET",
        headers,
        signal: controller.signal,
        credentials: "omit",
        redirect: "error",
        cache: "no-store",
      });
      if (controller.signal.aborted)
        throw new CmsError(opts.signal?.aborted ? "ABORTED" : "TIMEOUT");
      if (response.status === 304) {
        if (!cached) throw new CmsError("INVALID_RESPONSE", 304);
        if (started === epoch) {
          if (cacheable(response)) {
            cached.expires = now() + ttl;
            store(url, cached);
          } else cache.delete(url);
        }
        return structuredClone(cached.value) as T;
      }
      if (response.status !== 200) {
        let problem: unknown;
        try {
          problem = await json(response, limit);
        } catch {
          /* Non-JSON error responses retain only their HTTP status. */
        }
        const code =
          decode.record(problem) &&
          typeof problem.code === "string" &&
          /^[A-Z][A-Z0-9_]{0,63}$/.test(problem.code)
            ? problem.code
            : "HTTP_ERROR";
        if (nullable && response.status === 404 && code === "NOT_FOUND") {
          if (useCache && started === epoch) cache.delete(url);
          return null;
        }
        throw new CmsError(code, response.status);
      }
      const value = parse(await json(response, limit));
      if (controller.signal.aborted)
        throw new CmsError(opts.signal?.aborted ? "ABORTED" : "TIMEOUT");
      if (useCache && started === epoch) {
        cache.delete(url);
        if (cacheable(response)) {
          store(url, {
            value: structuredClone(value),
            etag: response.headers.get("ETag") ?? "",
            expires: now() + ttl,
          });
        }
      }
      return value;
    } catch (error) {
      if (useCache && started === epoch) cache.delete(url);
      if (opts.signal?.aborted) throw new CmsError("ABORTED");
      if (controller.signal.aborted) throw new CmsError("TIMEOUT");
      if (error instanceof CmsError) throw error;
      throw new CmsError("NETWORK_ERROR");
    } finally {
      clearTimeout(timer);
      opts.signal?.removeEventListener("abort", abort);
      controller.abort();
    }
  }
  const empty = () => new URLSearchParams();
  return {
    getPage(path, opts = {}) {
      input(
        typeof path === "string" &&
          path.startsWith("/") &&
          new TextEncoder().encode(path).length <= 2048,
      );
      return read("page", new URLSearchParams({ path }), opts, decode.page, true);
    },
    getDocument(value, opts = {}) {
      return read(`document/${id(value)}`, empty(), opts, decode.document, true);
    },
    getEntity(value, opts = {}) {
      return read(`entity/${id(value)}`, empty(), opts, (v) => decode.content(v, "entity"), true);
    },
    getAsset(value, opts = {}) {
      return read(`asset/${id(value)}`, empty(), opts, (v) => decode.content(v, "asset"), true);
    },
    async getRoutes(opts = {}) {
      return (await read("routes", empty(), opts, decode.routes, false))!;
    },
    getImage(value, opts: ImageOptions = {}) {
      const params = empty();
      input(opts.fmt === undefined || decode.formats.includes(opts.fmt));
      input(opts.fit === undefined || ["contain", "cover"].includes(opts.fit));
      input(
        opts.w === undefined || decode.widths.includes(opts.w as (typeof decode.widths)[number]),
      );
      const fit = opts.fit ?? "contain";
      input(
        fit === "cover"
          ? opts.h !== undefined && positive(opts.h) && opts.h <= 2560
          : opts.h === undefined || opts.h === 0,
      );
      for (const name of ["fmt", "fit", "w", "h"] as const) {
        if (opts[name] !== undefined) params.set(name, String(opts[name]));
      }
      return read(
        `asset/${id(value)}/image`,
        params,
        opts,
        (v) => decode.image(v, now()),
        true,
        true,
      );
    },
    clearCache() {
      epoch++;
      cache.clear();
    },
  };
}
