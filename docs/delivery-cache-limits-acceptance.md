# Кэш Delivery на origin и лимиты запросов: реализация и проверка

Дата: 2026-10-27. Ветка: `feature/delivery-cache-limits` от `dev` `a089a81`
(после [PR #92](https://github.com/QahNaaRlN/project-17/pull/92)).
[Спецификация STU-520…539](delivery-cache-limits.md) одобрена пользователем
2026-10-27 вместе с решениями §11.

## Реализовано

- Redis ([`redisstore`](../apps/server/internal/redisstore/redisstore.go)): клиент
  `go-redis/v9` по `CMS_REDIS_URL` с короткими тайм-аутами (соединение 500 мс,
  чтение и запись 200 мс); без переменной сервер работает без кэша. Смена
  доступности пишется в журнал один раз; `/readyz` от Redis не зависит (STU-520, 522).
- Compose — сервис `redis:7-alpine` без персистентности; стенд сценария §2 в CI —
  Redis как `services` и `CMS_REDIS_URL`; тесты Go — контейнер через testcontainers
  (`redistest`) (STU-521).
- Кэш ([`deliverycache`](../apps/server/internal/deliverycache/deliverycache.go)):
  `/page`, `/document`, `/routes`, `/entity`, `/asset` с ключом доставки. Запись
  хранит тело, `ETag`, `Surrogate-Key` и версии тегов `tagv:…`; попадание сверяет
  версии одним `MGET`. Версии, известные до чтения БД: `routes` для `/page` (его
  меняет каждая публикация), собственные ключи — для остальных ресурсов. Ответ
  больше 1 МиБ и ответ при недоступном Redis — `X-CMS-Cache: bypass`; ошибки и
  черновики не кэшируются (STU-523…525, 528, 529).
- Инвалидация: `publishing.enqueuePurge` ставит в outbox `delivery_cache_invalidate`
  с ключами публикации, отката или `promote`; задача меняет версии тегов (новое
  значение UUIDv7, без совпадения после вытеснения) и ставит `cdn_purge` с теми же
  ключами. Ошибка Redis — повтор River до purge CDN (STU-526, 527).
- Лимиты ([`ratelimit`](../apps/server/internal/ratelimit/ratelimit.go)): окно 1 с,
  `INCR` + `EXPIRE` конвейером; без Redis — счётчики в памяти, при сбое Redis — fail
  open. `/api/v1`: люди — запросы и команды, сервисные токены и агентские сессии —
  команды; Delivery — ключ доставки и субъект preview-токена. `429 RATE_LIMITED` с
  `Retry-After` и `no-store`; отказы агентам и сервисам — `access.denied` в журнале
  безопасности (STU-530…535).
- Конфигурация `CMS_RATE_*` с умолчаниями 08 §7; 0 выключает лимит (STU-531).

## Проверки

| Требование | Тест |
|---|---|
| STU-520, 531 конфигурация | `config`: `TestRedisAndRateLimits` |
| STU-522 журнал доступности | `redisstore`: `TestObserve` |
| STU-523…525 запись, версии, гонка | `deliverycache`: `TestCache`, `TestCacheRaceWithPublication`, `TestCacheLimits` |
| STU-524, 526, 536 HTTP: попадание по каждому ресурсу, байтовое совпадение заголовков и тела, `304` из кэша, свежесть после публикации и отката, черновики и ошибки без кэша | `httpapi`: `TestDeliveryOriginCache` |
| STU-526 порядок «инвалидация → purge CDN», повтор при сбое Redis | `jobs`: `TestInvalidationRunsBeforePurge`; `publishing`: `TestPublicationsEnqueuePurge` (ключи публикации, отката и `promote`) |
| STU-528 отказ Redis | `deliverycache`: `TestCacheWithoutRedis`; `httpapi`: `TestDeliveryWithoutRedis` |
| STU-530…534 лимиты | `ratelimit`: `TestLimiter`, `TestLimiterFailsOpen`; `httpapi`: `TestDeliveryRateLimits`, `TestServiceCommandLimit`, `TestLimitRequestsByActor` |
| STU-538 ориентир NFR-002 | `BenchmarkDeliveryPageCacheHit` |

## Результаты

- Микробенчмарк попадания через весь роутер (проверка ключа доставки в PostgreSQL,
  `GET` записи и `MGET` тегов), Windows 11, PostgreSQL и Redis в Docker Desktop:
  6,4 мс на запрос (2 000 запросов, последовательно). Это ориентир, не NFR-002:
  измерение p95 под нагрузкой — следующий пакет M6. Основная доля — сетевые
  обращения через проброс портов Docker Desktop.
- Проверка ключа доставки по-прежнему идёт в PostgreSQL на каждый запрос, в том
  числе при попадании; кэш ключей — кандидат по итогам нагрузочных тестов.
