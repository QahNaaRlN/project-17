# @cms/react

React adapter for a prepared, serializable IR plan. This first slice implements
semantic layout/text/link primitives and application-owned native components.
The complete scope and remaining M3 work are in [the renderer contract](../../docs/react-ir-renderer.md).

```tsx
// Server: the CmsClient contains delivery/preview credentials.
import { loadPage } from "@cms/react/server";
const plan = await loadPage(cms, "/home", {
  read: { preview },
  scope: { context: { locale: "ru" } },
  natives: { Card: { props: { title: {} } } },
});

// SSR and browser use the same plan and stable native registry.
import { CmsPage } from "@cms/react";
const registry = {
  Card: ({ values, className }) => <article className={className}>{values.title}</article>,
};
return plan ? <CmsPage plan={plan} registry={registry} /> : <NotFound />;
```

Native props arrive in `values`; `children`, `className`, `nodeId` and `instanceId`
are separate. Apply the generated class to your component's outer element.
For native object props declared responsive, use `{base: {your: "object"}}` in IR.
Native breakpoint props and pinned Composed versions are rejected in this slice.
Definitions may supply `validate(values)` to enforce application-specific types.

Only render plans from a trusted server. `CmsPage` does not accept arbitrary
HTML or spread CMS attributes onto DOM elements. It emits a `<style>` element
for used atoms; hosting CSP must allow those styles. Applications with a nonce-only
style policy should serve `plan.css` as a stylesheet and pass `css: ""` to the adapter.
The adapter keeps CSS in React so refreshes update or remove it together with content.

Native React components may use hooks. Native SSR exceptions belong to the host's
sanitized error response, not a per-node boundary. `loadPage` propagates page
failures, returns null for 404 and diagnoses missing live components. It fetches
at most 64 component documents per page and preserves preview/signal options.
It does not fetch entities or run application data loaders. Localized `$local`
content uses only the explicitly selected locale; there is no implicit fallback.

## Shared component declarations

Use `defineReactComponent({name, props, component, validate})` to infer a component's
`NativeProps<InferProps<typeof props>>` from literal manifest descriptors.
`createRegistry` from `@cms/component-registry` stores those declarations;
`nativeDefinitions(registry)` prepares the server plan and `createReactRegistry(registry)`
selects stable React implementations. Generate the manifest from this same registry
via `@cms/component-registry/manifest`. See the storefront registry for a complete example.
