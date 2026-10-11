# React-витрина «Тихая форма»

Первый рабочий интерфейс по варианту «Каталог в центре»: React 19, TypeScript,
Vite, Tailwind и собственные компоненты Button/Dialog с Radix. Контент читается
сервером через `@cms/core`; композиция отображается через `@cms/react`, изображения — через `@cms/runtime`.
Секреты CMS остаются на сервере. Кнопка «Обновить» перечитывает CMS без SDK-кэша.

## Запуск

Из корня репозитория: `pnpm install --frozen-lockfile`, затем `pnpm build`.
Нужны Node >=22.12 и запущенная локальная CMS с PostgreSQL, S3 и imgproxy.
Настройку CMS см. в [документации ассетов](../../docs/asset-upload-processing.md).

Передайте `CMS_STORE_CONFIG_FILE` — абсолютный путь к защищённому JSON-файлу
**вне репозитория**. Для опубликованной страницы:

```json
{
  "baseUrl": "http://127.0.0.1:18080",
  "project": "local-demo",
  "environment": "staging",
  "path": "/store",
  "deliveryKey": "REPLACE_IN_PROTECTED_FILE"
}
```

Не используйте этот placeholder как ключ. Для первого локального черновика
задайте также `CMS_SERVICE_TOKEN_FILE` (файл с bootstrap service token) и
выполните `pnpm --filter @cms/store-example seed`. Скрипт добавляет контракт
Storefront и контракты разделов в активный manifest, создаёт новый Change Set, четыре ассета,
документ `/store` и краткосрочный preview-токен. Публикация не выполняется.
Запускайте seed один раз; повтор создаёт новые объекты.

Файл содержит ключ доступа: Unix mode 0600, на Windows ограничьте ACL текущим
пользователем. Конфигурацию нельзя коммитить или передавать браузеру.
Запуск: `pnpm --filter @cms/store-example dev`, адрес
<http://127.0.0.1:18082>. Порт можно задать через `PORT` (1024–65535).
Для собранной версии задайте `NODE_ENV=production` и выполните
`pnpm --filter @cms/store-example start`.

Preview-токен CMS действует 15 минут. Для того же черновика выполните
`pnpm --filter @cms/store-example preview:renew`, затем перезапустите витрину.
Новые ассеты и документы при обновлении токена не создаются.
Сервер принимает подключения только с loopback; это не публичное развёртывание.

## Проверки

```sh
pnpm --filter @cms/store-example typecheck
pnpm --filter @cms/store-example test
pnpm --filter @cms/store-example test:mutation
pnpm --filter @cms/store-example exec playwright install chromium
pnpm --filter @cms/store-example test:e2e
```

Браузерные тесты запускают настоящий production build и SDK против
детерминированной HTTP-фикстуры CMS на портах 18083/18084. Проверяются SSR,
гидратация, Escape/возврат фокуса, реальные повторные запросы, состояния загрузки,
ошибки/пустоты/404, изображения, XSS и обе темы при ширине 320–1280 px.
Для заранее установленного Chromium допустим `PLAYWRIGHT_EXECUTABLE_PATH`.

Покрытие Vitest включает модель, загрузку, HTTP-обработчик и React-компоненты.
Точки запуска server/client исключены из процентного покрытия: их исполнение,
сборка и гидратация проверяются браузерными сценариями. Stryker проверяет
model/load/http с порогом 70%; React-поведение проверяется DOM и браузером.

## Реализовано и оставшиеся пакеты

Документ CMS содержит `Storefront` с `config: {base: {...}}` и дочерние разделы.
Их порядок и условия показа задаёт IR; карточки разворачивает Repeat с ключом `$item.id`.
Поддержаны общие привязки, условия, live Composed/Slot и первый набор атомарных стилей.
Исторический документ с единственным Storefront пока читается через совместимую композицию.
Дополнительные breakpoint overrides config отклоняются; адаптация native-компонентов
принадлежит CSS приложения. Это первый пакет рендерера, а не полная реализация M3.
Studio, streaming/selective hydration, preview bridge, pinned Composed,
rich text и остальные дизайн-свойства остаются следующими пакетами.
Список предметов пока не использует сущности Product.
Корзины, платежей и заказов нет; цены демонстрационные, целые рубли.

PNG-иллюстрации в `assets/` отрисованы из собственного HTML/CSS дизайн-макета;
сторонние фотографии и внешние шрифты не используются.
Критерии пакета: [STORE-001…008](../../docs/react-store-interface.md).
Текущий контракт рендерера: [IR/React](../../docs/react-ir-renderer.md).

## Единый реестр компонентов

`src/registry.ts` связывает React-компоненты, свойства manifest и runtime-описания.
`pnpm --filter @cms/store-example manifest:build` записывает проверенные
`.cms/manifest.json`, `.cms/manifest.hash` и `.cms/code-index.json` общим CLI;
`sourceRef` определяется автоматически, свойства описаны через `t.*`.
Сборка витрины выполняет это автоматически; seed берёт сгенерированные контракты.
Метаданные копируются и замораживаются,
компоненты сохраняют идентичность при обновлениях. Каталожные проверки принадлежности
товара остаются серверной проверкой значений конкретной страницы.
Витрина отдаёт хэш сборки через `/.well-known/cms-manifest-hash` для проверки
`cms manifest push --wait-for-deploy`. [CLI и границы пакета](../../docs/registry-dsl-cli.md).
Действия и загрузчики реализованы [в следующем пакете](../../docs/runtime-actions-data.md):
`store.products` вызывается до SSR, Repeat читает `$data.featured.items`,
`store.favorite` исполняется из события `favorite` в документе CMS.
В preview действия отключены до включения взаимодействия. Серверная загрузка
индекса, Product/Collection как сущности CMS и preview bridge остаются следующими.
