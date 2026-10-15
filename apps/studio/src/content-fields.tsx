import { useState, useEffect } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import type { Type } from "@cms/manifest";
import type { Api, WorkContext } from "./types.js";
import { RichEditor } from "./rich-editor.js";
export function JsonValue({
  value,
  label,
  onChange,
  onInvalid,
}: {
  value: unknown;
  label: string;
  onChange: (v: unknown) => void;
  onInvalid: (v: boolean) => void;
}) {
  const [raw, setRaw] = useState(() => JSON.stringify(value ?? null, null, 2));
  useEffect(() => {
    if (value === undefined) setRaw("null");
  }, [value === undefined]);
  return (
    <textarea
      aria-label={label}
      value={raw}
      rows={5}
      spellCheck={false}
      onChange={(e) => {
        setRaw(e.target.value);
        try {
          onChange(JSON.parse(e.target.value));
          onInvalid(false);
        } catch {
          onInvalid(true);
        }
      }}
    />
  );
}
export function ReferencePicker({
  api,
  session,
  schema,
  value,
  label,
  onChange,
  ru,
}: {
  api: Api;
  session: WorkContext;
  schema: Extract<Type, { type: "reference" | "asset" }>;
  value: unknown;
  label: string;
  onChange: (v: unknown) => void;
  ru: boolean;
}) {
  const kind = schema.type === "reference" ? "entity" : "asset",
    filter = schema.type === "reference" ? schema.schema : "";
  const list = useInfiniteQuery({
    queryKey: ["content-picker", session.changeset.id, session.changeset.seq, kind, filter],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      api.contentCatalog(session, kind, filter, pageParam, signal),
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    retry: false,
  });
  const items =
    list.data?.pages
      .flatMap((page) => page.items)
      .filter(
        (i) =>
          !i.deleted &&
          (schema.type !== "asset" ||
            ((!schema.assetKind || schema.assetKind === i.assetKind) &&
              (!schema.mimeTypes || schema.mimeTypes.includes(i.mimeType!)))),
      ) ?? [];
  const key = kind === "entity" ? "entityId" : "assetId",
    id = String((value as Record<string, unknown> | undefined)?.[key] ?? "");
  return (
    <div>
      <select
        aria-label={label}
        value={id}
        onChange={(e) =>
          onChange(
            e.target.value
              ? { ...(typeof value === "object" && value ? value : {}), [key]: e.target.value }
              : undefined,
          )
        }
      >
        <option value="">—</option>
        {id && !items.some((i) => i.id === id) && (
          <option value={id}>
            {ru ? "Текущая ссылка" : "Current reference"} · {id}
          </option>
        )}
        {items.map((i) => (
          <option key={i.id} value={i.id}>
            {i.title || i.schema || i.assetKind || kind} · {i.id.slice(-8)}
          </option>
        ))}
      </select>
      {list.isPending && <small>{ru ? "Загрузка…" : "Loading…"}</small>}
      {list.isError && (
        <button type="button" onClick={() => void list.refetch()}>
          {ru ? "Повторить каталог" : "Retry catalog"}
        </button>
      )}
      {list.hasNextPage && (
        <button type="button" onClick={() => void list.fetchNextPage()} disabled={list.isFetching}>
          {ru ? "Ещё варианты" : "More choices"}
        </button>
      )}
    </div>
  );
}
export function ContentValue({
  schema,
  value,
  label,
  onChange,
  onInvalid,
  api,
  session,
  ru,
}: {
  schema: Type;
  value: unknown;
  label: string;
  onChange: (v: unknown) => void;
  onInvalid: (bad: boolean) => void;
  api: Api;
  session: WorkContext;
  ru: boolean;
}) {
  if (schema.type === "richText")
    return <RichEditor {...{ schema, value, label, onChange, onInvalid, api, session, ru }} />;
  if (schema.type === "reference" || schema.type === "asset")
    return <ReferencePicker {...{ schema, value, label, onChange, api, session, ru }} />;
  if (schema.type === "boolean" || schema.type === "enum")
    return (
      <select
        aria-label={label}
        value={value === undefined ? "" : String(value)}
        onChange={(e) =>
          onChange(
            e.target.value === ""
              ? undefined
              : schema.type === "boolean"
                ? e.target.value === "true"
                : e.target.value,
          )
        }
      >
        <option value="">—</option>
        {(schema.type === "boolean" ? ["true", "false"] : schema.values).map((v) => (
          <option key={v}>{v}</option>
        ))}
      </select>
    );
  if (["list", "object", "link"].includes(schema.type))
    return <JsonValue {...{ value, label, onChange, onInvalid }} />;
  if (schema.type === "text")
    return (
      <textarea
        aria-label={label}
        value={value == null ? "" : String(value)}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  return (
    <input
      aria-label={label}
      type={schema.type === "number" ? "number" : "text"}
      step="any"
      value={value == null ? "" : String(value)}
      onChange={(e) =>
        onChange(
          schema.type === "number"
            ? e.target.value === ""
              ? undefined
              : Number(e.target.value)
            : e.target.value,
        )
      }
    />
  );
}
