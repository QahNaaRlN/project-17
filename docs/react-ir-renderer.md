# React IR renderer — first working slice

Status: implemented as the first working renderer slice. Depends on the Delivery/Image SDK and React storefront merged in PRs 31–33.

## Outcome

CMS owns section order and repeated product templates. Application code owns native components and their interactions. The existing storefront design, keyboard dialogs, image renewal and server-side secrets boundary remain acceptance criteria.

The example reads its typed root config on the server and supplies the manifest-declared `$context.store` to descendant bindings. It strips the unused config before preparing the browser plan. This does not simulate entity resolution or add a `$content` source without a content declaration.

## Contract

`@cms/core` evaluates own-property paths, bindings, templates, built-in formatters and predicates. Missing values stay missing; defaults apply only to null/undefined. No evaluation of executable expressions, prototype lookup or implicit locale fallback.

`@cms/runtime` compiles validated IR to a serializable render plan. It resolves conditions, Repeat (limit 200, stable item keys), live Composed references, component inputs and caller-scoped slots. Expansion is bounded, including composed recursion. Diagnostics contain codes and node IDs, never exception messages or secret values. Unknown/unsupported nodes and actions are omitted. Native definitions explicitly declare permitted props and responsive props. An object in a responsive prop must have `base`; an ordinary object is placed inside `base`.

First primitives: Box, Stack, Flex, Grid, Heading, Text, Label, Divider, Link, Repeat, Slot and Composed. Native components receive a props object and children, not arbitrary DOM attributes. Links accept http/https/mailto/tel, safe relative paths and anchor node references; page-route lookup is deferred. Native inputs and children are trusted application contracts, not CMS code.

First design slice: layout direction/wrap; spacing, color, radius, width and text alignment atoms, optional manifest-like token values and named min-width breakpoints. Only generated, validated CSS is emitted. Unsupported design properties reject the node with a diagnostic. No raw HTML, selectors, arbitrary CSS or event strings.

`@cms/react` renders a prepared plan as semantic React elements. `@cms/react/server` loads the page and missing live composed documents with the same preview options; it returns a plan or null. The browser receives only the plan, not the client/configuration/token. Native components are ordinary React components and may use hooks. Application error handling owns native exceptions during SSR; the existing example returns a sanitized 503. This slice does not claim per-node native SSR error isolation.

## Verification

Expansion is limited to 5000 visits, depth 32 and 2 MiB of serialized props. Repeat keys are limited to 256 characters. Live component loading is limited to 64 unique references. Exceeding a global expansion budget returns an empty plan with one diagnostic.

Unit tests cover safe paths, formatters, condition truth tables, Repeat/slot scopes, expansion limits, responsive wrappers, CSS/link injection rejection and escaped React SSR. Existing browser scenarios continue to run; new scenarios change CMS section order and hide a section without changing application code.

## Remaining dependencies

This is not full M3. Native registry generation and CLI/source indexing are implemented in [the registry slices](component-registry.md). [Actions and data sources](runtime-actions-data.md) now cover typed declarations, pre-SSR loading, event execution and pagination helpers. Streaming/selective hydration, native error isolation, Studio and preview bridge, automatic entity localization/resolution, pinned component versions, rich text/media primitives and the remaining design catalogue are separate packages. Delivery currently returns empty component/data maps; the server loader fetches live components explicitly. Local draft setup is authorized; production publication is not part of this package.
