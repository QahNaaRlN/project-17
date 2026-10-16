import type { Fields, Type } from "@cms/manifest";
import type { DataSourceUse } from "@cms/ir";
import type { BindingScope } from "@cms/core";
import { operands } from "./actions.js";
import { fieldValue, fieldValues, jsonValue, object, type ReadonlyContract } from "./values.js";

export interface PageItems<Item = unknown> {
  items: Item[];
  hasMore: boolean;
  cursor?: string;
}
export interface LoaderContext<App = unknown> {
  app: App;
  scope: BindingScope;
  signal: AbortSignal;
  cursor?: string;
}
export interface RuntimeSource {
  name: string;
  params: ReadonlyContract<Fields>;
  result: ReadonlyContract<Type>;
  paginated?: boolean;
  loader: (params: never, context: never) => unknown;
}
export interface SourceOptions {
  app?: unknown;
  signal?: AbortSignal;
  timeoutMs?: number;
  cursor?: string;
}
export interface SourceError {
  error: { code: string };
}

export function sourceResult(source: RuntimeSource, input: unknown): unknown {
  const result = jsonValue(input);
  if (!source.paginated) return fieldValue(source.result, result);
  if (
    source.result.type !== "list" ||
    !object(result) ||
    typeof result.hasMore !== "boolean" ||
    !Array.isArray(result.items) ||
    Object.keys(result).some((key) => !["items", "hasMore", "cursor"].includes(key)) ||
    (result.cursor !== undefined &&
      (typeof result.cursor !== "string" ||
        !result.cursor.length ||
        result.cursor.length > 2048)) ||
    (result.hasMore && !result.cursor)
  )
    throw new Error("CMS_SOURCE_RESULT_INVALID");
  return { ...result, items: fieldValue(source.result, result.items) };
}
/** Validation and timeout apply equally to initial and subsequent requests. */
export async function loadSource(
  use: DataSourceUse,
  sources: readonly RuntimeSource[],
  scope: BindingScope,
  options: SourceOptions = {},
): Promise<unknown | SourceError> {
  const controller = new AbortController();
  let timer: ReturnType<typeof setTimeout> | undefined;
  let cancel: (() => void) | undefined;
  try {
    const source = sources.find((item) => item.name === use.source);
    if (!source) throw new Error("CMS_SOURCE_UNKNOWN");
    const timeout = options.timeoutMs ?? 3000;
    if (!Number.isInteger(timeout) || timeout < 1 || timeout > 300_000)
      throw new Error("CMS_SOURCE_TIMEOUT_INVALID");
    if (
      options.cursor !== undefined &&
      (!source.paginated || !options.cursor.length || options.cursor.length > 2048)
    )
      throw new Error("CMS_SOURCE_CURSOR_INVALID");
    if (options.signal?.aborted) throw new Error("CMS_SOURCE_ABORTED");
    const params = fieldValues(source.params, operands(use.params ?? {}, scope, true));
    const canceled = new Promise<never>((_resolve, reject) => {
      cancel = () => {
        controller.abort();
        reject(new Error("CMS_SOURCE_ABORTED"));
      };
      options.signal?.addEventListener("abort", cancel, { once: true });
      timer = setTimeout(() => {
        controller.abort();
        reject(new Error("CMS_SOURCE_TIMEOUT"));
      }, timeout);
    });
    const value = await Promise.race([
      Promise.resolve().then(() =>
        source.loader(
          params as never,
          {
            app: options.app,
            scope,
            signal: controller.signal,
            ...(options.cursor === undefined ? {} : { cursor: options.cursor }),
          } as never,
        ),
      ),
      canceled,
    ]);
    try {
      return sourceResult(source, value);
    } catch {
      throw new Error("CMS_SOURCE_RESULT_INVALID");
    }
  } catch (error) {
    const codes = [
      "CMS_SOURCE_UNKNOWN",
      "CMS_SOURCE_TIMEOUT",
      "CMS_SOURCE_ABORTED",
      "CMS_SOURCE_TIMEOUT_INVALID",
      "CMS_SOURCE_CURSOR_INVALID",
      "CMS_SOURCE_PARAMS_INVALID",
      "CMS_SOURCE_RESULT_INVALID",
    ];
    const code =
      error instanceof Error && codes.includes(error.message)
        ? error.message
        : error instanceof Error && error.message === "CMS_VALUE_INVALID"
          ? "CMS_SOURCE_PARAMS_INVALID"
          : "CMS_SOURCE_FAILED";
    return { error: { code } };
  } finally {
    if (timer !== undefined) clearTimeout(timer);
    if (cancel) options.signal?.removeEventListener("abort", cancel);
  }
}
export async function loadSources(
  uses: Record<string, DataSourceUse>,
  sources: readonly RuntimeSource[],
  scope: BindingScope,
  options: SourceOptions = {},
): Promise<Record<string, unknown>> {
  const entries = Object.entries(uses);
  if (entries.length > 64) return { error: { code: "CMS_SOURCE_LIMIT" } };
  return Object.fromEntries(
    await Promise.all(
      entries.map(async ([key, use]) => [
        key,
        ["content", "constructor", "prototype", "__proto__"].includes(key)
          ? { error: { code: "CMS_SOURCE_KEY_INVALID" } }
          : await loadSource(use, sources, scope, options),
      ]),
    ),
  );
}
export function appendPage(source: RuntimeSource, current: PageItems, next: unknown): PageItems {
  if (!source.paginated) throw new Error("CMS_SOURCE_CURSOR_INVALID");
  const previous = sourceResult(source, current) as PageItems;
  const page = sourceResult(source, next) as PageItems;
  if (
    !previous.hasMore ||
    (page.hasMore && previous.cursor === page.cursor) ||
    previous.items.length + page.items.length > 2000
  )
    throw new Error("CMS_SOURCE_CURSOR_INVALID");
  return { ...page, items: [...previous.items, ...page.items] };
}
