# Матрица прослеживаемости требований

Каждое требование глав 00–12 — одна строка: статус и доказательство
([сверка](../spec-reconciliation.md), STU-610…612). `pnpm spec:check` в обязательном
job `Check (TS + Go)` проверяет строку на каждое требование глав, статус из списка,
ADR у отступлений и отложенных требований и ID требования в указанном тесте.

| Статус | Значение |
|---|---|
| выполнено | Реализовано по тексту главы; доказательство — тест с меткой ID или описание «по построению» |
| выполнено с отступлением | Реализовано иначе, чем в главе; решение — в ADR, глава описывает фактическое поведение |
| отложено | Не реализовано в MVP; ADR называет условие или этап |
| снято | Требование отменено ADR; идентификатор не переиспользуется |

Итого: выполнено — 241; выполнено с отступлением — 15; отложено — 16; снято — 0 (всего 272).

## 01 Требования

| ID | Глава | Статус | Доказательство |
|---|---|---|---|
| FR-001 | 01 | выполнено | тест: `apps/server/internal/commandbus/bus_test.go` |
| FR-002 | 01 | выполнено | тест: `apps/studio/test/handler.test.ts` |
| FR-003 | 01 | выполнено | тест: `apps/server/internal/changes/content_fuzz_test.go` |
| FR-004 | 01 | выполнено | тест: `apps/server/internal/publishing/promote_test.go` |
| FR-005 | 01 | выполнено | тест: `packages/component-registry/test/registry.test.ts` |
| FR-006 | 01 | выполнено | тест: `apps/server/internal/httpapi/isolation_test.go` |
| FR-010 | 01 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| FR-011 | 01 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| FR-012 | 01 | выполнено | тест: `apps/server/internal/httpapi/agents_test.go` |
| FR-013 | 01 | выполнено | тест: `apps/server/internal/composition/policydoc/policy_test.go` |
| FR-014 | 01 | выполнено | тест: `apps/server/internal/composition/manifestdoc/validate_test.go` |
| FR-015 | 01 | выполнено | тест: `apps/server/internal/composition/bindingdoc/validate_test.go` |
| FR-020 | 01 | выполнено | тест: `apps/server/internal/composition/validation/content_values_test.go` |
| FR-021 | 01 | выполнено | тест: `apps/server/internal/changes/content_test.go` |
| FR-022 | 01 | выполнено | тест: `apps/server/internal/reviewdiff/diff_test.go` |
| FR-023 | 01 | выполнено с отступлением | [ADR-0005](../adr/0005-schema-upcast.md) |
| FR-030 | 01 | выполнено | тест: `apps/server/internal/changes/changes_test.go`, `apps/server/internal/changes/content_test.go` |
| FR-031 | 01 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| FR-032 | 01 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| FR-033 | 01 | выполнено | тест: `apps/server/internal/publishing/history_test.go` |
| FR-040 | 01 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| FR-041 | 01 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| FR-050 | 01 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| FR-051 | 01 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| FR-052 | 01 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| FR-053 | 01 | выполнено | тест: `apps/server/internal/manifestregistry/code_index_test.go` |
| NFR-001 | 01 | выполнено | тест: `perf/README.md`, `perf/k6/load.js` |
| NFR-002 | 01 | выполнено | тест: `apps/server/internal/httpapi/cache_bench_test.go`, `perf/README.md` |
| NFR-003 | 01 | выполнено | тест: `perf/README.md`, `perf/k6/load.js` |
| NFR-004 | 01 | выполнено | тест: `perf/README.md`, `perf/k6/load.js` |
| NFR-005 | 01 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts`, `perf/README.md` |
| NFR-006 | 01 | выполнено | тест: `perf/README.md`, `perf/k6/load.js` |
| NFR-007 | 01 | выполнено | тест: `perf/README.md`, `perf/k6/load.js` |
| NFR-008 | 01 | отложено | [ADR-0001](../adr/0001-production-ops.md) |
| NFR-009 | 01 | отложено | [ADR-0001](../adr/0001-production-ops.md) |
| NFR-010 | 01 | отложено | [ADR-0001](../adr/0001-production-ops.md) |
| NFR-011 | 01 | выполнено | тест: `apps/server/internal/composition/ir/contract_test.go` |
| NFR-012 | 01 | выполнено | тест: `apps/server/internal/loadseed/loadseed_test.go` |
| NFR-013 | 01 | выполнено | тест: `apps/server/internal/composition/ir/contract_test.go` |
| NFR-014 | 01 | выполнено | По построению: операции (`operations`) и согласования (`approvals`) не удаляются ни одной командой или задачей; анонимизация (SEC-008) заменяет актора, не удаляя записи |
| NFR-015 | 01 | выполнено | По построению: `security_events` не удаляется ни одной командой или задачей; анонимизация (SEC-008) сохраняет записи |
| NFR-016 | 01 | отложено | [ADR-0003](../adr/0003-unmeasured-performance.md) |
| NFR-017 | 01 | выполнено | тест: `scripts/bundle-size.test.mjs` |
| NFR-018 | 01 | выполнено с отступлением | [ADR-0002](../adr/0002-observability-scope.md) |

## 02 IR

| ID | Глава | Статус | Доказательство |
|---|---|---|---|
| IR-001 | 02 | выполнено | тест: `apps/server/internal/changes/changes_test.go` |
| IR-002 | 02 | выполнено | По построению: Studio и агент меняют IR операциями (IR-003); генерации JSX ↔ IR в коде нет |
| IR-003 | 02 | выполнено | тест: `apps/server/internal/changes/changes_test.go` |
| IR-004 | 02 | выполнено | тест: `packages/ir/fixtures/invalid/design-css-string.json` |
| IR-005 | 02 | выполнено | тест: `apps/server/internal/composition/ir/conformance_test.go`, `apps/server/internal/composition/manifestdoc/validate_test.go` |
| IR-010 | 02 | выполнено | тест: `packages/ir/fixtures/invalid/root-missing.json` |
| IR-011 | 02 | выполнено | тест: `packages/ir/fixtures/invalid/id-mismatch.json` |
| IR-012 | 02 | выполнено | тест: `packages/ir/fixtures/invalid/multiple-parents-traversal-order.json`, `packages/ir/fixtures/invalid/multiple-parents.json` |
| IR-013 | 02 | выполнено | тест: `packages/ir/fixtures/invalid/cycle.json`, `packages/ir/fixtures/invalid/root-as-child.json` |
| IR-014 | 02 | выполнено | тест: `apps/server/internal/composition/ir/contract_test.go` |
| IR-015 | 02 | выполнено | тест: `packages/ir/fixtures/invalid/content-on-component.json`, `packages/ir/fixtures/invalid/inputs-on-page.json` |
| IR-020 | 02 | выполнено | тест: `apps/server/internal/composition/bindingdoc/validate_test.go`, `apps/server/internal/composition/manifestdoc/validate_test.go` |
| IR-021 | 02 | выполнено | тест: `apps/server/internal/composition/manifestdoc/validate_test.go` |
| IR-022 | 02 | выполнено | тест: `apps/studio/test/structure.test.ts` |
| IR-023 | 02 | выполнено | тест: `apps/server/internal/composition/ir/types_test.go` |
| IR-030 | 02 | выполнено | тест: `apps/server/internal/composition/ops/ops_test.go` |
| IR-031 | 02 | выполнено | тест: `apps/server/internal/composition/manifestdoc/validate_test.go` |
| IR-032 | 02 | выполнено | тест: `apps/server/internal/composition/manifestdoc/validate_test.go`, `packages/ir/fixtures/invalid/responsive-without-base.json` |
| IR-033 | 02 | выполнено | тест: `packages/ir/fixtures/invalid/breakpoint-reserved-name.json` |
| IR-040 | 02 | выполнено | тест: `apps/server/internal/composition/bindingdoc/validate_test.go`, `examples/react-store/e2e/store.spec.ts` |
| IR-041 | 02 | выполнено | тест: `apps/server/internal/changes/local_test.go`, `apps/server/internal/composition/a11ydoc/a11y_test.go` |
| IR-042 | 02 | выполнено | тест: `apps/server/internal/composition/bindingdoc/validate_test.go`, `apps/server/internal/composition/validation/bindings_test.go` |
| IR-043 | 02 | выполнено | тест: `apps/server/internal/composition/bindingdoc/validate_test.go` |
| IR-044 | 02 | выполнено | тест: `apps/server/internal/delivery/primary_test.go` |
| IR-045 | 02 | выполнено | тест: `packages/runtime/test/plan.test.ts` |
| IR-050 | 02 | выполнено | тест: `apps/server/internal/composition/bindingdoc/validate_test.go`, `examples/react-store/e2e/store.spec.ts` |
| IR-051 | 02 | выполнено | тест: `apps/server/internal/composition/manifestdoc/validate_test.go`, `packages/runtime/test/actions-data.test.ts` |
| IR-052 | 02 | выполнено | тест: `apps/server/internal/composition/bindingdoc/validate_test.go`, `apps/server/internal/composition/validation/bindings_test.go` |
| IR-053 | 02 | выполнено | тест: `apps/server/internal/capabilities/fulfillment_failure_test.go`, `apps/server/internal/composition/bindingdoc/validate_test.go` |
| IR-060 | 02 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| IR-061 | 02 | выполнено | тест: `apps/server/internal/composition/manifestdoc/validate_test.go`, `apps/server/internal/composition/validation/validation_test.go` |
| IR-062 | 02 | выполнено | тест: `apps/server/internal/composition/validation/limits_test.go`, `apps/server/internal/composition/validation/validation_test.go` |
| IR-063 | 02 | выполнено | тест: `apps/server/internal/composition/policydoc/policy_test.go`, `apps/server/internal/composition/validation/policy_test.go` |
| IR-070 | 02 | выполнено | тест: `packages/ir/fixtures/invalid/version-unsupported.json` |
| IR-071 | 02 | выполнено | Процесс: IR 1.0 — единственная версия; [schema](../../packages/ir/schema) и правила MINOR в [02 §12](02-ir.md); повышений версии не было |
| IR-072 | 02 | отложено | [ADR-0004](../adr/0004-ir-versioning.md) |
| IR-073 | 02 | отложено | [ADR-0004](../adr/0004-ir-versioning.md) |

## 03 Дизайн-система

| ID | Глава | Статус | Доказательство |
|---|---|---|---|
| DS-001 | 03 | выполнено | тест: `packages/manifest/fixtures/invalid/schema-bad-token-name.json`, `packages/manifest/test/builtins.test.ts` |
| DS-002 | 03 | выполнено | тест: `packages/runtime/test/design.test.ts` |
| DS-003 | 03 | выполнено | Формат manifest допускает `modes` у цветовых токенов ([schema](../../packages/manifest/schema)); темы в Studio — после MVP (Q-01, D-23) |
| DS-010 | 03 | выполнено | тест: `packages/react/test/page.test.ts` |
| DS-011 | 03 | выполнено | тест: `packages/react/test/page.test.ts` |
| DS-012 | 03 | отложено | [ADR-0014](../adr/0014-primitive-override.md) |
| DS-020 | 03 | выполнено | тест: `apps/server/internal/composition/policydoc/policy_test.go`, `apps/server/internal/composition/validation/policy_test.go` |
| DS-021 | 03 | выполнено | тест: `apps/server/internal/composition/validation/policy_test.go` |
| DS-022 | 03 | выполнено | тест: `apps/studio/test/structure.test.ts` |
| DS-023 | 03 | выполнено | тест: `apps/server/internal/composition/ops/policy_test.go` |
| DS-030 | 03 | выполнено | тест: `examples/react-store/e2e/store.spec.ts`, `examples/react-store/test/composition.test.ts` |
| DS-031 | 03 | выполнено | тест: `examples/react-store/e2e/store.spec.ts` |
| DS-032 | 03 | выполнено | тест: `packages/runtime/test/design.test.ts` |

## 04 Manifest

| ID | Глава | Статус | Доказательство |
|---|---|---|---|
| MF-001 | 04 | выполнено | тест: `apps/server/internal/composition/manifest/builtins_test.go`, `apps/server/internal/composition/manifest/pattern_test.go` |
| MF-002 | 04 | выполнено | тест: `apps/server/internal/composition/manifest/diff_test.go`, `apps/server/internal/composition/manifest/unicode_test.go` |
| MF-003 | 04 | выполнено | тест: `apps/server/internal/composition/manifestdoc/validate_test.go`, `apps/server/internal/composition/validation/validation_test.go` |
| MF-004 | 04 | выполнено | тест: `apps/server/internal/httpapi/server_test.go`, `apps/server/internal/manifestregistry/registry_test.go` |
| MF-005 | 04 | выполнено | тест: `examples/react-store/e2e/store.spec.ts`, `examples/react-store/test/http.test.ts` |
| MF-010 | 04 | выполнено | тест: `examples/react-store/test/composition.test.ts`, `packages/component-registry/test/dsl.test.ts` |
| MF-011 | 04 | выполнено | тест: `packages/component-registry/test/cli.test.ts`, `packages/component-registry/test/operations.test.ts` |
| MF-012 | 04 | выполнено | тест: `packages/component-registry/test/dsl.test.ts`, `packages/component-registry/test/operations.test.ts` |
| MF-020 | 04 | выполнено | тест: `apps/server/internal/composition/manifest/diff_test.go`, `apps/server/internal/composition/manifest/usage_test.go` |
| MF-021 | 04 | выполнено | тест: `apps/server/internal/composition/manifest/diff_test.go`, `apps/server/internal/manifestregistry/registry_test.go` |
| MF-022 | 04 | выполнено | тест: `apps/server/internal/manifestregistry/registry_test.go` |
| MF-023 | 04 | выполнено | тест: `apps/server/internal/composition/manifestdoc/validate_test.go`, `apps/server/internal/composition/validation/policy_test.go` |
| MF-024 | 04 | выполнено | тест: `packages/runtime/test/plan.test.ts` |
| MF-025 | 04 | выполнено | тест: `apps/server/internal/composition/manifest/diff_test.go`, `apps/server/internal/manifestregistry/registry_test.go` |
| MF-026 | 04 | выполнено | тест: `apps/server/internal/manifestregistry/registry_test.go` |
| MF-027 | 04 | выполнено | тест: `apps/server/internal/composition/validation/bindings_test.go`, `apps/server/internal/composition/validation/validation_test.go` |
| MF-030 | 04 | выполнено | тест: `apps/server/internal/capabilities/preview_test.go` |
| MF-031 | 04 | выполнено | тест: `apps/server/internal/publishing/publishing_test.go` |
| MF-032 | 04 | выполнено | тест: `apps/server/internal/capabilities/preview_test.go` |
| MF-033 | 04 | выполнено | тест: `apps/server/internal/manifestregistry/registry_test.go`, `apps/server/internal/platform/postgres/manifest_storage_test.go` |

## 05 Контент

| ID | Глава | Статус | Доказательство |
|---|---|---|---|
| CNT-001 | 05 | выполнено | тест: `packages/manifest/fixtures/invalid/schema-bad-schema-name.json`, `packages/manifest/fixtures/invalid/schema-field-reserved.json` |
| CNT-002 | 05 | выполнено | тест: `apps/server/internal/manifestregistry/registry_test.go`, `apps/server/internal/platform/postgres/manifest_storage_test.go` |
| CNT-003 | 05 | выполнено | тест: `apps/server/internal/changes/content_test.go`, `packages/manifest/fixtures/invalid/unique-on-asset.json` |
| CNT-004 | 05 | выполнено | тест: `apps/server/internal/changes/content_test.go` |
| CNT-010 | 05 | выполнено | тест: `apps/server/internal/changes/content_test.go`, `apps/server/internal/composition/validation/content_values_test.go` |
| CNT-011 | 05 | выполнено | тест: `apps/server/internal/changes/content_test.go`, `apps/server/internal/composition/validation/content_test.go` |
| CNT-012 | 05 | выполнено | тест: `apps/server/internal/changes/content_test.go` |
| CNT-013 | 05 | выполнено | тест: `apps/server/internal/changes/content_test.go`, `apps/server/internal/composition/validation/content_values_test.go` |
| CNT-020 | 05 | выполнено | По построению: `migrateFrom` — декларативные операции над полями в manifest ([04 §2.4](04-manifest.md)), не код; проверка формата — `TestBoundariesAreValid` |
| CNT-021 | 05 | выполнено | тест: `apps/server/internal/schemaflow/schemaflow_test.go` |
| CNT-022 | 05 | выполнено | тест: `apps/server/internal/schemaflow/schemaflow_test.go` |
| CNT-023 | 05 | отложено | [ADR-0005](../adr/0005-schema-upcast.md) |
| CNT-024 | 05 | выполнено | тест: `apps/server/internal/changes/content_test.go` |
| CNT-030 | 05 | выполнено с отступлением | [ADR-0006](../adr/0006-exact-locale.md) |
| CNT-031 | 05 | выполнено с отступлением | [ADR-0006](../adr/0006-exact-locale.md) |
| CNT-032 | 05 | выполнено | тест: `apps/server/internal/reviewdiff/diff_test.go` |
| CNT-040 | 05 | выполнено | тест: `apps/server/internal/assets/content_test.go`, `apps/server/internal/assets/faults_test.go` |
| CNT-041 | 05 | выполнено | тест: `apps/server/internal/assets/formats_test.go` |
| CNT-042 | 05 | выполнено | тест: `apps/server/internal/assets/formats_test.go`, `apps/server/internal/assets/storage_test.go` |
| CNT-043 | 05 | выполнено | тест: `apps/server/internal/assets/content_test.go`, `apps/server/internal/changes/content_test.go` |
| CNT-050 | 05 | выполнено | тест: `apps/server/internal/httpapi/images_test.go`, `apps/server/internal/imagedelivery/integration_test.go` |
| CNT-051 | 05 | выполнено | тест: `apps/server/internal/httpapi/images_test.go`, `apps/server/internal/imagedelivery/integration_test.go` |
| CNT-052 | 05 | выполнено | тест: `packages/runtime/test/image.test.ts`, `packages/sdk-core/test/boundaries.test.ts` |

## 06 Изменения и публикация

| ID | Глава | Статус | Доказательство |
|---|---|---|---|
| CHG-001 | 06 | выполнено | тест: `apps/server/internal/changes/changes_test.go` |
| CHG-002 | 06 | выполнено | тест: `apps/server/internal/changes/changes_test.go` |
| CHG-003 | 06 | выполнено | тест: `apps/server/internal/changes/changes_test.go` |
| CHG-010 | 06 | выполнено | тест: `apps/server/internal/assets/content_test.go`, `apps/server/internal/changes/content_fuzz_test.go` |
| CHG-011 | 06 | выполнено | тест: `apps/server/internal/changes/changes_test.go`, `apps/server/internal/composition/policydoc/policy_test.go` |
| CHG-012 | 06 | выполнено | тест: `apps/server/internal/changes/changes_test.go` |
| CHG-013 | 06 | выполнено | тест: `apps/server/internal/composition/ops/ops_test.go` |
| CHG-020 | 06 | выполнено с отступлением | [ADR-0007](../adr/0007-studio-history-groups.md) |
| CHG-021 | 06 | выполнено с отступлением | [ADR-0007](../adr/0007-studio-history-groups.md) |
| CHG-030 | 06 | выполнено | тест: `apps/server/internal/changes/content_test.go` |
| CHG-031 | 06 | выполнено | тест: `apps/server/internal/changes/content_test.go` |
| CHG-032 | 06 | выполнено с отступлением | [ADR-0008](../adr/0008-explicit-changesets.md) |
| CHG-033 | 06 | отложено | [ADR-0008](../adr/0008-explicit-changesets.md) |
| CHG-034 | 06 | выполнено | тест: `apps/server/internal/composition/validation/bindings_test.go`, `apps/server/internal/composition/validation/validation_test.go` |
| CHG-040 | 06 | выполнено | тест: `apps/server/internal/changes/content_test.go`, `apps/server/internal/composition/validation/policy_test.go` |
| CHG-041 | 06 | выполнено | тест: `apps/server/internal/workflow/rebase_test.go` |
| CHG-042 | 06 | выполнено | тест: `apps/server/internal/httpapi/events_test.go` |
| PUB-001 | 06 | выполнено | тест: `apps/server/internal/composition/validation/a11y_test.go`, `apps/server/internal/composition/zonepolicy/zones_test.go` |
| PUB-002 | 06 | выполнено | тест: `apps/server/internal/composition/validation/a11y_test.go`, `apps/server/internal/workflow/workflow_test.go` |
| PUB-003 | 06 | выполнено | тест: `apps/server/internal/agents/storage_test.go`, `apps/server/internal/composition/validation/a11y_test.go` |
| PUB-004 | 06 | выполнено | тест: `apps/server/internal/composition/validation/a11y_test.go`, `apps/server/internal/composition/validation/validation_test.go` |
| PUB-005 | 06 | выполнено | тест: `apps/server/internal/workflow/studio_test.go` |
| PUB-010 | 06 | выполнено | тест: `apps/server/internal/composition/validation/validation_test.go` |
| PUB-011 | 06 | выполнено | тест: `apps/server/internal/composition/validation/validation_test.go` |
| PUB-012 | 06 | выполнено | тест: `apps/server/internal/composition/validation/validation_test.go` |
| PUB-013 | 06 | выполнено | тест: `apps/server/internal/composition/validation/validation_test.go` |
| PUB-020 | 06 | выполнено | тест: `apps/server/internal/assets/content_test.go`, `apps/server/internal/changes/content_test.go` |
| PUB-021 | 06 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| PUB-022 | 06 | выполнено | тест: `apps/server/internal/publishing/publishing_test.go` |
| PUB-030 | 06 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| PUB-031 | 06 | выполнено | тест: `apps/server/internal/changes/content_test.go` |
| PUB-032 | 06 | выполнено | тест: `apps/server/internal/composition/validation/validation_test.go` |
| PUB-033 | 06 | выполнено | тест: `apps/server/internal/changes/content_test.go`, `apps/server/internal/schemaflow/schemaflow_test.go` |
| CHG-050 | 06 | выполнено | тест: `apps/server/internal/reviewdiff/diff_test.go` |
| CHG-051 | 06 | выполнено | тест: `apps/server/internal/publishing/history_test.go` |
| CHG-052 | 06 | выполнено | тест: `apps/server/internal/httpapi/publication_history_test.go` |

## 07 Хранилище

| ID | Глава | Статус | Доказательство |
|---|---|---|---|
| ST-001 | 07 | выполнено | тест: `apps/server/internal/httpapi/cache_test.go` |
| ST-002 | 07 | выполнено | тест: `apps/server/internal/httpapi/isolation_test.go` |
| ST-003 | 07 | выполнено | По построению: тела в JSONB (`object_versions.body`), идентичность, указатели, права, workflow и публикации — в реляционных таблицах ([миграции](../../apps/server/db/migrations)) |
| ST-004 | 07 | выполнено | goose, миграции применяются только вперёд при `cms migrate` ([миграции](../../apps/server/db/migrations)); секции Down — для локального отката |
| OPS-001 | 07 | отложено | [ADR-0001](../adr/0001-production-ops.md) |
| OPS-002 | 07 | отложено | [ADR-0001](../adr/0001-production-ops.md) |

## 08 API

| ID | Глава | Статус | Доказательство |
|---|---|---|---|
| API-001 | 08 | выполнено с отступлением | [ADR-0011](../adr/0011-openapi-per-package.md) |
| API-002 | 08 | выполнено | тест: `apps/server/internal/httpapi/agents_test.go` |
| API-010 | 08 | выполнено | тест: `apps/server/internal/assets/faults_test.go` |
| API-011 | 08 | выполнено с отступлением | [ADR-0012](../adr/0012-reason-for-agents.md) |
| API-012 | 08 | выполнено | тест: `apps/server/internal/httpapi/uploads_test.go` |
| API-030 | 08 | выполнено с отступлением | [ADR-0016](../adr/0016-sdk-resolves-page-dependencies.md) |
| API-031 | 08 | выполнено с отступлением | [ADR-0016](../adr/0016-sdk-resolves-page-dependencies.md) |
| API-032 | 08 | выполнено с отступлением | [ADR-0006](../adr/0006-exact-locale.md) |
| API-033 | 08 | выполнено | тест: `apps/server/internal/httpapi/content_test.go`, `apps/server/internal/httpapi/images_test.go` |
| API-034 | 08 | отложено | [ADR-0013](../adr/0013-not-found-redirect.md) |
| API-040 | 08 | выполнено | тест: `apps/server/internal/httpapi/content_test.go`, `apps/server/internal/httpapi/images_test.go` |
| API-041 | 08 | выполнено | тест: `apps/studio/test/editor.test.ts` |
| API-042 | 08 | выполнено | тест: `apps/server/internal/delivery/preview_test.go` |

## 09 Агент

| ID | Глава | Статус | Доказательство |
|---|---|---|---|
| AGT-001 | 09 | выполнено | тест: `apps/server/internal/agents/agents_test.go`, `apps/server/internal/agents/storage_test.go` |
| AGT-002 | 09 | выполнено | тест: `apps/server/internal/agents/content_risk_test.go` |
| AGT-003 | 09 | выполнено | тест: `apps/server/internal/agents/agents_test.go` |
| AGT-004 | 09 | выполнено | тест: `apps/server/internal/agents/agents_test.go` |
| AGT-010 | 09 | выполнено | тест: `apps/server/internal/agentpolicy/policy_test.go`, `apps/server/internal/httpapi/agents_test.go` |
| AGT-011 | 09 | выполнено | тест: `apps/server/internal/agentpolicy/policy_test.go`, `apps/server/internal/httpapi/agents_test.go` |
| AGT-012 | 09 | выполнено | тест: `apps/server/internal/agents/policy_flow_test.go` |
| AGT-013 | 09 | выполнено | тест: `apps/server/internal/agents/policy_test.go` |
| AGT-020 | 09 | выполнено | тест: `apps/server/internal/agents/agents_test.go` |
| AGT-021 | 09 | выполнено | тест: `apps/server/internal/agents/agents_test.go`, `apps/server/internal/agents/autonomous_test.go` |
| AGT-022 | 09 | выполнено | тест: `apps/server/internal/agents/agents_test.go`, `apps/server/internal/agents/autonomous_test.go` |
| AGT-023 | 09 | выполнено | тест: `apps/server/internal/agents/agents_test.go`, `apps/server/internal/agents/autonomous_test.go` |
| AGT-024 | 09 | выполнено | тест: `apps/server/internal/agents/agents_test.go`, `apps/server/internal/agents/storage_test.go` |
| AGT-030 | 09 | выполнено | тест: `apps/server/internal/httpapi/agents_test.go` |
| AGT-031 | 09 | выполнено | тест: `apps/server/internal/agents/content_risk_test.go`, `apps/server/internal/agents/storage_test.go` |
| AGT-032 | 09 | выполнено | тест: `apps/server/internal/httpapi/agents_test.go` |
| AGT-040 | 09 | выполнено | тест: `apps/server/internal/agents/provider_test.go` |
| AGT-041 | 09 | выполнено | тест: `apps/server/internal/agents/agents_test.go` |
| AGT-042 | 09 | выполнено | тест: `apps/server/internal/httpapi/agent_policy_test.go` |
| AGT-043 | 09 | выполнено | тест: `apps/server/internal/agents/provider_test.go` |
| AGT-050 | 09 | выполнено | тест: `apps/server/internal/agents/provider_test.go` |
| AGT-051 | 09 | выполнено | тест: `apps/server/internal/agents/provider_test.go`, `apps/server/internal/httpapi/agents_test.go` |
| AGT-052 | 09 | выполнено | тест: `apps/server/internal/agents/provider_test.go`, `apps/server/internal/httpapi/agents_test.go` |
| AGT-060 | 09 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| AGT-061 | 09 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| AGT-062 | 09 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| AGT-070 | 09 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts`, `packages/component-registry/test/cli.test.ts` |
| AGT-071 | 09 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts`, `packages/component-registry/test/cli.test.ts` |
| AGT-072 | 09 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts`, `packages/component-registry/test/cli.test.ts` |
| AGT-080 | 09 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| AGT-081 | 09 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |

