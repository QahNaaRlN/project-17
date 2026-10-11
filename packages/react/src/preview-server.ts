import { CmsError, type CmsClient, type Preview } from "@cms/core";
import { previewOrigin, previewPath } from "./preview-origin.js";
export { previewFrameAncestors } from "./preview-origin.js";
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
function claims(token: string, now: number): { exp: number; cs?: string } {
  if (token.length > 8192 || !/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(token))
    throw new Error("CMS_PREVIEW_TOKEN_INVALID");
  const encoded = token.split(".")[1]!.replaceAll("-", "+").replaceAll("_", "/");
  const value = JSON.parse(atob(encoded)) as { exp?: number; cs?: string | null };
  if (
    !Number.isSafeInteger(value?.exp) ||
    value.exp! * 1000 <= now ||
    (value.cs != null && (typeof value.cs !== "string" || !uuid.test(value.cs)))
  )
    throw new Error("CMS_PREVIEW_TOKEN_INVALID");
  return { exp: value.exp!, ...(value.cs == null ? {} : { cs: value.cs }) };
}
/** Parsing only. Every Delivery API read still verifies the JWT in the CMS. */
export function getPreviewState(
  cookie: string | null | undefined,
  now = Date.now(),
): Preview | undefined {
  try {
    const values = (cookie ?? "")
      .split(";")
      .map((part) => part.trim())
      .filter((part) => part.startsWith("cms_preview="));
    if (values.length !== 1) return undefined;
    const token = decodeURIComponent(values[0]!.slice("cms_preview=".length));
    const value = claims(token, now);
    return { token, ...(value.cs ? { changesetId: value.cs } : {}) };
  } catch {
    return undefined;
  }
}
export interface PreviewHandlerOptions {
  cms: CmsClient;
  appOrigin: string;
  studioOrigin: string;
  now?: () => number;
}
/** JWT signature/scope are checked by the existing authenticated Delivery API. */
export function createPreviewHandler(
  options: PreviewHandlerOptions,
): (request: Request) => Promise<Response> {
  const appOrigin = previewOrigin(options.appOrigin),
    studioOrigin = previewOrigin(options.studioOrigin);
  return async (request) => {
    const headers = {
      "Cache-Control": "no-store",
      "Referrer-Policy": "no-referrer",
      "Content-Security-Policy": `frame-ancestors ${studioOrigin}`,
      "X-Content-Type-Options": "nosniff",
    };
    const error = (status: number, code: string) =>
      Response.json({ error: { code } }, { status, headers });
    if (request.method !== "GET")
      return new Response(null, { status: 405, headers: { ...headers, Allow: "GET" } });
    let token: string, path: string, expires: number;
    try {
      const url = new URL(request.url);
      if (
        url.origin !== appOrigin ||
        url.searchParams.getAll("token").length !== 1 ||
        url.searchParams.getAll("path").length > 1
      )
        return error(400, "CMS_PREVIEW_REQUEST_INVALID");
      token = url.searchParams.get("token")!;
      path = previewPath(url.searchParams.get("path") ?? "/");
      expires = claims(token, (options.now ?? Date.now)()).exp;
    } catch {
      return error(400, "CMS_PREVIEW_REQUEST_INVALID");
    }
    try {
      await options.cms.getRoutes({ preview: { token }, signal: request.signal });
    } catch (cause) {
      return error(
        cause instanceof CmsError && [401, 403].includes(cause.status) ? cause.status : 503,
        "CMS_PREVIEW_VERIFY_FAILED",
      );
    }
    const maxAge = Math.min(900, Math.floor(expires - (options.now ?? Date.now)() / 1000));
    if (maxAge <= 0) return error(401, "CMS_PREVIEW_TOKEN_EXPIRED");
    return new Response(null, {
      status: 303,
      headers: {
        ...headers,
        Location: path,
        "Set-Cookie": `cms_preview=${encodeURIComponent(token)}; Path=/; Max-Age=${maxAge}; HttpOnly; Secure; SameSite=None; Partitioned`,
      },
    });
  };
}
