import { readFile, writeFile, readdir } from "node:fs/promises";
import { join } from "node:path";
import { execFileSync } from "node:child_process";
import process from "node:process";
import { createServer } from "node:http";
import { once } from "node:events";
import { expect, it, vi } from "vitest";
import { canonicalJson, manifestHash } from "@cms/manifest";
import { createRegistry, defineComponent } from "../src/index.js";
import { buildManifest } from "../src/manifest.js";
import { buildProject, pushManifest, runCli } from "../src/cli.js";
import { fixture, packageRoot, commit } from "./fixtures.js";

it("MF-001/MF-011/AGT-070/071/072: CLI uses actual exported implementations and bounded code metadata", async () => {
  const options = await fixture();
  const result = await buildProject(options);
  expect(result.manifest.components!.Card!.sourceRef).toEqual({
    file: "components/card.ts",
    export: "Card",
    line: 2,
  });
  expect(result.index.elements["component:Card"]).toEqual({
    definition: { file: "registry.ts", line: 3 },
    implementation: { file: "components/card.ts", export: "Card", line: 2 },
    dependencies: ["components/label.ts", "components/prefix.ts"],
    tests: ["components/card.test.ts"],
  });
  expect(result.index.repo).toEqual({ root: ".", commit });
  expect(result.index.conventions.registry).toBe("registry.ts");
  expect(result.index.conventions.manifestCommand).toBe("cms manifest build");
  expect(result.manifest.codeIndex).toEqual({
    hash: await manifestHash(result.index),
    uploaded: false,
  });
  expect(await readFile(join(options.output, "manifest.json"), "utf8")).toBe(
    canonicalJson(result.manifest) + "\n",
  );
  expect(await readFile(join(options.output, "manifest.hash"), "utf8")).toBe(result.hash + "\n");
  expect(await readFile(join(options.output, "code-index.json"), "utf8")).not.toContain(
    "return label",
  );
  expect(await readdir(options.output)).toEqual([
    "code-index.json",
    "manifest.hash",
    "manifest.json",
  ]);
  expect(await buildProject(options)).toEqual(result);
}, 20000);

it("MF-001: compiler errors prevent output and diagnostics omit source literals", async () => {
  const options = await fixture({
    "components/card.ts": `export const Card = (p: {title: number}) => p.title; // PRIVATE_SOURCE`,
  });
  await expect(buildProject(options)).rejects.toThrow(
    /CMS_TYPECHECK_FAILED: .*TS\d+ registry.ts:3/,
  );
  await expect(readFile(join(options.output, "manifest.json"))).rejects.toThrow();
});
it("MF-011: cannot attach a sourceRef to a replaced runtime implementation", async () => {
  const options = await fixture({
    "registry.ts": `import {createRegistry, defineComponent} from '@cms/component-registry';
import {Card} from './components/card.js';
const c = defineComponent({name: 'Card', component: Card, props: {title: {type: 'string', required: true}}});
export default createRegistry({components: [{...c, component: () => 'replacement'}]});`,
  });
  await expect(buildProject(options)).rejects.toThrow("CMS_SOURCE_IMPLEMENTATION_MISMATCH");
  await expect(readFile(join(options.output, "manifest.json"))).rejects.toThrow();
});
it("MF-001: invalid exports and invalid final manifest are rejected before writes", async () => {
  const options = await fixture();
  await expect(buildProject({ ...options, exportName: "missing" })).rejects.toThrow(
    "CMS_REGISTRY_EXPORT_INVALID",
  );
  await expect(buildProject({ ...options, app: { ...options.app, id: "" } })).rejects.toThrow(
    "CMS_MANIFEST_INVALID",
  );
  await expect(readFile(join(options.output, "manifest.json"))).rejects.toThrow();
});
it("MF-001: bin reports safe errors and never serializes exception contents", async () => {
  const bin = join(packageRoot, "bin/cms.mjs");
  expect(() =>
    execFileSync(process.execPath, [bin], { encoding: "utf8", stdio: "pipe" }),
  ).toThrow();
  const options = await fixture({
    "registry.ts": `throw new Error('DO_NOT_PRINT'); export default {};`,
  });
  try {
    execFileSync(
      process.execPath,
      [
        bin,
        "manifest",
        "build",
        "--registry",
        "registry.ts",
        "--app-id",
        "fixture",
        "--app-version",
        "1.0.0",
        "--framework",
        "other",
        "--commit",
        commit,
      ],
      { cwd: options.root, encoding: "utf8", stdio: "pipe" },
    );
    throw new Error("Expected failure");
  } catch (error) {
    expect((error as { stderr: string }).stderr).toBe("CMS_CLI_FAILURE\n");
  }
  await writeFile(
    options.registryFile,
    "throw new Error('CMS_TYPECHECK_FAILED: PRIVATE'); export default {};",
  );
  try {
    execFileSync(
      process.execPath,
      [
        bin,
        "manifest",
        "build",
        "--registry",
        "registry.ts",
        "--app-id",
        "fixture",
        "--app-version",
        "1.0.0",
        "--framework",
        "other",
        "--commit",
        commit,
      ],
      { cwd: options.root, encoding: "utf8", stdio: "pipe" },
    );
    throw new Error("Expected failure");
  } catch (error) {
    expect((error as { stderr: string }).stderr).toBe("CMS_TYPECHECK_FAILED\n");
  }
});