## 10 Runtime, SDK, preview

| ID | Глава | Статус | Доказательство |
|---|---|---|---|
| SDK-001 | 10 | выполнено | тест: `packages/runtime/test/image.test.ts`, `packages/runtime/test/plan.test.ts` |
| SDK-002 | 10 | выполнено | тест: `packages/sdk-core/test/boundaries.test.ts`, `packages/sdk-core/test/decode.test.ts` |
| SDK-010 | 10 | выполнено с отступлением | [ADR-0009](../adr/0009-app-owned-hydration.md) |
| SDK-011 | 10 | выполнено | тест: `examples/react-store/e2e/store.spec.ts`, `packages/runtime/test/actions-data.test.ts` |
| SDK-012 | 10 | выполнено с отступлением | [ADR-0009](../adr/0009-app-owned-hydration.md) |
| SDK-013 | 10 | выполнено | тест: `examples/react-store/test/composition.test.ts`, `packages/react/test/actions.test.ts` |
| SDK-014 | 10 | выполнено | тест: `examples/react-store/e2e/store.spec.ts`, `packages/react/test/preview-core.test.ts` |
| SDK-015 | 10 | выполнено | тест: `packages/react/test/page.test.ts` |
| SDK-016 | 10 | выполнено | тест: `packages/react/test/rich-text.test.ts` |
| SDK-020 | 10 | выполнено | тест: `packages/sdk-core/test/boundaries.test.ts`, `packages/sdk-core/test/cache-regression.test.ts` |
| SDK-021 | 10 | отложено | [ADR-0010](../adr/0010-no-revalidate-webhook.md) |
| SDK-022 | 10 | выполнено | тест: `apps/server/internal/publishing/purge_test.go` |
| SDK-030 | 10 | выполнено | тест: `apps/studio/test/editor.test.ts`, `examples/react-store/e2e/preview.spec.ts` |
| SDK-031 | 10 | выполнено | тест: `examples/react-store/e2e/preview.spec.ts`, `packages/react/test/preview-core.test.ts` |
| SDK-032 | 10 | выполнено | тест: `apps/studio/test/editor.test.ts`, `examples/react-store/e2e/preview.spec.ts` |
| SDK-033 | 10 | выполнено | тест: `examples/react-store/e2e/preview.spec.ts`, `examples/react-store/test/app.test.ts` |
| SDK-040 | 10 | выполнено | тест: `examples/react-store/e2e/preview.spec.ts`, `examples/react-store/test/preview.test.ts` |
| SDK-041 | 10 | выполнено | тест: `packages/react/test/preview-core.test.ts` |
| SDK-042 | 10 | выполнено | тест: `apps/studio/test/editor.test.ts`, `examples/react-store/e2e/preview.spec.ts` |

