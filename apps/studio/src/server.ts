import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { createStudioHandler, type ServerConfig } from "./handler.js";
const path = process.env.CMS_STUDIO_CONFIG_FILE;
if (!path) throw new Error("CMS_STUDIO_CONFIG_FILE is required");
const config = JSON.parse(await readFile(path, "utf8")) as ServerConfig;
const url = new URL(config.studioOrigin);
createServer(
  createStudioHandler(config, fileURLToPath(new URL("../client", import.meta.url))),
).listen(Number(url.port || (url.protocol === "https:" ? 18085 : 80)), "127.0.0.1", () =>
  console.log("CMS Studio listening on loopback"),
);
