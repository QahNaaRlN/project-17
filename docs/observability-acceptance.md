# Наблюдаемость OPS-020…023: отчёт о приёмке

Спецификация: [observability.md](observability.md), STU-590…609.
Дата: 2026-10-28. Ветка `feature/observability`,
[PR #104](https://github.com/QahNaaRlN/project-17/pull/104).

## Автоматическая приёмка

CI итогового SHA `77661b5`: `Check (TS + Go)`, `E2E scenario (§2)`, `Security`,
`Docker image` — пройдены; покрытие Go 95 % (порог 95 %).
[Load](https://github.com/QahNaaRlN/project-17/actions/runs/40709574113) на ветке —
зелёный по NFR-001…007 (NFR-005 p95 376 мс, NFR-002 p95 7,6 мс).

| Критерий §8 | Чем подтверждён |
|---|---|
| Правка в Studio — одна трасса HTTP → команда → SQL; задача связана ссылкой | `TestTracesAndMetrics` (in-memory спаны: маршрут-шаблон, `command …`, `sql …` в одной трассе вызывающего), `TestRiverMiddleware` (ссылка задачи на запрос) |
| Ошибка API содержит `traceId` | `TestTracesAndMetrics` (`traceId` равен трассе клиента; без аутентификации — новая трасса) |
| Метрики OPS-021 с ограниченными метками | `TestTracesAndMetrics`, `TestTelemetryIntakeAndCacheMetric`, `TestObserveQueues`, `TestRecordLLM`; метки — маршрут-шаблон, команда, код, этап, вид задачи, модель |
| Диагностика витрины увеличивает счётчик; сбой или лимит не влияет на рендер | `TestTelemetryIntakeAndCacheMetric`, `TestTelemetryLimitAndUnmatchedRoute`; `@cms/core` `telemetry.test.ts` (агрегирование, семплирование, сбой отправки); `@cms/react` `server.test.ts` |
| Без `OTEL_EXPORTER_OTLP_ENDPOINT` задержки не меняются | Load на ветке зелёный (экспорт выключен) |
| Существующие проверки не ослаблены | Пороги покрытия Go и TS прежние; сценарий §2 обязательный; STU-515 (секреты в логах) проходит |

Ручная проверка: `cms serve` с `OTEL_EXPORTER_OTLP_ENDPOINT` отправил трассы в локальный
OTLP-приёмник; запрос с `traceparent` продолжил трассу клиента, ответ с ошибкой вернул
`traceId`; строка `http request` содержит маршрут-шаблон, `traceId`, `spanId`.

## Что не проверено

- Профиль `grafana/otel-lgtm` проверен только `docker compose config`; интерфейс Grafana
  руками не открывался.
- Выгрузка метрик в ручной проверке не дошла до приёмника: процесс остановлен без
  корректного завершения; экспорт метрик проверен тестами через `ManualReader`.
- Один прогон сценария §2 упал на пороге IR-044 (p95 2011 мс при пороге 2000 мс; ранее
  1938–1955 мс); повторный прогон прошёл. Время обновления preview после правки сущности
  бимодально (~0,9 или ~1,9 с) — открытая задача M4 (IR-044 < 1 с), не связанная с пакетом.
