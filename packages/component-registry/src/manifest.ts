import { canonicalJson, manifestHash, validateManifest, type Manifest } from "@cms/manifest";
import { componentContract, type Registry } from "./registry.js";

export interface ManifestOptions {
  app: Manifest["app"];
  irVersions?: Manifest["irVersions"];
}
/** Server/build entry point, kept out of the browser registry module. */
export function buildManifest(registry: Registry, options: ManifestOptions): Manifest {
  const manifest = JSON.parse(
    canonicalJson({
      manifestVersion: "1.0",
      app: options.app,
      irVersions: options.irVersions ?? ["1.0"],
      breakpoints: registry.breakpoints,
      tokens: registry.tokens,
      icons: registry.icons,
      components: Object.fromEntries(
        registry.components.map((definition) => [definition.name, componentContract(definition)]),
      ),
    }),
  ) as Manifest;
  const result = validateManifest(manifest);
  if (!result.valid)
    throw new Error(
      "CMS_MANIFEST_INVALID: " +
        result.diagnostics
          .map((diagnostic) => diagnostic.code + " " + diagnostic.pointer)
          .join("; "),
    );
  return manifest;
}
export async function manifestArtifact(
  registry: Registry,
  options: ManifestOptions,
): Promise<{ manifest: Manifest; json: string; hash: string }> {
  const manifest = buildManifest(registry, options);
  return { manifest, json: canonicalJson(manifest), hash: await manifestHash(manifest) };
}
