# Наблюдаемость локально

Спецификация: [observability.md](observability.md) (OPS-020…023, STU-590…609).

## Запуск

```bash
docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/docker-compose.observability.yml up --build
```

Профиль добавляет контейнер `grafana/otel-lgtm` (Collector, Tempo, Prometheus, Grafana) и
включает экспорт в `cms`. Grafana — <http://127.0.0.1:3000>, вход `admin`/`admin`.

CMS, запущенная на хосте (`go run ./cmd/cms serve`), отправляет данные туда же:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318 OTEL_SERVICE_NAME=cms CMS_DEPLOYMENT_ENV=local go run ./cmd/cms serve
```

Без `OTEL_EXPORTER_OTLP_ENDPOINT` экспорт выключен (STU-590).

## Что смотреть

- **Трассы** — Explore → Tempo, поиск по `service.name = cms`. Запрос Studio — одна трасса:
  `POST /api/v1/commands/{name}` → `command apply-operations` → `sql …`. Задача River
  (`job purge`, `job render_check_leases`, …) — отдельная трасса со ссылкой на запрос,
  который её поставил.
- **Трасса по ошибке** — `traceId` из ответа problem+json (или строки лога `cms request
  failed` Studio BFF, или ошибки исполнителя проверок) вставляется в поиск Tempo.
- **Метрики** — Explore → Prometheus. Имена с точками экспортируются с подчёркиваниями:
  - `http_server_request_duration_seconds` — по `http_route`, методу и коду;
  - `cms_command_duration_seconds` — по `cms_command`, `cms_outcome`;
  - `cms_pipeline_stage_duration_seconds` — по `cms_stage`, `cms_status`;
  - `cms_jobs_depth`, `cms_jobs_oldest_age_seconds`, `cms_checks_queued`, `cms_checks_oldest_age_seconds`;
  - `cms_delivery_cache_requests_total` — hit ratio:
    `sum(rate(cms_delivery_cache_requests_total{cms_cache_result="hit"}[5m])) / sum(rate(cms_delivery_cache_requests_total[5m]))`;
  - `cms_llm_tokens_total`, `cms_llm_requests_total`;
  - `cms_runtime_diagnostics_total` — диагностики витрин по `cms_diagnostic_code`.
- **Логи** — JSON в stdout контейнера `cms` (`docker compose logs cms`); в записях
  запроса есть `traceId`, `spanId`, `projectId`, `actorId` (STU-602).

Семплирование — переменные `OTEL_TRACES_SAMPLER=parentbased_traceidratio` и
`OTEL_TRACES_SAMPLER_ARG=0.1` (production, решение §11.1); локально по умолчанию
сохраняются все трассы.
