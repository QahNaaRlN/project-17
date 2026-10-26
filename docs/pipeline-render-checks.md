# Pipeline: этапы 8–9 — сборка preview и axe

Статус: **одобрена пользователем 2026-10-26; решения §11 приняты; реализована и слита в dev 2026-10-26 в [PR #87](https://github.com/QahNaaRlN/project-17/pull/87), merge `c607dec`; см. [отчёт](pipeline-render-checks-acceptance.md)**.
Дата: 2026-10-26. Требования: STU-480…499 (STU-460…479 заняты пакетом
[переноса в production](studio-promote.md)).
Основа: `origin/dev` на `b242601` (после [PR #85](https://github.com/QahNaaRlN/project-17/pull/85)).
Цель — снять последнюю запись `pipeline-8-9` реестра
[`pending.ts`](../examples/react-store/e2e/scenario/pending.ts) и закрыть
отступление пакета переноса (§7 [studio-promote](studio-promote.md)): этапы 1–7
повторяются и при `promote`. Строка плана MVP: M6, «Этапы pipeline 8–11».

## 1. Проблема и границы

Pipeline публикации ([06 §5.1](spec/06-changes-publishing.md#51-этапы)) требует
для CS с риском ≥ medium:

- этап 8 — SSR каждой затронутой страницы через preview-развёртывание, HTTP 200, нет ошибок диагностики runtime;
- этап 9 — axe-core на отрендеренных страницах, блокируют нарушения `critical` и `serious`.

Сейчас `submit-changeset` синхронно в транзакции выполняет две проверки: `ir`
(этапы 1–4, 6, 7) и `policy` (этап 5), и сразу переводит CS в `in_review`,
`approved` или `failed`. Состояния `checking` из 06 §3.2 нет. CMS — Go-сервер без
браузера; отрендерить страницу приложения и запустить axe он не может. SDK в
режиме preview не выводит диагностики рендера в HTML, поэтому внешнему
наблюдателю нечего проверять, кроме кода ответа.

Пакет добавляет:

- асинхронные проверки и состояние `checking`;
- отдельный исполнитель проверок `apps/check-runner` (Node, Playwright Chromium, axe-core), который забирает проверки по API с арендой и сообщает результат;
- машиночитаемые диагностики рендера в HTML режима preview (`@cms/react`);
- результаты этапов 8–9 в ревью Studio;
- повторение этапов 1–7 при `promote`.

Вне пакета: этапы 10 (visual regression) и 11 (Lighthouse) — следующий пакет на
том же исполнителе (решение §11.2); бинарные артефакты (скриншоты, отчёты HTML);
этап 12 (внешние проверки приложения); масштабирование исполнителей и очередь
между проектами; production-развёртывание исполнителя.

## 2. Проверенная основа

| Основа | Состояние в dev | Чего не хватает |
|---|---|---|
| Проверки | Таблица `checks` (`stage`, `status` queued/running/passed/failed/warning/skipped, `blocking`, `details`, `artifacts`, `environment`, `content_hash`); `evaluate` пишет `ir` и `policy` | Асинхронных этапов, аренды, исполнителя |
| Состояния CS | `open → in_review/approved/failed` прямо при подаче | `checking` |
| Preview приложения | `create-preview-token {environment, changesetId}` (JWT 15 мин), `{appUrl}/api/cms/preview?token=&path=` ставит cookie и рендерит черновик (SDK-030…042) | Диагностики рендера не видны в HTML |
| URL приложения | `environments.app_url` задаётся `create-environment`; `cms bootstrap` создаёт staging и production без адреса | Команды смены адреса; использования в pipeline |
| Затронутые страницы | Страницы CS с маршрутом; `AnalyzeImpact` для опубликованных потребителей компонентов | Единый список страниц для рендера |
| `promote` | L1/L3, Composed, маршруты, согласования зон | Этапы 1–7 полностью (отступление пакета переноса) |

## 3. Требования

| ID | Требование |
|---|---|
| STU-480 | `submit-changeset` после синхронных `ir` и `policy` для CS с риском ≥ medium и без блокирующих отказов ставит в очередь проверки `preview` (этап 8) и `axe` (этап 9) по каждому целевому окружению CS с `appUrl`; CS переходит в `checking`. Риск `low` — обе проверки `skipped` с причиной `RISK_LOW`; CS идёт прежним путём. Блокирующий отказ `ir`/`policy` — сразу `failed`, асинхронные этапы не ставятся |
| STU-481 | Окружение без `appUrl`: проверки `preview`/`axe` получают `failed`, `blocking=true`, причина `CHECK_APP_URL_MISSING` — этапы обязательны по 06 §5.1 (решение §11.5). Окружения `cms bootstrap` создаются без адреса, поэтому вводится команда `set-environment-app-url {environment, appUrl}` (`project.admin`, проверка как у `create-environment`); стенды задают адрес витрины |
| STU-482 | Затронутые страницы: страницы CS с маршрутом в рабочей версии и опубликованные в окружении страницы, использующие изменённые компоненты CS (тот же анализ, что этап 7). Удалённые и без маршрута — не рендерятся. Больше 50 страниц — `preview` `failed` с `CHECK_PAGES_LIMIT` и списком |
| STU-483 | Переходы: `checking → in_review/approved` (по требуемым согласованиям), когда все блокирующие проверки текущего `content_hash` `passed`/`warning`/`skipped`; `checking → failed` при первом блокирующем `failed`. `reopen` из `checking` разрешён и отменяет незавершённые проверки (`skipped`, причина `CHANGESET_REOPENED`). Операции в `checking` запрещены (CHG-030) |
| STU-484 | Согласование и публикация требуют, чтобы `preview`/`axe` текущего `content_hash` и окружения публикации были пройдены; иначе `409 CHECKS_PENDING` или `422 CHECKS_FAILED`. Повторный `submit` после `reopen` ставит новые проверки; старые результаты не переиспользуются при другом `content_hash` |
| STU-485 | Команда `claim-check {}` (право `checks.run`, только сервисный актор исполнителя): атомарно (`FOR UPDATE SKIP LOCKED`) берёт старейшую `queued` проверку проекта, ставит `running`, `lease_id` и `lease_expires_at` (+5 мин) и возвращает: `checkId`, `leaseId`, `stage`, `changesetId`, `environment`, `appUrl`, страницы `[{objectId, path}]`, preview-токен на CS и окружение. Пустая очередь — `204` |
| STU-486 | Команда `complete-check {checkId, leaseId, status, details}`: только при совпадающей и не истёкшей аренде; `status` ∈ `passed`/`failed`/`warning`; `details` по схеме этапа (§5), ≤ 256 KiB; затем пересчёт состояния CS (STU-483) в той же транзакции. Чужая/истёкшая аренда — `409 CHECK_LEASE_LOST` без изменений; повтор с тем же ключом — тот же ответ |
| STU-487 | Истёкшая аренда возвращает проверку в `queued` (задача River раз в минуту), не более 2 повторов; третья — `failed`, причина `CHECK_RUNNER_TIMEOUT`. Проверка в `queued` дольше 15 мин получает в `details` признак `stale`, который видит Studio (исполнитель не запущен) |
| STU-488 | Право `checks.run` и сервисный актор `cms-checks`: создаются `cms bootstrap`; выдаются только сервисному актору; не дают чтения тел объектов, кроме выданного preview-токена на CS и окружение проверки. Агенты и люди этого права не получают |
| STU-489 | `@cms/react` в режиме preview выводит в SSR-HTML `<script type="application/json" id="cms-render-diagnostics">` с диагностиками плана рендера (`code`, `nodeId`) и диагностиками черновика из Delivery API; в обычном режиме элемент не выводится. Содержимое экранировано; без тел объектов и секретов |
| STU-490 | Исполнитель, этап 8: для каждой страницы открывает `{appUrl}/api/cms/preview?token=&path=` в Chromium, ждёт `load`; проверка `passed`, если у всех страниц HTTP 200 после перенаправления, нет диагностик уровня error в `cms-render-diagnostics` и нет необработанных исключений страницы; иначе `failed` с перечнем страниц, кодами и диагностиками. Таймаут страницы 30 с |
| STU-491 | Исполнитель, этап 9: на тех же страницах после этапа 8 запускает axe-core (правила WCAG 2.1 AA, `wcag2a`, `wcag2aa`); `failed`, если есть нарушения `critical` или `serious`; `warning`, если только `moderate`/`minor`; `passed` — без нарушений. В `details` — по странице: правило, impact, число узлов, до 20 селекторов. Этап 9 по странице не выполняется, если этап 8 для неё не прошёл (`skipped`, `PREVIEW_FAILED`) |
| STU-492 | Исполнитель `apps/check-runner`: процесс Node с конфигурацией (`CMS_API_URL`, учётные данные `cms-checks`, проект, параллельность 1…4); цикл `claim-check → рендер → complete-check`; пауза при пустой очереди 2 с; изолированный контекст браузера на проверку; ограничение происхождения навигации `appUrl` проверки; без сетевого доступа к другим источникам, кроме ресурсов страницы. Ошибки исполнителя не выдаются за результат страницы |
| STU-493 | `promote` повторяет этапы 1–7 против manifest и схем целевого окружения тем же механизмом, что `evaluate` (без записи `checks`): блокирующие отказы — `422 VALIDATION_FAILED` с диагностиками; `promote-preview` показывает их блокером. Этапы 8–9 при переносе не повторяются: те же версии уже отрендерены и проверены в окружении-источнике (решение §11.4) |
| STU-494 | Review API и Studio: проверки `preview`/`axe` по окружениям со статусами `queued`/`running`/`passed`/`warning`/`failed`/`skipped`, причинами и признаком `stale`; нарушения axe и диагностики рендера списком по страницам со ссылкой на страницу в preview Studio. Пока CS в `checking`, «Согласовать» недоступно с объяснением; SSE инвалидирует ревью при каждом `complete-check` |
| STU-495 | Отмена: `reopen` и `abandon` переводят `queued`/`running` проверки CS в `skipped`; поздний `complete-check` по такой проверке — `409 CHECK_LEASE_LOST` |
| STU-496 | Сценарий §2: стенд запускает исполнитель; шаг 7 проверяет, что `preview` и `axe` `passed` для staging; запись `pipeline-8-9` удаляется из `pending.ts`. Отдельный тест сценария: страница с нарушением `serious` (кнопка без имени) переводит CS в `failed` |
| STU-497 | Автоматические проверки: Go — постановка проверок по риску и окружениям, переходы `checking`, аренда/истечение/повторы, права, `CHECKS_PENDING`/`CHECKS_FAILED`, этапы 1–7 при `promote`; TS — диагностики SDK, исполнитель против фикстурного приложения (ошибка рендера, 500, нарушение axe, таймаут), Studio — состояния ревью |
| STU-498 | Ограничения: одна проверка — не больше 50 страниц и 10 мин; `details` ≤ 256 KiB с усечением списков и признаком `truncated`; исполнитель не сохраняет HTML и скриншоты |
| STU-499 | Документация: эта спецификация, 06 §3.2 и §5.1 (реализация), 08 (команды, коды, право), `docs/testing.md`, отчёт `docs/pipeline-render-checks-acceptance.md` с локальной приёмкой и CI |

## 4. Поток

```
submit ─▶ ir, policy (синхронно)
            │ блокирующий отказ ──▶ failed
            │ риск low ──▶ preview/axe skipped ──▶ in_review | approved
            ▼
         checking: preview, axe = queued (по окружениям CS)
            │
 check-runner: claim-check ─▶ Chromium: /api/cms/preview?token&path ─▶ axe
            │
         complete-check ─▶ все блокирующие пройдены ──▶ in_review | approved
                        └─ блокирующий failed ──────▶ failed
```

`content_hash` и окружение связывают результат с содержимым: правка требует
`reopen`, новый `submit` и новые проверки (PUB-004).

## 5. Схемы `details`

`preview`:

```json
{ "pages": [{ "objectId": "…", "path": "/store", "status": 200,
  "diagnostics": [{ "code": "CMS_COMPONENT_MISSING", "nodeId": "n_x", "severity": "error" }],
  "errors": ["TypeError: …"] }], "truncated": false }
```

`axe`:

```json
{ "pages": [{ "objectId": "…", "path": "/store", "violations": [{ "id": "button-name",
  "impact": "serious", "nodes": 2, "targets": ["#buy"] }] }], "truncated": false }
```

Причины без результата (`reason`): `RISK_LOW`, `CHECK_APP_URL_MISSING`,
`CHECK_PAGES_LIMIT`, `CHECK_RUNNER_TIMEOUT`, `CHANGESET_REOPENED`, `PREVIEW_FAILED`.

## 6. Исполнитель

- Отдельный пакет `apps/check-runner` и Docker-образ на базе образа Playwright; запуск локально — `pnpm --filter @cms/check-runner start`.
- Preview-токен выдаёт CMS в ответе `claim-check`; исполнитель не вызывает `create-preview-token` и не получает других прав чтения.
- Контекст браузера закрывается после проверки; cookie preview не переживает её.
- Журнал исполнителя — без токенов и тел страниц.

## 7. Отступления и связи

- 06 §5.1 «Preview build ... через preview-развёртывание»: используется развёртывание
  окружения с черновиком по preview-токену; отдельной сборки приложения под CS нет —
  изменения CS не меняют код приложения. Изменения кода проверяет CI приложения и
  `preview/<ref>` пакета Missing Capability.
- Закрывается отступление пакета переноса: этапы 1–7 при `promote` (STU-493).

## 8. Порядок реализации после одобрения

1. Сервер: миграция (`checking`, аренда, право), постановка и переходы, `claim-check`/`complete-check`, истечение аренды, гейты согласования и публикации, этапы 1–7 при `promote` (STU-480…488, 493, 495).
2. SDK: `cms-render-diagnostics` (STU-489).
3. Исполнитель и его тесты против фикстурного приложения (STU-490…492, 498).
4. Studio: ревью (STU-494).
5. Стенд, сценарий §2 без `pipeline-8-9`, отрицательный сценарий (STU-496, 497).
6. Документация и отчёт (STU-499).

## 9. Критерии приёмки пакета

- Сценарий §2 в CI: этапы 8–9 `passed`, в `pending.ts` нет записей.
- Нарушение `serious` или ошибка рендера делает CS `failed` и блокирует согласование.
- Потерянная аренда, падение исполнителя и `reopen` не оставляют CS навсегда в `checking` и не принимают поздний результат.
- `promote` с документом, не проходящим этапы 1–7 цели, отклоняется и показан блокером предпросмотра.
- Существующие проверки CI, пороги покрытия и E2E не ослаблены; job сценария остаётся обязательным.

## 10. Связанные документы

[06 §3.2, §5](spec/06-changes-publishing.md), [08 API](spec/08-api.md),
[10 runtime и preview](spec/10-runtime-sdk-preview.md), [preview bridge](preview-bridge.md),
[публикация в Studio](studio-publishing.md), [перенос в production](studio-promote.md),
[сценарий §2](e2e-scenario.md), [план MVP](spec/14-mvp-plan.md).

## 11. Принятые решения — 2026-10-26

1. **Где выполнять рендер и axe.** Отдельный исполнитель `apps/check-runner` на Node
   с Playwright и axe-core забирает проверки по API с арендой; браузер не входит в
   образ и процесс CMS (STU-485…487, 490…492).
2. **Объём пакета.** Этапы 8–9 и общий исполнитель; этапы 10–11 (visual regression,
   Lighthouse) — следующим пакетом на том же исполнителе вместе с решением о
   хранилище артефактов (§1).
3. **Состояние CS во время проверок.** Вводится `checking` по 06 §3.2; согласование
   недоступно до завершения блокирующих проверок (STU-480, 483, 494).
4. **Перенос в другое окружение.** `promote` синхронно повторяет этапы 1–7 против
   цели; этапы 8–9 не повторяются — те же версии уже отрендерены и проверены (STU-493).
5. **Окружение без `appUrl`.** Блокирующий отказ `CHECK_APP_URL_MISSING`; адрес
   задаётся `create-environment` или `set-environment-app-url` (STU-481).
