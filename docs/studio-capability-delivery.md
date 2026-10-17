# Studio: подписанная доставка context pack

Статус: STU-275…294 реализованы в `feature/studio-capability-delivery`;
локальная сквозная приёмка пройдена. [Черновой PR #61](https://github.com/QahNaaRlN/project-17/pull/61)
подготовлен в dev; [актуальные проверки](https://github.com/QahNaaRlN/project-17/pull/61/checks).
Слияние в dev не выполнено.
Дата: 2026-10-17. Основа — `origin/dev`
`9259d6272471cba057c0625bd554bb730ae64660`, после
[PR #60](https://github.com/QahNaaRlN/project-17/pull/60).
[CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/39028004513)
пройден: Check (TS + Go), Go race/coverage 95%, Studio 328 тестов,
60 браузерных сценариев и Docker image.
Продолжает [Missing Capability и context pack](studio-missing-capability.md),
[архитектуру агента](spec/09-agent.md#5-missing-capability),
OPS-010 и SEC-006 из [эксплуатационного контракта](spec/12-security-ops.md).

## 1. Результат и границы

Автор отправленного action-запроса проверяет конкретную версию context pack,
видит настроенного получателя и отдельно подтверждает внешнюю передачу.
CMS сохраняет доставку и задачу очереди в одной транзакции, подписывает HTTP
запрос, повторяет временные сбои и показывает журнал/результат. Разработчику
не нужно переносить JSON вручную. Ручное скачивание из PR #60 сохраняется.

Это транспортный пакет. `delivered` означает подтверждённое долговременное
сохранение пакета получателем, а не принятую инженерную задачу, открытый PR,
успешный CI или выполненную capability. Ответ получателя не меняет request
status, IR, pack, manifest, согласования или публикацию.

В границах: один настроенный endpoint на project/environment, action-запросы,
явная отправка человеком, HMAC-SHA256, outbox/River, ограниченные повторы,
отзыв доступа, отмена следующих попыток, Query API и формы Studio.

Вне границ: автоматическая отправка при submit/refresh; входящий webhook,
GitHub/трекер API, проверенные факты PR/CI, запуск кодового агента внутри CMS,
repository credentials, исполнение команд из pack, auto-fulfillment/requiredBy,
preview/<ref>, другие kinds, полный M5 и production-развёртывание.
Статусы accepted/in_progress/pr_open и resolve остаются ручными командами PR #60.

## 2. Проверенная исходная точка

| Область | Есть в dev после PR #60 | Нужно добавить |
|---|---|---|
| Capability | Scoped action-запросы, явный submit/resolve, expectedRevision/seq/manifest | Отдельный жизненный цикл доставки без изменения request revision |
| Pack | Immutable версии, canonical hash, ограниченный безопасный JSON | Передачу сохранённых байтов по явному согласию на конкретного получателя |
| Права | capability.request, design.read, code.propose | Отдельное capability.deliver и запрет делегирования этого права агенту |
| Очередь | River, transactional jobs.Enqueue, workers CDN/assets/Agent | Worker доставки, отдельную очередь, попытки, lease и восстановление |
| Конфигурация | Серверные env, настройки проекта; сетевой CDN purger | Ограниченный каталог endpoint, HMAC keyring, безопасный transport |
| Studio | Карточка/очередь, pack preview/download, exact retry, dirty guards | Подтверждение внешней передачи и отдельный журнал попыток |

Опорные места: [capabilities](../apps/server/internal/capabilities/commands.go),
[pack/query](../apps/server/internal/capabilities/queries.go),
[River](../apps/server/internal/jobs/jobs.go),
[app wiring](../apps/server/internal/app/app.go),
[права](../apps/server/internal/auth/rights.go),
[форма](../apps/studio/src/capability-panel.tsx),
[BFF](../apps/studio/src/handler.ts).
HTTPPurger не является готовым webhook transport: его redirect/network policy
и неподписанный запрос нельзя просто переиспользовать для context pack.

## 3. Требования

| ID | Требование |
|---|---|
| STU-275 | Передача — отдельный явный жест после submit. Ни старые запросы, ни включение endpoint, ни новый pack не создают внешнюю отправку автоматически. |
| STU-276 | Получатель выбирается из операторского каталога данного project/environment. project.admin включает endpoint; произвольный URL/секрет из Studio, MCP, pack или команды не принимается. |
| STU-277 | Новый capability.deliver выдаётся явно и не входит в права по умолчанию/делегируемые права Agent. Send/retry доступны только человеку — владельцу исходного CS с capability.request/design.read/capability.deliver. |
| STU-278 | До send показаны точный pack/hash, recipient URL и revision. Подтверждение внешней передачи отдельно от согласия поделиться pack с code.propose. Optimistic gates фиксируют request revision, pack revision/hash, target revision/fingerprint и текущий CS/manifest. |
| STU-279 | Delivery item, его неизменяемые body/hash и первая задача River записываются атомарно через Command Bus. Сбой rollback не оставляет отправку; до commit сети нет. Exact retry возвращает тот же deliveryId. |
| STU-280 | HTTP body содержит только сохранённый безопасный pack и ограниченный envelope. Пересборка живого контекста worker запрещена. Один и тот же deliveryId всегда имеет одни и те же байты body, включая createdAt. |
| STU-281 | Каждый HTTP запрос имеет timestamp, deliveryId, keyId, body hash и HMAC-SHA256. Подпись покрывает все эти значения. Получатель проверяет ±5 мин и дедуплицирует deliveryId до внешних побочных эффектов. |
| STU-282 | Transport запрещает redirects, proxy из окружения и непубличные адреса; проверяет DNS и фактическое соединение на каждой попытке. TLS hostname verification обязательна. Секреты и body не попадают в журналы. |
| STU-283 | Только совпадающий durable receipt считается delivered. 2xx без корректного receipt не является успехом; чужие/поддельные дополнительные поля ответа не исполняются и не меняют CMS. |
| STU-284 | Доставка at-least-once: повторы с тем же ID/body, ограниченный бюджет, сохранённые nextAttemptAt и попытки. Потеря ответа или падение после сохранения получателем не создаёт второй job у корректного получателя. |
| STU-285 | Lease/CAS защищают от параллельных workers и позднего ответа. После рестарта queued/retry_wait/просроченная sending восстанавливаются. Статусы/тайминги попыток доступны в журнале без тела ответа. |
| STU-286 | Перед каждой попыткой повторно проверяются текущие права инициатора, проект/окружение/target, состояние request/CS, точная заглушка и свежесть выбранного pack. Отзыв/смена контекста останавливают будущие попытки. |
| STU-287 | Человек может отменить queued/retry_wait/sending; это отменяет дальнейшие попытки. Уже начатый HTTP нельзя гарантированно отозвать. Поздний receipt сохраняется в аудите и не снимает отмену. |
| STU-288 | После retryable exhaustion явный retry продолжает тот же deliveryId/body, не создаёт новую передачу с новым ID. Нужны свежие права/gates и прежний target. Не более трёх циклов по шесть попыток. |
| STU-289 | Delivery status отделён от capability status. Не изменяются request revision, CS seq/history, pack versions, manifest или approvals. Доставка имеет собственные revision и аудит. |
| STU-290 | Query API изолирован по проекту/окружению и повторяет текущую видимость request. code.propose видит безопасный журнал; design.read сам по себе не открывает чужие доставки. Все ответы no-store. |
| STU-291 | Studio показывает получателя, очередь/попытку/план повтора/ошибку/receipt и явные send/retry/cancel. Lost response повторяет тот же frozen payload/key. Ввод сохраняется при конфликте, переходы/locale защищены dirty guards. |
| STU-292 | RU/EN, клавиатура, focus/Escape, 375 px; delivered не подписывается «разработчик принял» или «готово». Нет автоматического открытия внешних URL или передачи секрета в браузер. |
| STU-293 | Оператор может отключить endpoint и ротировать ключи. Новые задания не используют отсутствующий ключ; старые не переподписываются случайным новым ключом. Метрики/аудит различают очередь, подтверждение, отмену и неизвестный результат. |
| STU-294 | Реальная приёмка CMS/PostgreSQL/River/OIDC/Studio и отдельного receiver проверяет подпись, потерю ответа после durable write, dedup, restart/lease, права, смену endpoint и два проекта. Обычная публикация с pending по-прежнему блокируется. |

## 4. Получатель, права и согласие

Оператор задаёт `CMS_CAPABILITY_WEBHOOK_CONFIG_FILE`: абсолютный путь к локальному
JSON-файлу, доступному только процессу CMS/оператору. Отсутствие переменной
отключает возможность доставки; существующий ручной workflow продолжает работать.
Файл не лежит в репозитории/публичном каталоге. Его содержимое, secret, путь
и ошибки с исходным значением не возвращаются в API/логи или idempotent responses.

Корневой JSON имеет форму `{"endpoints":[...]}`. Каталог содержит entries
`{endpointId, projectId, environment, url, activeKeyId, keys}`.
endpointId/keyId — ASCII `[a-zA-Z0-9_-]{1,64}`; projectId — UUID; environment
существует и standard. keys — отображение keyId → base64 от минимум 32 случайных
байт, максимум 128 байт; activeKeyId обязан присутствовать в keys.
Дубликаты, неизвестные поля, пустой/неполный каталог,
некорректный URL или ключ при заданном файле — ошибка запуска. Файл ограничен
1 MiB, каталог — 256 entries, URL — 2048 символов, keyring — 8 keys на endpoint.
Секретов в БД нет. Delivery сохраняет SHA-256 отпечаток ключа; при запуске
CMS отклоняет повторное использование сохранённого keyId с другим секретом.
Ключ не является CMS service/agent token и не даёт доступа к API CMS.

Команда project.admin выбирает endpointId своего project/environment и enabled.
В БД сохраняются публичный точный URL, endpointId, target revision и fingerprint
канонических `{endpointId, projectId, environment, url}`. Один target на окружение.
Изменить URL через API нельзя; операторский файл и состояние БД должны совпасть.
После смены URL нужен явный set с новой revision; прежние доставки не перенаправляются.
Отключение/смена target останавливает ожидающие доставки; включение не возобновляет их.

Query даёт пользователю URL/название получателя, enabled, ready, revision,
fingerprint и controlled unavailableReason. Keyring/secret/key file никогда не выдаются.
Ключи ротируются перезапуском с новым activeKeyId и сохранением прежних keys до
завершения старых доставок. Delivery фиксирует keyId при создании; retry использует
именно его. Удалённый ключ даёт KEY_UNAVAILABLE без fallback. Нельзя переиспользовать
keyId с другим секретом. Hot reload, внешнее secret storage и provision UI — вне v1.

Владелец выбирает последний pack. Send разрешён при submitted/accepted/in_progress/
pr_open, собственном открытом standard CS, актуальных seq/pageHash/manifest,
CODE-привязке и точном pending. Обе формы IR — ActionCall и массив — эквивалентны.
Draft/terminal requests, schema/preview CS, неподтверждённый индекс, чужая
страница, изменённый event или устаревший pack дают конкретный отказ без сети.
Устаревший pack сначала обновляется существующим refresh после просмотра человеком.

Preview передачи фиксирует requestRevision и полный gate tuple. packRevision
может быть меньше requestRevision после ручных engineering-переходов — это не
повод переписывать pack. Сам по себе accepted/pr_open не меняет контекст.
Live refresh редактора не заменяет данные, на которые пользователь дал согласие.

## 5. Данные, команды и чтение

Следующая миграция после 00020: `00021_capability_delivery.sql`.

- `capability_delivery_targets`: id, project_id/environment_id, endpoint_id,
  public_url, endpoint_fingerprint, enabled, revision, changed_by/at;
  UNIQUE(project_id,environment_id), scoped FK на environment.
- `capability_deliveries`: id, project_id, request_id, pack_revision/hash,
  target_id/revision/fingerprint, destination_url, key_id, initiated_by,
  body bytea/body_hash, state, revision, cycle, attempts_in_cycle/total,
  next_attempt_at, lease_token/until, cancelled_at, delivered_at, controlled error,
  created_at/updated_at. Body bytes immutable. UNIQUE(project_id,request_id,
  pack_revision,target_id,target_revision) на всю историю, не только active.
- `capability_delivery_attempts`: delivery/project, ordinal, cycle, attempt_id,
  lease token, started/finished_at, HTTP status, controlled outcome, receipt
  identifiers/hash, retryable, next_attempt_at. UNIQUE(delivery_id,ordinal).
- `capability_delivery_audit`: project/delivery, собственный event seq,
  actor/initiator, action/reason, revisions, timestamps, безопасные outcome IDs.

Scoped FK связывают project/request/pack/target/actor; человек может быть global
principal, поэтому учитывать role_bindings, как в 00020. Повторная проверка прав
не должна разрушать историю после отзыва роли. Delivery ссылается на сохранённый
pack; изменение/deletion payload и pack при retention запрещено. В v1 нет очистки
истории доставок; дальнейший retention — отдельный эксплуатационный контракт.

Все команды используют обычный envelope reason/Idempotency-Key, строгие payload,
текущие права и транзакцию Command Bus. Сетевых вызовов в обработчиках команд нет.

| Команда | Payload и поведение |
|---|---|
| set-capability-delivery-target | `{environment, endpointId, enabled, expectedRevision}`; human + project.admin; первая запись expectedRevision=0, изменение — точная текущая revision. Проверяет операторский каталог. |
| send-capability-context | `{requestId, expectedRequestRevision, expectedPackRevision, expectedPackHash, expectedSeq, expectedManifestHash, expectedTargetRevision, expectedTargetFingerprint, confirmExternalShare:true}`; только владелец-человек с тремя правами выше. Создаёт item/envelope/job или возвращает существующий item с тем же immutable identity. |
| retry-capability-delivery | `{deliveryId, expectedDeliveryRevision, expectedRequestRevision, expectedSeq, expectedManifestHash, expectedTargetRevision, confirmExternalShare:true}`; только failed с retryable exhaustion, прежними pack/target и cycle<3. Новый цикл сохраняет ID/body/keyId. Cancelled/delivered/permanent failures не возобновляются. |
| cancel-capability-delivery | `{deliveryId, expectedDeliveryRevision}`; инициатор с текущими правами либо human project.admin для аварийной остановки. Delivered не превращается в cancelled; прочие terminal статусы не откатываются. |

Ответ send/retry: `{delivery:{id,state,revision,packRevision,packHash,targetId,
targetRevision,bodyHash,cycle,attemptsTotal,nextAttemptAt,createdAt}, deduplicated}`.
Повтор с другим Idempotency-Key не создаёт вторую запись той же identity;
терминальный существующий item возвращается как есть, а не посылается повторно.
Новый pack/target revision означает новую передачу и новое явное согласие.

Query API:

- `GET /api/v1/capability-delivery-endpoints?environment=...`: human project.admin
  получает только entries данного project/environment: endpointId, public URL,
  fingerprint и ready; без keyring. Это источник вариантов формы настройки.
- `GET /api/v1/capability-delivery-target?environment=...`: безопасная конфигурация.
  До первой настройки — configured=false, revision=0, ready=false; отсутствие
  настройки не выдаётся за сетевой сбой и не включает доставку автоматически.
- `GET /api/v1/capability-requests/{id}/delivery-preview`: точный сохранённый pack,
  его hash, public target и gate tuple для отправки; только допустимый отправитель.
- `GET /api/v1/capability-requests/{id}/deliveries?limit=25&cursor=...`: порядок
  createdAt/id DESC, максимум 100; cursor привязан к project/request/actor.
- `GET /api/v1/capability-deliveries/{id}`: безопасный item, последние 50 попыток,
  audit, canSend/canRetry/canCancel с причинами; без raw body ответа и секретов.

Request-specific чтение наследует capabilities.Get/Download visibility и
environment pinning. Target Query доступен human project.admin либо текущему
читателю с capability.request+design.read или code.propose; agent исключён.
Каталог доступных endpoint требует human project.admin независимо от этих прав.
Все Query читают согласованный read-only snapshot; preview возвращает именно
его gate tuple, а не последовательно собранные версии request/pack/target/CS.
Ни project.admin сам по себе, ни знание UUID не открывают pack чужого проекта.
Аварийная cancel администратора не должна возвращать pack/данные запроса.
Фоновые записи используют service provenance и causal initiated_by, а не
выдают сохранённого человека за текущего авторизованного актора.

Ошибки: 403 FORBIDDEN; 404 NOT_FOUND без раскрытия чужого scope; 409
CAPABILITY_DELIVERY_REVISION_CONFLICT / CONTEXT_STALE / TARGET_CHANGED /
STATE_INVALID / RETRY_EXHAUSTED; 413 CAPABILITY_DELIVERY_LIMIT_EXCEEDED;
503 CAPABILITY_DELIVERY_UNAVAILABLE. CONTEXT_STALE, TARGET_CHANGED, STATE_INVALID
и RETRY_EXHAUSTED в этой строке также имеют префикс CAPABILITY_DELIVERY_.
Настройка/сеть используют стабильные
reason codes: TARGET_DISABLED, KEY_UNAVAILABLE, ADDRESS_BLOCKED, AUTH_REVOKED,
REQUEST_TERMINAL, ANCHOR_CHANGED, HTTP_RETRYABLE, HTTP_PERMANENT, ACK_INVALID,
NETWORK_UNCERTAIN. Текст upstream error/response в UI не передаётся.

## 6. HTTP-протокол и сеть

POST на точный URL. Envelope v1:

```json
{
  "version": "1.0",
  "event": "capability.context.delivery.v1",
  "deliveryId": "<uuid>",
  "projectId": "<uuid>",
  "environment": "staging",
  "requestId": "<uuid>",
  "requestRevision": 5,
  "packRevision": 2,
  "packHash": "sha256:<64 hex>",
  "targetId": "<uuid>",
  "targetRevision": 1,
  "createdAt": "<UTC RFC3339>",
  "contextPack": {}
}
```

contextPack — именно JSON сохранённой выбранной версии, не ссылка с CMS token.
Секретов, raw IR/props/locals/entity/loader values и исходников не добавляется.
Envelope canonical JSON по общему Go/TS контракту; максимум 640 KiB UTF-8,
исходный pack по-прежнему максимум 512 KiB. SHA-256 этих байтов — bodyHash.
Результат сериализации сохраняется единожды, а не восстанавливается из jsonb
на каждой попытке. requestRevision в envelope — revision в момент создания item.

Headers: Content-Type application/json; X-CMS-Delivery-Id; X-CMS-Key-Id;
X-CMS-Timestamp (Unix seconds); X-CMS-Body-Hash (`sha256:<lowercase hex>`);
X-CMS-Signature (`v1=<lowercase hex HMAC-SHA256>`). Вход подписи UTF-8:
`v1\n<keyId>\n<timestamp>\n<deliveryId>\n<bodyHash>\n`.
Новая попытка имеет свежий timestamp/подпись, прежние deliveryId/body/hash/keyId.
Receiver сначала проверяет свою разрешённую привязку project/environment/target
и keyId, hash, подпись constant-time и окно ±300 секунд, затем durable dedup
по `(projectId,deliveryId)`. Один валидный ключ не разрешает чужой project/target.
Нужны синхронизированные часы.

Успех — любой 2xx с JSON `{deliveryId, bodyHash, status:"stored"}`, совпадающим
с запросом, максимум 4 KiB. Receipt допустим только после durable write/dedup.
Повтор того же ID/body возвращает тот же смысл receipt без второй постановки
инженерной задачи; тот же ID с другим hash отклоняется 409. CMS не использует
никакие PR/status/URL/command поля ответа. Невалидный 2xx считается ACK_INVALID
и повторяется с тем же ID: это неизвестный исход, а не доказанная потеря.

Production URL: только HTTPS, порт 443 либо отсутствует, без userinfo/query/
fragment; путь фиксирован каталогом. Запрещены IP literals и localhost, private,
loopback, link-local, multicast, unspecified, reserved/documentation адреса,
IPv4-mapped обходы и metadata endpoints. Любой непубличный адрес в DNS-ответе
блокирует попытку целиком. Dial использует проверенный IP, TLS ServerName/Host
исходного hostname; повторного незащищённого resolve между проверкой и connect нет.
Проверка повторяется на каждом соединении/попытке. Redirect не выполняется;
HTTP proxy из окружения отключён, не подключается cookie jar/CMS Authorization.
Таймаут всего запроса 10 с, response headers максимум 32 KiB,
response body максимум 4 KiB; тело не логируется.

Для локальной приёмки отдельный process flag
`CMS_CAPABILITY_WEBHOOK_ALLOW_LOOPBACK_HTTP=true` разрешает только literal
127.0.0.1/::1 и явно заданный порт loopback receiver. Он выключен по умолчанию,
не меняется через API/проект, не разрешает LAN/metadata/redirects. Не использовать
OIDC loopback flag как разрешение этого transport. Секрет — отдельный тестовый.

## 7. Очередь, повторы и гонки

Состояния: queued → sending → delivered; временный сбой → retry_wait → sending;
исчерпание/постоянный отказ → failed; отмена/отзыв/неактуальный контекст → cancelled.
Успешный receipt не означает request.accepted. Отдельная River queue доставки
имеет максимум 2 workers на процесс и не занимает очередь CDN/assets/Agent.

Перед HTTP worker в короткой транзакции проверяет gates и получает lease/CAS,
создаёт попытку и фиксирует ordinal. HTTP выполняется вне транзакции/row locks.
Lease — 30 с; при фиксации результата проверяются token/revision и cancelled_at.
Внутренние gates не копируются из сохранённого Rights snapshot: читаются текущие
человек/роль/проект, enabled/fingerprint/key, request/CS/anchor и свежесть pack.
Допустимые ручные engineering-переходы без смены pack сами по себе не отменяют доставку.

Сбой до network после claim может израсходовать попытку — это безопаснее скрытого
сброса счётчика. Падение после network до записи receipt восстанавливается как
неизвестный исход с тем же ID/body. Периодическая recovery-задача River каждые
30 с восстанавливает просроченные leases и due queued/retry_wait. DB item — источник
истины; потерянная/discarded River job не должна навсегда оставить item sending.
Один item имеет один действующий lease; просроченный worker не фиксирует состояние.
После паузы/падения процесса или разрыва сети возможны пересекающиеся обработки
на стороне receiver: durable dedup обязателен и для конкурентных дублей.

Шесть попыток в цикле. Базовые интервалы после сбоев: 30 с, 2 мин, 10 мин,
30 мин, 2 ч; jitter 0…20% сохраняется как nextAttemptAt. 408/425/429/5xx,
timeout/reset и ACK_INVALID retryable; прочие 4xx и все 3xx — permanent.
Retry-After для 429/503 принимается как delta seconds или HTTP-date,
ограничивается 5 с…2 ч и не уменьшает базовый interval. Остальные headers
ответа не управляют CMS. После шести попыток — failed с retryable exhaustion.
Явный retry до трёх циклов всего не меняет ID/body/keyId/pack/target.

Перед новым циклом/попыткой устаревший pack, закрытый CS, снятый pending,
terminal request, disabled/изменённый target или отозванные права дают cancelled,
без автоматического обновления pack/адресата. Permanent failures требуют
исправления причины и нового явного решения; не маскируются как transient retry.
Повторная передача после permanent/cancelled требует новой версии pack либо
target и нового согласия, а не обхода immutable identity старого delivery item.
Операции не берут locks в обратном порядке к environment → CS → request;
target/delivery блокируются после этих объектов, когда они требуются командой.

Cancel во время sending ставит cancelled_at и прекращает будущие попытки.
Проверка прав/состояния линейризуется в транзакции claim: отзыв или смена
request/target после claim подчиняются тем же ограничениям уже начатой попытки.
Поздний receipt записывается как acknowledged_after_cancel в попытке, item остаётся
cancelled. UI сообщает, что уже переданный пакет вернуть нельзя. Отмена/отключение
не дают гарантии «получатель ничего не получил». История delivered не удаляется.
Логи/метрики: ID/project, attempt/cycle, duration, status/error code, depth/age
очереди; без body/описаний/секретов/подписей, query strings и raw response.

## 8. Studio и реализация

В карточке request — отдельный блок «Передача разработчику». Показывать status
request отдельно от доставки. Send открывает диалог с URL, сохранённым pack,
hash и revision. Checkbox: «Передать этот пакет указанному внешнему получателю».
Кнопка выключена до подтверждения. Наличие code.propose share из PR #60
не заменяет этот checkbox; включение настройки не отправляет старые запросы.

Потерянный ответ send/retry/cancel сохраняет frozen key/payload; форма не создаёт
новую попытку «на всякий случай». Конфликт сохраняет ввод, перечитывает состояние
и требует заново просмотреть получателя/контекст. Нельзя перенести согласие через
live refresh/смену пользователя/CS/локали. Escape/focus/dirty guard и 375 px
проверяются настоящим React root/браузером. Пока карточка открыта, статус читается
раз в 2 с; в фоне — не чаще 15 с, с остановкой при unmount/logout и terminal state.

В журнале — ordinal, время, HTTP code, controlled outcome, следующая попытка,
неизвестный исход и receipt. «Получатель сохранил пакет» не заменяется словами
«действие готово». Настройка target — компактная отдельная панель
«Получатель запросов» в общей навигации для human project.admin, независимо
от доступа к списку запросов или design.read. В форме — выбор серверного
endpointId из admin Query, enabled, revision; секреты не запрашиваются.
BFF пропускает только новые allowlisted commands/queries, проверяет environment,
сессию/CSRF и не принимает URL/права через payload. Agent/MCP не получает send,
retry/cancel/config или сетевой инструмент, capability.deliver нельзя делегировать.

Порядок реализации: миграция/queries/sqlc и права; каталог/безопасный transport;
delivery service/команды/Query и River worker/recovery; Studio/BFF; receiver fixture
и реальная приёмка. Командные/HTTP DTO синхронизировать с API документацией.
Изменение прав в bootstrap не должно автоматически включить deliver всем ролям.

## 9. Проверки и готовность

| Набор | Обязательные проверки |
|---|---|
| Транзакции | rollback item/job/body, exact retry после commit, concurrent send с разными keys, revision conflicts, recovery после restart, отсутствие сети до commit |
| Подпись | golden bytes/hash/signature Go + receiver, timestamp ±300 с, изменённые body/ID/keyId, неверная подпись, rotation/удалённый key без fallback |
| Сеть | public DNS, mixed/private answers, rebinding между validate/dial, TLS mismatch, IPv6/mapped, redirects, proxy env, oversize/slow ответ, HTTP loopback flag выключен |
| Повторы | 429/503/Retry-After, 5xx/timeout/reset, malformed 2xx, permanent 4xx/3xx, 6×3 budget, lease contention, crash до/после durable write, late receipt после cancel |
| Доступ | два проекта/окружения, чужой UUID, code.propose projection без live CS, service/agent forbidden, отзыв deliver/design.read/capability.request, закрытый CS/снятый pending/stale pack/target change |
| UI | RU/EN/375 px/keyboard, отдельное согласие, exact retry, сохранённый ввод при conflict/revocation, poll cleanup/logout, delivered без подмены инженерных статусов |

Реальная приёмка: отдельный CMS/PostgreSQL/River/OIDC и receiver с собственной
durable inbox/unique deliveryId. Автор создаёт/отправляет action-запрос формами,
администратор включает настроенный loopback endpoint, автор подтверждает внешнюю
передачу. Receiver проверяет HMAC, сохраняет pack и первую попытку намеренно
обрывает после commit до ответа. После рестарта CMS worker повторяет тот же
ID/body; receiver подтверждает dedup, inbox содержит одну запись. Studio показывает
delivered и две попытки, request/pack/CS/manifest не меняются из-за доставки.
Негативные ветви — отзыв прав, cancel в полёте, смена target, два проекта, stale
pack и отказ публикации pending — проходят с настоящей БД и HTTP receiver.

Не требуется настоящий кодовый агент/GitHub или LLM key: fixture доказывает только
протокол доставки и durable dedup. Не объявлять её реальным открытием PR/CI.
CI: генерация/sqlc, Go build/vet/race, Go coverage ≥95%, TS/DOM/runtime тесты с
прежними порогами, format/lint/typecheck/build и браузерная приёмка. Порог не снижать.
Spec-only проверка: локальные ссылки, уникальность STU-ID, согласованность DTO,
прав/состояний/повторов и явные implemented/remaining/dependencies в roadmap.

STU-275…294 реализованы: миграция 00021, отдельные target/delivery/attempt/audit,
immutable bytea и key pin, четыре команды, пять Query-маршрутов, очередь с двумя
workers и периодическим восстановлением, безопасный transport, Studio/BFF.
Проверки реализации описаны в [отчёте приёмки](studio-capability-delivery-acceptance.md).
После пакета отдельно специфицировать доверенные факты
PR/CI, preview/<ref>, automatic fulfillment/requiredBy и остальные kinds/policies.
Сквозной шаг 10 и полный M5 пока остаются частичными.
