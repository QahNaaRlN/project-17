# @cms/ir

Формат IR 1.0 — типизированного промежуточного представления UI ([спецификация](../../docs/spec/02-ir.md)).

| Что | Где |
|---|---|
| JSON Schema — единственный источник истины (IR-005) | [`schema/ir-1.0.schema.json`](schema/ir-1.0.schema.json) |
| Типы TypeScript и схема как модуль (генерируются) | `src/generated/` |
| Структурный валидатор (уровень L1) | `validateDocument` |
| Вложенная форма ↔ нормализованная | `normalize`, `toNested` |
| Генерация ID узлов | `generateNodeId`, `isNodeId` |
| Conformance-фикстуры для всех реализаций | [`fixtures/`](fixtures/) |

## Использование

```ts
import { normalize, validateDocument, type IrDocument } from "@cms/ir";

const doc: IrDocument = normalize({
  irVersion: "1.0",
  kind: "page",
  root: { type: "Stack", children: [{ type: "Heading", bindings: { text: "$content.title" } }] },
});

const { valid, diagnostics } = validateDocument(doc);
// diagnostics: [{ code, severity, pointer, nodeId?, message, params? }]
```

## Что проверяет `validateDocument`

- версию IR (IR-070);
- JSON Schema: форма документа и узлов, синтаксис привязок и условий, токены и сырые значения, адаптивные значения, ссылки, зоны;
- инварианты дерева: корень существует, ключ равен `id`, нет ссылок на отсутствующие узлы, у узла один родитель, нет циклов и сирот (IR-010…013);
- ограничения: 5 000 узлов, глубина 32, 2 МиБ, глубина условия 8 (IR-014);
- свойство не задано одновременно в `props` и `bindings` (IR-020).

Проверки по manifest, привязкам, дизайн-системе и политикам (уровни L2–L7) сюда не входят — они требуют контекста проекта.

## Разработка

```bash
pnpm --filter @cms/ir gen    # пересоздать src/generated после изменения схемы
pnpm --filter @cms/ir test
```

При изменении схемы добавляйте фикстуры в `fixtures/valid` и `fixtures/invalid`.
