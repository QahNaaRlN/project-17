const NODE_ID_PATTERN = /^[A-Za-z0-9_-]{4,32}$/;
const ALPHABET = "abcdefghijklmnopqrstuvwxyz0123456789";
const RANDOM_LENGTH = 8;

export function isNodeId(value: unknown): value is string {
  return typeof value === "string" && NODE_ID_PATTERN.test(value);
}

/**
 * Новый ID узла: `n_` + 8 случайных символов [a-z0-9] (02-ir.md §3).
 * Повторяет генерацию, пока ID встречается в `taken`.
 */
export function generateNodeId(taken: ReadonlySet<string> = new Set()): string {
  for (;;) {
    const bytes = new Uint8Array(RANDOM_LENGTH);
    globalThis.crypto.getRandomValues(bytes);
    let id = "n_";
    // 256 не делится на 36 — отбрасываем байты ≥ 252, чтобы распределение было равномерным.
    for (const byte of bytes) {
      if (byte >= 252) continue;
      id += ALPHABET[byte % ALPHABET.length];
    }
    if (id.length === 2 + RANDOM_LENGTH && !taken.has(id)) return id;
  }
}