it("MF-001: validates CLI arguments and permits explicit commits outside Git", async () => {
  const options = await fixture();
  for (const args of [[], ["wrong", "build"], ["manifest", "wrong"]])
    await expect(runCli(args)).rejects.toThrow("CMS_USAGE");
  for (const flags of [
    ["registry", "x"],
    ["--unknown", "x"],
    ["--registry"],
    ["--registry", "--out"],
    ["--registry", "a", "--registry", "b"],
  ])
    await expect(runCli(["manifest", "build", ...flags])).rejects.toThrow("CMS_ARGUMENT_INVALID");
  const args = [
    "manifest",
    "build",
    "--registry",
    "registry.ts",
    "--commit",
    commit,
    "--app-id",
    "fixture",
    "--app-version",
    "1.0.0",
    "--framework",
    "other",
  ];
  await expect(runCli(args, options.root)).resolves.toBeUndefined();
  await expect(runCli(["manifest", "build", "--commit", "no"], options.root)).rejects.toThrow(
    "CMS_COMMIT_INVALID",
  );
  await expect(
    runCli(["manifest", "build", "--commit", "b".repeat(40)], packageRoot),
  ).rejects.toThrow("CMS_COMMIT_MISMATCH");
  await expect(runCli(["manifest", "build"], options.root)).rejects.toThrow("CMS_COMMIT_REQUIRED");
  await expect(runCli(["manifest", "build", "--commit", commit], options.root)).rejects.toThrow(
    "CMS_ARGUMENT_REQUIRED: --framework",
  );
  await expect(
    runCli(
      args.map((s) => (s === "other" ? "angular" : s)),
      options.root,
    ),
  ).rejects.toThrow("CMS_FRAMEWORK_INVALID");
  await expect(
    runCli(
      args.map((s) => (s === "registry.ts" ? "../outside.ts" : s)),
      options.root,
    ),
  ).rejects.toThrow("CMS_REGISTRY_OUTSIDE_ROOT");
}, 20000);

