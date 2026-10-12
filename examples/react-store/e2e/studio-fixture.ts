import type { IncomingMessage, ServerResponse } from "node:http";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { createStudioHandler } from "../../../apps/studio/src/handler.js";
import type { PageResponse } from "@cms/core";
type IrDocument = PageResponse["document"];
import { catalog } from "../scripts/catalog.js";
import { storeDocument } from "../src/composition.js";
export async function studioFixture(id: string, previewToken: string) {
  const previewTokens = new Set([previewToken]);
  const credential = "cms_svc_e2e_local_studio_actor";
  const manifest = JSON.parse(
    await readFile(new URL("../.cms/manifest.json", import.meta.url), "utf8"),
  );
  const studio = createStudioHandler(
    {
      baseUrl: "http://127.0.0.1:18084",
      studioOrigin: "http://127.0.0.1:18084",
      appOrigin: "http://127.0.0.1:18083",
      project: "local-demo",
      environment: "staging",
      credential,
    },
    fileURLToPath(new URL("../../../apps/studio/dist/client", import.meta.url)),
  );
  let active = false,
    document: IrDocument,
    seq = 1,
    fault = "",
    state = "open";
  const draftTitles = new Map<string, string>(),
    createKeys = new Map<string, string>();
  const createdId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c99";
  function reset(enabled: boolean) {
    active = enabled;
    seq = 1;
    fault = "";
    state = "open";
    draftTitles.clear();
    createKeys.clear();
    draftTitles.set(id, "Studio browser draft");
    const config = structuredClone(catalog);
    config.heroAsset = { assetId: id };
    config.products.forEach((p) => {
      p.asset = { assetId: id };
    });
    delete config.products[0]!.priceRub;
    document = storeDocument(config);
    document.nodes.n_store!.children!.push("n_title");
    document.nodes.n_title = {
      id: "n_title",
      type: "Heading",
      props: { level: 2 },
      bindings: { text: "$local.t_title" },
    };
    document.localContent = {
      t_title: { type: "text", value: { ru: "Заголовок черновика", en: "Draft heading" } },
    };
  }
  reset(false);
  const changeset = (draftId = id) => ({
    id: draftId,
    title: draftTitles.get(draftId),
    state,
    kind: "standard",
    seq,
    targets: ["staging"],
  });
  return {
    acceptsPreview: (authorization: string | undefined) =>
      typeof authorization === "string" &&
      authorization.startsWith("Preview ") &&
      previewTokens.has(authorization.slice(8)),
    reset,
    get document() {
      return active ? document : null;
    },
    async handle(req: IncomingMessage, res: ServerResponse): Promise<boolean> {
      const path = new URL(req.url ?? "/", "http://127.0.0.1:18084").pathname;
      if (
        path === "/editor" ||
        path.startsWith("/api/studio/") ||
        path.startsWith("/assets/index-")
      ) {
        await studio(req, res);
        return true;
      }
      if (path === "/studio-control" && req.method === "POST") {
        let body = "";
        for await (const chunk of req) body += String(chunk);
        const control = JSON.parse(body) as { reset?: boolean; fault?: string; state?: string };
        if (control.reset) reset(true);
        fault = control.fault ?? "";
        state = control.state ?? state;
        res.end("ok");
        return true;
      }
      if (!path.startsWith("/api/v1/")) return false;
      res.setHeader("Content-Type", "application/json");
      res.setHeader("Cache-Control", "no-store");
      const fail = (status: number, code: string) => {
        res.writeHead(status);
        res.end(JSON.stringify({ code, detail: credential }));
      };
      if (
        req.headers.authorization !== "Bearer " + credential ||
        req.headers["x-cms-project"] !== "local-demo"
      ) {
        fail(401, "UNAUTHENTICATED");
        return true;
      }
      if (path === "/api/v1/manifest") {
        res.end(JSON.stringify({ manifest, manifestHash: "sha256:" + "a".repeat(64) }));
        return true;
      }
      if (path === "/api/v1/pages") {
        res.end(
          JSON.stringify({
            items: [{ id, path: "/store", title: "Store", hasHead: true }],
            nextCursor: null,
          }),
        );
        return true;
      }
      if (path === "/api/v1/working-drafts") {
        res.end(
          JSON.stringify({
            items: state === "open" ? [...draftTitles.keys()].map(changeset) : [],
            nextCursor: null,
          }),
        );
        return true;
      }
      if (path.startsWith("/api/v1/changesets/") && draftTitles.has(path.split("/").at(-1)!)) {
        res.end(JSON.stringify(changeset(path.split("/").at(-1)!)));
        return true;
      }
      if (path === `/api/v1/documents/${id}`) {
        res.end(JSON.stringify({ id, kind: "page", path: "/store", body: document }));
        return true;
      }
      let body = "";
      for await (const chunk of req) body += String(chunk);
      const envelope = JSON.parse(body);
      if (path === "/api/v1/commands/create-changeset") {
        const key = String(req.headers["idempotency-key"]);
        const prior = createKeys.get(key);
        if (!prior) {
          createKeys.set(key, createdId);
          draftTitles.set(createdId, envelope.payload.title);
        }
        if (fault === "create-lost" && !prior) {
          fault = "";
          fail(503, "STUDIO_RESPONSE_LOST");
          return true;
        }
        res.end(JSON.stringify({ result: changeset(prior ?? createdId) }));
        return true;
      }
      if (path === "/api/v1/commands/create-preview-token") {
        const claims = JSON.parse(
          Buffer.from(previewToken.split(".")[1]!, "base64url").toString("utf8"),
        );
        claims.cs = envelope.payload.changesetId;
        const token =
          "header." + Buffer.from(JSON.stringify(claims)).toString("base64url") + ".fixture";
        previewTokens.add(token);
        res.end(
          JSON.stringify({
            result: {
              token,
              expiresAt: new Date(Date.now() + 900_000).toISOString(),
            },
          }),
        );
        return true;
      }
      if (path !== "/api/v1/commands/apply-operations") {
        fail(404, "NOT_FOUND");
        return true;
      }
      if (fault === "reject") {
        fail(403, "FORBIDDEN");
        return true;
      }
      if (fault === "conflict") {
        seq++;
        fail(409, "CHANGESET_SEQ_CONFLICT");
        return true;
      }
      if (envelope.payload.expectedSeq !== seq) {
        fail(409, "CHANGESET_SEQ_CONFLICT");
        return true;
      }
      if (state !== "open") {
        fail(409, "CHANGESET_STATE_INVALID");
        return true;
      }
      for (const op of envelope.payload.operations) {
        const p = op.payload;
        if (op.type === "localContent.set")
          (document.localContent ??= {})[p.key] = {
            type: "text",
            value: p.locale ? { [p.locale]: p.value } : p.value,
          };
        else if (op.type === "node.setBinding")
          (document.nodes[p.nodeId]!.bindings ??= {})[p.prop] = p.binding;
        else
          Object.assign(
            (document.nodes[p.nodeId]![op.type === "node.setDesign" ? "design" : "props"] ??= {}),
            p.set,
          );
        seq++;
      }
      if (fault === "lost") {
        fail(503, "STUDIO_RESPONSE_LOST");
        return true;
      }
      res.end(JSON.stringify({ result: { changeset: changeset() } }));
      return true;
    },
  };
}
