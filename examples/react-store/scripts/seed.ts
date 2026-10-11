import { readFile, writeFile } from "node:fs/promises";
import { createHash, randomUUID } from "node:crypto";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { catalog, storefrontContract } from "./catalog.js";

const baseUrl = process.env.CMS_BASE_URL ?? "http://127.0.0.1:18080";
if (!["127.0.0.1", "localhost", "[::1]"].includes(new URL(baseUrl).hostname))
  throw new Error("Seeding is limited to a local CMS");
const project = process.env.CMS_PROJECT ?? "local-demo";
const environment = process.env.CMS_ENV ?? "staging";
const tokenFile = process.env.CMS_SERVICE_TOKEN_FILE;
const configFile = process.env.CMS_STORE_CONFIG_FILE;
if (!tokenFile || !configFile)
  throw new Error("CMS_SERVICE_TOKEN_FILE and CMS_STORE_CONFIG_FILE are required");
const token = (await readFile(tokenFile, "utf8")).match(/cms_svc_[A-Za-z0-9_-]+/)?.[0];
if (!token) throw new Error("Service token missing");
const headers = { Authorization: `Bearer ${token}`, "X-CMS-Project": project };
async function command(name: string, payload: unknown): Promise<Record<string, unknown>> {
  const response = await fetch(`${baseUrl}/api/v1/commands/${name}`, {
    method: "POST",
    headers: { ...headers, "Idempotency-Key": randomUUID(), "Content-Type": "application/json" },
    body: JSON.stringify({ payload, reason: "Local React storefront demonstration" }),
  });
  if (!response.ok) {
    const body = (await response.json()) as { code?: string; params?: unknown };
    throw new Error(
      `Command ${name}: ${response.status} ${body.code ?? ""} ${JSON.stringify(body.params ?? {})}`,
    );
  }
  return ((await response.json()) as { result: Record<string, unknown> }).result;
}
const manifestResponse = await fetch(`${baseUrl}/api/v1/manifest?environment=${environment}`, {
  headers,
});
let manifest: Record<string, unknown>;
if (manifestResponse.ok) {
  const result = (await manifestResponse.json()) as Record<string, unknown>;
  manifest = (result.manifest ?? result) as Record<string, unknown>;
} else if (manifestResponse.status === 409)
  manifest = {
    manifestVersion: "1.0",
    app: { id: "store-example", version: "1.0.0", framework: "react" },
    irVersions: ["1.0"],
    breakpoints: { sm: 640, md: 768, lg: 1024 },
    tokens: {},
  };
else throw new Error(`Manifest read failed: ${manifestResponse.status}`);
manifest.components = {
  ...((manifest.components as Record<string, unknown>) ?? {}),
  Storefront: storefrontContract,
};
const registration = await command("register-manifest", { environment, manifest });
if (registration.activation !== "active")
  throw new Error("Unexpected schema candidate; no publication performed");
const cs = await command("create-changeset", {
  title: "Тихая форма — React витрина",
  targets: [environment],
});
let seq = 0;
const value = structuredClone(catalog);
for (const key of ["hero", "vase", "lamp", "bowl"]) {
  const bytes = await readFile(fileURLToPath(new URL(`../assets/${key}.png`, import.meta.url)));
  const upload = await command("create-asset-upload", {
    filename: `store-${key}.png`,
    mimeType: "image/png",
    size: bytes.length,
    sha256: createHash("sha256").update(bytes).digest("hex"),
  });
  const put = await fetch(String(upload.uploadUrl), {
    method: "PUT",
    headers: { "Content-Type": "image/png" },
    body: bytes,
  });
  if (!put.ok) throw new Error(`Upload failed: ${put.status}`);
  await command("complete-asset-upload", { assetId: upload.assetId });
  const deadline = Date.now() + 45_000;
  let ready = false;
  while (Date.now() < deadline) {
    const response = await fetch(`${baseUrl}/api/v1/asset-uploads/${upload.assetId}`, { headers });
    if (!response.ok) throw new Error(`Upload status failed: ${response.status}`);
    const status = (await response.json()) as { status: string };
    if (status.status === "ready") {
      ready = true;
      break;
    }
    if (status.status === "failed") throw new Error("Asset processing failed");
    await new Promise((resolve) => setTimeout(resolve, 300));
  }
  if (!ready) throw new Error("Asset processing timeout");
  const applied = await command("apply-operations", {
    changesetId: cs.id,
    expectedSeq: seq,
    operations: [{ type: "asset.create", payload: { uploadId: upload.assetId } }],
  });
  seq = Number((applied.changeset as { seq: number }).seq);
  const id = String((applied.operations as { target: string }[])[0]!.target);
  if (key === "hero") value.heroAsset = { assetId: id };
  else value.products.find((p) => p.id === key)!.asset = { assetId: id };
}
const path = "/store";
const applied = await command("apply-operations", {
  changesetId: cs.id,
  expectedSeq: seq,
  operations: [
    {
      type: "document.create",
      payload: {
        kind: "page",
        path,
        root: { id: "n_store", type: "Storefront", props: { config: { base: value } } },
        meta: { title: "Тихая форма" },
      },
    },
  ],
});
const preview = await command("create-preview-token", { environment, changesetId: cs.id });
await writeFile(
  resolve(configFile),
  JSON.stringify(
    { baseUrl, project, environment, path, preview: { token: preview.token, changesetId: cs.id } },
    null,
    2,
  ),
  { mode: 0o600 },
);
const report = {
  changesetId: cs.id,
  seq: (applied.changeset as { seq: number }).seq,
  pageId: (applied.operations as { target: string }[])[0]!.target,
  path,
  assetIds: [value.heroAsset!.assetId, ...value.products.map((p) => p.asset!.assetId)],
};
await writeFile(resolve(configFile) + ".report.json", JSON.stringify(report, null, 2));
console.log(
  "Created draft storefront and four processed assets. Protected server configuration saved; nothing published.",
);
