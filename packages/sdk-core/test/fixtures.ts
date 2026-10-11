export const ID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
export const VERSION = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
export const ir = {
  irVersion: "1.0",
  kind: "page",
  root: "n_root",
  nodes: { n_root: { id: "n_root", type: "Box" } },
};
export const page = {
  page: { objectId: ID, versionId: VERSION, path: "/", params: {} },
  document: ir,
  components: {},
  data: {},
  dataSources: {},
};
export const doc = { objectId: ID, versionId: VERSION, path: null, document: ir };
export const asset = {
  id: ID,
  versionId: VERSION,
  kind: "asset",
  deleted: false,
  data: { title: "Picture" },
};
export const routes = { items: [{ objectId: ID, versionId: VERSION, path: "/" }] };
export const model = {
  assetId: ID,
  versionId: VERSION,
  width: 320,
  height: 200,
  fit: "contain",
  format: "webp",
  focalPoint: { x: 0.5, y: 0.5 },
  expiresAt: "2030-01-01T00:00:00Z",
  variants: [
    { width: 320, url: `https://images.test/assets/v1/staging/${ID}/${"a".repeat(64)}?sig=abc` },
  ],
};
export const config = {
  baseUrl: "https://cms.test",
  project: "store",
  environment: "staging",
  deliveryKey: "cms_pub_secret",
};
export function json(value: unknown, headers: Record<string, string> = {}, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });
}