## 11 Studio

| ID | Глава | Статус | Доказательство |
|---|---|---|---|
| STU-001 | 11 | выполнено | тест: `apps/server/internal/changes/local_test.go`, `apps/server/internal/httpapi/workspace_test.go` |
| STU-002 | 11 | выполнено | По построению: редактор Studio — собственные компоненты ([apps/studio/src](../../apps/studio/src)); зависимостей page-builder в [package.json](../../apps/studio/package.json) нет |
| STU-003 | 11 | выполнено | тест: `apps/server/internal/changes/local_test.go`, `apps/server/internal/composition/ops/local_test.go` |
| STU-010 | 11 | выполнено | тест: `apps/studio/test/editor.test.ts`, `apps/studio/test/ui.test.tsx` |
| STU-011 | 11 | выполнено | тест: `apps/server/internal/httpapi/json_test.go` |
| STU-020 | 11 | выполнено | тест: `examples/react-store/e2e/studio-structure.spec.ts` |
| STU-021 | 11 | выполнено | тест: `apps/server/internal/changes/local_test.go`, `apps/studio/test/api.test.ts` |
| STU-022 | 11 | выполнено | тест: `apps/studio/test/history-editor.test.ts` |
| STU-023 | 11 | отложено | [ADR-0015](../adr/0015-m4-editor-gaps.md) |
| STU-024 | 11 | выполнено | тест: `apps/studio/test/component-extract-ui.test.tsx` |
| STU-025 | 11 | выполнено | тест: `apps/studio/test/presence.test.tsx`, `examples/react-store/e2e/studio-realtime.spec.ts` |
| STU-026 | 11 | выполнено | тест: `apps/studio/test/editor.test.ts`, `apps/studio/test/ui.test.tsx` |
| STU-030 | 11 | отложено | [ADR-0003](../adr/0003-unmeasured-performance.md) |
| STU-031 | 11 | отложено | [ADR-0003](../adr/0003-unmeasured-performance.md) |
| STU-032 | 11 | выполнено | тест: `apps/studio/test/ui.test.tsx`, `examples/react-store/e2e/studio.spec.ts` |
| STU-033 | 11 | выполнено | тест: `apps/studio/test/ui.test.tsx`, `examples/react-store/e2e/studio.spec.ts` |

