# GitHub PR/CI: реализация и проверка

Дата: 2026-10-18. Ветка: `feature/studio-capability-github` от `dev` `cf59c45`.
[Спецификация STU-295…314](studio-capability-github.md) одобрена пользователем.
Реализация и исправления находятся в [черновом PR #62](https://github.com/QahNaaRlN/project-17/pull/62),
в dev не слиты. Внешняя проверка STU-314 пройдена на публичном и разрешённом
приватном репозиториях. Итоговая приёмка и слияние пакета остаются отдельным
решением; M5 целиком не завершён.

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

## Приватная часть: внешняя проверка пройдена

Первоначальный read-only fine-grained PAT подтвердил приватный PR, но Checks
ответил 403: на доступном экране прав не было Checks/read. CMS сохранила
`permission_denied`, current отсутствовал, last-known остался. Этот отказ
сохранён в истории; широкие credentials CLI в CMS не передавались.

После явного разрешения владельца создан и установлен GitHub App только для
разрешённого приватного тестового репозитория. Metadata, Contents, Pull requests,
Checks и Commit statuses — read; аккаунтных прав, webhooks и OAuth нет.
Ключ сохранён отдельным серверным файлом вне Git с доступом только владельцу.
Лишний неиспользуемый ключ отозван после отдельного подтверждения. CMS из
чистого `1080dbe` сама получила installation token, проверила единственный
repository scope и прочитала приватные PR и Checks production HTTP-клиентом.

| Переход в приватном репозитории | Результат CMS |
|---|---|
| Новый head, контролируемая ошибка | pending → failure на одном точном SHA |
| Следующий head | pending → success на новом точном SHA |
| Фактический squash merge | merged_commit; head success, integration pending |
| Завершение отдельного merge CI | success по двум результатам на разных точных SHA |

Политика доверяет точному имени `Acceptance check` и GitHub Actions app 15368;
App авторизации и App издателя проверок имеют разные назначения. Требование
integration включено оператором явно. Успешный head не заменил ожидающий
merge check: агрегат оставался pending до завершения отдельного задания.
Приватные идентификаторы, SHA и адреса серверных секретов не публикуются.

Три синтетических задания выполнены на трёх отдельных ephemeral self-hosted
runner в Docker из официального образа GitHub, закреплённого по digest.
Проверены non-root, cap-drop ALL, no-new-privileges, ограничение 2 CPU/2 GiB
и отсутствие host mounts, Docker socket, ключа CMS. После каждого задания
контейнер и регистрация удалены; итоговая проверка подтвердила отсутствие всех
трёх. Настройки оплаты GitHub не менялись. Это приёмочный инструмент вне CMS;
CMS по-прежнему не запускает CI и не исполняет код репозитория.

После приватного merge/success совпали request status/revision, external_refs,
CS seq/state и хэши pack с исходным снимком. История из 24 наблюдений сохранила
шесть приватных переходов и все 16 ранее захваченных снимков с теми же facts
и digest. Реальные submit/publish затем повторно подтвердили failed по
`ACTION_PENDING_CAPABILITY`, publish 409 `CHANGESET_STATE_INVALID` и отсутствие
опубликованных указателей страницы. CS возвращён в open штатной командой;
последующее сравнение с исходным бизнес-состоянием также прошло.

Локальный отчёт и скриншоты сохранены отдельно от публичного репозитория.
Детали механизма — [контракт GitHub App](studio-capability-github-app.md).

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
- Полный [CI реализации App `1080dbe`](https://github.com/QahNaaRlN/project-17/actions/runs/39075190645)
  прошёл Check (TS + Go) и Docker image. Код внешней приватной проверки
  соответствует этому чистому commit.
- Состояние полного CI текущего отчёта:
  [проверки текущего SHA PR](https://github.com/QahNaaRlN/project-17/pull/62/checks).
  Новый результат нельзя выводить только из CI прежнего SHA.

Отдельно остаются preview/<ref> mapping, automatic fulfillment/requiredBy,
новые kinds и условные политики агента. GitHub merged/success не подтверждает
развёртывание, совместимость manifest или выполнение Missing Capability.
