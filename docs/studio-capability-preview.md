# Studio: preview/<ref> для проверенного PR

Статус: **черновик спецификации, ожидает одобрения пользователя; код не начат**.
Дата: 2026-10-19. Требования: STU-315…334.
Основа: `origin/dev` на `566f4b1` (после [PR #64](https://github.com/QahNaaRlN/project-17/pull/64));
функциональная база — [PR #62](https://github.com/QahNaaRlN/project-17/pull/62),
merge `359981e31c188d02a28a0bafa26ed418f2235ecb`,
[CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/39246472929) пройден.
Продолжает [Missing Capability](studio-missing-capability.md),
[подписанную доставку](studio-capability-delivery.md),
[проверенные факты GitHub PR/CI](studio-capability-github.md) и
[регистрацию manifest](manifest-registration.md). Архитектурная цель — строка
«Preview» в [жизненном цикле Missing Capability](spec/09-agent.md#51-жизненный-цикл)
и [окружения preview](spec/04-manifest.md#6-окружения-preview) MF-030…MF-033.
Пакет №1 очереди M5 в [плане MVP](spec/14-mvp-plan.md#текущее-состояние-и-приоритет--2026-10-19).

## 1. Результат и границы

CI приложения разворачивает сборку PR и регистрирует её manifest в эфемерном
окружении `preview/pr-<N>`, связанном с тем же репозиторием и PR, что и
проверенная GitHub-связь запроса (STU-295…314). CMS фиксирует происхождение
каждой регистрации: commit индекса кода, точный head SHA, развёрнутый appUrl.
Когда этот commit совпадает с head SHA последнего наблюдения GitHub, владелец CS
открывает свой черновик в этом preview-окружении: Delivery preview-проекция
подставляет вместо `pending:<requestId>` реализованное в PR действие, и Studio
показывает страницу в развёрнутом preview-приложении, где действие исполняется
в режиме interact. IR, CS, запрос и публикация при этом не меняются.

Preview — способ посмотреть работу разработчика до слияния. Он не доказывает
корректность бизнес-логики, не означает слияния, развёртывания в standard
и не выполняет запрос. Подстановка существует только в ответе preview-чтения;
опубликовать страницу с `pending` по-прежнему нельзя (IR-053, STU-269).

В границах: метаданные происхождения preview-окружения и его регистраций,
`delete-environment` и TTL 14 дней (MF-032), явные запреты MF-031, вычисляемое
сопоставление запрос ↔ preview, preview-проекция pending с аргументами,
сохранёнными человеком вне IR, Query API, переключатель окружения просмотра
и раздел «Предпросмотр» карточки запроса в Studio, отдельный операторский список
допустимых origin preview-приложений, RU/EN, миграция 00023.

Вне границ (следующие пакеты или отдельные решения):

- **автоматическое fulfillment и группы requiredBy** — пакет №2. Регистрация
  в preview и даже совпадение SHA не переводят запрос в `fulfilled`, не вызывают
  `resolve-capability-request` и не добавляют операций в CS;
- регистрация manifest в standard по слиянию PR, `promote` в Studio;
- component/dataSource/token-запросы (пакет №3), условные политики агента (№4);
- запуск сборок/деплоев, обращения CMS к хостингу preview-приложений, проверка
  того, что по appUrl действительно отдаётся указанный commit (см. §6);
- редактирование страницы в режиме preview-окружения (только просмотр);
- GitHub Enterprise, входящие webhook, несколько репозиториев на одно standard-окружение;
- новые MCP-инструменты: агент читает краткую сводку через существующий
  `get_capability_request` в пределах уже разрешённой видимости.

## 2. Исходная точка

| Область | Есть в dev | Нужно добавить |
|---|---|---|
| Окружения | `environments.kind ∈ {standard, preview}`, `app_url`, неиспользуемый `expires_at`, `preview_key`; `create-environment` для preview разрешён `manifest.register` | Источник (base environment, PR), учёт регистраций, `delete-environment`, TTL, архив вместо физического удаления |
| Manifest | `register-manifest` в preview активирует сразу и хранит снимки схем (MF-033); code index с полным `repo.commit` (40/64 hex) | Обязательный индекс и `headSha` для preview с источником; журнал регистраций с commit и appUrl |
| MF-031 | CS `targets` допускают только standard; capability/delivery/github отвергают preview | Явная проверка во всех путях publish/promote/rollback/approve/schema и единый код ошибки |
| GitHub | Связь request → PR, наблюдения с head SHA, freshUntil, last-known | Сопоставление наблюдения с регистрацией preview без новых сетевых вызовов |
| Preview-токен | `create-preview-token` для любого окружения проекта и CS | Проверка видимости CS, статуса окружения и привязки токена к регистрации |
| Studio | Один `appOrigin` в конфигурации; CSP `frame-src` на него | Список допустимых origin preview-приложений, выбор окружения просмотра |
| Runtime | `pending:` не исполняется ни в planEvents, ни в executeActions | Изменений нет: подстановка делается на сервере до runtime |

Опорный код: [manifestregistry](../apps/server/internal/manifestregistry/registry.go),
[projects](../apps/server/internal/projects/projects.go),
[preview-токен](../apps/server/internal/delivery/preview.go),
[GitHub-факты](../apps/server/internal/capabilities/github.go),
[GitHub-клиент/конфигурация](../apps/server/internal/capabilitygithub/config.go),
[проверка pending](../apps/server/internal/composition/bindingdoc/validate.go),
[runtime actions](../packages/runtime/src/actions.ts),
[Studio editor](../apps/studio/src/editor.ts), [BFF](../apps/studio/src/handler.ts).

## 3. Требования

| ID | Требование |
|---|---|
| STU-315 | Preview-окружение с источником создаётся `create-environment` kind=preview с `source {baseEnvironment, pullNumber}`. Имя строго `preview/pr-<pullNumber>`; repository берётся только из включённой GitHub-конфигурации baseEnvironment (STU-295), не из payload. baseEnvironment — существующее standard. Preview без source (MF-030) остаётся допустимым, но никогда не сопоставляется с запросами |
| STU-316 | `register-manifest` в preview с источником требует готовый code index и `headSha`; `headSha` совпадает с `repo.commit` индекса (полный 40/64 hex), иначе `PREVIEW_COMMIT_MISMATCH` без записи. Каждая регистрация — append-only запись {manifest, commit, appUrl, registeredBy, registeredAt, seq}; повтор того же hash/commit/appUrl идемпотентен |
| STU-317 | Новый head: регистрация с другим commit в то же окружение заменяет активный manifest (MF-033) и становится текущей; прежние записи остаются историей. Commit никогда не выводится из имени ветки, appUrl или времени. Регистрация не меняет head/published контента, CS, запросы, GitHub-связи |
| STU-318 | Сопоставление запрос ↔ preview вычисляется при чтении, не хранится командой: request недрафтовый и нетерминальный; активная GitHub-связь; repository ID и PR number совпадают с source окружения; baseEnvironment = environment запроса; commit текущей регистрации = head SHA последнего наблюдения этой связи. Иначе — состояние с конкретной причиной (§5), без подстановки |
| STU-319 | Состояния сопоставления: `ready`, `ready_last_known` (совпало с last-known, наблюдение несвежее), `commit_behind` (GitHub видит более новый head), `commit_unverified` (регистрация новее наблюдения), `key_missing`, `contract_incompatible`, `not_registered`, `no_github_link`, `environment_expired`. Состояние не выводится из ручного pr_open или зелёного CI |
| STU-320 | Подстановка pending допустима только в `ready`/`ready_last_known`, только для того requestId и узла/события из requiredBy, только если ключ есть в активном manifest preview, его args совместимы с proposedContract по существующему валидатору и сохранённые preview-аргументы проходят проверку L1–L4 этого manifest. Остальные pending и чужие запросы остаются pending |
| STU-321 | Preview-аргументы задаёт владелец CS формой STU-240 по контракту preview-manifest командой `set-capability-preview-args` (expectedRequestRevision, environment, manifestHash). Они хранятся вне IR, привязаны к manifest hash и устаревают при новой регистрации; не меняют seq CS, историю, pack, approval hash. Действие без обязательных args не требует формы |
| STU-322 | Подстановка выполняется только в Delivery preview-ответе для токена, выпущенного на это preview-окружение и этот CS. Published-чтение, preview standard-окружения, проверки подачи/согласования/публикации и сохранённые diagnostics по-прежнему видят `pending:<id>` и блокируют публикацию (IR-053, STU-269) |
| STU-323 | Содержимое preview: рабочие версии CS поверх опубликованного состояния baseEnvironment; preview-окружение собственных публикаций не имеет. Diagnostics CS по preview-manifest вычисляются при чтении и не записываются в CS; MF-033-обновление диагностик не трогает CS standard-окружений |
| STU-324 | MF-031: publish, promote (источник и цель), rollback, approve/submit с preview в targets, schema-активация и `resolve-capability-request` против preview-manifest отклоняются `ENVIRONMENT_PREVIEW_READ_ONLY` до любых изменений. Существующее ограничение targets standard сохраняется и покрыто тестом с этим ID |
| STU-325 | `delete-environment` (service `manifest.register` либо project.admin) только для kind=preview: архивирует окружение, снимает active manifest, отзывает preview-токены сменой preview_key, освобождает имя для повторного создания. Manifests, индексы, журнал регистраций и аудит сохраняются. Standard — `ENVIRONMENT_NOT_EPHEMERAL`. Повтор идемпотентен |
| STU-326 | TTL MF-032: окружение архивируется автоматически через 14 дней после последней регистрации (для окружения без регистраций — после создания). Периодическая River-задача идемпотентна, конкурентно безопасна с регистрацией под блокировкой окружения; регистрация продлевает срок. Ни Query, ни просмотр в Studio срок не продлевают |
| STU-327 | Права: создание/регистрация/удаление — как в STU-315/325, агент и human без этих прав — запрет. Просмотр CS в preview — только тот, кто уже видит CS и имеет design.read; code.propose не даёт доступа к IR/preview страницы (STU-266). Сводка сопоставления в карточке запроса — по правам GitHub-проекции (STU-308). Чужой проект/окружение — одинаковый 404 |
| STU-328 | appUrl регистрации — абсолютный HTTPS (HTTP только loopback в dev), без userinfo/query/fragment, origin из операторского списка `CMS_PREVIEW_APP_ORIGINS` (точные origin либо шаблон одного левого DNS-ярлыка). Иначе `PREVIEW_APP_ORIGIN_NOT_ALLOWED`. Studio получает тот же список для CSP frame-src и postMessage; произвольный URL не встраивается и не открывается |
| STU-329 | Preview-токен выпускается на окружение и регистрацию (seq); при новой регистрации, архиве или смене состояния сопоставления текущий preview в Studio показывает устаревание и требует явного перезапуска. Поздние сообщения старого iframe не применяются. Токен не даёт доступа к другим CS/окружениям |
| STU-330 | Studio: в карточке запроса раздел «Предпросмотр» — окружение, PR, короткий/полный commit, время регистрации, состояние с причиной, кнопка «Открыть в предпросмотре». В редакторе — выбор окружения просмотра (рабочее standard или preview/pr-N); в preview только просмотр и interact, панели правки и подача недоступны с объяснением. Узел с подставленным действием помечен «Действие из PR #N, не опубликуемо» |
| STU-331 | Подстановка и регистрации не меняют request status/revision, external_refs, GitHub-связь/наблюдения, pack/delivery, IR, CS seq/history, approvals, publication. `ready` не переводит запрос в fulfilled и не включает resolve; существующий ручной resolve по standard-manifest работает как раньше |
| STU-332 | Аудит: создание/регистрация/архив/TTL окружения, preview-аргументы и выпуск preview-токена. Метрики без appUrl, имён веток и токенов. Ответы no-store; в DTO нет preview_key, тела manifest сверх существующих проекций, URL из GitHub |
| STU-333 | Миграция/SQL/команды/TTL-задача/Delivery-проекция/BFF/Studio и отрицательные сценарии покрыты тестами с ID требований; пороги и таймауты существующих тестов не ослабляются; полный CI обязателен |
| STU-334 | Локальная приёмка (§9) с настоящими CMS/PostgreSQL/OIDC/Studio, двумя людьми, двумя сборками React-витрины (standard и preview) подтверждает сценарий от регистрации до просмотра и исполнения действия, новый head, архив/TTL и отказ публикации. Внешняя проверка с реальным GitHub — отдельно и только с разрешения |

## 4. Модель данных и команды

Миграция 00023:

- `environments`: `source_base_environment_id` (FK на standard того же проекта),
  `source_config_id`, `source_repository_id`, `source_pull_number`,
  `last_registered_at`, `archived_at`; `expires_at` начинает использоваться.
  Уникальность имени заменяется частичным индексом по неархивным окружениям;
  CHECK: source только у kind=preview, все поля source заданы вместе.
  Частичный уникальный индекс активного preview на
  (project, base environment, repository ID, pull number).
- `preview_registrations`: id, project, environment, seq, manifest id, commit,
  app_url, registered_by, registered_at; UNIQUE(environment, seq).
  Composite FK project/environment/manifest. Append-only.
- `capability_preview_args`: project, request, environment, manifest id, args jsonb,
  revision, set_by, set_at; одна текущая запись на (request, environment).
- Новые права не вводятся; используются существующие `manifest.register`,
  `project.admin`, `design.read`, `capability.request`, `behavior.use`.

Down удаляет только новые таблицы/колонки; данные #60…#62 не затрагиваются.

| Команда | Payload сверх project/reason/idempotency key | Кто | Результат |
|---|---|---|---|
| create-environment (расширение) | name, kind=preview, appUrl?, source {baseEnvironment, pullNumber} | service manifest.register, project.admin | Окружение с источником; repository из конфигурации base |
| register-manifest (расширение) | manifest, environment, codeIndexUploadId, headSha, appUrl? | service manifest.register | Как сейчас + `previewRegistration {seq, commit}` |
| delete-environment | environment, expectedRegistrationSeq? | service manifest.register, project.admin | archived, archivedAt; повтор — тот же ответ |
| set-capability-preview-args | requestId, expectedRequestRevision, environment, manifestHash, args, expectedArgsRevision | Человек — владелец CS, capability.request/design.read/behavior.use | Сохранённые args и revision |

`create-environment` для preview с source проверяет: включённая
GitHub-конфигурация baseEnvironment, `pullNumber` — положительное целое,
имя = `preview/pr-<pullNumber>`. Повторное создание уже существующего неархивного
окружения с тем же source возвращает его (идемпотентно для CI, перезапускающего
пайплайн); с другим source — `ENVIRONMENT_EXISTS`. Сетевых вызовов GitHub нет:
существование PR подтверждается только наблюдениями связи запроса (STU-318).

`register-manifest` в preview с source берёт блокировку окружения в общем порядке
(окружения по имени → CS → объекты), проверяет `archived_at IS NULL`, индекс,
commit и appUrl, затем выполняет существующую активацию MF-033 и в той же
транзакции пишет регистрацию и `last_registered_at`, `expires_at = +14 дней`.
Регистрация в архивное окружение — `ENVIRONMENT_ARCHIVED`; CI должен заново
выполнить `create-environment`.

`delete-environment` под той же блокировкой: `archived_at`, `active_manifest_id = NULL`,
новый `preview_key`, аудит. Связанные preview-args и регистрации остаются для истории,
но становятся недоступны для подстановки. `expectedRegistrationSeq` защищает от
удаления окружения, в которое только что зарегистрирована новая сборка (опционально
для CI; без него удаление безусловно).

## 5. Сопоставление и проекция

Query в проектном scope, без сетевых вызовов:

- `GET /api/v1/capability-requests/{id}/preview?environment=<standard>` — список
  неархивных preview-окружений того же source, для каждого: имя, PR, commit
  текущей регистрации, registeredAt, appUrl (только origin + path), состояние
  STU-319 с причиной, ключ в manifest (да/нет), совместимость, статус preview-args.
  Права — как GitHub-проекция (STU-308).
- `GET /api/v1/environments` — существующий список дополняется для preview
  source, последней регистрацией, expiresAt; архивные не возвращаются. Внутренние
  обходы окружений (workflow, primary content, reviewdiff) тоже исключают архивные.
- `GET /api/v1/changesets/{id}/preview?environment=preview/pr-N` — diagnostics CS
  по preview-manifest (вычисляются при чтении) и список подстановок
  {requestId, nodeId, event, key, state}. Права — видимость CS и design.read.

Правило совпадения commit: берётся последнее наблюдение активной связи (current
или last-known), его `headSha` сравнивается с commit текущей регистрации
побайтно. `integrationSha` в сопоставлении не участвует: preview-сборка PR
по определению собрана из head; CI merge-коммита — тема пакета №2.
Если наблюдение старше `freshUntil`, состояние `ready_last_known` и Studio
показывает время наблюдения и кнопку существующего refresh (STU-305 лимиты).

Delivery preview-проекция (сервер, перед отдачей приложению): для каждой цепочки
события с элементом `pending:<id>` проверяется STU-320; при успехе элемент
заменяется на `{action: key, args: previewArgs}`; иначе цепочка остаётся как есть
и runtime её не исполняет. Проекция детерминирована, не меняет хэш рабочего IR
в CMS; хэш отданного preview-snapshot считается от уже проецированного тела,
чтобы JSON Patch protocol моста не смешивал версии. Studio не отправляет patch
в iframe preview-окружения (только reload), так как правка там недоступна.

## 6. Происхождение и что не доказывается

Цепочка доверия: GitHub API (STU-299…301) подтверждает repository/PR/head SHA;
сервисный актор CI с `manifest.register` заявляет commit через индекс и `headSha`;
CMS сверяет их между собой и с наблюдением. CMS **не** проверяет, что сборка
по appUrl действительно содержит этот commit, что manifest построен из него
без подмены, и что CI-актор собирал именно этот PR: это доверие к сервисному
токену, так же как для standard-регистрации. UI называет состояние
«Сборка заявлена для commit abc1234; commit совпадает с PR #N по GitHub», а не
«проверено». Подписанная аттестация сборки — открытый вопрос (§11).

Fork: PR из fork допускается так же, как в STU-301 (head repo не расширяет
список репозиториев); источник preview — base repository. Запуск CI для fork и
его доступ к сервисному токену — ответственность оператора; рекомендация
документа — не выдавать `manifest.register` пайплайнам fork-PR.

## 7. Отказы, диагностики и устаревание

| Код | Когда |
|---|---|
| `ENVIRONMENT_PREVIEW_READ_ONLY` | Любая попытка MF-031 (STU-324) |
| `ENVIRONMENT_NOT_EPHEMERAL` | delete-environment для standard |
| `ENVIRONMENT_ARCHIVED` | Регистрация, токен или args для архивного окружения |
| `PREVIEW_SOURCE_INVALID` | Нет/выключена GitHub-конфигурация base, имя ≠ `preview/pr-<N>`, base не standard |
| `PREVIEW_COMMIT_MISMATCH` | headSha ≠ commit индекса, нет индекса, сокращённый SHA |
| `PREVIEW_APP_ORIGIN_NOT_ALLOWED` | appUrl вне операторского списка или с userinfo/query/fragment |
| `PREVIEW_ARGS_STALE` | args сохранены для другого manifest hash |
| `PREVIEW_ARGS_INVALID` | args не проходят L1–L4 preview-manifest |

Состояния STU-319 — не ошибки команд, а поле Query с причиной. Устаревание видно
в трёх местах: карточка запроса, переключатель окружения, баннер открытого
preview. Ни одно состояние не стирает ввод и не меняет CS; переход
`ready` → `commit_behind` после нового push в PR снимает подстановку при следующем
чтении preview. Studio перечитывает сопоставление при открытии, вручную и по
новому событию realtime проекта о регистрации/архиве preview-окружения
(событие содержит только имя окружения и seq, без appUrl и commit).

## 8. Studio

Конфигурация BFF получает `previewAppOrigins` из того же списка, что и сервер;
CSP `frame-src` = `appOrigin` + этот список; проверка `event.origin` моста
сравнивает с origin выбранной регистрации, а не с любым из списка.

Карточка запроса: раздел «Предпросмотр» после «GitHub». Для каждого preview —
окружение, PR, commit (короткий, полный по раскрытию), время регистрации и
истечения, состояние с объяснением, форма preview-аргументов (если нужна),
«Открыть в предпросмотре». Developer с code.propose видит состояние, но не кнопку
открытия CS (STU-327).

Редактор: выбор «Окружение просмотра» рядом с переключателем режима.
В preview-окружении: только Design (выбор узла) и Interact; палитра, свойства,
DnD, undo/redo, подача отключены с подписью «Режим предпросмотра PR #N: только
просмотр». Несохранённые правки блокируют переключение (существующий dirty guard).
Баннер: commit, время, состояние; при устаревании — «Перезапустить предпросмотр».
Узел с подставленным действием отмечен значком и подсказкой; pending без
подстановки — существующий маркер с причиной из STU-319.

RU/EN, клавиатура, focus/Escape, 375 px, exact retry `set-capability-preview-args`
после потери ответа, конфликт revision без потери ввода — как в предыдущих пакетах.

## 9. Проверки и локальная приёмка

| Область | Обязательные сценарии |
|---|---|
| Окружения/DB | Создание с/без source; неверное имя/base/конфигурация; повтор create; уникальность source; миграция up/down без потери #60…#62; архив и повторное создание того же имени |
| Регистрация | Нет индекса; короткий/несовпадающий headSha; appUrl вне списка, с query/userinfo; повтор hash; новый head; регистрация в архив; гонка регистрации и delete; rollback при сбое записи регистрации |
| TTL | 14 дней без регистраций; продление регистрацией; Query не продлевает; два worker; рестарт; архив отзывает токены |
| Сопоставление | Все состояния STU-319; чужой repo/PR/base; терминальный/draft запрос; unlink; last-known и stale; новый push до/после сборки; fork |
| Проекция | Только свой requestId/узел/событие; несовместимый контракт; отсутствующий ключ; args устарели/невалидны; published и standard preview видят pending; hash snapshot; runtime исполняет подставленное действие только в interact |
| MF-031 | publish/promote/rollback/submit/approve/schema/resolve с preview — отказ до изменений; targets standard |
| Права | code.propose без доступа к CS; агент/human без manifest.register; чужой проект; отзыв design.read при открытом preview |
| Studio | RU/EN/375 px; клавиатура/focus/Escape; режим только просмотра; dirty guard; устаревание и перезапуск; CSP/origin; exact retry args |
| Regression | Request status/revision, CS seq/history, pack/delivery, GitHub-наблюдения, approvals неизменны; ручной resolve и блокировка публикации pending работают |

План локальной приёмки (как в STU-274/314, отчёт отдельным acceptance-документом):

1. Поднять CMS/PostgreSQL/OIDC/Studio локально, два человека: владелец CS
   (designer) и разработчик (code.propose); сервисный токен CI с `manifest.register`.
2. Включить GitHub-конфигурацию для staging. Для детерминированности —
   HTTP fixture GitHub (наблюдения PR с head SHA A, затем B); реальный GitHub —
   отдельный необязательный шаг с разрешённым PR, без изменения внешнего репозитория.
3. Владелец CS создаёт запрос `commerce.product.quickPreview` формами, заглушка
   в странице; разработчик связывает PR #N.
4. Собрать вторую копию `examples/react-store` с добавленным действием на
   отдельном loopback-порту (preview-развёртывание); CLI: `create-environment`
   `preview/pr-N` с source, `upload-code-index` с commit A, `register-manifest`
   с `headSha=A` и appUrl второй копии.
5. Карточка запроса: `ready`; задать args формой; открыть CS в preview,
   в Interact клик открывает модальное окно из PR. В staging-preview той же
   страницы узел остаётся pending.
6. Попытки publish/promote/submit с preview — отказ; подача CS в staging
   по-прежнему блокируется pending.
7. Fixture: новый head B → `commit_behind`, подстановка снята, баннер устарел;
   регистрация с B → снова `ready`, args `PREVIEW_ARGS_STALE` при смене hash.
8. `delete-environment` → архив, preview в Studio недоступен, токен отклоняется;
   повторное создание того же имени работает. TTL — тестом с управляемыми часами,
   а не ожиданием 14 дней.
9. Проверить неизменность request revision/status, CS seq, pack, GitHub-наблюдений;
   ручной resolve после регистрации в standard работает как в STU-268.
10. RU/EN, 375 px, новый браузер для опубликованной страницы без изменений.

Проверки реализации: sqlc/codegen и чистый generated diff, Go build/vet/race,
PostgreSQL tests, TS/React/runtime/Studio тесты, format/lint/typecheck/build,
браузерные сценарии и Docker image. Полный M5/MVP не объявляется завершённым.

## 10. Порядок реализации после одобрения

1. Миграция 00023, source/архив/регистрации, расширения create-environment и
   register-manifest, delete-environment, MF-031 и contract tests.
2. TTL River-задача, блокировки, отзыв токенов.
3. Сопоставление, Query API, preview-args и Delivery-проекция с тестами runtime.
4. BFF/CSP, карточка запроса, выбор окружения просмотра, RU/EN/accessibility.
5. Полный CI, локальная приёмка, отчёт implemented/remaining/dependencies.

Следующий пакет — автоматическое fulfillment и requiredBy: он сможет опереться
на журнал регистраций (commit → manifest) этого пакета, но требует регистрации
в standard и атомарной замены заглушек; preview-аргументы могут стать префиллом
его формы, но не согласием на замену.

## 11. Открытые вопросы для продуктового решения

1. **Имя окружения.** `preview/pr-<N>` уникально в проекте; если два standard-окружения
   настроены на один репозиторий, второе получит конфликт. Принять это ограничение
   v1 или включать base в имя (`preview/staging-pr-<N>`)?
2. **Свежесть наблюдения.** Разрешать подстановку по last-known (`ready_last_known`)
   или требовать свежее наблюдение GitHub (тогда каждые 15 минут нужен refresh
   в пределах лимитов STU-305)?
3. **Preview-аргументы.** Включить форму args в этот пакет (нужна для
   `quickPreview(product)`) или ограничить v1 действиями без обязательных args?
4. **Origin preview-приложений.** Разрешить шаблон `https://*.preview.example.com`
   (типично для Vercel/Netlify) или только точный список origin?
5. **TTL при активном PR.** Архивировать через 14 дней без регистраций даже при
   открытом PR (как в MF-032) или продлевать, пока связь видит PR открытым?
6. **Закрытие PR.** Архивировать preview автоматически, когда наблюдение GitHub
   показывает closed/merged, или только по CI-команде и TTL?
7. **Аттестация сборки.** Достаточно ли доверия сервисному токену CI для связи
   commit ↔ развёртывание, или нужна подписанная аттестация (отдельный пакет)?
8. **Режим только просмотра.** Устраивает ли запрет правки в preview-окружении
   (правка — переключиться обратно на standard) для v1?
