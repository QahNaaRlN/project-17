# 10. Runtime, SDK и Preview

## 1. Пакеты

| Пакет | Каталог | Ответственность | MVP |
|---|---|---|---|
| `@cms/ir` | `packages/ir` | JSON Schema IR, сгенерированные типы TS, conformance-фикстуры | да |
| `@cms/core` | `packages/sdk-core` | Клиент Delivery API, парсер и валидатор IR, вычисление привязок и условий, форматтеры | да |
| `@cms/runtime` | `packages/design-runtime` | Обход дерева, раскрытие `Repeat`, генерация стилей из дизайн-свойств, контракты примитивов | да |
| `@cms/component-registry` | `packages/component-registry` | `defineComponent`/`defineAction`/…, CLI `cms manifest build|push`, индекс кода | да |
| `@cms/react` | `packages/sdk-react` | Адаптер React: примитивы, `CmsPage`, `loadPage`, SSR, мост preview | да |
| `@cms/vue`, `@cms/svelte` | `packages/sdk-vue`, `packages/sdk-svelte` | Адаптеры | после MVP |
| `@cms/mcp` | `packages/mcp` | Описания инструментов MCP, stdio-прокси | да |

| ID | Требование |
|---|---|
| SDK-001 | `@cms/core` и `@cms/runtime` НЕ ДОЛЖНЫ зависеть от фреймворка. Адаптер реализует только отображение «примитив/компонент → элемент фреймворка» и жизненный цикл. |
| SDK-002 | Пакеты публикуются с одинаковой версией (fixed versioning); `@cms/core` проверяет совместимость `irVersion` документа. |

## 2. Использование в приложении (React)

```tsx
import { createCmsClient } from "@cms/core";
import { loadPage, CmsPage, getPreviewState } from "@cms/react/server";
import registry from "@/cms/registry";

const cms = createCmsClient({
  baseUrl: process.env.CMS_URL!,
  project: "store",
  environment: process.env.CMS_ENV!,             // staging | production
  deliveryKey: process.env.CMS_DELIVERY_KEY!,
});

export default async function Route({ params }: { params: { path: string[] } }) {
  const preview = await getPreviewState();          // из cookie, установленной /api/cms/preview
  const page = await loadPage(cms, registry, {
    path: "/" + params.path.join("/"),
    locale: "ru",
    preview,
    context: { viewer: { authenticated: false } },
  });
  if (!page) return notFound();
  return <CmsPage page={page} registry={registry} />;
}
```

## 3. Рендеринг

### 3.1. `loadPage`

1. Запрос к Delivery API (published или draft).
2. Проверка `requires`: все компоненты и действия есть в локальном registry; несовпадение `manifestHash` — предупреждение в телеметрию.
3. Вычисление параметров источников данных и параллельный вызов загрузчиков registry (таймаут по умолчанию 3 с на источник).
4. Возврат сериализуемой модели страницы (для передачи с сервера на клиент).

### 3.2. Обход дерева

Для каждого узла, начиная с `root`:

1. Вычислить `when`; если ложно — пропустить поддерево.
2. Вычислить привязки в текущей области (`$content`, `$item`, `$props`, `$context`, …).
3. Для `Repeat` — повторить шаблон для каждого элемента (до `limit`) с новой областью `$item`/`$index`; при пустом списке — слот `empty`.
4. Для `Composed` — отрендерить документ компонента с областью `$props` из входов экземпляра; слоты — узлы экземпляра.
5. Дизайн-свойства → атомарные классы (DS-030); свойства → props элемента.
6. Обработчики `on` → функции, вызывающие действия registry или встроенные действия.

| ID | Требование |
|---|---|
| SDK-010 | Рендеринг ДОЛЖЕН работать при SSR и потоковой отдаче. Гидратируются только узлы с обработчиками событий или состоянием (`Modal`, `Video`, интерактивные компоненты). |
| SDK-011 | Ключ React-элемента — ID узла; для экземпляров `Repeat` — `<nodeId>:<key элемента или индекс>`. |
| SDK-012 | Ошибка рендеринга экземпляра Composed-компонента или native-компонента ДОЛЖНА изолироваться (error boundary) и не ломать страницу. |
| SDK-013 | Неизвестный тип или действие — узел не рендерится, отправляется диагностика (MF-024). |
| SDK-014 | Ошибка источника данных: `$data.<key>` получает `{ "error": { "code" } }`, условия `exists` по его полям ложны, `Repeat` рендерит `empty`. |
| SDK-015 | Значения `url`/`link` ДОЛЖНЫ проверяться по белому списку схем перед выводом в `href`/`src` (IR-045). |
| SDK-016 | Rich text ДОЛЖЕН рендериться из структуры RichText v1 без `dangerouslySetInnerHTML`. |

### 3.3. Действия

```ts
defineAction({
  name: "commerce.addToCart",
  args: { /* … */ },
  handler: async (args, ctx) => { await ctx.app.cart.add(args.product.id, args.quantity); },
});
```

