# Перенос в production и откат production в Studio: реализация и проверка

Дата: 2026-10-26. Ветка: `feature/studio-promote` от `dev` `96ba0cb`; слита в dev
2026-10-26 в [PR #84](https://github.com/QahNaaRlN/project-17/pull/84), merge `2cba143`; [CI итогового SHA](https://github.com/QahNaaRlN/project-17/actions/runs/40311593042) и [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/40313597429) пройдены.
[Спецификация STU-460…479](studio-promote.md) одобрена пользователем 2026-10-26
вместе с решениями §11.

## Реализовано

- Сервер ([`promote_preview.go`](../apps/server/internal/publishing/promote_preview.go)):
  `GET /publication-history/{id}/promote-preview?toEnvironment=` и `/diff` —
  объекты «сейчас в цели → версия публикации» с признаком `skipped`, блокеры
  (`PROMOTE_OUTDATED:<id>`, `PROMOTE_NOTHING`, `PROMOTE_NOT_SUPPORTED`,
  `PROMOTE_SCHEMA_NOT_SUPPORTED`, `PROMOTE_SAME_ENVIRONMENT`, `PROMOTE_FORBIDDEN`,
  `ENVIRONMENT_NOT_PUBLISHABLE`, `VALIDATION_FAILED`, `PATH_TAKEN`, коды
  `RequireZoneApprovals`), диагностики, зависимости для
  `CONTENT_REFERENCE_UNPUBLISHED` и snapshot (STU-462…465, 468).
- Команда `promote` принимает `expectedPromoteSnapshot`: под блокировками окружений
  проекта пересчитывает предпросмотр, требует свежее право `content.publish` и
  причину 1…2000 символов, расхождение — `409 PROMOTE_SNAPSHOT_STALE` (STU-466).
- Studio-BFF: `promotionTargets` в конфигурации и `/api/studio/config`; история и
  откат — в базовом окружении и целях; `promote` — только в цель из списка, со
  снимком, причиной и проверкой окружения исходной публикации (STU-460, 461, 467).
- Studio ([`promote-panel.tsx`](../apps/studio/src/promote-panel.tsx)): выбор окружения
  в разделе «Публикации» (в адресе раздела), «Перенести в <цель>…» с предпросмотром,
  блокерами, ссылкой на публикацию-зависимость, полным сравнением, причиной и
  повторной проверкой снимка; неизвестный исход повторяет тот же запрос; после
  успеха открывается новая публикация цели со ссылкой на приложение цели
  (STU-469…474, 476).
- Сценарий §2: стенд передаёт Studio production-витрину как цель; шаги 8–9
  выполняются в Studio, записи `studio-promote` и `studio-production-rollback`
  удалены из `pending.ts` (STU-477).

## Отступления и уточнения при реализации

| Тема | Решение | Основание |
|---|---|---|
| Транзакция promote-preview | REPEATABLE READ read-write, всегда откатывается; остальные маршруты истории — read-only | Требования согласований считаются кодом команды (`RequireZoneApprovals`), который берёт блокировки строк; отдельная копия логики разошлась бы с командой |
| Согласования при переносе | Блокер появляется только по требованиям зон и политики агентов, как в команде; общее число согласований политики проекта `promote` не перепроверяет | Поведение команды не менялось (решение §11.3); подключение — вместе с этапами pipeline M6 |
| Занятость маршрута | Сравнение по точному пути, как в предпросмотре отката; совпадение формы с параметрами ловит уникальный индекс команды (`PATH_TAKEN`) | Тот же подход, что STU-166 |
| `studio-review-stand.mjs` | Не изменён; вторая витрина и цель есть в `e2e-stand.mjs` | Приёмка пакета выполнена сценарием §2 на общем стенде |

## Локальная проверка

Windows, PostgreSQL 16 в Docker, бинарь CMS с тегом `e2e`, `pnpm e2e:scenario`.
Alice публикует в staging через Studio; Bob в Studio переносит публикацию сущности
`Collection`, затем страницы в production, видит страницу на production-витрине,
выбирает production и отменяет перенос страницы. Два прогона подряд — **PASS**.

| Ограничение | p95 (два прогона) | Гейт |
|---|---|---|
| Видимость в production после переноса | 106–107 мс | ≤ 10 с |
| Откат в production | 20–27 мс | ≤ 10 с |
| IR-044, правка поля в превью | 923–924 мс | > 2 с |
| NFR-005, `document.patch` | 177–234 мс | > 800 мс |

## Автоматические проверки

- Go: `TestPromotePreviewGuardedAndReplay` (предпросмотр, diff, причина, точный
  повтор, `PROMOTE_SNAPSHOT_STALE`, `PROMOTE_NOTHING`, `PROMOTE_OUTDATED`),
  `TestPromoteSnapshotStaleConditions` (политика, указатель цели, согласование,
  контракт окружения), `TestPromotePreviewBlockersAndAccess`, `TestPromoteDependencies`,
  HTTP `TestPublicationHistoryPromotePreview`.
- Studio: BFF (`promotionTargets`, окружения, guard `promote`, откат production),
  UI (`test/promote.test.tsx`: перенос, неизвестный исход, устаревший снимок,
  блокеры, выбор окружения, адаптер и валидатор); 419 тестов, пороги покрытия.

## CI

[CI итогового SHA](https://github.com/QahNaaRlN/project-17/actions/runs/40311593042): `Check (TS + Go)` с `go test -race` и порогами покрытия, `Docker image`,
`E2E scenario (§2)` (2 мин 44 с) — пройдены; [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/40313597429) пройден.
