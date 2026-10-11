import { mkdir, writeFile } from "node:fs/promises";
import { manifestArtifact } from "@cms/component-registry/manifest";
import { registry } from "../src/registry.js";
const artifact = await manifestArtifact(registry, {
  app: { id: "store-example", version: "1.0.0", framework: "react" },
});
await mkdir(".cms", { recursive: true });
await writeFile(".cms/manifest.json", artifact.json + "\n", "utf8");
await writeFile(".cms/manifest.hash", artifact.hash + "\n", "utf8");
console.log(artifact.hash);
