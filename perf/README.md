# Нагрузочные тесты NFR-001…007

Спецификация: [docs/load-tests.md](../docs/load-tests.md). Результаты прогонов —
[docs/load-test-report.md](../docs/load-test-report.md).

```bash
pnpm load
```

Нужны Docker, Go и Node (pnpm). Команда:

1. Собирает `cms` (с тегом `e2e` — его использует стенд сценария §2), генератор
   `loadseed`, Studio, витрину и исполнитель проверок.
2. Поднимает PostgreSQL 16 (`shared_buffers=1GB`) и Redis 7 в контейнерах
   (`cms-perf-postgres`, `cms-perf-redis`) или берёт `CMS_DATABASE_URL` /
   `CMS_REDIS_URL` — база должна быть пустой и на loopback.
3. Применяет миграции и заполняет профиль 01 §5: 20 проектов, по 100 000 сущностей
   и 10 000 страниц, ключи доставки и 50 токенов редакторов.
4. Запускает `cms serve` (порт 18290), прокси nginx на месте CDN (18291) и k6
   ([`k6/load.js`](k6/load.js)): прогрев 2 мин → 10 мин Delivery 200 RPS на origin,
   50 пользователей Studio, публикации профиля, 10 публикаций и откатов по 50 страниц →
   10 мин того же потока Delivery через прокси.
5. Повторяет сценарий §2 под фоновой нагрузкой k6 со строгим порогом NFR-005 400 мс.

Пороги k6 и проверка сценария роняют прогон. Всё складывается в `perf/results/`:
`k6-measure.json` (сводка с p95), журналы `k6-*.log`, `cms.log`, `scenario/`,
`environment.json`. Ключи и токены — только в `*.private.json`, в артефакт CI они не
попадают.

Отладка на меньшем объёме (не подтверждает NFR):

```bash
LOAD_PROJECTS=2 LOAD_ENTITIES=2000 LOAD_DOCUMENTS=500 LOAD_WARMUP=20s LOAD_DURATION=60s \
LOAD_RPS=50 LOAD_MIN_MISSES=10 LOAD_PUBLISHES=2 LOAD_BACKGROUND_S=420 pnpm load
```

`LOAD_SKIP_BUILD=true` пропускает сборку: бинарники лежат в `perf/.bin`, приложения —
в своих `dist`. Каталог `perf/results/` очищается в начале каждого прогона.
