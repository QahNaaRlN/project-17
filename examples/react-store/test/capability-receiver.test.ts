import { afterEach, expect, it } from "vitest";
import { createHmac } from "node:crypto";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
// @ts-expect-error Node fixture deliberately has no app/runtime dependency.
import { createReceiver, verifyDelivery, bodyHash } from "../../../scripts/capability-receiver.mjs";
const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
});
const project = "36b984a7-95a0-450a-b887-3cde9d722742",
  target = "36b984a7-95a0-450a-b887-3cde9d722743",
  id = "36b984a7-95a0-450a-b887-3cde9d722744",
  key = Buffer.alloc(32, 107);
const config = {
  bindings: [
    {
      projectId: project,
      environment: "staging",
      targetId: target,
      keys: { key1: key.toString("base64") },
    },
  ],
};
function signed(body: Buffer, timestamp = String(Math.floor(Date.now() / 1000))) {
  const hash = bodyHash(body);
  return {
    "Content-Type": "application/json",
    "x-cms-delivery-id": id,
    "x-cms-key-id": "key1",
    "x-cms-timestamp": timestamp,
    "x-cms-body-hash": hash,
    "x-cms-signature":
      "v1=" +
      createHmac("sha256", key).update(`v1\nkey1\n${timestamp}\n${id}\n${hash}\n`).digest("hex"),
  };
}
const envelope = () =>
  Buffer.from(
    JSON.stringify({
      version: "1.0",
      event: "capability.context.delivery.v1",
      deliveryId: id,
      projectId: project,
      environment: "staging",
      targetId: target,
      contextPack: { safe: true },
    }),
  );
it("shares a fixed HMAC vector with Go and rejects altered scopes, timestamps, body or signature", () => {
  const body = Buffer.from('{"event":"golden"}'),
    hash = bodyHash(body);
  expect(hash).toBe("sha256:1000135f9acd10ca8b475df42a6f35753314700f512e8116908865e6a56c9e72");
  expect(
    "v1=" +
      createHmac("sha256", key).update(`v1\nkey1\n1792233600\n${project}\n${hash}\n`).digest("hex"),
  ).toBe("v1=e00af5db60f28e8753da205e97be3a63ff53729cee18afebd50ef2aa6a6829d8");
  const value = envelope(),
    headers = signed(value);
  expect(verifyDelivery(headers, value, config)?.deliveryId).toBe(id);
  for (const changed of [
    { ...headers, "x-cms-signature": "v1=bad" },
    { ...headers, "x-cms-key-id": "other" },
    { ...headers, "x-cms-delivery-id": project },
    { ...headers, "x-cms-timestamp": "0" },
  ])
    expect(verifyDelivery(changed, value, config)).toBeNull();
  expect(verifyDelivery(headers, Buffer.from("changed"), config)).toBeNull();
  expect(verifyDelivery(headers, value, { bindings: [] })).toBeNull();
});
it("commits before a lost receipt, deduplicates concurrent retries and survives receiver restart", async () => {
  const directory = await mkdtemp(join(tmpdir(), "capability-inbox-")),
    databasePath = join(directory, "inbox.sqlite");
  cleanups.push(() => rm(directory, { recursive: true, force: true }));
  let mode = "drop-first";
  let receiver = createReceiver({ databasePath, readConfig: () => config, readMode: () => mode });
  const start = async () => {
    await new Promise<void>((r) => receiver.server.listen(0, "127.0.0.1", r));
    return `http://127.0.0.1:${receiver.server.address().port}`;
  };
  let origin = await start();
  const body = envelope();
  await expect(
    fetch(origin + "/context", { method: "POST", headers: signed(body), body }),
  ).rejects.toThrow();
  expect(receiver.db.prepare("SELECT count(*) AS count FROM inbox").get().count).toBe(1);
  await new Promise<void>((r) => receiver.server.close(r));
  receiver = createReceiver({ databasePath, readConfig: () => config, readMode: () => mode });
  origin = await start();
  cleanups.push(() => new Promise<void>((r) => receiver.server.close(r)));
  mode = "normal";
  const replies = await Promise.all(
    Array.from({ length: 3 }, () =>
      fetch(origin + "/context", { method: "POST", headers: signed(body), body }).then((r) =>
        r.json(),
      ),
    ),
  );
  for (const reply of replies)
    expect(reply).toEqual({ deliveryId: id, bodyHash: bodyHash(body), status: "stored" });
  expect(receiver.db.prepare("SELECT calls FROM inbox").get().calls).toBe(4);
  expect(receiver.db.prepare("SELECT count(*) AS count FROM inbox").get().count).toBe(1);
  const changed = Buffer.from(
    JSON.stringify({ ...JSON.parse(body.toString()), contextPack: { other: true } }),
  );
  expect(
    (await fetch(origin + "/context", { method: "POST", headers: signed(changed), body: changed }))
      .status,
  ).toBe(409);
});
