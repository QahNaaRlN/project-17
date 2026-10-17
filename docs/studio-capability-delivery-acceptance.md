# Подписанная доставка context pack: приёмка

Дата: 2026-10-17. Пакет STU-275…294 реализован в
`feature/studio-capability-delivery`, от `dev` после PR #60. Слияние в dev
не выполнено. [Черновой PR #61](https://github.com/QahNaaRlN/project-17/pull/61),
[CI конечного SHA](https://github.com/QahNaaRlN/project-17/pull/61/checks).

## Реальный локальный сценарий

Проверены отдельные PostgreSQL/CMS/River, Studio, локальный OIDC с Alice/Bob,
витрина и самостоятельный HTTP receiver с SQLite inbox (WAL, synchronous=FULL).
Это проверка протокола передачи, а не работа внешнего кодового агента/GitHub.
Production endpoints, ключи репозитория и LLM не использовались.

Автор создал action-запрос и подтвердил исходный pack формами Studio.
Bob получил только project.admin и смог открыть «Получатель запросов» без
design.read/content.read/code.propose; список capability-запросов ему не открывался.
После явного выбора operator endpoint Alice просмотрела точный URL/pack/hash
и отдельно разрешила внешнюю передачу.

Получатель проверил scope, timestamp, body hash и HMAC, записал пакет в SQLite,
завершил транзакцию и намеренно оборвал первый ответ. CMS показала retry_wait.
После рестарта CMS очередь повторила тот же deliveryId и те же body bytes.
Получатель подтвердил durable dedup; Studio показала «Пакет сохранён получателем»
и две попытки. В inbox осталась одна запись для этой доставки (calls=2).
Request revision/status, CS seq, manifest, число pack и request audit не изменились.
Дополнительный send с другим ключом команды вернул прежнюю terminal identity.

Проверенные негативные ветви на настоящих БД и HTTP:

- отмена во время sending: пакет уже сохранён, поздний ответ записан как
  acknowledged_after_cancel, item остаётся cancelled;
- отзыв роли отправителя до claim: cancelled/AUTH_REVOKED, получатель не вызван;
- новая revision target до claim: cancelled/TARGET_CHANGED без перенаправления;
- правка страницы после согласия: cancelled/ANCHOR_CHANGED без refresh пакета;
- тот же человек с ролью другого проекта не читает чужой deliveryId;
- pending остаётся диагностикой ACTION_PENDING_CAPABILITY, публикация не проходит,
  published pointer не создаётся;
- English и ширина 375 px: внешнее согласие и журнал остаются отдельными от
  инженерного статуса, горизонтального переполнения страницы нет.

Финальный повтор на новой БД выполнен с коммитом
`1d35041dd8f93451d21d573670ba8266704a702c`; JSON-отчёт содержит worktreeDirty=false.
После этого менялись только документация и синхронизация тестовых fixtures.
Артефакты локального стенда лежат в `local/delivery-final-acceptance/`
родительского workspace: delivery-acceptance.json
и delivery-mobile.png. Приватные конфигурации, журналы с окружением и SQLite
не включаются в Git/PR.

## Воспроизведение

Нужны локальные Docker/PostgreSQL, Node с node:sqlite, Playwright Chromium,
собранные CMS/Studio/store и отдельная пустая БД. `CMS_DATABASE_URL` должен
указывать только на loopback БД `cms_delivery_acceptance_<suffix>` в собственном
тестовом контейнере project17-studio-behavior-tests. Не использовать рабочие БД.

1. Применить миграции CMS к отдельной БД. Собрать библиотеки, CMS и Studio.
2. Выполнить scripts/studio-capability-baseline.mjs с CMS_PNPM_CLI и
   STUDIO_REVIEW_OUTPUT: временная витрина не содержит store.favorite;
   исходный registry восстанавливается в finally.
3. Запустить scripts/studio-review-stand.mjs с CMS_REVIEW_BINARY, CMS_REVIEW_DIR,
   CMS_REVIEW_BASE_PORT (пять свободных loopback портов), CMS_DATABASE_URL,
   CMS_REVIEW_CAPABILITIES=true, CMS_REVIEW_CAPABILITY_DELIVERY=true.
4. Запустить scripts/studio-live-capability-delivery.mjs с теми же binary/database,
   CMS_PNPM_CLI, STUDIO_REVIEW_ADMIN_FILE из каталога стенда и STUDIO_REVIEW_OUTPUT.
   Скрипт меняет только изолированную fixture: CODE policy, роли, pause очереди,
   второй проект; перезапускает принадлежащий стенду CMS и проверяет inbox.
5. Проверить PASS и отчёт, затем остановить процессы из собственного
   studio-work-review.processes.json. Для нового полного прогона использовать
   новую БД/каталог. Не переиспользовать секреты fixture вне loopback стенда.

## Автоматические проверки

Go/PostgreSQL проверяют atomic outbox, immutable body, дедупликацию concurrent
send, отдельный аудит без сдвига request revision, stale gates, текущие права,
три цикла по шесть попыток, lease contention/crash, late receipt, ошибки хранения
и восстановимость после частичного сбоя. Проверяются Query scope/cursor/no-store,
human-only команды и исключение capability.deliver из AgentRights/default admin.

Transport-тесты проверяют общий с receiver фиксированный HMAC-вектор,
изменённый body/ID/key/timestamp, TLS trust/hostname и исходные Host/SNI,
публичный DNS с закреплённым dial IP, смешанные/закрытые ответы, mapped IPv6,
rebinding между попытками, несовпадение peer IP, запрет redirect и размеры receipt.
Проверяются ACK_INVALID, 4xx/5xx/Retry-After, новый timestamp и старый keyId.
Получатель отдельно проверяется на concurrent durable dedup и свой рестарт.

React/DOM и BFF проверяют отдельное согласие, frozen payload/key после потери
ответа, reconsent при conflict, сохранение ввода, dirty/focus/Escape, polling и
cleanup, права инженера, admin-only вход, environment pin и закрытые маршруты.
Полный локальный прогон: Studio 343 теста, store example 117, все девять
TS/React/runtime пакетов; Go build/vet и полный PostgreSQL-набор с покрытием 95%.
Пороги Studio 90/80/90/90 и Go 95% не снижены; race выполняется в Linux CI.

## Оставшиеся границы

Delivered подтверждает сохранение только у доверенного получателя с корректным
durable receipt. Отмена или отзыв прав после claim не возвращают уже переданный
пакет. Endpoint/keyring требуют настройки оператора и синхронизированных часов.
GitHub/tracker callbacks, проверенные PR/CI факты, preview/<ref>, автоматическое
fulfillment/requiredBy и остальные kinds в этот пакет не входят; M5 остаётся частичным.
