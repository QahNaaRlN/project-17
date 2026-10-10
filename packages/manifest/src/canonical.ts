import { validUnicode } from "./unicode.js";

/**
 * Канонический JSON manifest (MF-002): ключи объектов по возрастанию кодовых единиц UTF-16,
 * без пробелов, строки и числа — как у JSON.stringify. Go-реализация даёт тот же результат.
 */
export function canonicalJson(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonicalJson).join(",")}]`;
  if (value !== null && typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>)
      .filter(([, v]) => v !== undefined)
      .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
    return `{${entries.map(([k, v]) => `${unicodeString(k)}:${canonicalJson(v)}`).join(",")}}`;
  }
  return typeof value === "string" ? unicodeString(value) : JSON.stringify(value);
}

function unicodeString(value: string): string {
  if (!validUnicode(value)) throw new TypeError("manifest содержит некорректный Unicode");
  return JSON.stringify(value);
}

/** manifestHash — "sha256:" + SHA-256 канонического JSON в hex. */
export async function manifestHash(manifest: unknown): Promise<string> {
  const bytes = new TextEncoder().encode(canonicalJson(manifest));
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return (
    "sha256:" + [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("")
  );
}
