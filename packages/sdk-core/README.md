# @cms/core

Первый пакет: framework-neutral клиент существующего Delivery API. Совместим
с Node.js 22.12+ и средами со стандартными Fetch, AbortController, structuredClone.
React и renderer не требуются.

```ts
import { createCmsClient, CmsError } from "@cms/core";

const cms = createCmsClient({
  baseUrl: "http://127.0.0.1:18080",
  project: "local-demo",
  environment: "staging",
  deliveryKey: process.env.CMS_DELIVERY_KEY!,
});

try {
  const page = await cms.getPage("/products/shirt"); // null on 404 NOT_FOUND
  const routes = await cms.getRoutes();
  console.log(page?.document.root, routes.items);
} catch (error) {
  if (error instanceof CmsError) console.error(error.code, error.status);
  else throw error;
}
```

Токены передавайте из защищённой конфигурации приложения. Клиент не выпускает
токены и не вызывает Command API. `baseUrl` — HTTP(S) origin без пути, query,
fragment или credentials. Один клиент обслуживает один проект/окружение и
published ключ; для других создайте другой экземпляр.

Методы: `getPage`, `getDocument`, `getEntity`, `getAsset`, `getRoutes`, `getImage`,
`clearCache`. Все методы чтения принимают `{signal?, preview?: {token, changesetId?}}`.

```ts
const draft = await cms.getPage("/", {
  preview: { token: previewToken, changesetId },
  signal: abortController.signal,
});
const model = await cms.getImage(assetId, {
  fmt: "webp", fit: "cover", w: 640, h: 480,
});
// model can be passed to @cms/runtime Image; refresh before expiresAt.
```

Опубликованные ответы: LRU 500 записей / 60 секунд; после TTL — условный GET,
304 использует проверенное тело. `{cache:false}` отключает кэш;
`cache:{capacity,ttlMs}` изменяет лимиты. Preview и image model никогда не
используют этот кэш. `clearCache` запрещает начатым запросам заполнить его снова.
Ответы копируются. Ошибки не возвращают старое тело. Отзыв published ключа
обнаруживается при следующем запросе к серверу, а не на локальном cache hit.

Транспорт: timeout 10 секунд, максимум 8 MiB (настраиваются `timeoutMs` /
`maxResponseBytes`), cookies не отправляются, redirects запрещены. `fetch` и
`now` можно внедрить для тестов. Клиент не повторяет запросы автоматически.

`CmsError`: серверные `code`/`status`, либо `INVALID_ARGUMENT`, `INVALID_RESPONSE`,
`IR_INVALID`, `NETWORK_ERROR`, `TIMEOUT`, `ABORTED`, `RESPONSE_TOO_LARGE`.
Тексты сервера, URL и секреты в ошибки не копируются. `IR_INVALID` содержит
диагностики существующего валидатора. Невалидный preview-документ также
отклоняется; Studio для редактирования будет использовать свой Query API.

Локаль/fallback не эмулируются; карты локализованного контента остаются данными.
Вычисление привязок/условий, форматтеры, registry/data loaders, renderer, React,
SSR и preview bridge остаются следующими пакетами.

Контракт и границы: [docs/sdk-core-delivery.md](../../docs/sdk-core-delivery.md).
