# @cms/component-registry

One application registry supplies component contracts, implementations and optional
resolved-value validators. Framework-neutral; no React imports. JSON metadata is
snapshotted and frozen, implementations retain their identity.

```ts
import { createRegistry, defineComponent, nativeDefinitions } from "@cms/component-registry";
const registry = createRegistry({
  breakpoints: { sm: 640 },
  components: [defineComponent({
    name: "Card", component: Card,
    props: { title: { type: "text", content: true, required: true } },
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
The helper infers resolved `values` from literal manifest prop declarations,
including required/optional fields, enum literals and nested objects/lists.
Unsupported value forms remain `unknown`; reference resolution is not performed.
Type inference does not validate external values. Supply `validate` for bindings.

Manifest generation validates the complete result before returning canonical JSON
and a SHA-256 hash. Component code and validators are never serialized.
The build entry point imports the manifest validator; the browser entry point does
not. No network access, publication, automatic source indexing or generic CLI.
See [implemented scope and remaining requirements](../../docs/component-registry.md).
