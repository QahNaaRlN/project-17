# Первый пакет @cms/core: Delivery client

Контракт SDK-001/002/020, API-033/040/042. Пакет не зависит от React или DOM
рендеринга; использует стандартный Fetch API и существующий валидатор `@cms/ir`.

## Объём

- `createCmsClient({baseUrl, project, environment, deliveryKey?, fetch?, cache?, timeoutMs?, maxResponseBytes?, now?})`.
- `getPage(path, options?)`, `getDocument(id, options?)`, `getEntity(id, options?)`,
  `getAsset(id, options?)`, `getRoutes(options?)`, `getImage(id, imageOptions?)`.
- В options: `preview: {token, changesetId?}`, `signal`. Preview использует
  `Authorization: Preview`; published — `Bearer`. Клиент не создаёт токены.
- `getImage` принимает `fmt`, `fit`, `w`, `h` по существующему серверному контракту.
- Страница/документ/контент с `404 NOT_FOUND` возвращает `null`; остальные отказы
  бросают `CmsError` с `code`, `status`, `diagnostics` (для невалидного IR).
- Ответы проверяются до передачи потребителю; документы и Composed документы
  проверяются `validateDocument`, включая `irVersion`. Черновик с невалидным IR
  отклоняется; диагностики доступны в ошибке. Это клиент чтения для runtime,
  не интерфейс редактирования невалидного черновика Studio.
- Маркеры локали не отправляются: locale/fallback ещё не реализованы сервером.

## Кэш и транспорт

Published LRU принадлежит экземпляру клиента и фиксированному ключу/проекту/
окружению; по умолчанию 500 записей и 60 секунд. В пределах TTL возвращается
копия проверенного ответа, после TTL — условный GET с ETag, 304 обновляет TTL.
`clearCache()` очищает записи и запрещает уже начатым запросам заполнить кэш.
На ошибках нет stale fallback. Кэш не подтверждает немедленный отзыв ключа:
до истечения TTL уже полученные published данные доступны локально.

Preview и image model обходят кэш полностью, не используют If-None-Match.
`no-store`/`private` ответа запрещают сохранение в published LRU. Подписанные URL
потребитель обновляет по `expiresAt`; клиент не загружает бинарные изображения.
Результаты копируются, чтобы изменения вызывающего кода не меняли кэш.

Fetch: GET, credentials omit, redirect error, cache no-store. Bearer/preview
не передаются в URL; ошибки клиента не включают секреты, тела ответа или URL.
Есть таймаут, AbortSignal и ограничение ответа (по умолчанию 8 MiB), включая
stream без Content-Length. Успешные ответы обязаны быть JSON.

## Следующие пакеты

Вычисление привязок/условий, форматтеры, registry и загрузчики источников данных,
renderer/React/SSR, preview bridge и Studio остаются отдельными шагами.
Внешний CDN и production-развёртывание этим пакетом не настраиваются.
