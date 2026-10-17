# Studio: Missing Capability и context pack

Статус: STU-255…274 реализованы и слиты в dev 2026-10-17 в
[PR #60](https://github.com/QahNaaRlN/project-17/pull/60), merge
`9259d6272471cba057c0625bd554bb730ae64660`.
Реальная локальная приёмка и [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/39028004513)
пройдены: Check (TS + Go), Go race/coverage 95%, Studio 328, 60 браузерных
сценариев и Docker image.
Дата: 2026-10-16. Основа — `origin/dev` на
`35ccee43467d8e36a412c93a2e15c78a08df330d`, после
[PR #59](https://github.com/QahNaaRlN/project-17/pull/59).
[CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/38922047905)
пройден: Check (TS + Go) и Docker image. Пакет продолжает
[действия и условия](studio-behavior-conditions.md),
[Studio Agent](studio-agent.md) и [Registry DSL/CLI](registry-dsl-cli.md).
Архитектурная цель: [Missing Capability и структурный контекст](spec/09-agent.md#5-missing-capability),
FR-052/053, AGT-060…062, AGT-070…072, AGT-080/081.

## 1. Результат и границы

Дизайнер в CODE-зоне описывает недостающее действие, проверяет предложенный
контракт и состав передаваемого контекста, затем явно отправляет запрос.
В выбранном событии появляется `pending:<requestId>`; публикация блокируется.
Разработчик получает ограниченный JSON context pack, работает в репозитории
со своими правами и вручную добавляет ссылку на PR. После развёртывания приложения
и активации совместимого manifest владелец CS явно заменяет заглушку настоящим
действием. Обычные согласование и публикация остаются обязательными.

Первый пакет поддерживает только `kind: action`, одну привязку
`changesetId/documentId/nodeId/event` на запрос и стандартное исходное окружение.
Он включает серверную загрузку существующего индекса кода, безопасный context
pack, формы запроса/очереди, ручной handoff и два scoped MCP-инструмента.
CMS не читает репозиторий и не исполняет код или команды из context pack.

Webhook/HMAC/outbox доставки кодовому агенту, трекеры, автоматическое определение
статуса PR/CI, fulfillment по регистрации manifest, массовая замена requiredBy
и разрешение pending в `preview/<ref>` — следующие пакеты. Также вне границ
component/dataSource/token requests, кодогенерация, автоматическое открытие PR,
полная условная policy, component.extract, advanced bindings, Figma и production.
Полный M5 и сквозной шаг 10 MVP этим первым пакетом не закрываются.
Это поэтапная реализация §09.5, а не замена его целевого автоматического процесса.

## 2. Проверенная исходная точка

| Область         | Есть в dev                                                                                          | Нужно добавить                                                                                                      |
| --------------- | --------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| Формы поведения | События, каталог, args, exact retry, history, preview; STU-237…254 слиты                            | Создание запроса и управляемая заглушка рядом с формой события                                                      |
| IR/проверки     | Синтаксис pending в IR; CODE-проверка в policydoc; ACTION_PENDING_CAPABILITY блокирует публикацию   | Проверку существования/проекта/привязки запроса; безопасный preview заглушки                                        |
| Права           | capability.request и code.propose уже объявлены в auth                                              | Командные и Query-проверки, проекции для автора и разработчика                                                      |
| CLI             | source-index.ts строит пути, экспорты, строки, зависимости глубины 2 и тесты; cli.ts проверяет hash | Передачу индекса на сервер и связь с неизменяемым manifest                                                          |
| Сервер manifest | manifests.code_index_key существует; register-manifest отклоняет codeIndexUploadId и uploaded:true  | Проверенное хранение индекса и атомарную привязку при регистрации                                                   |
| Запросы/pack    | capability_requests описаны только в архитектурном DDL; в миграциях до 00019 их нет                 | Реальные миграции, команды, Query API, аудит и версии pack                                                          |
| Agent/MCP       | Каталог семантических инструментов и ограничения сессий                                             | report_missing_capability и get_capability_request; сейчас агент объясняет отсутствие возможности и останавливается |

Опорный код: [manifestregistry](../apps/server/internal/manifestregistry/registry.go),
[CLI](../packages/component-registry/src/cli.ts),
[индекс](../packages/component-registry/src/source-index.ts),
[MCP tools](../apps/server/internal/agents/tools.go),
[права](../apps/server/internal/auth/rights.go),
[проверка pending](../apps/server/internal/composition/bindingdoc/validate.go).
Существующая сборка индекса не означает, что серверный context pack уже работает.

## 3. Требования

| ID      | Требование                                                                                                                                                                                                                                                                                                                                                                          |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| STU-255 | В панели события есть «Нужно другое действие…» для выбранного узла собственного открытого standard CS в эффективной CODE-зоне. Показываются причина недоступности, готовые действия и ссылка на доступный существующий запрос. Наличие кнопки не выдаёт behavior.use или design.zones.manage.                                                                                       |
| STU-256 | Форма задаёт ключ action, заголовок, описание, acceptance и proposedContract.args через существующие типы manifest. Поиск существующих действий/открытых запросов детерминированный, без LLM. Есть валидация имени, ограничений и конфликтов; нет ручного JSON, JS/HTTP или кода handler.                                                                                           |
| STU-257 | Запрос хранится в PostgreSQL с project, kind, исходным environment/manifestHash, creator/onBehalfOf, одной привязкой requiredBy, status, revision и аудитом. Draft не меняет IR и не передаёт контекст разработчикам. Любое изменение использует expectedRevision и Command Bus.                                                                                                    |
| STU-258 | Ключ отсутствует в активном manifest исходного окружения и среди builtin. Открытый запрос уникален по project/environment/kind/key под транзакционной блокировкой/уникальным индексом. Гонка создания не даёт два запроса; ошибки и ответы не раскрывают недоступные чужие draft.                                                                                                   |
| STU-259 | Человек явно подтверждает «Отправить запрос и добавить заглушку». Сервер в одной транзакции проверяет revision/seq/manifest, создаёт immutable pack, переводит draft в submitted и записывает node.setBehavior для выбранного события. Замена непустой цепочки требует отдельного явного подтверждения показанного diff. Ошибка откатывает всё.                                     |
| STU-260 | Создание требует capability.request и design.read; изменение IR дополнительно behavior.use, владения CS, CODE и всех обычных locked/primary-content проверок. code.propose не даёт права редактировать чужую страницу, согласовывать или публиковать. Права повторяются при команде, чтении/download и agent-session revalidation.                                                  |
| STU-261 | V1 реализует draft → submitted → accepted → in_progress → pr_open; reject/cancel и явное завершение описаны ниже. Клиент не присваивает произвольный status. Переходы и причины аудируются; конфликт revision не затирает ввод. Автоматического fulfilled по одному совпадению ключа нет.                                                                                           |
| STU-262 | Pack фиксирует request revision, project, environment, manifest hash, code-index hash/commit, CS seq и версию/хэш страницы. Новые manifest/CS не переписывают экспортированный pack. Перед отправкой проверяется свежесть; после изменений показывается stale и предлагается явная пересборка новой ревизией.                                                                       |
| STU-263 | CLI загружает code-index.json с hash и привязкой к manifest; сервер проверяет строгую схему, размер, пути и canonical hash. Индекс immutable, изолирован по проекту; повтор с тем же содержимым не создаёт новый файл. Нет исходников, архивов, URL-fetch, чтения файлов или исполнения conventions на сервере.                                                                     |
| STU-264 | register-manifest принимает только готовый индекс того же проекта и заявленного hash; false uploaded не заменяет проверку хранения. Upload/register не меняют manifest.json/manifest.hash и deployed hash после build. Сохранённые manifest и связанный индекс нельзя подменить повторной регистрацией. Старые клиенты без индекса продолжают работать без context pack.            |
| STU-265 | Pack собирается без LLM: UI-chain, нужные контракты/схемы, записи индекса, задача и ограничения. Выбор и сортировка стабильны; максимум 40 уникальных файлов, приоритет definitions/implementations, затем tests, registry и dependencies. Truncation и отсутствующие данные явны; содержимое файлов и данные сущностей не включаются.                                              |
| STU-266 | Автор видит preview точного состава pack до submit. После его явной отправки code.propose получает только безопасную handoff-проекцию и pack, без доступа к raw IR/preview token/сущностям/чужой очереди CS. Draft доступен автору/его действующей scoped agent-session; нельзя угадать UUID другого проекта или скачать pack после отзыва права.                                   |
| STU-267 | Разработчик явно принимает задачу, отмечает работу и добавляет проверенную по формату HTTPS-ссылку на PR. CMS хранит ссылку и аудит, но не обращается к GitHub и не объявляет PR слитым/CI пройденным по этой строке. Download/copy не создают внешнюю задачу и не меняют status.                                                                                                   |
| STU-268 | Явное «Применить доступное действие» доступно владельцу открытого CS после pr_open и появления совместимого ключа в активном standard manifest исходного окружения. Args настраиваются формой STU-240. Под одной транзакцией проверяются актуальные seq/revision/manifest, точная заглушка, контракт и права; node.setBehavior и fulfilled_manifest_id/status фиксируются атомарно. |
| STU-269 | Pending никогда не исполняется; наличие pending блокирует всё событие, включая остальные шаги цепочки. Preview сохраняет узел с безопасной диагностикой/маркером, Studio оставляет его в дереве. Публикация повторно отклоняет pending, даже если status запроса fulfilled или ключ существует в другом окружении.                                                                  |
| STU-270 | Все команды идемпотентны. Потерянный ответ повторяется с теми же key/body/seq/revision, без второго запроса/pack/IR-жеста. IR-изменения попадают в общую историю с inverse/rebase. Отмена IR-жеста не отменяет отправленный запрос и не откатывает работу разработчика; рассинхронизация явно показана.                                                                             |
| STU-271 | report_missing_capability создаёт только draft в разрешённых CS/document/environment; get_capability_request читает scoped проекцию. Используются общий каталог MCP/Agent, текущие права делегата/политика, quota и reason. Агент не отправляет handoff, не меняет engineering statuses, не получает code.propose и не выполняет resolve/публикацию.                                |
| STU-272 | Формы, карточка и очередь запросов доступны RU/EN, с labels, фокусом, Escape, клавиатурой и 375 px. Dirty guard сохраняет ввод при переходах/локалях/отказе/конфликте. Отмена после отзыва прав не требует записи. Состояния отсутствующего индекса/контракта/привязки объясняются без раскрытия приватных данных.                                                                  |
| STU-273 | Manifest drift, удалённый узел/событие, undo, чужая правка, закрытый CS, отменённый запрос и архив окружения не вызывают скрытых изменений страницы. Сервер возвращает конкретный конфликт и сохраняет историю. Снятие заглушки — отдельная явная операция владельца с обычными правами.                                                                                            |
| STU-274 | Реальная приёмка CMS/PostgreSQL/OIDC/CLI/React создаёт запрос формами, передаёт pack второму человеку, регистрирует собранное приложением действие, явно заменяет pending и публикует после независимого согласования. Проверяются exact retry, конфликты, отзыв прав, два проекта, stale pack, preview и новый публичный браузер.                                                  |

## 4. Модель данных и команды

Добавить следующую после 00019 миграцию, запросы sqlc и модели. Архитектурный
DDL §07 — ориентир, не уже существующие таблицы. В `capability_requests` добавить
kind=action, environment id/name, source manifest id/hash, revision bigint,
одну requiredBy-привязку с event, proposed_contract, acceptance, external_refs,
creator/onBehalfOf/session, timestamps и fulfilled_manifest_id. Создать уникальный
partial index открытых project/environment/kind/key и индексы очереди.
Терминальные состояния: fulfilled/rejected/cancelled. Ключ/окружение/привязка
после submit не меняются; новая задача требует нового запроса.
Исходный manifest сохраняется как provenance; каждая новая версия pack имеет
собственные pinned manifest id/hash и page context, не переписывая исходные.

Отдельно хранить immutable code-index uploads, версии context pack с canonical
hash и append-only историю изменений запроса. Для bounded JSON v1 использовать
PostgreSQL jsonb, без обязательного S3. code_index_key может быть непрозрачным
идентификатором подтверждённой записи; это не URL и не путь локального файла.
Нельзя сохранить request/pack без project-scoped внешних связей и creator.

| Команда                    | Payload и результат                                                                                                                                                      | Кто                                                                |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------ |
| upload-code-index          | manifestHash, indexHash, index; возвращает uploadId/hash/ready                                                                                                           | service + manifest.register                                        |
| create-capability-request  | environment, expectedManifestHash, changesetId, expectedSeq, documentId, nodeId, event, key, title, description, proposedContract, acceptance; возвращает draft/revision | Человек capability.request/design.read либо scoped агент           |
| update-capability-request  | id, expectedRevision, редактируемые поля draft; либо явный transition и reason/PR URL                                                                                    | Автор draft; engineering-переходы — человек/service code.propose   |
| submit-capability-request  | id, expectedRevision, expectedSeq, expectedManifestHash, confirmReplaceExisting; возвращает submitted, новый seq, pack hash и IR-жест                                    | Человек — владелец CS, capability.request/design.read/behavior.use |
| refresh-capability-context | id, expectedRevision, expectedSeq, expectedManifestHash; возвращает новую immutable версию pack                                                                          | Автор с теми же правами чтения контекста                           |
| resolve-capability-request | id, expectedRevision, expectedSeq, expectedManifestHash, handlers; возвращает fulfilled, seq, manifest id и IR-жест                                                      | Человек — владелец CS, capability.request/design.read/behavior.use |

Все команды используют существующий конверт Command Bus с Idempotency-Key
и reason. Upload idempotency дополнительно обеспечивается содержимым.
expectedSeq нужен и при создании draft для привязки контекста; создание draft
не меняет seq CS. Create/refresh доступны только для собственного открытого
standard CS; у агента владельцем считается действующий onBehalfOf, проверенный
через session. Наличие design.read/code.propose не разрешает создать запрос
по чужому черновику. Отсутствие ключа в manifest проверяется при create/submit;
появление ключа после submit не запрещает engineering transitions или resolve.
В submit/resolve блокировки CS, request и environment берутся
в согласованном с publication/manifest порядке, чтобы не образовать deadlock.
Внутреннее применение операции использует общие changes/ops, авторизацию,
inverse, quota, outbox и аудит; прямой UPDATE body страницы запрещён.
Submit сначала проверяет preview ожидаемого изменения, а внутри транзакции
строит pack по итоговому IR/seq после вставки pending. В pack фиксируется хэш
этого body, чтобы только что отправленная задача не стала stale из-за собственного
IR-жеста. Context-preview моделирует ту же операцию без записи.
Studio сохраняет seq/revision/manifestHash именно просмотренного pack в payload
отправки/обновления; live refresh редактора не переносит согласие на новый контекст.
Изменение любого из этих значений приводит к конфликту и повторной проверке.

Лимиты v1: title 1…200 символов, description 1…4000, acceptance 1…16 строк
до 500 символов, args до 32 полей/глубины 8 по валидатору manifest,
external_refs до 8 ссылок по 2048 символов. Неизвестные поля payload отвергаются.
Предлагаемый контракт не создаёт новый элемент manifest и не разрешает handler.
В форме и API v1 доступны string/text/number/boolean/enum/reference/link,
required, enum values, schema для reference, числовые min/max и длины строк.
Object/list args и произвольные pattern/formatter остаются за границами
конструктора proposedContract; неподдержанные типы не упрощаются автоматически.

Переходы: автор редактирует/удаляет draft и может отменить нетерминальный запрос;
разработчик с code.propose делает submitted→accepted→in_progress→pr_open,
submitted/accepted/in_progress→rejected с причиной. PR URL обязателен для pr_open.
Возврата из терминального состояния и ручного присвоения fulfilled нет.
Resolve — отдельный переход pr_open→fulfilled. Роль project.admin сама по себе
не обходит требуемые права и CS ownership. Cancel/reject не удаляют pending.

Submit создаёт chain из одного `{action: pending:<id>}` без args; прежняя цепочка
сохраняется в inverse. Resolve заменяет только точную одиночную заглушку данного
запроса одним настоящим ActionCall с проверенными args. Изменённая/смешанная
цепочка требует ручной правки поведения; команда ничего не ищет и не заменяет
по всему IR. Любой новый pending при обычном apply-operations также проверяется
на существующий submitted/accepted/in_progress/pr_open запрос, project и anchor;
draft/cancelled/чужой запрос использовать нельзя. Для undo/redo прежней операции
запрос сохраняется в аудите; при устаревшем статусе заглушка допустима только как
исторический неисполняемый IR с диагностикой и запретом публикации. Это исключение
не выдаёт право вставлять её новой обычной командой.
History-исключение подтверждается исходной операцией защищённого журнала,
а не переданным клиентом флагом. Legacy pending без сохранённого запроса виден
только для чтения/явного снятия; соседняя правка не превращает его в новый запрос.

Совместимость при resolve проверяется строго: каждое proposed поле существует
в реальных args и SimpleContentCompatible(proposed, actual) истинно; новые
реальные поля допустимы только как optional/default. handlers содержит ровно
один вызов request.key; реальные значения дополнительно проходят актуальную
валидацию ActionCall/scopes/типов без преобразований. Смена предложенного
контракта для обхода несовместимости после submit запрещена.

Draft удаляется отменой с аудитом, не физическим DELETE. Undo submit/resolve
откатывает только IR-группу. Request status и экспортированный pack сохраняются;
карточка выводит «Заглушка снята/изменена» или «Реализация применена, затем отменена».
Повторно применить действие после undo можно обычной формой поведения;
fulfilled не означает, что каждый CS всё ещё содержит результат.

## 5. Индекс и воспроизводимый context pack

### Загрузка и неизменяемые хэши

CLI сохраняет текущий build: manifest.json с codeIndex.hash и uploaded:false,
manifest.hash, code-index.json. После проверки файлов и optional wait-for-deploy
push загружает индекс, затем передаёт codeIndexUploadId в register-manifest.
Нельзя менять uploaded на true и пересчитывать manifest после развёртывания:
это сломает exact deployed hash. Факт доступности индекса возвращается сервером
как отдельное derived поле codeIndexAvailable; клиентский флаг не является
доказательством загрузки. При реализации уточнить описание uploaded в §04/Query
API, не переименовывая поле и не меняя JSON Schema manifest без необходимости.

Upload содержит canonical JSON не более 1 MiB, до 1000 elements, repo.root='.',
полный commit из 40/64 hex, conventions и записи текущего CodeIndex интерфейса.
Проверяются hash, типы/лимиты строк, уникальные безопасные POSIX relative paths,
положительные номера строк и sourceRef/element соответствия регистрируемому
manifest. Абсолютные/UNC/drive paths, backslash, сегменты '.'/'..', управляющие
символы, node_modules/dist/generated/.git и поля с содержимым файлов запрещены.
Данные conventions — строки для человека; сервер их не выполняет.
Внешние зависимости и generated исключает и CLI, и серверный валидатор путей.
Для upload-code-index HTTP-конверт ограничить 2 MiB, декодированный canonical
index — 1 MiB; лимиты остальных команд не расширять. Неизвестные JSON-поля
и содержимое сверх лимита отклоняются до записи blob/аудита содержимого.

Upload привязан к project и manifestHash; register проверяет uploadId/indexHash
и manifest.codeIndex.hash в своей транзакции. При новой регистрации ссылка
индекса фиксируется вместе с manifest. Повтор сохраняет исходные body, автора,
время и индекс (MF-002), другой индекс не подменяет прежний. Для ранее
зарегистрированного manifest без связи v1 не выполняет скрытый backfill:
повтор не обогащает его, Query сообщает отсутствие индекса. Нужна новая сборка
manifest с новым hash и загруженным индексом. Непривязанные uploads живут 24 ч;
связанные с manifest/pack не удаляются сборщиком. Legacy register без upload
остаётся работоспособным; submit запроса без подтверждённого индекса блокируется.

### Состав и алгоритм

Pack строится из request + working версии страницы + активного manifest/индекса
исходного окружения под одним согласованным снимком. Никаких repository credentials,
сетевого обхода, чтения исходников CMS-файловой системой или дополнительного LLM.

1. Собрать page path и путь предков выбранного node: id/type/name, событие и
   эффективную CODE-политику. Выбрать native/composed типы поддерева; глубину и
   число элементов ограничить 8/100 с явным truncated, без раскрытия чужого body.
2. Собрать контракты выбранных типов, текущих используемых actions/dataSources
   (pending не является зарегистрированным действием),
   элементов с тем же первым domain-сегментом предлагаемого ключа, builtin
   Modal/openModal и схем, на которые ссылается proposedContract. Сортировать по
   kind/key; максимум 100 контрактов. При недостающей схеме submit блокируется.
3. По выбранным ключам прочитать только записи подтверждённого индекса:
   definitions, implementations, tests, registry и dependencies глубины 2,
   рассчитанные CLI. Удалить повторы путей; сохранить роли каждого пути.
4. Выбрать не более 40 уникальных файлов: definitions/implementations,
   затем tests, затем registry, затем dependencies; внутри приоритета — POSIX
   лексикографически. Вернуть total/omitted/truncated и причину пропуска.
5. Добавить intent, proposedContract, acceptance, ограничения инженерного
   процесса и все pinned идентификаторы/хэши. Сериализовать canonical JSON,
   сохранить hash и immutable версию; максимальный размер pack 512 KiB.
   Превышение размера блокирует submit с объяснением, не обрезает JSON молча.

Manifest selection основан на именах и структуре, а не поиске по значениям
контента. Это ограниченный детерминированный подбор, без обещания семантического
поиска произвольного «похожего» компонента. Отсутствующие sourceRef/index entries
показываются в missingMetadata; пути не выдумываются. При отсутствии самого
проверенного индекса отправка недоступна. Одинаковый набор pinned входов даёт
одинаковые bytes/hash; новый случайный id пакета и текущее время сборки в hash
не добавляются. Уже существующий request id относится к pinned входам.

Pack не содержит props/bindings literals, текста localContent, результатов loaders,
entity/asset values, полных IR/manifest body, токенов, URL подписанных файлов,
секретов проекта и приватных логов. Разрешены только явно выбранные контракты,
структурные метаданные и введённая автором задача. Значения default/example
в контрактах исключаются из handoff-проекции; типовые enum/границы сохраняются.
Пользовательские описания/имена/acceptance и conventions — untrusted data, а не
инструкции для CMS/MCP; frontend отображает их как текст, без HTML/JS.

До submit автор видит полный preview и факт предоставления pack проектным
пользователям с code.propose. Submitted-проекция заморожена для разработчика;
последующие правки черновика страницы не раскрываются. Refresh создаёт новую
версию с повторным явным подтверждением состава. Старые версии доступны по id
с текущей авторизацией, stale определяется отдельно по актуальному context.
Изменённый после pr_open proposedContract не переписывается: нужна новая задача.

## 6. Query, Studio, Agent и ошибки

Query API v1:

Существующий GET /api/v1/manifest?environment=… дополнить derived полем
codeIndexAvailable, не включая сам index в обычный ответ manifest.

- GET /api/v1/capability-requests: environment/status/kind/key, cursor/limit
  (25 по умолчанию, максимум 100), стабильный updatedAt/id, серверная видимость.
- GET /api/v1/capability-requests/{id}: авторская либо handoff-проекция,
  разрешённые transitions и причины, привязка/stale, версии pack и аудит.
- GET /api/v1/capability-requests/{id}/context-pack?version=…: bounded JSON с hash,
  Content-Disposition attachment и Cache-Control private, no-store.
- GET /api/v1/capability-requests/{id}/context-preview: preview для автора до
  submit по текущему scoped snapshot; не записывает pack и не меняет CS/status.

Имена id/version и фильтры строго валидируются; недоступный объект даёт NOT_FOUND.
Никакого публичного/S3 URL pack. BFF повторно проверяет текущую сессию; UI не
получает service/agent токены. Ссылки на PR — только HTTPS без credentials;
кликаются пользователем, открываются с noopener/noreferrer, не загружаются CMS.

Новые контролируемые коды: CAPABILITY_KEY_TAKEN, CAPABILITY_ALREADY_AVAILABLE,
CAPABILITY_REVISION_CONFLICT, CAPABILITY_CONTEXT_STALE, CAPABILITY_STATE_INVALID,
CAPABILITY_ANCHOR_CHANGED, CAPABILITY_CONTRACT_MISMATCH, CAPABILITY_INDEX_UNAVAILABLE,
CODE_INDEX_INVALID, CODE_INDEX_HASH_MISMATCH, CODE_INDEX_IMMUTABLE,
CONTEXT_PACK_LIMIT_EXCEEDED. Сохраняются CHANGESET_SEQ_CONFLICT, FORBIDDEN,
POLICY_ZONES_MANAGE_REQUIRED и ACTION_PENDING_CAPABILITY. Ошибки не возвращают
сырой index/pack/exception; параметры ограничены безопасными полями/pointers.

Studio добавляет карточку запроса к событию и отдельную очередь Missing Capability
в текущем проекте/окружении. Request revision, page seq и engineering status
показываются раздельно. Экспорт доступен без автоматической смены status.
Нет фиктивной кнопки запуска кодового агента при отсутствии интеграции.
После появления ключа карточка предлагает явную проверку/настройку args;
каталог не исполняет handler, для проверки остаётся явный interact.

MCP report_missing_capability использует те же поля/валидацию create, reason,
expectedSeq, bound CS/document/environment, session/onBehalfOf и эффективные
capability.request/design.read. Draft учитывается в квоте как одна новая команда;
идемпотентный replay не расходует её второй раз. get_capability_request доступен
только для запросов текущего делегата в разрешённой привязке. Агенту недоступны
engineering transitions, submit/resolve, полный код, произвольный URL и download
чужого pack. Agent run показывает созданный draft человеку и завершает шаг;
права и инструкции не расширяются текстом задачи или manifest.

## 7. Реализация и проверки

Порядок реализации:

1. Общий строгий CodeIndex-контракт/Go-TS fixtures; миграция и sqlc; upload и
   register/CLI с проверкой неизменности deployed hash и MF-002.
2. Capability service, commands/Query, revision/dedup/visibility; pinned builder
   pack и previews. Аудит request не смешивать с undo-жестами IR.
3. Атомарные submit/resolve на существующих ops; CODE/locked/content gates;
   проверка pending в обычных командах, inverse/history/rebase и публикации.
4. Формы, карточка/очередь, RU/EN/375 px, точные retry и server-side cursor.
   Runtime безопасного pending; обновление общего каталога Agent/MCP.
5. Реальная приёмка и CI; синхронизация описаний Query/manifest/MCP/хранения
   с фактической реализацией и roadmap без объявления полного M5 завершённым.

Затронутые места: apps/server/db/migrations и queries, internal/store,
manifestregistry, новый capability/context service, changes/workflow/validation,
httpapi/BFF, agents/tools; packages/component-registry, manifest/code-index
conformance, runtime/react и packages/mcp; apps/studio формы/очередь/behavior.
IR 1.0 и типы node.setBehavior сохраняются; новая IR-операция не нужна.

| Проверка       | Обязательные доказательства                                                                                                                                                                                                  |
| -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Индекс/CLI     | traversal/absolute/unknown fields/oversize/hash mismatch, два проекта, чужой upload, canonical Go/TS, отсутствие исходников, exact deployed hash, retry upload/register, legacy manifest без index                           |
| Запросы        | одновременный duplicate key, expectedRevision/seq/manifest, rollback pack+status+ops, пагинация/видимость, replay после commit, quota/agent revocation, отсутствующий CODE/behavior.use/content.read/locked manage           |
| Pack           | golden fixture детерминированности, приоритет и лимит 40, недостающие metadata, stale/новая версия, нет secrets/props/local/entity/loader values, отозванный download и межпроектный UUID                                    |
| IR/runtime     | pending в FREE/STRICT/CODE, отсутствующий/чужой/терминальный request, event с pending не исполняется целиком, preview не теряет узел, public pipeline блокирует даже forged fulfilled, история/undo/rebase без обхода правил |
| Fulfillment v1 | ключ в preview/другом env/candidate не подходит; несовместимые args, смена manifest во время resolve, чужая правка event, только точная заглушка, одна IR-группа и атомарный статус, approvals не обходятся                  |
| UI/MCP         | RU/EN/375 px/keyboard/focus, dirty/forbidden/conflict/exact retry, ручной export без сетевой доставки, scoped draft tool без submit/engineering permissions и prompt injection                                               |

Приёмка: отдельный loopback CMS/PostgreSQL/OIDC и два человека; первая сборка
React-витрины ещё без store.quickPreview, но с загруженным индексом. Автор
создаёт CODE-запрос с acceptance, просматривает pack и явно отправляет pending.
Проверяются preview без эффектов и отказ публикации. Разработчик со своим
code.propose скачивает только pack, вручную отмечает accepted/in_progress/pr_open.
Тестовая реализация/registry/тесты собираются обычным CLI из репозитория;
новый deployed hash совпадает с зарегистрированным активным standard manifest.
Владелец явно настраивает args и заменяет pending, проходит независимое
согласование/публикацию. Новый public browser видит настоящее действие, не draft.
Нельзя выдавать локально смоделированные PR/CI/webhook за реальную GitHub-интеграцию.

Перед завершением: generation/sqlc и Go build, целевые Go/PostgreSQL/TS наборы,
пороги покрытия без снижения, format/lint/typecheck/build, браузерные сценарии
и Linux CI с race. Для spec-only изменения достаточно проверить ссылки,
уникальность STU-ID, полноту API/матрицы состояний и форматирование.

## 8. Критерий готовности и продолжение

Реализация использует миграцию 00020, внутренний capabilities service, обычный
Command Bus/history и загруженный индекс, связанный с неизменяемым manifest.
Studio содержит типизированную форму, очередь, карточку, просмотр/скачивание
контекста и явное применение доступного действия. Встроенный Agent создаёт
только draft и завершает запуск с переходом к карточке для человека.

Повторяемая приёмка: `scripts/studio-capability-baseline.mjs`, затем
`scripts/studio-review-stand.mjs` с `CMS_REVIEW_CAPABILITIES=true` и
`scripts/studio-live-capabilities.mjs`. Нужны отдельная мигрированная loopback
база, собранные CMS/Studio, путь к установленному pnpm и отдельный каталог
для приватной конфигурации/результатов. Скрипты не выводят credentials.
Вместо нового quickPreview сценарий использует реальное store.favorite:
первая сборка временно исключает объявление, затем исходник восстанавливается,
приложение собирается и регистрируется через CLI с exact deployed hash.
Два OIDC-пользователя проходят формы draft/submit, потерю ответа и точный retry,
экспорт pack, ручные engineering-переходы, resolve и независимую публикацию;
новый public browser выполняет действие. Проверены сохранение узла без эффектов,
неизменность старого pack и отсутствие горизонтального скролла на 375 px.
Ссылка на PR в этом сценарии — введённые человеком тестовые данные;
получение фактов GitHub/CI и внешняя доставка этим не подтверждаются.

Конфликты seq/revision/manifest, scope/отзыв прав, rollback, legacy pending,
undo/redo, лимиты/приватность и межпроектная изоляция проверяются отдельными
Go/PostgreSQL и TS/DOM/runtime тестами. Пороги покрытия сохранены.
При ревью исправлены обработка одиночного ActionCall при подтверждении замены,
его включение в pack и resolve одиночного pending, многочастные ключи действий
и ограничения text в форме. Регрессионные тесты и CI итогового SHA
`baba754101d0075bcad1487f9ec3e5883f37d0b1`, затем CI merge прошли.

Следующий пакет — [подписанная доставка context pack](studio-capability-delivery.md),
STU-275…294: спецификация готова, реализация ещё не начата.
Далее остаются источник достоверных PR/CI фактов, automatic fulfillment с группами
requiredBy, preview/<ref> mapping, component/dataSource/token kinds и условные agent policies.
Сквозной шаг 10 MVP закрывается после этих интеграций; M4/M5 целиком не закрыты.
