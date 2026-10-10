# 04. Manifest приложения и Component Registry

Приложение публикует в CMS manifest: компоненты, примитивы, токены, схемы контента, действия, источники данных, форматтеры и capabilities. На основе manifest CMS строит палитру Studio, формы свойств, валидацию и контекст для агента.

## 1. Жизненный цикл

```
код приложения (defineComponent / defineAction / …)
   └─ cms manifest build ─▶ .cms/manifest.json + .cms/code-index.json
         └─ cms manifest push --env <env> ─▶ команда register-manifest
               └─ diff с активным manifest окружения ─▶ анализ влияния
                     └─ активация (или отказ с отчётом)
```

| ID | Требование |
|---|---|
| MF-001 | Manifest ДОЛЖЕН генерироваться из кода приложения CLI `cms manifest build`; ручное редактирование JSON не поддерживается. |
| MF-002 | Сохранённый manifest неизменяем и идентифицируется SHA-256 канонического JSON (`manifestHash`). |
| MF-003 | У каждого окружения ровно один активный manifest. Валидация и публикация в окружение используют его. |
| MF-004 | Регистрация manifest выполняется командой `register-manifest` от актора `service` с правом `manifest.register`, обычно из CI после деплоя приложения. |
| MF-005 | Manifest ДОЛЖЕН регистрироваться после того, как соответствующая версия приложения развёрнута в окружении. Ответственность за порядок несёт CI; CLI поддерживает флаг `--wait-for-deploy <url>` (ожидание, пока `GET <url>/.well-known/cms-manifest-hash` вернёт хэш). |

## 2. Формат

```json
{
  "manifestVersion": "1.0",
  "app": { "id": "store-web", "version": "2.14.0", "build": "git:3f2a1c9", "framework": "react" },
  "irVersions": ["1.0"],
  "breakpoints": { "sm": 640, "md": 768, "lg": 1024, "xl": 1280 },
  "tokens": {
    "spacing": { "none": "0", "xs": "4px", "sm": "8px", "md": "16px", "lg": "24px", "xl": "40px", "2xl": "64px" },
    "colors": { "surface": "#ffffff", "surfaceAccent": "#f4f1ec", "text": "#1c1b1a", "textMuted": "#6b6762", "primary": "#1f4fd1", "onPrimary": "#ffffff" },
    "typography": {
      "body":    { "fontFamily": "Inter, sans-serif", "fontSize": { "base": "16px" }, "lineHeight": "1.5", "fontWeight": 400 },
      "display": { "fontFamily": "Inter, sans-serif", "fontSize": { "base": "36px", "lg": "56px" }, "lineHeight": "1.1", "fontWeight": 700, "large": true }
    }
  },
  "icons": ["cart", "search", "arrow-right"],
  "primitives": {},
  "components": {},
  "actions": {},
  "dataSources": {},
  "formatters": {},
  "schemas": {},
  "capabilities": {},
  "codeIndex": { "hash": "sha256:…", "uploaded": true }
}
```

### 2.1. Компонент

```json
"ProductCard": {
  "origin": "native",
  "description": "Карточка товара с ценой и изображением",
  "category": "Commerce",
  "container": false,
  "props": {
    "product": { "type": "reference", "schema": "Product", "required": true },
    "variant": { "type": "enum", "values": ["default", "compact"], "default": "default", "responsive": true }
  },
  "slots": { "footer": { "allowedTypes": ["Button", "Link"], "max": 2 } },
  "events": { "select": {} },
  "provides": { "product": { "type": "reference", "schema": "Product" } },
  "capabilities": ["commerce.product.display"],
  "since": "2.10.0",
  "deprecated": null,
  "sourceRef": { "file": "src/components/ProductCard.tsx", "export": "ProductCard", "line": 12 }
}
```

