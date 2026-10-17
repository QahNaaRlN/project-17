# GitHub PR/CI: реализация и проверка

Дата: 2026-10-18. Ветка: `feature/studio-capability-github` от `dev` `cf59c45`.
[Спецификация STU-295…314](studio-capability-github.md) одобрена пользователем.
Реализация и исправления находятся в [черновом PR #62](https://github.com/QahNaaRlN/project-17/pull/62),
в dev не слиты. Публичная внешняя проверка выполнена; приватная часть STU-314
заблокирована способом аутентификации. Весь пакет и M5 не считаются принятыми.

## Реализовано

- Миграция 00022, sqlc, scoped config/link/observations/audit, River worker,
  lease/recovery, конечное polling window и общий credential budget.
- Серверный каталог с ключом чтения, явная настройка оператором,
  human-only link/unlink/refresh, optimistic gates, exact retry и outbox.
- GitHub REST API с проверкой repository/base/PR identity, точных head и
  integration SHA, родителей test merge, trusted publisher, pagination и rerun.
  Неполные, пропущенные и неоднозначные проверки не считаются успехом.
- Query API/BFF и отдельная карточка Studio: история, свежесть, last-known,
  RU/EN, dirty guards; серверные секреты не выдаются клиентам и pack.

## Независимое ревью и исправления

На исходном `c60e629` воспроизведены два дефекта интерфейса: ошибка повторного
чтения оставляла старый success актуальным, а запоздалый ответ мог заменить
новое disabled-состояние. Исправление `0fb66c6` вводит порядок применения
чтений, инвалидирует незавершённые чтения при смене контекста и сохраняет
старые факты только как last-known при ошибке. Три регрессии проверяют также
восстановление после успешного чтения.

Живая проверка выявила третий дефект: запросы API `2026-03-10` не содержат
`merge_commit_sha`. Два реальных запроса одним ключом подтвердили отличие от
`2022-11-28`; версия fixture ранее не учитывалась. Обновлённые fixtures сначала
дали RED на exact test/actual merge, затем PASS после закрепления совместимой
версии `2022-11-28`. После пересборки тот же CMS получил настоящий merge SHA
и его независимые Checks. Причина документирована в
[GitHub breaking changes](https://docs.github.com/en/rest/about-the-rest-api/breaking-changes?apiVersion=2026-03-10).
Версия поддерживается до 2028-03-10 по [GitHub API versions](https://docs.github.com/en/rest/about-the-rest-api/api-versions).

## Внешняя проверка собственным клиентом CMS

С разрешения владельца подготовлен синтетический
[публичный тестовый PR](https://github.com/QahNaaRlN/project-17-acceptance-public/pull/1).
Использованы настоящий GitHub, PostgreSQL, River, подписанный локальный OIDC,
Studio/BFF и тестовые человеческие учётные записи автора и разработчика.
GitHub факты не подменялись SQL, fixtures или CLI-ответами: CMS сама выполнила
GET своим production HTTP-клиентом с отдельным серверным ключом чтения.

| Переход | Точный SHA | Результат CMS |
|---|---|---|
| Исходный PR | `c866da93d847b5607a5c1e11c68a0d29890e3ba0` | success |
| Новый коммит, затем ошибка проверки | `4afd07702b585e5d57451d0c6915915a264288c1` | pending → failure |
| Следующий коммит | `cfa23c5dac3c633540bba58479cd69f76ce6ef51` | pending → success |
| Реальное squash merge | `d9415d0b698d3ce09396846d3726408cc09a3b02` | merged_commit; отдельный integration check success |

Политика доверяет GitHub Actions app 15368 и точному имени `Acceptance check`.
После merge явно включены оба правила, head и integration; итог success получен
из двух результатов на разных точных SHA. Для наблюдения pending тестовый
workflow имеет ограниченное окно 45 секунд. Штатный cooldown 60 секунд,
лимиты и поколения CMS не менялись.

Проверки GitHub: [failure](https://github.com/QahNaaRlN/project-17-acceptance-public/actions/runs/39072808612),
[новый head success](https://github.com/QahNaaRlN/project-17-acceptance-public/actions/runs/39072905945),
[merge success](https://github.com/QahNaaRlN/project-17-acceptance-public/actions/runs/39073045795).
История CMS сохранила разные SHA и исходные unknown-наблюдения до исправления
API version. Последующее исправление не переписало старую историю.

После наблюдений, включая merge/success и отказ приватного доступа, совпали
request status/revision, external_refs, CS seq/state и хэши context pack с
начальным снимком. Автоматического fulfillment не произошло.
Отдельная реальная подача CS дала failed с `ACTION_PENDING_CAPABILITY`;
последующий publish отклонён 409 `CHANGESET_STATE_INVALID`, опубликованных
указателей страницы — 0. Причина failed подтверждена диагностикой pending,
а не только общим отказом publish. Эти команды проверки изменили workflow
состояние уже после сравнения инвариантов; GitHub-наблюдения его не меняли.

## Приватная часть: BLOCKED

Созданный с явным подтверждением ключ ограничен одним разрешённым приватным
тестовым репозиторием и чтением Contents, Metadata, Pull requests, Commit statuses;
срок — до 25 октября 2026. Экран GitHub не предлагает Checks/read.
Прямое чтение приватного PR этим ключом даёт 200, приватных Checks — 403.
Штатный CMS worker повторил `permission_denied`: current отсутствует,
last-known сохранён, Studio показывает отсутствие актуального подтверждения.
Публичные Checks доступны; этот результат не закрывает приватный сценарий.

[Общий раздел ограничений PAT](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens)
также указывает ограничение Checks API, хотя документация отдельного endpoint
перечисляет fine-grained PAT. Вывод основан на реальном ответе и доступных
правах интерфейса. Приватные идентификаторы, ключи и данные окружения здесь
не публикуются. Широкий классический PAT не использован.

Для полного STU-314 подготовлено [серверное расширение GitHub App](studio-capability-github-app.md)
с выбранным репозиторием и Checks/read, обновлением токенов и безопасными отказами.
Локальные HTTP/RSA/cache тесты проходят; это пока не доказательство внешней App.
Регистрация, установка и повторная приватная проверка ещё не выполнены.

## Проверки реализации

- Go capabilitygithub/capabilities/httpapi: PASS `-race -count=1` с настоящим
  изолированным PostgreSQL, повторено после исправления API version.
- Studio: 357 тестов в 40 файлах; statements 92.44%, branches 85.65%,
  functions 91.59%, lines 94.04%, штатные пороги сохранены. Полный локальный
  прогон использовал maxWorkers=2; тесты и таймауты не ослаблялись.
- ESLint, TypeScript, сборка Studio и Chromium STU-312 на 375 px — PASS.
  Этот браузерный сценарий использует HTTP boundary fixtures; живая проверка
  описана отдельно выше. Реальный интерфейс также проверен в браузере.
- [Полный CI исправления UI `0fb66c6`](https://github.com/QahNaaRlN/project-17/actions/runs/39071606758)
  прошёл Check (TS + Go) и Docker image, включая штатные Docker-тесты,
  пороги покрытия и браузерный набор. Предыдущие полные CI также прошли.
- Состояние полного CI нового исправления совместимости и этого отчёта:
  [проверки текущего SHA PR](https://github.com/QahNaaRlN/project-17/pull/62/checks).
  Новый результат нельзя выводить только из CI прежнего SHA.

Отдельно остаются preview/<ref> mapping, automatic fulfillment/requiredBy,
новые kinds и условные политики агента. GitHub merged/success не подтверждает
развёртывание, совместимость manifest или выполнение Missing Capability.
