export function previewOrigin(input: string): string {
  const url = new URL(input);
  if (
    url.origin === "null" ||
    url.username ||
    url.password ||
    url.pathname !== "/" ||
    url.search ||
    url.hash ||
    (url.protocol !== "https:" &&
      !(url.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname)))
  )
    throw new Error("CMS_PREVIEW_ORIGIN_INVALID");
  return url.origin;
}
export function previewPath(input: unknown): string {
  const unsafe = (value: string) =>
    Array.from(value).some(
      (char) => char === "\\" || char.charCodeAt(0) <= 32 || char.charCodeAt(0) === 127,
    );
  if (
    typeof input !== "string" ||
    input.length > 2048 ||
    !/^\/(?!\/)/.test(input) ||
    unsafe(input) ||
    input.includes("#")
  )
    throw new Error("CMS_PREVIEW_PATH_INVALID");
  // A percent-encoded slash/backslash at the start must not become an external redirect.
  const decoded = decodeURIComponent(input);
  if (!/^\/(?!\/)/.test(decoded) || unsafe(decoded)) throw new Error("CMS_PREVIEW_PATH_INVALID");
  return input;
}
export function previewFrameAncestors(studioOrigin: string | undefined, enabled: boolean): string {
  return enabled && studioOrigin ? previewOrigin(studioOrigin) : "'none'";
}
