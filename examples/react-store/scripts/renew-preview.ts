import { readFile, writeFile } from "node:fs/promises";
import { randomUUID } from "node:crypto";
const configFile = process.env.CMS_STORE_CONFIG_FILE;
const tokenFile = process.env.CMS_SERVICE_TOKEN_FILE;
if (!configFile || !tokenFile)
  throw new Error("CMS_STORE_CONFIG_FILE and CMS_SERVICE_TOKEN_FILE are required");
const config = JSON.parse(await readFile(configFile, "utf8")) as {
  baseUrl: string;
  project: string;
  environment: string;
  preview: { token: string; changesetId: string };
};
if (
  !["127.0.0.1", "localhost", "[::1]"].includes(new URL(config.baseUrl).hostname) ||
  !config.preview?.changesetId
)
  throw new Error("Renewal is limited to an existing local draft configuration");
const token = (await readFile(tokenFile, "utf8")).match(/cms_svc_[A-Za-z0-9_-]+/)?.[0];
if (!token) throw new Error("Service token missing");
const response = await fetch(`${config.baseUrl}/api/v1/commands/create-preview-token`, {
  method: "POST",
  headers: {
    Authorization: `Bearer ${token}`,
    "X-CMS-Project": config.project,
    "Content-Type": "application/json",
    "Idempotency-Key": randomUUID(),
  },
  body: JSON.stringify({
    payload: { environment: config.environment, changesetId: config.preview.changesetId },
    reason: "Renew local storefront preview",
  }),
});
if (!response.ok) throw new Error(`Preview renewal failed: ${response.status}`);
const result = (await response.json()) as { result: { token: string } };
config.preview.token = result.result.token;
await writeFile(configFile, JSON.stringify(config, null, 2), { mode: 0o600 });
console.log("Preview renewed; restart the storefront to load it. No content changed or published.");
