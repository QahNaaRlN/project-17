# CMS Studio — первый редактор

React/Vite, Zustand для документа и выбора, TanStack Query для bootstrap.
Редактируются открытые `standard` Change Sets через публичный Command API.
Доступны списки страниц и своих рабочих черновиков, создание именованного CS.
История CS, отмена/повтор целой правки и Ctrl/Cmd+Z / Ctrl/Cmd+Shift+Z доступны
через [пакет истории](../../docs/studio-history.md); в текстовых полях сочетания
оставлены для отмены ввода. История и отмена относятся ко всему CS.
SSE, production identity и полный M4 впереди.
[Контракт и границы](../../docs/studio-editor.md).
[Страницы и рабочие черновики](../../docs/studio-workspace.md).

## Локальный запуск

1. Запустить локальный CMS и приложение, подключённое к нему. Зарегистрировать
   manifest для окружения, создать CS с этим окружением в `targets` и страницу.
   Для витрины это выполняет `pnpm --filter @cms/store-example seed`.
2. За пределами репозитория подготовить защищённый JSON-файл с полями:
   `baseUrl` (например `http://127.0.0.1:18080`), `studioOrigin`
   (`http://127.0.0.1:18085`), `appOrigin` (`http://127.0.0.1:18082`),
   `project`, `environment`, `credential` (действующий `cms_svc_…` токен актора).
   Токен должен принадлежать владельцу CS и иметь `design.read`, `design.compose`,
   `content.read`, `content.write`. Секрет хранится только в этом файле.
3. Приложение запустить с `CMS_STUDIO_ORIGIN`, равным `studioOrigin`, и
   `CMS_APP_ORIGIN`, равным `appOrigin`. Оно должно обслуживать путь страницы и
   `/api/cms/preview` (SDK-030); витрина поддерживает свой configured CMS path.
4. Собрать workspace (`pnpm build`). Передать путь к файлу серверу Studio:

   ```powershell
   $env:CMS_STUDIO_CONFIG_FILE = 'C:\path\outside-repo\studio.json'
   pnpm --filter @cms/studio start
   ```

5. Открыть `studioOrigin`, выбрать страницу по маршруту, затем свой черновик
   или «Новый черновик» и указать имя. Доступна также ссылка
   `/#document=<UUID>&changeset=<UUID>`; IDs не являются секретами.

Сервер привязан к `127.0.0.1`, проверяет Host/Origin/Fetch Metadata и пропускает
только manifest, CS, document, pages, working-drafts, apply-operations,
create-changeset, history, undo/redo и create-preview-token. Он не
предназначен для публичного ingress. Для production нужны пользовательские
сессии и SSO, а не общий service credential.

## Проверка

`pnpm --filter @cms/studio test` — unit/DOM/HTTP contract с coverage 90/80/90/90.
После `pnpm build`: `pnpm --filter @cms/store-example test:e2e` — настоящие
Studio и приложение в Chromium. CMS подменяется только на HTTP-границе;
сохранение/права/undo на PostgreSQL проверяет `TestStudioLocalTextPersistence`.
