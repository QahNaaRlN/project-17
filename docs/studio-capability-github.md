# Studio: проверенные факты GitHub PR и CI

Статус: **одобрена пользователем 2026-10-18; код реализован в
`feature/studio-capability-github`; внешняя проверка STU-314 пройдена**.
[CI актуального SHA PR #62](https://github.com/QahNaaRlN/project-17/pull/62/checks).
Дата: 2026-10-18. Требования: STU-295…314.
Основа: `origin/dev` на `cf59c4568cf276800922d80ee73e9c15650d25d7`,
после [PR #61](https://github.com/QahNaaRlN/project-17/pull/61).
[CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/39037306790)
пройден. Продолжает [Missing Capability](studio-missing-capability.md),
[подписанную доставку](studio-capability-delivery.md) и
[архитектуру агента](spec/09-agent.md#5-missing-capability).
Одобрение получено; итоговая приёмка и слияние в dev ещё не выполнены.
[Черновой PR #62](https://github.com/QahNaaRlN/project-17/pull/62).
[Реализация, проверки и ограничения](studio-capability-github-acceptance.md).

## 1. Результат и границы

Разработчик явно связывает отправленный action-запрос с одним PR настроенного
репозитория. CMS читает GitHub REST API и показывает проверенные идентификаторы
репозитория/PR, состояние PR, точные коммиты и результаты заданных проверок CI.
Владелец страницы различает ручной инженерный статус, доставку context pack,
факты GitHub и наличие действия в активном manifest своего окружения.

Ссылка от человека остаётся утверждением человека, пока API не подтвердил PR.
Успешный CI означает успех конкретного набора проверок конкретного коммита
на момент наблюдения. Он не доказывает корректность бизнес-логики,
развёртывание приложения или выполнение capability. Подтверждённый PR
означает существование и состояние PR; соответствие его кода запросу в этом
пакете не доказывается. Связь создаёт уполномоченный разработчик.

В границах: GitHub.com, один репозиторий на project/environment, одна активная
PR-связь на request, ручные link/unlink/refresh, ограниченное фоновое обновление,
Checks и Commit Statuses, история наблюдений, формы Studio, RU/EN, миграция 00022.
Доступ к публичному или приватному репозиторию задаёт оператор явно.

Вне границ: входящие webhook, GitHub Enterprise Server, пользовательский OAuth GitHub App и входящие webhook App,
чтение исходников/diff/логов/артефактов, запуск или перезапуск CI, открытие/изменение/
слияние PR, отправка комментариев, выполнение кода, preview/<ref> mapping,
автоматическое fulfillment, группы requiredBy, новые kinds, условные политики
агента, Figma и production-развёртывание. Существующий ручной resolve сохраняется.
Новые команды не добавляются в MCP; агент читает безопасную проекцию существующим
get_capability_request только в пределах уже разрешённой видимости.

## 2. Исходная точка и порядок зависимостей

PR #60 хранит external_refs и ручные submitted/accepted/in_progress/pr_open;
PR #61 подтверждает сохранение пакета получателем, но не читает GitHub.
Доступные опоры:

- [команды запросов](../apps/server/internal/capabilities/commands.go),
  [Query API](../apps/server/internal/capabilities/queries.go);
- [доставка](../apps/server/internal/capabilities/delivery.go),
  [очередь River](../apps/server/internal/jobs/jobs.go);
- [миграция запросов](../apps/server/db/migrations/00020_capability_requests.sql),
  [миграция доставки](../apps/server/db/migrations/00021_capability_delivery.sql);
- [Studio](../apps/studio/src/capability-panel.tsx) и
  [BFF](../apps/studio/src/handler.ts).

| Кандидат | Зависимости и решение |
|---|---|
| Факты PR/CI — этот пакет | Опирается на #60 и общую очередь; #61 уже слит. Не требует работающего кодового агента или доставки. Следующий пакет: даёт наблюдаемость результата внешней разработки |
| preview/<ref> mapping | Нужны проверенные repository/PR/ref/commit и контракт развёрнутого приложения/manifest; отдельная следующая спецификация |
| Автоматическое выполнение/requiredBy | Нужны совместимость manifest конкретного окружения, происхождение версии и атомарная замена актуальных заглушек; PR/CI сами по себе недостаточны |
| component/dataSource/token requests | Расширяют контракты и проверки; рациональнее после приёмки полного action-пути |
| Условные agent policies | Отдельное расширение авторизации; не является блокером read-only GitHub-интеграции |

## 3. Требования

| ID | Требование |
|---|---|
| STU-295 | Каталог GitHub задаёт оператор; project.admin явно включает конфигурацию для своего окружения. Нет произвольного API URL, токена или repository из формы, MCP, pack либо ответа receiver |
| STU-296 | Link проверяет текущие code.propose, видимость недрафтового нетерминального request, project/environment, expectedRequestRevision, expectedLinkRevision и expectedConfigRevision. PR number — положительное целое; репозиторий берётся только из конфигурации |
| STU-297 | Старые external_refs остаются ручными ссылками и не получают verified автоматически. Одна активная PR-связь; замена требует явного подтверждения, unlink сохраняет историю. Link сам по себе не меняет инженерный статус |
| STU-298 | Команда и постановка первой River-задачи атомарны; до commit сети нет. Exact retry возвращает те же link/job identifiers, а не повторную связь/наблюдение |
| STU-299 | Репозиторий и PR подтверждаются GitHub API по числовым ID и настроенным owner/name/base branch. Перенос, переименование, другой repo/base или несовпадение ID даёт controlled mismatch, а не автоматическую перенастройку |
| STU-300 | Наблюдение привязано к link generation, config revision, repository ID, PR ID/number, head repo/SHA, base SHA и отдельным integration SHA/type. Повторное чтение PR вокруг сбора CI исключает применение результата после смены версии |
| STU-301 | head, временное слияние открытого PR и фактический merge-коммит различаются. Нет переноса зелёного CI между SHA, тестовым слиянием и опубликованной версией. Fork head не расширяет список доступных репозиториев |
| STU-302 | CI считается по явному непустому набору required checks из конфигурации с exact name/context, источником, доверенным app/creator ID и целью head/integration. Нет проверок, пропуски, neutral, skipped и неполные страницы не дают success |
| STU-303 | Для каждого правила выбран самый новый результат/попытка на точном SHA. Старый success не перекрывает новый pending/failure. Неоднозначные дубликаты и неподдерживаемые состояния дают unknown с причиной |
| STU-304 | Факты PR и CI имеют собственные revision, observedAt, freshUntil и историю. Ошибка обновления не превращается в closed/failure/success; прежний снимок доступен только как last-known, текущее состояние — unavailable либо stale |
| STU-305 | Фоновое обновление ограничено; ручной refresh coalesced, не позволяет расходовать API без ограничений. Rate limits/Retry-After и временные ошибки сохраняются в расписании; бесконечных немедленных повторов нет |
| STU-306 | Lease/CAS и generation gates защищают от параллельных workers, смены PR/config и позднего ответа. Рестарт восстанавливает просроченные задачи, не теряет историю и не применяет старый ответ к новой связи |
| STU-307 | Worker перед запросом и перед сохранением проверяет актуальные настройки/права инициатора. Unlink, отключение конфигурации, отзыв code.propose или terminal request останавливают новые обращения; ответы в полёте не становятся текущими |
| STU-308 | Авторизация Query повторяет видимость request и доступ к GitHub-проекции. Межпроектные ID не раскрываются. project.admin без design.read/code.propose не получает сведения о приватном PR через настройку интеграции |
| STU-309 | Сеть разрешает только фиксированный HTTPS GitHub API; нет redirects, proxy из окружения, приватных адресов, API/Link URL из недоверенного ответа. Ограничены таймауты, объёмы и число страниц; секреты не журналируются |
| STU-310 | API выдаёт только разрешённую нормализованную проекцию. Нет raw bodies, токена, PR body/title, исходников, CI output/logs, actor email, произвольного details_url. Текст и ссылки не исполняются; все ответы no-store |
| STU-311 | Факты не изменяют request status/revision, external_refs, IR, CS seq/history, pack, delivery, manifest, approvals или publication. Merged/success не переводит request в fulfilled и не снимает pending publication block |
| STU-312 | Studio раздельно показывает ручной статус, доставку, PR, CI и доступность manifest; точный SHA/тип, время и stale/ошибки доступны. RU/EN, клавиатура, focus/Escape, 375 px, dirty guards и exact retry сохраняются |
| STU-313 | Миграция/SQL/API/worker/Studio и отрицательные сценарии покрыты тестами с ID требований. Пороги и таймауты существующих тестов не ослабляются; полный установленный CI остаётся обязательным |
| STU-314 | Реальная приёмка с разрешённым GitHub-репозиторием подтверждает новый коммит, незавершённый/успешный/неуспешный CI, merge и отсутствие auto-fulfillment. HTTP fixture не выдаётся за реальный GitHub; отсутствие доступа явно блокирует внешнюю приёмку |

## 4. Конфигурация и права

Новая переменная `CMS_CAPABILITY_GITHUB_CONFIG_FILE` указывает на приватный
операторский JSON-файл вне репозитория. Без файла новые функции отключены,
ручной процесс #60 и доставка #61 продолжают работать. Невалидный заданный файл
останавливает запуск; секреты/путь/сырые ошибки не попадают в API и логи.

Каталог `repositories` содержит до 256 записей:
`{configId, projectId, environment, repositoryId, owner, name, baseBranch,
credentialId, token, requiredChecks}`. Project — UUID, environment — standard;
repositoryId/appId/creatorId — положительные числовые ID (передавать в DTO как
десятичные строки без потери точности), owner/name — строгие GitHub path segments,
baseBranch — непустая строка до 255 байт без управляющих символов.
configId/credentialId — ASCII `[a-zA-Z0-9_-]{1,64}`. Неизвестные поля,
дубликаты configId и project/environment, пустые credentials/checks запрещены.
Размер файла ≤1 MiB, token ≤4096 байт, requiredChecks — 1…32 правила,
name/context ≤200 байт; дубликат идентичного правила запрещён.

V1 использует серверный fine-grained personal access token с доступом только
к выбранному репозиторию: Metadata, Pull requests, Checks, Commit statuses,
Contents — read. Contents нужен для проверки родителей временного commit;
исходники не запрашиваются. Write permissions не требуются; CMS не должна
использовать токен с правом изменения как основание для новых операций.
Оператор отвечает за ограничение токена и срок действия. Ротация — перезапуск
с новым credentialId; никаких секретов в БД, Studio, pack или идемпотентном ответе.
Серверное расширение [GitHub App installation authentication](studio-capability-github-app.md)
реализовано для приватных Checks. Регистрация, установка в разрешённый
приватный репозиторий и реальная проверка выполнены с явным разрешением владельца.

Проверка 2026-10-18 выявила расхождение этого принятого контракта с доступными
правами GitHub: экран fine-grained PAT не предлагает Checks/read, чтение
приватного PR проходит (200), приватных Checks — отклоняется (403).
Публичный сценарий работает с PAT; приватный сценарий проверен через GitHub App.
Указанное выше требование Checks/read для PAT не считается реализуемым лишь
по документации endpoint; для приватного доступа используется installation auth.
Контракт нового способа аутентификации описан в [расширении App](studio-capability-github-app.md);
приватное требование подтверждено реальными переходами CI, включая отдельный
merge check; границы пакета сохраняются.
Подробности — [отчёт приёмки](studio-capability-github-acceptance.md).

Правило проверки: `{source: check_run|commit_status, name, target: head|integration,
appId? , creatorId?}`. Для check_run обязателен appId; для commit_status — creatorId.
Имена точные, без regex/wildcards. Для Project 17 исходная операторская политика —
`Check (TS + Go)` и `Docker image` из доверенного GitHub Actions app на head;
app ID подтверждается API при настройке, не угадывается по имени приложения.
Политика CMS явно задана и не выдаётся за прочитанные GitHub rulesets/branch protection.
В реальном PR #62 GitHub Actions app ID 15368 публикует обе проверки на
`head.sha`; на `merge_commit_sha` открытого PR проверок нет. Это проверено через
GitHub REST API, не выводится из checkout workflow. `target=head` означает
принадлежность результата head SHA в API, а не доказательство фактически
выполненного checkout. Не переносить этот success на integration SHA. Если
проект требует CI именно после merge, оператор выбирает integration отдельно;
до появления проверок на этом SHA итог unknown, а не success.

`set-capability-github-config` с project.admin выбирает configId/enabled,
expectedConfigRevision (0 для отсутствующей записи) и reason. Хранятся публичные
идентификаторы, fingerprint канонической несекретной конфигурации, credentialId,
revision и аудит. Сам токен/его производные в DTO не возвращаются.
При изменении каталога fingerprint перестаёт совпадать: синхронизация блокируется
до явного set. Смена credentialId также требует set. Выключение увеличивает
generation и инвалидирует текущие факты; включение не запускает старые связи
без нового явного refresh от уполномоченного человека.

Новые link/unlink/refresh — только human с code.propose и существующим доступом
к недрафтовому request. Service/agent не принимаются. Настройка администратора
не выдаёт code.propose. Для приватной проекции дополнительно к видимости request
нужны code.propose либо human owner с capability.request/design.read;
делегированный агент видит её только при разрешении всех текущих прав владельца
и своей сессии. Разрешение интеграции означает согласие проекта на такую
минимальную передачу метаданных участникам CMS; это явно написано перед enabled.

## 5. Команды, хранение и Query

| Команда | Payload сверх project/reason/idempotency key | Результат |
|---|---|---|
| set-capability-github-config | environment, configId, enabled, expectedConfigRevision | Безопасная конфигурация и revision |
| link-capability-github-pr | requestId, pullNumber, expectedRequestRevision, expectedLinkRevision, expectedConfigRevision, confirmReplace | linkId/revision/generation, sync queued |
| unlink-capability-github-pr | requestId, expectedRequestRevision, expectedLinkRevision | Неактивная связь новой revision |
| refresh-capability-github-pr | requestId, expectedRequestRevision, expectedLinkRevision, expectedConfigRevision | Существующий или новый syncId и nextAllowedAt |

Первое связывание: expectedLinkRevision=0, confirmReplace=false. Замена активной
связи требует confirmReplace=true и актуальной revision. Повтор того же PR —
no-op связи, не обход cooldown. Request должен быть отправлен и нетерминален;
ручной pr_open не является обязательным предусловием и не присваивается командой.
Нельзя заранее ссылкой перевести draft в доступный разработчику запрос.

Миграция 00022 добавляет scoped таблицы configs, links, observations, sync jobs/
attempts и audit; конкретные SQL-имена следуют принятому стилю capabilities.
Composite FK project/request/environment, одна активная связь на request,
уникальные generation/attempt и собственные revision обязательны. Config/link
изменения и outbox атомарны. Network вне транзакции; commit наблюдения использует
короткую транзакцию и актуальные CAS/rights gates. Прямой UPDATE страниц запрещён.
Unlink/replace не удаляет immutable наблюдения. Старые external_refs не мигрируются
в verified. Down удаляет только новые таблицы/права, не данные #60/#61.

Нормализованное observation содержит repositoryId, prId/number,
state=open|closed|merged, draft, headRepositoryId/headSha, baseSha/baseRef,
integrationSha/type=test_merge|merged_commit|unavailable, mergedAt,
configRevision/fingerprint, generation, policyDigest, CI aggregate, результаты
правил (source/id/name/appId или creatorId, exactSha, raw status/conclusion из
ограниченных enum, нормализованный outcome), observedAt/freshUntil и digest.
Неподдерживаемый enum нормализуется в unknown, не в success.

Query в проектном scope:

- `GET /api/v1/capability-github-config?environment=…` — безопасное состояние;
  администратору доступны ID/политика/readiness, но не PR и не секрет;
- `GET /api/v1/capability-requests/{id}/github?environment=…` — link, current,
  lastKnown, syncState, controlled reason, nextAllowedAt;
- `GET /api/v1/capability-requests/{id}/github-observations?environment=…&limit=…&cursor=…`
  — immutable история, limit 1…100 (по умолчанию 20), стабильный scoped cursor.

В get_capability_request добавляется только краткое github summary при тех же
правах; без N+1 GitHub-запросов. Чтение API никогда не вызывает сеть GitHub.
Недоступный request и чужой scope возвращают одинаковый 404. В окружении без
настройки — controlled disabled для видимого request. Неизвестные/повторные query
поля и невалидные payload отвергаются. API DTO/types следуют текущему codegen.
Без новых колонок или фильтров всей очереди работ в этом пакете.

## 6. Получение и достоверность фактов

Worker использует только `https://api.github.com`, Accept GitHub JSON и
`X-GitHub-Api-Version: 2022-11-28`. Живая проверка выявила, что API
2026-03-10 удалил необходимое `merge_commit_sha`; V1 закрепляет поддерживаемую
версию 2022-11-28 (поддержка до 2028-03-10). Это исправление совместимости
для принятого контракта exact SHA; фикстуры моделируют реальную зависимость
поля от версии. Источники: [breaking changes](https://docs.github.com/en/rest/about-the-rest-api/breaking-changes?apiVersion=2026-03-10),
[API versions](https://docs.github.com/en/rest/about-the-rest-api/api-versions).
Для App исключение из GET-only — фиксированный POST выпуска installation token
и фиксированный GET проверки installation repositories, с теми же сетевыми бюджетами.
Все пути конструируются сервером из каталога,
номера PR и проверенных SHA/ID; никаких переходов по url/details_url/Link из ответа.
Repository GET сверяет числовой ID и owner/name. PR GET сверяет PR ID/number,
base repo ID/ref, head repo ID/SHA. Fork допускается только как происхождение head:
запросы выполняются в base repository, токен не отправляется в fork.

Одна попытка: repository GET → PR GET → выбранные commit/CI GET → повторный PR GET.
Сравниваются state/draft/merged/merge SHA/head/base IDs/ref/SHA до и после.
Если набор изменился, снимок CI не становится текущим; состояние changed_during_sync,
ограниченный повтор по общему бюджету. Отдельные API-ответы не образуют атомарный
снимок GitHub: UI всегда показывает время наблюдения, дальнейшие решения требуют
свежего чтения. Никаких обещаний непрерывной актуальности между опросами.

| Цель | Как определяется SHA | Что не допускается |
|---|---|---|
| head | Точный head.sha из PR; CI читается в base repo | Использование ветки вместо SHA, чтение fork с credential |
| integration открытого PR | mergeable=true, merge_commit_sha; Git commit API подтверждает ровно родителей [base.sha, head.sha]; повторный PR подтверждает ту же пару | mergeable=null/false, отсутствующий commit, подмена родителей: integration unavailable |
| integration merged PR | merge_commit_sha из подтверждённого merged PR, тип merged_commit | Перенос результатов временного merge; требование двух родителей для squash/rebase |
| integration closed без merge | unavailable | Представление старого тестового merge как текущего результата |

Checks: list check-runs на SHA, filter=all, все страницы; ограничение по appId
из политики. Commit Statuses: list statuses на SHA, все страницы, а не только
combined state. Последний не подтверждает наличие полного набора требуемых
проверок и не заменяет Checks API.

Check runs группируются по name/appId/check_suite.id; новая suite для того же
правила/коммита определяется по времени создания suite, затем её ID. В suite
актуальная попытка — максимальный check_run.id; pending новой попытки перекрывает
старый success. Данные suite при необходимости читаются отдельным разрешённым GET.
Если API не позволяет однозначно выбрать suite/попытку или есть конфликтующие
одновременные кандидаты, outcome=unknown/ambiguous, а не выбор удобного success.
Для statuses требуется точное context/creatorId, новый результат определяется
created_at и затем ID; неподходящий издатель не подменяет доверенного.
Дополнительные необязательные проверки не меняют итог и не называются обязательными.

Агрегат: success только когда все правила дали success в текущем полном снимке;
failure при failure/error/timed_out/action_required/startup_failure хотя бы одного
обязательного правила; cancelled если нет failure и есть cancelled; pending если
нет предыдущих и есть queued/in_progress/pending; иначе unknown. Отсутствие
правила, skipped, neutral, stale conclusion, недоступная цель или неопределённый
издатель дают unknown. При неполном сборе нет нового агрегата: sync unavailable,
старый снимок last-known. UI показывает отдельные результаты, а не только цвет.

Политика не утверждает, что проверялся именно этот PR: CI может повторно
использовать тот же SHA, а доверенный издатель может изменить workflow.
Проверка workflow contents и attestation выполнения находится вне пакета.

## 7. Обновление, ошибки и безопасность

Связь хранит инициатора human, поколение и собственное расписание. Перед сетью
и commit повторяются права/видимость/config/generation. Для повторного включения
после отзыва прав нужен явный refresh уполномоченным разработчиком с фиксацией
нового инициатора и generation. История остаётся доступна только по текущим правам.
Terminal request прекращает polling; финальный снимок остаётся историческим.

После link/refresh выполняется одна синхронизация и не более пяти автоматических
обновлений с интервалом не меньше 5 минут; успешный снимок freshUntil=observedAt+15 мин.
Периодическое восстановление не создаёт бесконечную подписку: после лимита/30 мин
новые опросы требуют явного refresh. Один in-flight sync на link, ручной cooldown
60 секунд на link и общий лимит проекта 20 новых sync/час; coalescing не создаёт
новые попытки. Rate limit budget общий по credentialId в пределах развёртывания;
при нескольких CMS-инстансах координация сохраняется в PostgreSQL.

Одна синхронизация — до трёх сетевых попыток: исходная, затем не раньше 1 и 5 мин,
с jitter; Retry-After/X-RateLimit-Reset может только отодвинуть попытку, не ускорить.
Provider block действует на все link этого credentialId. Одна попытка ≤60 сек,
каждый HTTP ≤10 сек; deadline завершает чтение. Lease 90 сек, recovery не раньше
истечения lease; CAS не принимает запоздалый ответ. Периодические ticks и recovery
идемпотентны; после исчерпания retry нужна новая явная команда, не бесконечный job.

401 — credential_unavailable, 403 без признаков rate limit — permission_denied,
404 — not_found_or_inaccessible (не доказательство удаления), 429/rate-limit 403 —
rate_limited, 5xx/network — temporary_unavailable, malformed/oversize — invalid_response.
Перенаправление при переносе repo не проходится; repository_mismatch требует
операторской настройки. Детали исходного ответа и credential не возвращаются.
Любая ошибка оставляет предыдущий снимок last-known; свежим он больше не считается.

Максимум 100 записей/страницу и 10 страниц на endpoint/цель, 2 MiB/ответ,
10 MiB распакованных данных и 40 HTTP GET на попытку суммарно. Пересечение лимита
даёт incomplete, не частичный success. Repository/PR/commit/suite GET тоже входят
в бюджет. Пагинация строится сервером с page=N; внешние Link не исполняются.
304/ETag в v1 не используются, чтобы не собирать снимок из разных поколений.
Доступ только HTTPS:443, обычная TLS hostname verification, публичные DNS/IP,
проверка фактического peer, без redirects/proxy. Производственный host фиксирован;
HTTP fixture вводится только зависимостью в тестах, без runtime bypass env.

В БД только нормализованные поля из §5; raw JSON не хранится. Canonical PR URL
строится как `https://github.com/{owner}/{name}/pull/{number}`. Произвольный CI URL
не выдаётся: UI ведёт в `/pull/{number}/checks` и показывает exact SHA/историю.
У ссылок явный пользовательский переход и rel=noopener noreferrer.
Метрики содержат outcomes/counts/durations, не token, body, owner/name или URL;
аудит содержит scope, actor, config/link generations и controlled reason.

## 8. Studio и совместимость

В карточке запроса отдельный раздел «GitHub»: репозиторий, номер PR, состояние,
draft, тип и короткий SHA с возможностью увидеть полный, required checks,
время наблюдения и следующий допустимый refresh. Формы берут repository из
настройки, вводят только номер PR; старую ручную ссылку можно явно выбрать для
разбора строгого github.com/owner/repo/pull/N без query/fragment/credentials.
Несовпадение configured repository не запускает сеть.

Link/unlink/replace доступны по правам, требуют reason и явных действий;
replace показывает прежний и новый PR. Refresh не стирает ввод. Lost response
повторяется тем же idempotency key/payload; конфликт требует перечитать состояние,
а не молча перепривязать. Повторный клик coalesced. GET и открытие модального окна
не запускают GitHub. Смена CS/project/locale защищает несохранённые изменения.

Ручное pr_open подписано «Сообщено разработчиком», внешнее open/merged —
«GitHub, проверено …». Доставка delivered подписана как сохранение пакета
получателем. Отдельно доступность action берётся из существующего manifest,
без нового валидатора совместимости и без вывода о выполнении из зелёного CI.
Все пустые/disabled/stale/unavailable/unknown состояния явно различаются.
История наблюдений доступна постранично. Нет PR title/body, HTML из провайдера,
автоматического открытия ссылок или передачи credentials браузеру.

## 9. Проверки и приёмка

| Область | Обязательные сценарии |
|---|---|
| Config/auth | Нет файла; ошибочный файл; другая роль/проект/env; приватный PR; agent/service запрет команд; отзыв прав после enqueue и в полёте; admin без read; отключение/ротация/смена repo; два CMS worker |
| Commands/DB | Rollback без job; exact retry после потери ответа; конкурентный link/replace/unlink; stale revision; поколение ответа; FK/isolation; legacy refs не verified; миграция up/down без потери #60/#61 |
| GitHub provenance | Чужой repo ID/base; renamed/transferred repo; fork; смена head/base/mergeable во время sync; test merge родители; squash/rebase; закрытый PR; удалённый head; новый SHA не наследует CI |
| CI | Checks отдельно от Statuses; 0 checks; missing/skipped/neutral; trusted publisher; spoof name; вторая страница; новый failed/pending rerun после success; suite ambiguity; unknown enum; incomplete budget; CI успех не означает fulfillment |
| Worker/network | 401/403/404/429/5xx; Retry-After; cooldown/project/credential budgets; coalescing; crash/lease/recovery; slow/oversize; redirect/SSRF/proxy; поздний ответ; terminal request; конечный polling window |
| Studio | RU/EN/375 px; keyboard/focus/Escape; disabled/no rights; все состояния; link/replace/unlink; exact retry; dirty guards; смена CS/project; безопасная история/ссылки; раздельные labels |
| Regression | Request revision/status, CS seq, pack/hash/delivery, manifest, approvals неизменны; существующий manual resolve работает; pending продолжает блокировать публикацию |

HTTP boundary fixtures обеспечивают детерминированные негативные сценарии,
включая недоступные для воспроизведения реальные сбои. Внешняя приёмка проводится
отдельно на разрешённом тестовом репозитории с реальным GitHub API и двумя людьми
в CMS/PostgreSQL/OIDC/Studio. Используется подготовленный PR с наблюдаемыми CI
состояниями: новая версия, pending, failure, success, merge и отдельный CI merge.
Любые изменения внешнего репозитория/открытие PR/перезапуск CI требуют отдельной
авторизации; реализация пакета не означает разрешения этих внешних действий.
Можно использовать существующий разрешённый PR и его наблюдаемые переходы;
если их недостаточно, статус приёмки остаётся частичным, а не выдумывается.

Проверить: линк с точным repository ID/PR ID, новая версия инвалидирует старый CI,
история хранит разные SHA, merge не означает deployment; pending блокирует
публикацию до существующего явного resolve по совместимому active manifest.
Разрыв API или отзыв токена показывает unavailable/last-known. Приватный сценарий
проверяется на разрешённом приватном репозитории либо явно отмечается как
не пройденный внешне; fixture не подменяет внешнюю проверку.
Секреты, приватные идентификаторы и данные окружения не входят в отчёт/репозиторий.

Проверки реализации: sqlc/codegen и чистый generated diff, Go build/vet/race,
PostgreSQL tests, все TS/React/runtime/Studio тесты с существующими порогами,
format/lint/typecheck/build, установленная браузерная приёмка и Docker image.
Не считать HTTP fixture доказательством внешней интеграции. Полный M5/MVP
не объявляется завершённым после этого пакета.

## 10. Порядок реализации после одобрения

1. Миграция, типы, конфигурация/права, атомарные команды и contract tests.
2. GitHub-клиент, provenance/CI normalizer и негативные HTTP fixtures.
3. River worker/recovery/budgets, Query/API/BFF и безопасные проекции.
4. Формы и история Studio, RU/EN/accessibility, регрессионные сценарии.
5. Полный CI, разрешённая внешняя приёмка, отчёт implemented/remaining/dependencies.

Следующий документ после этого пакета — preview/<ref> mapping; затем отдельно
автоматическое fulfillment/requiredBy. Пакеты могут делить механизмы происхождения
версии, но успешный CI не заменяет контракт развёртывания/manifest.

## 11. Источники контрактов GitHub

Проверены 2026-10-18; перед реализацией сверить закреплённую API version.

- [Pull requests: get, head/base и merge_commit_sha](https://docs.github.com/en/rest/pulls/pulls#get-a-pull-request).
- [Check runs: exact ref, pagination, app_id и filter](https://docs.github.com/en/rest/checks/runs#list-check-runs-for-a-git-reference).
- [Commit statuses](https://docs.github.com/en/rest/commits/statuses#list-commit-statuses-for-a-reference).
- [Git commits](https://docs.github.com/en/rest/git/commits#get-a-commit).
- [Check suites](https://docs.github.com/en/rest/checks/suites#get-a-check-suite).
- [REST API best practices: rate limits и последовательные запросы](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api).
- [Ошибки и недоступные ресурсы](https://docs.github.com/en/rest/using-the-rest-api/troubleshooting-the-rest-api).
