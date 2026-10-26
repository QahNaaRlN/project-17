# Этапы pipeline 8–9: реализация и проверка

Дата: 2026-10-26. Ветка: `feature/pipeline-render-checks` от `dev` `26a84be`; слита в dev
2026-10-26 в [PR #87](https://github.com/QahNaaRlN/project-17/pull/87), merge `c607dec`; [CI итогового SHA](https://github.com/QahNaaRlN/project-17/actions/runs/40380254098) и [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/40384179897) пройдены.
[Спецификация STU-480…499](pipeline-render-checks.md) одобрена пользователем
2026-10-26 вместе с решениями §11.

## Реализовано

- Сервер ([`render_checks.go`](../apps/server/internal/workflow/render_checks.go)):
  после синхронных `ir` и `policy` подача ставит `preview` и `axe` по каждому
  целевому окружению; CS переходит в `checking`. Риск `low` — `skipped`
  (`RISK_LOW`); нет страниц — `skipped` (`NO_PAGES`); нет адреса приложения —
  блокирующий `failed` (`CHECK_APP_URL_MISSING`); больше 50 страниц —
  `CHECK_PAGES_LIMIT` (STU-480…482).
- Затронутые страницы: страницы CS с маршрутом и опубликованные страницы,
  использующие изменённый живой компонент, транзитивно; закреплённые версии
  (`ref.version`) не затрагиваются (STU-482).
- Переходы: все блокирующие решены — `in_review` или `approved`; первый
  блокирующий отказ — `failed`, оставшиеся проверки — `skipped`
  (`CHANGESET_FAILED`); `reopen` и rebase отменяют незавершённые проверки
  (`CHANGESET_REOPENED`, `CHANGESET_REBASED`), rebase ставит новые (STU-483, 495).
- `publish` требует решённых этапов 8–9 окружения: `409 CHECKS_PENDING`,
  `422 CHECKS_FAILED` (STU-484).
- `claim-check` (аренда 5 мин, страницы, preview-токен на CS и окружение) и
  `complete-check` (только действующая аренда, `details` ≤ 256 KiB со списком
  `pages`), `409 CHECK_LEASE_LOST`; задача River раз в минуту возвращает
  истёкшие аренды в очередь, третья потеря — `CHECK_RUNNER_TIMEOUT`; очередь
  дольше 15 мин помечается `stale` (STU-485…487).
- Право `checks.run`: не входит в роль `admin` bootstrap; `cms check-runner-token
  -slug S` создаёт сервисного актора `cms-checks` с ролью `check-runner` и печатает
  токен с областью `checks.run` (STU-488).
- `set-environment-app-url` (STU-481); подпись preview-токенов вынесена в пакет
  `previewtoken`, общий для Delivery API и pipeline.
- `@cms/react`: в режиме preview SSR выводит `<script type="application/json"
  id="cms-render-diagnostics">` — коды, узлы и уровень диагностик плана и черновика;
  экранирование `< > & U+2028 U+2029` (STU-489).
- Исполнитель [`apps/check-runner`](../apps/check-runner/src/runner.ts):
  Chromium, изолированный контекст на проверку, навигация только на `appUrl`,
  этап 8 — HTTP 200, нет диагностик `error`, исключений страницы и отсутствия блока
  диагностик; этап 9 — axe-core (`wcag2a`, `wcag2aa`, `wcag21a`, `wcag21aa`),
  `critical`/`serious` — отказ, остальные — `warning`, страницы без рендера —
  `PREVIEW_FAILED` (STU-490…492, 498).
- Studio: состояние «Идут проверки страниц», этапы 8–9 по окружениям со статусами,
  причинами, `stale` и находками по страницам; в `checking` доступен только возврат
  в работу (STU-494).
- Сценарий §2: стенд задаёт адреса витрин, выпускает токен и запускает исполнитель;
  шаг 7 требует `preview`/`axe` для staging; реестр `pending.ts` пуст; отдельный тест —
  кнопка без имени даёт `failed` (STU-496).

## Отступления и уточнения при реализации

| Тема | Решение | Основание |
|---|---|---|
| Пустая очередь `claim-check` | Ответ `{"check": null}` вместо `204` | Command API всегда отвечает конвертом `result`; клиенту не нужен отдельный путь |
| Выключение этапов в тестах | Таблица `project_render_checks` (нет строки — этапы включены), без команды; `cmstest` и HTTP-тесты выключают этапы | Командные тесты не поднимают приложение и исполнитель; настройки проекта тесты перезаписывают целиком, а новый столбец `projects` ломал бы откат миграций со старым кодом |
| Токен исполнителя | Отдельная команда `cms check-runner-token`, а не второй токен в выводе `bootstrap` | Стенды разбирают вывод `bootstrap`; токен исполнителя выпускается и ротируется отдельно |
| STU-493, этапы 1–7 при `promote` | Код не менялся: перенос уже выполняет ту же проверку `CheckPublication`, что этап `ir`; тесты `a11y_test` и `validation_test` доказывают отказ по доступности и валидации цели. Устаревшая фраза 06 §7.2.1 исправлена | Проверено чтением кода и существующими тестами |
| Уровни диагностик рендера | Предупреждения: заглушки Missing Capability (`*_PENDING_CAPABILITY`, `CMS_SOURCE_PENDING`) и `CMS_IMAGE_UNAVAILABLE`; остальное — ошибки | Черновик с заглушкой допустим до выполнения запроса; хранилища изображений на стендах нет |
| `abandon` | Отдельной отмены нет | `abandon` возможен только из `open`, где незавершённых проверок уже нет |

## Найдено первым прогоном этапа 9

Первый прогон сценария §2 с исполнителем отклонил страницу шага 7 — это настоящие
нарушения, исправленные в пакете:

| Нарушение | Исправление |
|---|---|
| `role-img-alt` (serious): заглушка `Image` без источника получала `role="img"` с пустым именем, даже при `decorative` | `@cms/react`: декоративная или безымянная заглушка — `aria-hidden`, без роли |
| `color-contrast` (serious): текст «Изображение недоступно» цвета `--muted` на тонированных карточках витрины | `examples/react-store`: цвет `--ink` |

Также выяснилось, что экран «Работа» Studio не подключён к потоку событий: панель
публикации перечитывает ревью раз в 3 с, пока CS в `checking` (STU-494).

## Локальная проверка

Windows, PostgreSQL 16 в Docker, бинарь CMS с тегом `e2e`, `pnpm e2e:scenario`.
Два прогона подряд — **PASS** обоих тестов сценария: основной (шаги 1–10; на шаге 7
`preview` и `axe` для staging пройдены, CS ушёл из `checking` в `in_review`) и
отрицательный (кнопка без имени на странице `/store`: этап 8 пройден, этап 9 —
`button-name`, CS `failed`, согласование — `409`). Журнал исполнителя содержит
только идентификаторы проверок, статусы и состояние CS.

| Ограничение | p95 | Гейт |
|---|---|---|
| Видимость в production | 95 мс | ≤ 10 с |
| Откат в production | 17 мс | ≤ 10 с |
| IR-044, правка поля в превью | 1935 мс | > 2 с |
| NFR-005, `document.patch` | 138 мс | > 800 мс |

IR-044 по-прежнему у самого гейта 2× (раньше 0,9–1,9 с): поток событий сервера
опрашивает изменения раз в секунду; запас по-прежнему мал.

Go: полный набор пакетов на Windows зелёный, кроме `capabilitygithub`
(падает и на `dev` в этом окружении); `capabilities` проходит за ~18 мин
(увеличенный `-timeout`). TS: `@cms/react` 76, Studio 421, исполнитель 6,
витрина 122 модульных и 61 фикстурный браузерный сценарий.

## Автоматические проверки

- Go: `TestRenderChecksLeaseCompleteAndPublish`, `TestRenderChecksFailureAppURLAndNoPages`,
  `TestRenderChecksLeaseExpiryAndReopen`, `TestRenderPagesFollowsLiveComponents`;
  права и перечень 09-agent.md §2.1.
- TS: `@cms/react` (диагностики preview), `apps/check-runner` (настоящий Chromium:
  ошибка рендера, 500, исключение, нет блока диагностик, уход с `appUrl`, axe,
  усечение, клиент), Studio (ревью, панель публикации в `checking`, перечитывание).

## CI

[CI итогового SHA](https://github.com/QahNaaRlN/project-17/actions/runs/40380254098): `Check (TS + Go)` (lint, typecheck, тесты с Chromium, `go test -race`, покрытие Go
95.0% — ровно на пороге), `Docker image`, `E2E scenario (§2)` — сценарий 1,2 мин, оба
теста пройдены; 12,5 мин job ушли на установку Chromium. [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/40384179897) пройден. До этого в PR
два исправления: порядок импортов (gofmt) и тесты ветвей и сбоев БД, поднявшие покрытие
с 94.6%.

## Не проверено

- Реальное CDN-приложение и production-развёртывание исполнителя.