## 12 Сервер, безопасность, эксплуатация

| ID | Глава | Статус | Доказательство |
|---|---|---|---|
| OPS-010 | 12 | выполнено | тест: `apps/server/internal/assets/faults_test.go`, `apps/server/internal/publishing/purge_test.go` |
| OPS-011 | 12 | выполнено | тест: `apps/server/internal/httpapi/agents_test.go` |
| SEC-001 | 12 | выполнено | тест: `apps/server/internal/identity/identity_test.go` |
| SEC-002 | 12 | выполнено | тест: `apps/server/internal/auth/rights_test.go` |
| SEC-003 | 12 | выполнено | тест: `apps/studio/test/handler.test.ts` |
| SEC-004 | 12 | выполнено | тест: `apps/server/internal/httpapi/images_test.go` |
| SEC-005 | 12 | выполнено | тест: `apps/server/internal/assets/faults_test.go`, `apps/server/internal/changes/history_test.go` |
| SEC-006 | 12 | выполнено | тест: `apps/server/internal/capabilitywebhook/transport_test.go` |
| SEC-007 | 12 | выполнено | тест: `scripts/security-audit.test.mjs` |
| SEC-008 | 12 | выполнено | тест: `apps/server/internal/app/app_test.go`, `apps/server/internal/identity/security_events_test.go` |
| OPS-020 | 12 | выполнено | тест: `apps/server/internal/httpapi/observability_test.go` |
| OPS-021 | 12 | выполнено | тест: `apps/server/internal/httpapi/observability_test.go` |
| OPS-022 | 12 | выполнено | тест: `examples/react-store/e2e/scenario/mvp.spec.ts` |
| OPS-023 | 12 | выполнено с отступлением | [ADR-0002](../adr/0002-observability-scope.md) |
