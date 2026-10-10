# Conformance-фикстуры manifest

Общий набор примеров для всех реализаций валидатора manifest (TypeScript — `@cms/manifest`, Go — `apps/server/internal/composition/manifest`). Формат файлов — как у фикстур IR (`packages/ir/fixtures`).

## `valid/`

Каждый файл — manifest. Валидатор ДОЛЖЕН вернуть пустой список диагностик. `hashes.json` — ожидаемый `manifestHash` (MF-002) каждого файла: обе реализации канонического JSON должны давать его.

## `invalid/`

```json
{
  "description": "Что нарушено и какое требование спецификации",
  "expect": [{ "code": "MANIFEST_UNKNOWN_SCHEMA", "pointer": "/components/Card/props/product/schema" }],
  "manifest": { "...": "..." }
}
```

Валидатор ДОЛЖЕН признать manifest невалидным и для каждого элемента `expect` вернуть диагностику с этим `code` и `pointer`. Дополнительные диагностики допустимы.

## Правила

- Один файл — одно правило (нарушение может встречаться в нескольких местах). Имя файла описывает нарушение в kebab-case.
- Новая проверка добавляется вместе с фикстурами.
- Полный результат валидации (тексты, params, порядок, отсутствие лишних диагностик) по всем invalid-фикстурам фиксируют снимки: `packages/manifest/test/__snapshots__/golden.json` (TS) и `apps/server/internal/composition/schemadiag/testdata/golden.json` (Go). После изменения фикстур или сообщений снимки обновляются командами `pnpm vitest run -u` и `go test ./internal/composition/schemadiag -run TestGolden -update`. Семантические диагностики в снимках TS и Go совпадают полностью; тексты `MANIFEST_SCHEMA_VIOLATION` различаются — их дают разные библиотеки JSON Schema.