async function bundle() {
  const options = await fixture();
  const manifest = buildManifest(
    createRegistry({
      components: [defineComponent({ name: "Card", component: () => null, props: {} })],
    }),
    { app: options.app },
  );
  const index = { repo: { root: ".", commit }, elements: {} };
  manifest.codeIndex = { hash: await manifestHash(index), uploaded: false };
  // A directory independent of the build tests; fake service token only.
  for (const [name, value] of [
    ["manifest.json", JSON.stringify(manifest)],
    ["manifest.hash", await manifestHash(manifest)],
    ["code-index.json", JSON.stringify(index)],
    ["token", "cms_svc_TEST_ONLY"],
  ])
    await writeFile(join(options.root, name!), value!);
  return {
    directory: options.root,
    baseUrl: "http://127.0.0.1:1",
    project: "fixture",
    environment: "staging",
    tokenFile: join(options.root, "token"),
    timeoutMs: 1000,
  };
}
it("MF-004/MF-005: waits for the deployed hash, then registers exactly the validated artifact", async () => {
  const options = await bundle();
  const hash = await readFile(join(options.directory, "manifest.hash"), "utf8");
  const requests: string[] = [];
  let body = "";
  const server = createServer(async (req, res) => {
    requests.push(req.url!);
    if (req.url === "/.well-known/cms-manifest-hash") {
      res.end(requests.length === 1 ? "old hash" : hash);
      return;
    }
    expect(req.method).toBe("POST");
    expect(req.headers.authorization).toBe("Bearer cms_svc_TEST_ONLY");
    expect(req.headers["x-cms-project"]).toBe("fixture");
    expect(req.headers["idempotency-key"]).toMatch(/^[a-f0-9-]{36}$/);
    for await (const chunk of req) body += chunk;
    res.setHeader("Content-Type", "application/json");
    res.end('{"activation":"active"}');
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const url = `http://127.0.0.1:${(server.address() as { port: number }).port}`;
  try {
    expect(await pushManifest({ ...options, baseUrl: url, waitForDeploy: url })).toEqual({
      activation: "active",
    });
    expect(requests).toEqual([
      "/.well-known/cms-manifest-hash",
      "/.well-known/cms-manifest-hash",
      "/api/v1/commands/register-manifest",
    ]);
    expect(JSON.parse(body).payload).toEqual({
      environment: "staging",
      manifest: JSON.parse(await readFile(join(options.directory, "manifest.json"), "utf8")),
    });
    vi.stubEnv("CMS_SERVICE_TOKEN_FILE", options.tokenFile);
    await runCli([
      "manifest",
      "push",
      "--dir",
      options.directory,
      "--base-url",
      url,
      "--project",
      "fixture",
      "--env",
      "staging",
      "--wait-for-deploy",
      url,
      "--timeout",
      "1000",
    ]);
  } finally {
    vi.unstubAllEnvs();
    server.closeAllConnections();
    await new Promise<void>((done) => server.close(() => done()));
  }
});
it("MF-004: bundle tampering, invalid auth, URLs and timeouts prevent registration", async () => {
  const options = await bundle();
  const manifest = await readFile(join(options.directory, "manifest.json"), "utf8");
  await writeFile(join(options.directory, "manifest.json"), "{}");
  await expect(pushManifest(options)).rejects.toThrow("CMS_MANIFEST_INVALID");
  await writeFile(join(options.directory, "manifest.json"), manifest);
  await writeFile(join(options.directory, "manifest.hash"), "wrong");
  await expect(pushManifest(options)).rejects.toThrow("CMS_MANIFEST_HASH_MISMATCH");
  await writeFile(
    join(options.directory, "manifest.hash"),
    await manifestHash(JSON.parse(manifest)),
  );
  await writeFile(join(options.directory, "code-index.json"), "{}");
  await expect(pushManifest(options)).rejects.toThrow("CMS_CODE_INDEX_HASH_MISMATCH");
  const good = await bundle();
  await writeFile(good.tokenFile, "private-invalid");
  await expect(pushManifest(good)).rejects.toThrow("CMS_SERVICE_TOKEN_INVALID");
  await writeFile(good.tokenFile, "cms_svc_TEST_ONLY");
  for (const baseUrl of ["http://example.com", "https://user:secret@example.com", "file:///tmp/a"])
    await expect(pushManifest({ ...good, baseUrl })).rejects.toThrow("CMS_URL_INVALID");
  for (const timeoutMs of [0, 300001, 1.5, NaN])
    await expect(pushManifest({ ...good, timeoutMs })).rejects.toThrow("CMS_TIMEOUT_INVALID");
  await expect(
    pushManifest({ ...good, waitForDeploy: good.baseUrl, timeoutMs: 20 }),
  ).rejects.toThrow("CMS_DEPLOYMENT_HASH_TIMEOUT");
  await expect(runCli(["manifest", "push"])).rejects.toThrow("CMS_ARGUMENT_REQUIRED");
});
it("MF-004: HTTP failure reports status without the server's private response", async () => {
  const options = await bundle();
  const server = createServer((_req, res) => {
    res.writeHead(403);
    res.end("private response");
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  try {
    await expect(
      pushManifest({
        ...options,
        baseUrl: `http://127.0.0.1:${(server.address() as { port: number }).port}`,
      }),
    ).rejects.toThrow("CMS_MANIFEST_REGISTER_FAILED: HTTP 403");
  } finally {
    server.closeAllConnections();
    await new Promise<void>((done) => server.close(() => done()));
  }
});
