import process from "node:process";
import { randomUUID } from "node:crypto";
import { execFileSync } from "node:child_process";
import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { isAbsolute, join, relative, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { register } from "tsx/esm/api";
import { canonicalJson, manifestHash, validateManifest, type Manifest } from "@cms/manifest";
import { buildManifest } from "./manifest.js";
import { createRegistry, type RegistryOptions } from "./registry.js";
import { projectProgram, sourceIndex, type CodeIndex } from "./source-index.js";

export interface BuildOptions {
  registryFile: string;
  exportName?: string;
  configFile: string;
  root: string;
  app: Manifest["app"];
  commit: string;
  output: string;
}
export interface BuildResult {
  manifest: Manifest;
  hash: string;
  index: CodeIndex;
}
export async function buildProject(options: BuildOptions): Promise<BuildResult> {
  const program = projectProgram(options.configFile);
  const loader = register({ namespace: randomUUID(), tsconfig: options.configFile });
  try {
    const loaded = (await loader.import(
      pathToFileURL(options.registryFile).href,
      import.meta.url,
    )) as Record<string, unknown>;
    const exported = loaded[options.exportName ?? "default"];
    if (
      !exported ||
      typeof exported !== "object" ||
      !Array.isArray((exported as RegistryOptions).components)
    )
      throw new Error("CMS_REGISTRY_EXPORT_INVALID");
    const registry = createRegistry(exported as RegistryOptions);
    const sources = sourceIndex(
      program,
      options.registryFile,
      registry,
      options.root,
      options.commit,
    );
    for (const definition of registry.components) {
      const ref = sources.refs[definition.name]!;
      const implementation = (await loader.import(
        pathToFileURL(resolve(options.root, ref.file)).href,
        import.meta.url,
      )) as Record<string, unknown>;
      if (!ref.export || implementation[ref.export] !== definition.component)
        throw new Error("CMS_SOURCE_IMPLEMENTATION_MISMATCH: " + definition.name);
    }
    const manifest = buildManifest(registry, { app: options.app });
    for (const [name, ref] of Object.entries(sources.refs))
      manifest.components![name]!.sourceRef = ref;
    manifest.codeIndex = { hash: await manifestHash(sources.index), uploaded: false };
    if (!validateManifest(manifest).valid) throw new Error("CMS_MANIFEST_INVALID");
    const hash = await manifestHash(manifest);
    // All validation precedes writes. Manifest is the final commit marker.
    await mkdir(options.output, { recursive: true });
    for (const [name, value] of [
      ["code-index.json", canonicalJson(sources.index)],
      ["manifest.hash", hash],
      ["manifest.json", canonicalJson(manifest)],
    ]) {
      const target = join(options.output, name!);
      const temporary = target + ".tmp-" + randomUUID();
      try {
        await writeFile(temporary, value! + "\n", { flag: "wx" });
        await rename(temporary, target);
      } finally {
        await rm(temporary, { force: true });
      }
    }
    return { manifest, hash, index: sources.index };
  } finally {
    await loader.unregister();
  }
}

export interface PushOptions {
  directory: string;
  baseUrl: string;
  project: string;
  environment: string;
  tokenFile: string;
  waitForDeploy?: string;
  timeoutMs?: number;
}
function endpoint(input: string): URL {
  const url = new URL(input);
  if (
    url.username ||
    url.password ||
    (url.protocol !== "https:" &&
      !(url.protocol === "http:" && ["127.0.0.1", "localhost", "[::1]"].includes(url.hostname)))
  )
    throw new Error("CMS_URL_INVALID");
  return url;
}
export async function pushManifest(options: PushOptions): Promise<unknown> {
  const json = await readFile(join(options.directory, "manifest.json"), "utf8");
  const manifest = JSON.parse(json) as Manifest;
  if (!validateManifest(manifest).valid) throw new Error("CMS_MANIFEST_INVALID");
  const hash = await manifestHash(manifest);
  if ((await readFile(join(options.directory, "manifest.hash"), "utf8")).trim() !== hash)
    throw new Error("CMS_MANIFEST_HASH_MISMATCH");
  const index = JSON.parse(
    await readFile(join(options.directory, "code-index.json"), "utf8"),
  ) as unknown;
  if (manifest.codeIndex?.hash !== (await manifestHash(index)))
    throw new Error("CMS_CODE_INDEX_HASH_MISMATCH");
  const token = (await readFile(options.tokenFile, "utf8")).trim();
  if (!/^cms_svc_[A-Za-z0-9_-]+$/.test(token)) throw new Error("CMS_SERVICE_TOKEN_INVALID");
  const base = endpoint(options.baseUrl);
  const timeout = options.timeoutMs ?? 30_000;
  if (!Number.isInteger(timeout) || timeout < 1 || timeout > 300_000)
    throw new Error("CMS_TIMEOUT_INVALID");
  if (options.waitForDeploy) {
    const deployed = endpoint(options.waitForDeploy);
    deployed.pathname = deployed.pathname.replace(/\/$/, "") + "/.well-known/cms-manifest-hash";
    const deadline = Date.now() + timeout;
    let matched = false;
    while (Date.now() < deadline) {
      try {
        const response = await fetch(deployed, {
          redirect: "error",
          signal: AbortSignal.timeout(Math.max(1, deadline - Date.now())),
        });
        if (response.ok && (await response.text()).trim() === hash) {
          matched = true;
          break;
        }
      } catch {
        /* A deployment can be unavailable during rollout. */
      }
      await new Promise((done) =>
        setTimeout(done, Math.min(100, Math.max(0, deadline - Date.now()))),
      );
    }
    if (!matched) throw new Error("CMS_DEPLOYMENT_HASH_TIMEOUT");
  }
  const url = new URL("/api/v1/commands/register-manifest", base);
  const response = await fetch(url, {
    method: "POST",
    redirect: "error",
    signal: AbortSignal.timeout(timeout),
    headers: {
      Authorization: "Bearer " + token,
      "X-CMS-Project": options.project,
      "Content-Type": "application/json",
      "Idempotency-Key": randomUUID(),
    },
    body: JSON.stringify({
      payload: { environment: options.environment, manifest },
      reason: "Application manifest from cms CLI",
    }),
  });
  if (!response.ok) throw new Error("CMS_MANIFEST_REGISTER_FAILED: HTTP " + response.status);
  return (await response.json()) as unknown;
}
export async function runCli(args: string[], cwd = process.cwd()): Promise<void> {
  if (args[0] !== "manifest" || !["build", "push"].includes(args[1] ?? ""))
    throw new Error("CMS_USAGE: cms manifest build|push");
  const values = new Map<string, string>();
  const allowed =
    args[1] === "build"
      ? [
          "registry",
          "export",
          "tsconfig",
          "root",
          "out",
          "app-id",
          "app-version",
          "framework",
          "commit",
        ]
      : ["dir", "base-url", "project", "env", "token-file", "wait-for-deploy", "timeout"];
  for (let i = 2; i < args.length; i += 2) {
    const key = args[i]!.replace(/^--/, "");
    const value = args[i + 1];
    if (
      !args[i]!.startsWith("--") ||
      !allowed.includes(key) ||
      values.has(key) ||
      !value ||
      value.startsWith("--")
    )
      throw new Error("CMS_ARGUMENT_INVALID");
    values.set(key, value);
  }
  const required = (key: string): string => {
    const value = values.get(key);
    if (!value) throw new Error("CMS_ARGUMENT_REQUIRED: --" + key);
    return value;
  };
  if (args[1] === "push") {
    await pushManifest({
      directory: resolve(cwd, values.get("dir") ?? ".cms"),
      baseUrl: required("base-url"),
      project: required("project"),
      environment: required("env"),
      tokenFile: resolve(
        cwd,
        values.get("token-file") ?? process.env.CMS_SERVICE_TOKEN_FILE ?? required("token-file"),
      ),
      ...(values.has("wait-for-deploy") ? { waitForDeploy: values.get("wait-for-deploy")! } : {}),
      ...(values.has("timeout") ? { timeoutMs: Number(values.get("timeout")) } : {}),
    });
    return;
  }
  const root = resolve(cwd, values.get("root") ?? ".");
  let commit = values.get("commit");
  let head: string | undefined;
  try {
    head = execFileSync("git", ["rev-parse", "HEAD"], {
      cwd: root,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
  } catch {
    /* Explicit commits also support exported source archives. */
  }
  commit ??= head;
  if (!commit) throw new Error("CMS_COMMIT_REQUIRED");
  if (!/^[a-f0-9]{40,64}$/.test(commit)) throw new Error("CMS_COMMIT_INVALID");
  if (head && commit !== head) throw new Error("CMS_COMMIT_MISMATCH");
  const framework = required("framework");
  if (!["react", "vue", "svelte", "other"].includes(framework))
    throw new Error("CMS_FRAMEWORK_INVALID");
  const registryFile = resolve(cwd, required("registry"));
  const rel = relative(root, registryFile);
  if (rel === ".." || rel.startsWith(".." + "/") || rel.startsWith(".." + "\\") || isAbsolute(rel))
    throw new Error("CMS_REGISTRY_OUTSIDE_ROOT");
  await buildProject({
    root,
    registryFile,
    configFile: resolve(cwd, values.get("tsconfig") ?? "tsconfig.json"),
    output: resolve(cwd, values.get("out") ?? ".cms"),
    commit,
    exportName: values.get("export") ?? "default",
    app: {
      id: required("app-id"),
      version: required("app-version"),
      framework: framework as Manifest["app"]["framework"],
      build: "git:" + commit,
    },
  });
}
