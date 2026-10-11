# @cms/component-registry

One application registry supplies component contracts, implementations and optional
resolved-value validators. Framework-neutral; no React imports. JSON metadata is
snapshotted and frozen, implementations retain their identity.

```ts
import { createRegistry, defineComponent, nativeDefinitions, t } from "@cms/component-registry";
export function Card(props: { title: string }) { return props.title; }
const registry = createRegistry({
  breakpoints: { sm: 640 },
  components: [defineComponent({
    name: "Card", component: Card,
    props: { title: t.text().content().required().max(200) },
    validate: values => typeof values.title === "string",
  })],
});
const natives = nativeDefinitions(registry);
// Server/build only:
import { manifestArtifact } from "@cms/component-registry/manifest";
const artifact = await manifestArtifact(registry, {
  app: { id: "my-app", version: "1.0.0", framework: "react" },
});
```

For React, use `defineReactComponent` and `createReactRegistry` from `@cms/react`.
The helper infers resolved `values` from literal or DSL prop declarations,
including required/optional fields, enum literals and nested objects/lists.
`defineComponent` checks ordinary implementation props. Reference resolution and
application of defaults are not performed by adapters.
Type inference does not validate external values. Supply `validate` for bindings.

Manifest generation validates the complete result before returning canonical JSON
and a SHA-256 hash. Component code and validators are never serialized.
The build entry point imports the manifest validator; the browser entry point has
no React, TypeScript or CLI imports. `t.*` supports all 17 manifest kinds, immutable
modifiers, nested objects/lists and slots. Unsupported modifiers fail typechecking;
the shared validator checks constraints and references. Schema fields can use
descriptor `.schema` in `registry.schemas`; `registry.capabilities` accepts metadata.
An object with a `base` key is a responsive wrapper; put ordinary objects inside `base`.

## CLI build

Build workspace dependencies first; run from the application directory:

```sh
pnpm exec cms manifest build --registry src/registry.ts --export registry --root ../.. --tsconfig tsconfig.json --app-id my-app --app-version 1.0.0 --framework react
```

The CLI typechecks and loads the trusted application registry, resolves sourceRef
using the [TypeScript compiler API](https://github.com/microsoft/TypeScript/wiki/Using-the-Compiler-API),
then verifies the loaded implementation identity. After validation it writes
`.cms/manifest.json`, `manifest.hash` and `code-index.json`; `--out` changes the directory.
`--export` defaults to `default`, `--root` to `.`, `--tsconfig` to `tsconfig.json`.
Framework is `react`, `vue`, `svelte` or `other`.

Use direct factory calls with static names and named, exported implementations.
Import aliases, namespace imports, reexports and default exports work. Opaque
wrappers, inline implementations, ambiguous names and runtime substitutions fail
before output. Include the registry in tsconfig and keep it inside the repository root.
The index contains paths, exports, lines, dependencies up to depth 2 and directly
importing tests, without source text. Node modules, generated, dist and declaration
files are excluded. Git HEAD binds the index and app build. `--commit` must match
HEAD when Git is available; exported archives require an explicit hexadecimal SHA.
Release builds should use a clean checkout. Build executes the application's trusted
imports; never use an untrusted project as input.

## CLI push

```sh
pnpm exec cms manifest push --dir .cms --base-url https://cms.example.com --project my-project --env production --token-file /protected/service-token --wait-for-deploy https://app.example.com --timeout 30000
```

The protected file contains only a service token with `manifest.register` scope;
`CMS_SERVICE_TOKEN_FILE` can supply its path. Never put tokens in command arguments.
HTTPS is required except for loopback HTTP; redirects are rejected. Push verifies
both bundle hashes before sending. `--wait-for-deploy` polls the application's
plain-text `/.well-known/cms-manifest-hash` until it equals the generated hash.
Timeout is milliseconds, 1–300000, default 30000. A mismatch prevents registration.
Deploy first, then run push in the release job.

Push only registers the manifest; it does not deploy or publish content. Registration
may produce a schema candidate requiring the existing CMS migration/review flow.
The server has no code-index upload API: `uploaded: false` remains explicit.
The executable prints only known error codes, never exception/HTTP response bodies.
`@cms/component-registry/cli` exposes build/push APIs and detailed safe compiler
diagnostics. `defineAction` and `defineDataSource` use the same DSL, infer handler
arguments and loader results, and participate in manifest/source-index generation.
Handler/loader code is never serialized. See [actions and sources](../../docs/runtime-actions-data.md) and
[implemented scope](../../docs/registry-dsl-cli.md).