`ctx` содержит: `app` (сервисы приложения, переданные в `CmsPage`), `navigate`, `openModal`, `closeModal`, `track`, `node` (ID узла-источника). Ошибка действия передаётся в обработчик `onActionError` приложения; цепочка прерывается (IR-050).

## 4. Кэширование и инвалидация в приложении

| ID | Требование |
|---|---|
| SDK-020 | Клиент `@cms/core` ДОЛЖЕН использовать `ETag`/`If-None-Match` и in-memory LRU (по умолчанию 500 записей, 60 с) для published-ответов. В черновом режиме кэш не используется. |
| SDK-021 | После публикации CMS отправляет подписанный (HMAC-SHA256) webhook `publication.created` на `revalidateUrl` окружения: `{ publicationId, environment, paths[], objectIds[] }`. `@cms/react` предоставляет обработчик для фреймворков с серверным кэшем страниц. |
| SDK-022 | CDN-инвалидация Delivery API выполняется CMS по `Surrogate-Key` (API-033) из задачи после публикации. |

## 5. Preview

### 5.1. Подключение

1. Studio получает preview-токен (`create-preview-token`) и открывает в iframe `{appUrl}/api/cms/preview?token=…&path=…`.
2. Обработчик приложения (из `@cms/react`) проверяет токен в CMS, устанавливает cookie `cms_preview` (`HttpOnly; Secure; SameSite=None; Partitioned`, 15 мин) и перенаправляет на `path`.
3. Страница в режиме preview рендерится на сервере из черновика, затем на клиенте подключается мост preview, который обменивается сообщениями со Studio через `postMessage`.

| ID | Требование |
|---|---|
| SDK-030 | В режиме preview DOM-элементы узлов ДОЛЖНЫ иметь атрибут `data-cms-node="<nodeId>"`, экземпляры `Repeat` — `data-cms-instance="<nodeId>:<n>"`. В обычном режиме атрибуты не выводятся. |
| SDK-031 | Мост ДОЛЖЕН принимать сообщения только от origin Studio из конфигурации приложения (`CMS_STUDIO_ORIGIN`); Studio — только от origin `appUrl` окружения. |
| SDK-032 | Приложение ДОЛЖНО разрешать встраивание в iframe только с origin Studio (`Content-Security-Policy: frame-ancestors <studio-origin>`) и только в режиме preview. |
| SDK-033 | По умолчанию preview работает в режиме «Дизайн»: клики выбирают узлы, действия не исполняются. Режим «Взаимодействие» исполняет действия против бэкенда окружения; Studio предупреждает о побочных эффектах. |

### 5.2. Протокол сообщений

Конверт:

```json
{ "cms": "preview", "v": 1, "type": "select", "id": "m-42", "payload": { "nodeId": "n_card01" } }
```

**Studio → приложение**

| Тип | Payload | Назначение |
|---|---|---|
| `init` | `sessionId`, `changesetId`, `locale`, `mode: design\|interact` | Начало сессии |
| `document.patch` | `documentId`, `baseHash`, `patch` (JSON Patch над нормализованным документом), `hash` | Быстрое применение дизайн- и структурных изменений без запроса к серверу |
| `invalidate` | `objectIds`, `seq` | Перезапросить черновик (изменения контента, привязок, источников данных) |
| `select` / `hover` | `nodeId \| null` | Подсветка выбранного / наведённого узла |
| `navigate` | `path` | Переход на другую страницу |
| `mode` | `design \| interact` | Смена режима |
| `zones.show` | `enabled` | Показ границ зон и их режимов |
| `token.refresh` | — | Сигнал обновить cookie preview |

**Приложение → Studio**

| Тип | Payload | Назначение |
|---|---|---|
| `ready` | `protocolVersion`, `sdkVersion`, `manifestHash`, `path`, `documentId` | Мост готов |
| `rendered` | `hash`, `durationMs` | Отрисована версия документа |
| `node.click` | `nodeId`, `instance?`, `modifiers` | Выбор узла на холсте |
| `node.hover` | `nodeId \| null` | Наведение |
| `layout` | `{ nodeId: [{x, y, w, h}] }`, `viewport`, `scroll` | Геометрия узлов для оверлеев Studio (не чаще раза в 60 мс) |
| `diagnostics` | `items[]` | Ошибки и предупреждения runtime |
| `navigated` | `path`, `documentId` | Пользователь перешёл по ссылке |
| `error` | `code`, `message` | Ошибка моста |

| ID | Требование |
|---|---|
| SDK-040 | Если `baseHash` из `document.patch` не совпадает с текущим хэшем документа в приложении, мост ДОЛЖЕН запросить полный черновик (как при `invalidate`). |
| SDK-041 | Версия протокола согласуется в `init`/`ready`; при несовместимости Studio показывает требование обновить SDK. |
| SDK-042 | Оверлеи выбора, рамки и ручки перетаскивания рисует Studio поверх iframe по данным `layout`; приложение не встраивает UI редактора. |

### 5.3. Viewport

Studio меняет ширину iframe (пресеты по breakpoint'ам manifest + произвольная ширина). Активный breakpoint определяет, какой ключ адаптивного значения редактирует панель дизайна.
