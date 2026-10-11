# React-витрина «Тихая форма»

Первый рабочий интерфейс по варианту «Каталог в центре»: React 19, TypeScript,
Vite, Tailwind и собственные компоненты Button/Dialog с Radix. Контент читается
сервером через `@cms/core`; изображения отображаются через `@cms/runtime`.
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
Storefront в активный manifest, создаёт новый Change Set, четыре ассета,
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

## Границы первого пакета

Весь контент — документ CMS с native-компонентом `Storefront` и
`config: {base: {...}}`. Дополнительные breakpoint overrides config отклоняются:
адаптация этого компонента принадлежит CSS приложения. Это не универсальный
IR renderer, не Studio и не полная реализация этапа M3. Bindings, Repeat,
Composed, data loader, streaming/selective hydration и preview bridge остаются
следующими пакетами. Список предметов пока не использует сущности Product.
Корзины, платежей и заказов нет; цены демонстрационные, целые рубли.

PNG-иллюстрации в `assets/` отрисованы из собственного HTML/CSS дизайн-макета;
сторонние фотографии и внешние шрифты не используются.
Критерии пакета: [STORE-001…008](../../docs/react-store-interface.md).
