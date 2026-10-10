// MF-002: JSON.stringify сохраняет одиночные суррогаты, Go JSON decoder теряет их.
// Проверяем до валидации структуры и до канонизации, не заменяя исходные символы.
export function validUnicode(value: string): boolean {
  return !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/.test(value);
}

export function hasInvalidUnicode(value: unknown): boolean {
  if (typeof value === "string") return !validUnicode(value);
  if (Array.isArray(value)) return value.some(hasInvalidUnicode);
  if (value !== null && typeof value === "object") {
    return Object.entries(value).some(
      ([key, item]) => !validUnicode(key) || hasInvalidUnicode(item),
    );
  }
  return false;
}
