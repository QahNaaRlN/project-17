// STU-446: local CDN purge receiver for the §2 scenario. Records every
// authorized purge (Surrogate-Key list) for the scenario to read; loopback only.
import { createServer } from "node:http";
import { URL } from "node:url";

const port = Number(process.env.E2E_PURGE_PORT ?? 18196);
const token = process.env.E2E_PURGE_TOKEN ?? "";
if (!Number.isInteger(port) || port < 1024 || port > 65535 || token.length < 16)
  throw Error("E2E_PURGE_PORT and a 16+ character E2E_PURGE_TOKEN are required");
const purges = [];
createServer(async (req, res) => {
  const url = new URL(req.url ?? "/", "http://127.0.0.1");
  res.setHeader("Content-Type", "application/json");
  if (req.method === "GET" && url.pathname === "/healthz") return res.end("{}");
  if (req.headers.authorization !== "Bearer " + token) {
    res.writeHead(401);
    return res.end("{}");
  }
  if (req.method === "GET" && url.pathname === "/purges") return res.end(JSON.stringify(purges));
  if (req.method === "POST" && url.pathname === "/purge") {
    let body = "";
    for await (const chunk of req) body += String(chunk);
    const keys = JSON.parse(body).keys;
    if (!Array.isArray(keys) || keys.some((k) => typeof k !== "string")) {
      res.writeHead(400);
      return res.end("{}");
    }
    purges.push({ at: Date.now(), keys });
    console.log("purge " + keys.join(" "));
    return res.end("{}");
  }
  res.writeHead(404);
  res.end("{}");
}).listen(port, "127.0.0.1", () => console.log("Purge receiver ready on " + port));
