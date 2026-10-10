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

- Один файл — одно нарушение. Имя файла описывает нарушение в kebab-case.
- Новая проверка добавляется вместе с фикстурами.
