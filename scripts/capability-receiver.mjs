// Isolated reference receiver. Its SQLite inbox is independent of CMS/River.
// This is a delivery acceptance fixture, not a code agent or PR integration.
import { createServer } from "node:http";
import { createHash, createHmac, timingSafeEqual } from "node:crypto";
import { DatabaseSync } from "node:sqlite";
import { readFileSync } from "node:fs";
import { setTimeout as delay } from "node:timers/promises";
import { pathToFileURL } from "node:url";
const { Buffer, Date } = globalThis;

export const bodyHash = (body) => "sha256:" + createHash("sha256").update(body).digest("hex");
export function verifyDelivery(headers, body, config, now = Date.now()) {
  try {
    const value = JSON.parse(body.toString("utf8"));
    const id = headers["x-cms-delivery-id"],
      keyId = headers["x-cms-key-id"],
      timestamp = headers["x-cms-timestamp"],
      hash = headers["x-cms-body-hash"],
      signature = headers["x-cms-signature"];
    if (
      ![id, keyId, timestamp, hash, signature].every((v) => typeof v === "string") ||
      !/^[0-9]+$/.test(timestamp) ||
      Math.abs(Number(timestamp) - Math.floor(now / 1000)) > 300 ||
      hash !== bodyHash(body) ||
      value.event !== "capability.context.delivery.v1" ||
      value.version !== "1.0" ||
      value.deliveryId !== id
    )
      return null;
    const binding = config.bindings.find(
      (b) =>
        b.projectId === value.projectId &&
        b.environment === value.environment &&
        b.targetId === value.targetId,
    );
    const encoded = binding?.keys?.[keyId];
    if (!encoded) return null;
    const key = Buffer.from(encoded, "base64");
    if (key.length < 32 || key.length > 128) return null;
    const expected =
      "v1=" +
      createHmac("sha256", key)
        .update(`v1\n${keyId}\n${timestamp}\n${id}\n${hash}\n`)
        .digest("hex");
    const actualBytes = Buffer.from(signature),
      expectedBytes = Buffer.from(expected);
    if (actualBytes.length !== expectedBytes.length || !timingSafeEqual(actualBytes, expectedBytes))
      return null;
    return value;
  } catch {
    return null;
  }
}

export function createReceiver({ databasePath, readConfig, readMode = () => "normal" }) {
  const db = new DatabaseSync(databasePath);
  db.exec(
    "PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; CREATE TABLE IF NOT EXISTS inbox(project_id TEXT NOT NULL,delivery_id TEXT NOT NULL,body_hash TEXT NOT NULL,body BLOB NOT NULL,calls INTEGER NOT NULL,stored_at TEXT NOT NULL,PRIMARY KEY(project_id,delivery_id));",
  );
  const server = createServer(async (req, res) => {
    const json = (status, value) => {
      res.writeHead(status, { "Content-Type": "application/json" });
      res.end(JSON.stringify(value));
    };
    if (req.method === "GET" && req.url === "/healthz") return json(200, { ready: true });
    if (req.method !== "POST" || req.url !== "/context") return json(404, { error: "not_found" });
    try {
      const chunks = [];
      let size = 0;
      for await (const chunk of req) {
        size += chunk.length;
        if (size > 640 * 1024) return json(413, { error: "too_large" });
        chunks.push(chunk);
      }
      const body = Buffer.concat(chunks),
        config = readConfig(),
        value = verifyDelivery(req.headers, body, config);
      if (!value) return json(401, { error: "signature_or_scope" });
      // Repeated headers are rejected before the signature check, including a
      // duplicate that Node would otherwise join into a comma-separated value.
      const names = req.rawHeaders.filter((_, i) => i % 2 === 0).map((h) => h.toLowerCase());
      if (
        [
          "x-cms-delivery-id",
          "x-cms-key-id",
          "x-cms-timestamp",
          "x-cms-body-hash",
          "x-cms-signature",
        ].some((h) => names.filter((n) => n === h).length !== 1)
      )
        return json(401, { error: "duplicate_header" });
      const hash = bodyHash(body);
      let calls;
      db.exec("BEGIN IMMEDIATE");
      try {
        const existing = db
          .prepare("SELECT body_hash,calls FROM inbox WHERE project_id=? AND delivery_id=?")
          .get(value.projectId, value.deliveryId);
        if (existing && existing.body_hash !== hash) {
          db.exec("ROLLBACK");
          return json(409, { error: "identity_body_conflict" });
        }
        if (existing) {
          calls = existing.calls + 1;
          db.prepare("UPDATE inbox SET calls=? WHERE project_id=? AND delivery_id=?").run(
            calls,
            value.projectId,
            value.deliveryId,
          );
        } else {
          calls = 1;
          db.prepare("INSERT INTO inbox VALUES(?,?,?,?,?,?)").run(
            value.projectId,
            value.deliveryId,
            hash,
            body,
            calls,
            new Date().toISOString(),
          );
        }
        db.exec("COMMIT");
      } catch {
        db.exec("ROLLBACK");
        return json(503, { error: "storage_unavailable" });
      }
      const mode = readMode();
      if (mode === "drop-first" && calls === 1) {
        req.socket.destroy();
        return;
      }
      if (mode === "hold") {
        const deadline = Date.now() + 8000;
        while (readMode() === "hold" && Date.now() < deadline) await delay(100);
      }
      return json(200, { deliveryId: value.deliveryId, bodyHash: hash, status: "stored" });
    } catch {
      return json(400, { error: "invalid_request" });
    }
  });
  server.on("close", () => db.close());
  return { server, db };
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const path = process.env.CAPABILITY_RECEIVER_CONFIG_FILE,
    databasePath = process.env.CAPABILITY_RECEIVER_DATABASE,
    modePath = process.env.CAPABILITY_RECEIVER_MODE_FILE,
    port = Number(process.env.CAPABILITY_RECEIVER_PORT);
  if (!path || !databasePath || !Number.isInteger(port) || port < 1024 || port > 65535)
    throw Error("Receiver private config, database and loopback port required");
  const { server } = createReceiver({
    databasePath,
    readConfig: () => JSON.parse(readFileSync(path, "utf8")),
    readMode: () => (modePath ? readFileSync(modePath, "utf8").trim() : "normal"),
  });
  server.listen(port, "127.0.0.1");
  process.on("SIGTERM", () => server.close());
  process.on("SIGINT", () => server.close());
}