| Поле | Описание |
|---|---|
| `origin` | `native` (написан разработчиком) или `generated` (предложен агентом, прошёл PR). Composed-компоненты в manifest не входят — они живут в CMS. |
| `props` | Типизированные свойства (§4). `content: true` — контентное свойство (IR-041). `responsive: true` — допускает адаптивное значение. |
| `container` | Допускает `children`. |
| `slots` | Именованные слоты с ограничениями. |
| `events` | События, на которые можно назначать действия. |
| `provides` | Значения контекста, доступные потомкам в слотах как `$context.<key>`. |
| `capabilities` | Capabilities приложения, которые реализует компонент. |
| `deprecated` | `{ "since", "replacement", "message" }`. Studio скрывает компонент из палитры, валидатор выдаёт предупреждение. |
| `sourceRef` | Расположение реализации — для структурного контекста агента ([09-agent.md §6](09-agent.md#6-структурный-контекст-репозитория)). |

### 2.2. Действие

```json
"commerce.addToCart": {
  "description": "Добавить товар в корзину",
  "args": {
    "product":  { "type": "reference", "schema": "Product", "required": true },
    "quantity": { "type": "number", "integer": true, "min": 1, "max": 99, "default": 1 }
  },
  "capabilities": ["commerce.cart.add"],
  "sourceRef": { "file": "src/cms/actions/cart.ts", "export": "addToCart" }
}
```

Имя действия: `^[a-z][a-zA-Z0-9]*(\.[a-z][a-zA-Z0-9]*)+$`. Встроенные действия ([02-ir.md §7](02-ir.md#7-поведение-behavior-ir)) зарезервированы.

### 2.3. Источник данных

```json
"commerce.products.list": {
  "description": "Товары коллекции",
  "params": {
    "collection": { "type": "string", "required": true },
    "limit": { "type": "number", "integer": true, "min": 1, "max": 48, "default": 12 }
  },
  "result": { "type": "list", "of": { "type": "object", "fields": {
    "id": { "type": "string" }, "title": { "type": "text" }, "price": { "type": "number" },
    "image": { "type": "asset", "assetKind": "image" }, "slug": { "type": "string" }
  } } },
  "paginated": true,
  "cache": { "ttlSeconds": 60 },
  "sourceRef": { "file": "src/cms/data/products.ts", "export": "listProducts" }
}
```

### 2.4. Схемы контента

**[Решение]** Схемы контента определяются в коде приложения («schema as code») и поставляются в разделе `schemas`. Так изменения схем проходят ревью вместе с кодом, который от них зависит. Регистрация manifest с изменёнными схемами создаёт Change Set со схемными операциями ([05-content.md §4](05-content.md#4-миграции-схем)).

### 2.5. Capability приложения

```json
"commerce.product.display": { "description": "Отображение карточки товара", "domain": "commerce" }
```

Ключ capability — точечное имя. Capability считается доступной в окружении, если она объявлена в активном manifest и хотя бы один компонент или действие на неё ссылается.

### 2.6. Реализация формата (этап M1)

- Формат описан JSON Schema `packages/manifest/schema/manifest-1.0.schema.json` — единственный источник истины. Пакет `@cms/manifest` (TypeScript) и `apps/server/internal/composition/manifest` (Go) валидируют manifest одинаково; совпадение проверяют общие фикстуры `packages/manifest/fixtures` (MF-001).
- Система типов §4 — `$defs/Type` с дискриминатором `type`; модификаторы `required`, `responsive`, `localized`, `unique`, `content`, `description`, `deprecated` допустимы у любого типа, но семантическая проверка ограничивает `content` типами `text`, `richText`, `asset`, `link`, а `localized` и `unique` — полями схем контента верхнего уровня (`unique` — у `string`, `text`, `number`, CNT-003).
- Форматтер (§2, `formatters`): `{ description?, input: [типы входного значения], args?: поля, sourceRef? }`. Операции `migrateFrom` описаны точно по 05 §4.2.
- Семантические проверки выполняются для структурно корректного manifest; коды: `MANIFEST_IR_VERSION_UNSUPPORTED`, `MANIFEST_NAME_RESERVED` (имя встроенного примитива или действия), `MANIFEST_NAME_DUPLICATE` (примитив и компонент с одним именем), `MANIFEST_UNKNOWN_TYPE` (слоты, `nodeRef`), `MANIFEST_UNKNOWN_SCHEMA`, `MANIFEST_UNKNOWN_CAPABILITY`, `MANIFEST_UNKNOWN_BREAKPOINT` (размеры шрифтов), `MANIFEST_UNKNOWN_FIELD` (`display`), `MANIFEST_DEFAULT_INVALID`, `MANIFEST_RANGE_INVALID`, `MANIFEST_MODIFIER_INVALID`, `MANIFEST_FIELD_RESERVED` (CNT-001), `MANIFEST_MIGRATION_INVALID`; нарушения схемы — `MANIFEST_SCHEMA_VIOLATION`. Диагностики отсортированы по указателю и коду.
- `manifestHash` (MF-002) — `sha256:` + SHA-256 канонического JSON: ключи по возрастанию кодовых единиц UTF-16, без пробелов, строки и числа — как у `JSON.stringify`. Обе реализации дают одинаковый хэш на фикстурах (`fixtures/valid/hashes.json`).
- Синтаксис `pattern` в типах `string` не проверяется: регулярные выражения JavaScript и RE2 (Go) различаются; проверка значений по `pattern` появится вместе с валидацией контента.

## 3. API определения в коде

```ts
import { defineComponent, defineAction, defineDataSource, createRegistry, t } from "@cms/component-registry";
import { ProductCard } from "../components/ProductCard";

export const productCard = defineComponent({
  name: "ProductCard",
  component: ProductCard,
  category: "Commerce",
  props: {
    product: t.reference("Product").required(),
    variant: t.enum(["default", "compact"]).default("default").responsive(),
  },
  slots: { footer: t.slot().allow("Button", "Link").max(2) },
  provides: { product: t.reference("Product") },
  capabilities: ["commerce.product.display"],
});

export const addToCart = defineAction({
  name: "commerce.addToCart",
  args: { product: t.reference("Product").required(), quantity: t.number().int().min(1).max(99).default(1) },
  capabilities: ["commerce.cart.add"],
  handler: async ({ product, quantity }, ctx) => ctx.app.cart.add(product.id, quantity),
});

export default createRegistry({
  tokens, breakpoints, icons,
  components: [productCard],
  actions: [addToCart],
  dataSources: [/* … */],
  schemas: [/* defineSchema(...) */],
});
```

| ID | Требование |
|---|---|
| MF-010 | Один и тот же объект registry ДОЛЖЕН использоваться и CLI для генерации manifest, и runtime для рендеринга. Это исключает расхождение manifest и реализации. |
| MF-011 | `sourceRef` ДОЛЖЕН вычисляться CLI автоматически (через TypeScript compiler API), а не задаваться вручную. |
| MF-012 | Типы свойств компонента в TypeScript ДОЛЖНЫ проверяться на соответствие объявлению `props` при компиляции (`defineComponent` выводит тип props из `t.*`). |

## 4. Система типов

Одна система типов используется для свойств компонентов, входов Composed-компонентов, полей схем контента, аргументов действий и параметров/результатов источников данных.

| Тип | Параметры | Значение в JSON |
|---|---|---|
| `string` | `minLength`, `maxLength`, `pattern` | строка (не контент, не локализуется) |
| `text` | `maxLength`, `multiline` | строка (контент) |
| `richText` | `marks`, `blocks` | RichText v1 ([05-content.md §3](05-content.md#3-rich-text)) |
| `number` | `min`, `max`, `integer` | число |
| `boolean` | — | `true`/`false` |
| `enum` | `values` | строка из `values` |
| `date`, `datetime` | — | `YYYY-MM-DD` / RFC 3339 |
| `url` | `schemes` | строка |
| `link` | — | Link ([02-ir.md §7](02-ir.md#7-поведение-behavior-ir)) |
| `asset` | `assetKind: image\|video\|file`, `mimeTypes` | `{ "assetId", "alt"? }` |
| `reference` | `schema` | `{ "entityId" }`; при разрешении — сущность |
| `list` | `of`, `min`, `max` | массив |
| `object` | `fields` | объект |
| `color` | — | имя цветового токена |
| `nodeRef` | `nodeType` | NodeId в том же документе |

Общие модификаторы: `required`, `default`, `content` (только для `text`, `richText`, `asset`, `link`), `responsive`, `localized` (поля схем), `description`.

### 4.1. Совместимость при привязке

| Источник → цель | Допустимо |
|---|---|
| `text` ↔ `string` | да |
| `number`, `date`, `datetime` → `text`/`string` | да; без `format` применяется форматтер по умолчанию |
| `richText` → `text` | нет |
| `url` → `link` | да (как `{kind: "url"}`) |
| `reference(S)` → `reference(S)` | да; разные схемы — нет |
| `asset(K)` → `asset(K)` | да; разные `assetKind` — нет |
| `list(T)` → `Repeat.items` | да; `$item` имеет тип `T` |
| `enum` → `enum` | если значения источника ⊆ значений цели |
| `null` / отсутствующее значение | допустимо для необязательных; для обязательных — предупреждение в черновике, ошибка при публикации, если путь может отсутствовать и нет `default` |

## 5. Совместимость версий manifest

### 5.1. Классификация изменений

| Изменение | Класс |
|---|---|
| Добавлен компонент, действие, источник, токен, необязательное свойство, слот, событие, breakpoint | совместимое |
| Свойство помечено `deprecated` | совместимое |
| Изменено значение токена | совместимое (влияет на визуал; отмечается в отчёте) |
| Удалён компонент, действие, источник данных, токен, свойство, значение enum, слот, событие, breakpoint | несовместимое |
| Свойство стало обязательным; сужен тип или диапазон | несовместимое |
| Изменён тип результата источника данных (удалено поле) | несовместимое |
| Удалена поддерживаемая версия IR | несовместимое |

### 5.2. Правила активации

| ID | Требование |
|---|---|
| MF-020 | При регистрации CMS ДОЛЖНА вычислять diff с активным manifest окружения и анализ влияния: какие документы и версии (head и published в этом окружении) используют затронутые элементы. |
| MF-021 | Совместимый manifest активируется автоматически. |
| MF-022 | Несовместимый manifest, затрагивающий хотя бы один опубликованный в окружении документ, ДОЛЖЕН отклоняться с отчётом `MANIFEST_BREAKING_IN_USE`. Удаление выполняется в две фазы: (1) `deprecated` + миграционный Change Set, убирающий использование; (2) удаление в следующей версии приложения. |
| MF-023 | Несовместимый manifest, затрагивающий только черновики, активируется; затронутые открытые Change Set'ы получают диагностику и статус `needs_attention`. |
| MF-024 | Runtime ДОЛЖЕН безопасно обрабатывать неизвестный тип узла или действие: не рендерить узел, не падать, отправить диагностику в телеметрию и (в preview) в Studio. |

## 6. Окружения preview

| ID | Требование |
|---|---|
| MF-030 | Сборки приложения для preview (например, ветка pull request) МОГУТ регистрировать manifest в эфемерное окружение типа `preview` с именем `preview/<ref>`. |
| MF-031 | В окружение типа `preview` нельзя публиковать. Оно служит для просмотра черновиков, в том числе использующих capability из ещё не слитого PR. |
| MF-032 | Эфемерное окружение удаляется командой `delete-environment` из CI или автоматически через 14 дней без регистраций. |
