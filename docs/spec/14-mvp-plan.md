# 14. План MVP, критерии приёмки и эксперимент

## 1. Этапы

Оценки длительности даны для команды из 2 backend (Go), 3 frontend (TS/React), 1 дизайнера дизайн-системы и 0,5 QA. Этапы M3 и M4 частично идут параллельно.

| Этап | Содержание | Результат | Оценка |
|---|---|---|---|
| M0. Основа | Monorepo, CI, docker compose, OIDC, проекты, акторы, роли, Command Bus, миграции БД, OpenAPI-каркас | Пустая CMS с входом и командами-заглушками | 3 нед. |
| M1. IR и manifest | JSON Schema IR, codegen, валидаторы Go/TS, conformance-фикстуры; `@cms/component-registry`, CLI `manifest build/push`; `register-manifest`, совместимость | Manifest эталонного приложения зарегистрирован; документы валидируются | 4 нед. |
| M2. Объекты, CS, публикация | `objects`/`versions`, операции каталога, CS, rebase, undo, pipeline (этапы 1–7, 13–15), публикация, откат, Delivery API, кэш | Документ можно изменить через API, согласовать, опубликовать и откатить | 6 нед. |
| M3. Runtime и React | `@cms/core`, `@cms/runtime`, `@cms/react`, примитивы, генерация стилей, SSR, действия, источники данных; `examples/react-store` | Эталонное приложение рендерит страницы из CMS | 5 нед. |
| M4. Studio | Visual/Structure, preview-протокол, панели, DnD, контент-редактор, CS-панель, согласование, история | Сквозной ручной сценарий в Studio | 8 нед. |
| M5. Агент | MCP, агентские сессии, политика агента, встроенный оркестратор, режим Agent, Missing Capability, индекс кода, context pack | Сквозной сценарий с агентом | 4 нед. |
| M6. Укрепление | Этапы pipeline 8–11, нагрузочные тесты, безопасность, документация, эксперимент | Готовность к эксперименту | 3 нед. |

Итого ≈ 26–30 недель с учётом параллельности. Это исходная оценка для указанной
команды, не актуальный календарный прогноз оставшейся работы.

### Текущее состояние и приоритет — 2026-10-26

Интеграционная база: `dev`, merge `2cba143` после PR #84;
[CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/40313597429) пройден. Выпуск из `main` ещё не отражает реализованный объём `dev`.
Наличие отдельных пакетов не означает завершение M0…M6: итоговые критерии
MVP и сквозная автоматическая приёмка ниже остаются обязательными.

| Этап | Состояние | Осталось |
|---|---|---|
| M0. Основа | Готов | OpenAPI ведётся по пакетам (`apps/server/api`), единого каркаса нет; внешний IdP и production не настроены |
| M1. IR и manifest | Готов (PR #2…#3, #16…#28, #35…#36) | — |
| M2. Объекты, CS, публикация | Готов (PR #6…#15, #29…#31) | Кэш ответов на origin и лимиты Delivery API перенесены в M6 |
| M3. Runtime и React | Готов (PR #32…#34, #37…#38) | — |
| M4. Studio | Частично (PR #39…#59, #67, #84) | Каталог и вставка компонентов, advanced bindings, буфер/wrap, IR-044, NFR-приёмка |
| M5. Агент | Готов по очереди пакетов (PR #48, #60…#62, #68, #71, #73, #75, #78) | Приёмки шли без реального GitHub и живой LLM; сквозной сценарий §2 в CI — M6 |
| M6. Укрепление | Начат (PR #81) | Этапы pipeline 8–11, нагрузочные тесты NFR-001…007, безопасность, эксперимент |

| Порядок | Пакет | Статус и условие перехода |
|---|---|---|
| Выполнено | Missing Capability/context pack, STU-255…274 | PR #60 слит в dev; локальная приёмка и CI пройдены |
| Выполнено | Подписанная доставка, STU-275…294 | PR #61 слит в dev; локальная приёмка и CI PR/merge пройдены; это подтверждение сохранения pack, не результата разработки |
| Выполнено | [Проверенные факты GitHub PR/CI](../studio-capability-github.md), STU-295…314 | PR #62 слит в dev 2026-10-19; внешняя проверка STU-314 на публичном и приватном репозиториях и CI merge пройдены |
| Выполнено | [preview/<ref>](../studio-capability-preview.md), STU-315…334 | PR #68 слит в dev 2026-10-20; локальная [приёмка](../studio-capability-preview-acceptance.md) двумя людьми и CI merge пройдены; реальный GitHub в приёмке не использовался |
| Выполнено | [Автоматическое fulfillment и группы requiredBy](../studio-capability-fulfillment.md), STU-335…354 | [PR #71](https://github.com/QahNaaRlN/project-17/pull/71) слит в dev 2026-10-20; локальная [приёмка шага 10](../studio-capability-fulfillment-acceptance.md) и CI итогового SHA пройдены; реальный GitHub в приёмке не использовался |
| Выполнено | [Запросы недостающих компонентов](../studio-capability-component.md), STU-355…374 | [PR #73](https://github.com/QahNaaRlN/project-17/pull/73) слит в dev 2026-10-21; локальная [приёмка](../studio-capability-component-acceptance.md) и CI итогового SHA пройдены (Go coverage 95.2%); реальный GitHub не использовался |
| Выполнено | [Запросы источников данных и токенов](../studio-capability-data-token.md), STU-375…394 | [PR #75](https://github.com/QahNaaRlN/project-17/pull/75) слит в dev 2026-10-21; локальная [приёмка](../studio-capability-data-token-acceptance.md) и CI итогового SHA пройдены (Go coverage 95.1%); реальный GitHub не использовался |
| Выполнено | [Условные политики агента](../studio-agent-policy.md), STU-420…439 | [PR #78](https://github.com/QahNaaRlN/project-17/pull/78) слит в dev 2026-10-21; локальная [приёмка](../studio-agent-policy-acceptance.md), [CI итогового SHA](https://github.com/QahNaaRlN/project-17/actions/runs/39570897945) и [CI merge](https://github.com/QahNaaRlN/project-17/actions/runs/39572946996) пройдены; Go coverage 95.08% |
| Выполнено | [Сквозной сценарий §2 в CI](../e2e-scenario.md), STU-440…459 | [PR #81](https://github.com/QahNaaRlN/project-17/pull/81) слит в dev 2026-10-26; локальная [приёмка](../e2e-scenario-acceptance.md), [CI итогового SHA](https://github.com/QahNaaRlN/project-17/actions/runs/40288321699) (job `E2E scenario (§2)` 2 мин 46 с) и [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/40294758824) пройдены; после пяти зелёных прогонов подряд job обязателен для PR в `dev` и `main` с 2026-10-26 |
| Выполнено | [Перенос в production и откат production в Studio](../studio-promote.md), STU-460…479 | [PR #84](https://github.com/QahNaaRlN/project-17/pull/84) слит в dev 2026-10-26; локальная [приёмка](../studio-promote-acceptance.md), [CI итогового SHA](https://github.com/QahNaaRlN/project-17/actions/runs/40311593042) и [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/40313597429) пройдены; шаги 8–9 сценария §2 выполняются в Studio |
| 1 | [Этапы pipeline 8–9: сборка preview и axe](../pipeline-render-checks.md), STU-480…499 | Реализовано в ветке `feature/pipeline-render-checks`: локальная [приёмка](../pipeline-render-checks-acceptance.md); ожидает PR в dev; снимает запись `pipeline-8-9` сценария §2; этапы 10–11 — следующий пакет |

Порядок — выбранная последовательность планирования; утверждённые
требования есть только у пакетов со спецификацией. Автоматическое выполнение проверяет manifest, а не только
зелёный CI.
До полного M4 также остаются каталог и вставка компонентов (решение Q-1
пакета component.extract), advanced bindings, буфер обмена и wrap. Перед готовностью к эксперименту M6: недостающие этапы pipeline,
нагрузочные NFR-001…007, безопасность и сценарий §2 без временных обходов.
Внешние OIDC/production endpoints и Figma не считаются настроенными из-за локальной
приёмки. Оптимизация CI — сопутствующая работа: сохранить исправления registry/CPU,
затем измерить время и добавить Docker cache без ослабления проверок.

Сквозной сценарий §2 по шагам:

| Шаг | Состояние |
|---|---|
| 1. `manifest push` | Реализован (CLI, регистрация и активация manifest) |
| 2. Сборка блока на токенах | Реализован в Studio; preview 375/1024 px проверяется пакетными E2E |
| 3. Сохранить как компонент | Реализован ([PR #67](https://github.com/QahNaaRlN/project-17/pull/67), слит 2026-10-20): `component.extract`, мастер STU-024, `create_component`; [спецификация](../studio-component-extract.md) STU-400…419, [локальная приёмка](../studio-component-extract-acceptance.md) двумя людьми пройдена; мастер проверяется сценарием §2 в CI |
| 4. Сущность, привязки, Repeat | Реализован (PR #57, #58); IR-044 и порог < 1 с не подтверждены |
| 5. Правка агентом через MCP | Реализован (PR #48); живой вызов требует ключа и модели |
| 6. Preview и выбор узла | Реализован (PR #38, #50); NFR-005 (< 400 мс) не измерен |
| 7. Проверка и согласование | Реализован, кроме этапов pipeline 8–9 (M6); в сценарии — обход `pipeline-8-9` |
| 8. Публикация и `promote` | Реализован в API и Studio (PR #84); в сценарии — в Studio |
| 9. Откат | Реализован (API и Studio, PR #54, #84), включая production; в сценарии — в Studio |
| 10. Missing Capability | Реализован (PR #60…#62, #68, #71): ручной путь, доставка pack, факты PR/CI, preview/<ref>, автоматическое fulfillment и группы requiredBy; локальная приёмка шага 10 пройдена; в CI шаг проходит с сервером фактов GitHub вместо реального GitHub |

Сценарий §2 проходит автоматически в CI; job `E2E scenario (§2)` обязателен для PR
в `dev` и `main`. Остался один временный обход из
[`pending.ts`](../../examples/react-store/e2e/scenario/pending.ts) — этапы pipeline 8–9.
Критерий готовности 1 будет закрыт, когда снят и он; остальные критерии §3 пока не выполнены.

### Реализованные пакеты и оставшиеся границы

Текущий срез M4: [Studio editor](../studio-editor.md) реализует путь «открыть
черновик → выбрать узел → изменить локальный текст/отступ → сохранить →
перезагрузить». Он не закрывает целиком M4 или сквозную приёмку MVP ниже.
[Каркас и Страницы](../studio-shell-pages.md) реализуют начальный каталог в новой
палитре с серверным поиском. [Редактор и живой preview](../studio-editor-preview.md)
добавляют рабочую панель, вписывание холста, защиту несохранённых свойств,
отложенную загрузку режимов и проверенную связку с React-витриной.
Пакет [страниц и рабочих черновиков](../studio-workspace.md) добавляет выбор
из списков и создание собственного CS; вход в работу больше не требует двух ID.
[История и отмена](../studio-history.md) добавляют просмотр действий CS и
атомарный undo/redo одной пользовательской правки. [Палитра и структура](../studio-structure.md)
добавляют создание, перемещение/DnD, удаление и дублирование с новыми ID.
Пакет [проверок и публикации](../studio-publishing.md) добавляет подачу,
диагностики, согласование по правам и явную публикацию (STU-070…074).
Пакет [входа и доступа к согласованию](../studio-identity.md) добавляет
универсальный OIDC, отзывные серверные сессии, явные роли и очередь работ
(STU-080…084). Реальный IdP и production-развёртывание ещё не настроены.
Пакет [Работ и согласования](../studio-work-approval.md) объединяет очередь,
выбранную карточку проверок/решений и историю в новой палитре (STU-144…148).
Локальная приёмка с подписанным OIDC-провайдером, двумя людьми, настоящим
CMS/PostgreSQL и React-витриной подтверждает сохранение → подачу → запрет
самосогласования → согласование другим человеком → явную публикацию.
Внешний IdP и production остаются отдельными зависимостями. Синхронизация
с Figma отложена. Пакет
[сравнения изменений перед согласованием](../studio-review-diff.md),
STU-149…161 **реализован и слит в dev в PR #53; локальная приёмка и CI пройдены**:
сравнение базы и публикации с рабочими версиями всего CS, согласованный
снимок, безопасный Query API и защита решения от устаревшего сравнения.
[История публикаций и безопасный откат](../studio-publication-history.md)
(STU-162…175) реализованы и слиты в dev в PR #54; локальная приёмка и CI пройдены.
[Полная очередь работ и серверный поиск](../studio-work-queue.md)
(STU-176…189) реализованы и слиты в dev в PR #55; локальная приёмка и CI пройдены.
[Создание работ с контентом без страницы](../studio-content-work.md)
(STU-190…202) реализовано и слито в dev в PR #56; локальная приёмка и CI пройдены.
Пакет добавляет самостоятельное создание CS из очереди, сущности и файлы
через существующие формы, затем согласование и явную публикацию без страницы.
[Привязки контента к странице](../studio-content-bindings.md), STU-203…218,
реализованы и слиты в `dev` 2026-10-16 в
[PR #57](https://github.com/QahNaaRlN/project-17/pull/57), merge `e9b70bc`.
Локальная сквозная приёмка и [CI конечного SHA PR](https://github.com/QahNaaRlN/project-17/actions/runs/38861497965)
пройдены. Пакет включает выбор основной сущности, типизированные привязки,
resolver by:entity, локали и preview/published с подписанными изображениями.
[CI merge-коммита в dev](https://github.com/QahNaaRlN/project-17/actions/runs/38862616860)
также пройден: Check (TS + Go) и Docker image.

Пакет [источники данных и повторяющиеся блоки](../studio-data-sources.md),
STU-219…236: **реализован и слит в dev в
[PR #58](https://github.com/QahNaaRlN/project-17/pull/58)** 2026-10-16,
merge `c4b8286df779dbf1f41f0c23f46d73e7c7249f84`.
Добавлены формы подключений/параметров из manifest, типизированные `$data`
и `$item`, Repeat/empty, история и preview с реальными загрузчиками приложения.
Локальная сквозная приёмка пройдена.
[CI итогового SHA PR `4f921bb`](https://github.com/QahNaaRlN/project-17/actions/runs/38894564872)
и [CI merge-коммита в dev](https://github.com/QahNaaRlN/project-17/actions/runs/38895859007)
пройдены: Check (TS + Go) и Docker image, включая Linux race/Go coverage 95%,
сборки и 60 браузерных сценариев. Исправления ревью покрыты Go/TS-фикстурой
совместимости URL/Link и UI-тестом сброса Repeat после отзыва доступа.
Зависимости эксплуатации: активный manifest и совместимый серверный registry,
права design.compose/content.read, preview bridge и image delivery.
Figma не требуется. Конструктор поведения/условий и component.extract в пакет
не входят; полный шаг 4 MVP и IR-044 ещё не закрыты.
Полный M4 остаётся частичным.

[Действия и условия показа](../studio-behavior-conditions.md), STU-237…254:
**реализованы и слиты в dev в
[PR #59](https://github.com/QahNaaRlN/project-17/pull/59)** 2026-10-16,
merge `35ccee43467d8e36a412c93a2e15c78a08df330d`.
Добавлены цепочки с типизированными аргументами, navigate/scrollTo, конструктор
when, работа со скрытыми узлами и design/interact. Используются существующие
node.setBehavior/node.setCondition; добавлены отдельные права behavior/condition,
строгая совместимость, защита locked и устаревшего контекста.
При ревью устранено продолжение отложенных цепочек после смены плана/режима
и unmount; три регрессионных сценария проходят с настоящим React root.
Локальная приёмка с двумя людьми и новым публичным браузером пройдена.
[CI итогового SHA PR `7d31eba`](https://github.com/QahNaaRlN/project-17/actions/runs/38920401345)
и [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/38922047905)
пройдены: Check (TS + Go), Docker image, Go race/coverage 95%, Studio 316,
React 73 и 60 браузерных сценариев. Полный M4/IR-044 остаётся частичным.
Зависимости: manifest events/actions/args, registry клиентского приложения,
пакет #58 и preview bridge. Для приёмки — два OIDC-пользователя, PostgreSQL
и управляемые обработчики приложения. Figma не требуется.
Пакет [Structure, CodeMirror и JSON diff](../studio-json-diff.md) добавляет JSON
узла/страницы, диагностики/подсказки и атомарный diff с историей (STU-090…094).
Пакет [сущностей, RichText и ассетов](../studio-content.md) добавляет каталог
контента, схемные формы, локали проекта, ссылки, RichText v1, metadata и upload
с явным добавлением/заменой файла в CS (STU-100…104).
Пакет [SSE/presence](../studio-realtime.md) реализован (STU-110…114).
Пакет [Studio Agent и MCP](../studio-agent.md) добавляет делегированные сессии,
семантические инструменты, серверный OpenRouter и шаги с отменой (STU-120…125).
Для живого вызова нужны серверный ключ, модель и вход пользователя с agent.delegate.
Пакет M5 — [Missing Capability и context pack](../studio-missing-capability.md),
STU-255…274: **реализован и слит в dev 2026-10-17 в
[PR #60](https://github.com/QahNaaRlN/project-17/pull/60)**,
merge `9259d6272471cba057c0625bd554bb730ae64660`.
Первый срез: запрос недостающего action в CODE-зоне, проверенная загрузка индекса,
детерминированный pack, scoped MCP draft, ручная передача разработчику и явная
замена pending после появления совместимого действия в активном manifest.
Зависимости: PostgreSQL/миграции и Command Bus, активный manifest с индексом
точного commit, capability.request/code.propose и отдельные права изменения IR,
CLI/приложение и два OIDC-пользователя для приёмки. Figma и ключ LLM не нужны
для ручного пути; встроенный Agent требует своей действующей конфигурации.
Полный M5 ещё не завершён: другие виды запросов и условные политики остаются
следующими пакетами (факты PR/CI, preview/<ref> и автоматическое fulfillment
слиты позже в PR #62, #68 и #71). CLI уже строит индекс кода;
его серверное хранение/неизменяемая связь с manifest реализованы в STU-263…264.
Локальная приёмка 2026-10-17: настоящий CMS/PostgreSQL/OIDC, формы двух людей,
точный retry после потери ответа, безопасный export, реальная сборка/CLI push,
явный resolve, независимое согласование и выполнение на опубликованной странице
в новом браузере. Мобильная ширина 375 px проверена. При ревью исправлены
одиночные обработчики/заглушки, подтверждение замены, состав pack, многочастные
ключи и ограничения text. [CI итогового SHA](https://github.com/QahNaaRlN/project-17/actions/runs/39027310418)
и [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/39028004513)
пройдены: Check (TS + Go), Go race/coverage 95%, Studio 328 тестов,
60 браузерных сценариев и Docker image.

Пакет M5 — [подписанная доставка context pack](../studio-capability-delivery.md),
STU-275…294: **реализован и слит в dev 2026-10-17 в
[PR #61](https://github.com/QahNaaRlN/project-17/pull/61)**,
merge `cf59c4568cf276800922d80ee73e9c15650d25d7`.
Отдельное согласие на внешнего получателя, HMAC-SHA256, transactional outbox/River,
ограниченные повторы/dedup, текущие права и журнал доставки. Delivered не меняет
инженерный статус запроса, IR или публикацию. Зависимости: пакет #60, новая
миграция/worker, capability.deliver, операторский endpoint/keyring, доступный
получатель с durable receipt/dedup и синхронизированные часы. Для приёмки —
изолированный PostgreSQL/OIDC и HTTP receiver; Figma и LLM key не требуются.
Внешний кодовый агент и production endpoints настраиваются отдельно.
Локальная [приёмка](../studio-capability-delivery-acceptance.md) 2026-10-17 пройдена:
реальные формы, отдельный SQLite receiver, потеря квитанции после commit,
рестарт CMS, тот же ID/body и одна запись inbox; две попытки и delivered в Studio.
Проверены отзыв прав, отмена в полёте, смена target, stale pack, два проекта,
блокировка публикации pending и EN/375 px. Администратор без design.read
может настраивать получателя. Статусы запроса и CS не меняются от доставки.
[CI конечного SHA PR](https://github.com/QahNaaRlN/project-17/actions/runs/39036418490)
и [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/39037306790)
пройдены.

Пакет M5 — [проверенные факты GitHub PR/CI](../studio-capability-github.md),
STU-295…314: **реализован и слит в dev 2026-10-19 в [PR #62](https://github.com/QahNaaRlN/project-17/pull/62)**, merge `359981e31c188d02a28a0bafa26ed418f2235ecb`.
Явная привязка запроса к PR настроенного репозитория, Checks и Commit Statuses
на точном SHA от доверенного издателя, GitHub App для приватных репозиториев.
Публичная и приватная внешняя проверка STU-314 и [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/39246472929) пройдены.
Автоматической отправки старых запросов пакет не включает.

Пакет M5 — [preview/<ref>](../studio-capability-preview.md), STU-315…334:
**реализован и слит в dev 2026-10-20 в [PR #68](https://github.com/QahNaaRlN/project-17/pull/68)**, merge `59ce50f6ab7ca3d5faf9f2de1fe501273f550270`.
Окружения `preview/pr-<N>` для проверенного PR, регистрация manifest с `headSha`,
точные origin сборок, `delete-environment`, TTL 14 дней и архив по closed/merged;
просмотр CS на preview с подстановкой действия вместо `pending:<id>` только в ответе
этого preview и формой аргументов. Содержимое — рабочие версии CS поверх head
(уточнение STU-323). [Локальная приёмка](../studio-capability-preview-acceptance.md)
двумя людьми и [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/39387944386) пройдены; покрытие Go — на пороге 95%.
Далее остаются другие kinds и условные policies.

Пакет M5 — [автоматическое fulfillment и группы requiredBy](../studio-capability-fulfillment.md),
STU-335…354: **реализован и слит в dev 2026-10-20 в [PR #71](https://github.com/QahNaaRlN/project-17/pull/71)**, merge `4a76e82dcec8b749451c16c40abdbf395d16c941`.
Таблица привязок запроса и присоединение из других CS, системный актор `cms-system`
с узким правом замены `pending:<id>`, оценка условий STU-339 после активации
standard-manifest (слияние PR, CI слияния, commit сборки равен слиянию или его
потомок по GitHub compare), замена по CS одной группой операций, аргументы
выполнения, undo/повтор и раздел «Выполнение» в Studio.
[Локальная приёмка шага 10](../studio-capability-fulfillment-acceptance.md) пройдена;
факты GitHub в ней — фикстура изолированной БД. CI итогового SHA PR пройден,
Go coverage 95.1%. Полный M5 не закрыт: запросы component/dataSource/token и
условные политики остаются.

Пакет M5 — [запросы недостающих компонентов](../studio-capability-component.md),
STU-355…374: **реализован и слит в dev 2026-10-21 в [PR #73](https://github.com/QahNaaRlN/project-17/pull/73)**, merge `4262d93`.
Вид запроса `component`: контракт свойств/слотов, заглушка-узел с полем `pending`
(IR-053), блокировка публикации, экземпляры в нескольких CS, pack с похожими
компонентами, preview/<ref>, автоматическое выполнение через `node.clearPending`
от `cms-system`, мастер Studio. [Локальная приёмка](../studio-capability-component-acceptance.md)
пройдена; CI итогового SHA — Go coverage 95.2% (цель пакета 95.3% не достигнута).
Далее — запросы dataSource/token и условные политики.

Пакет M5 — [запросы недостающих источников данных и токенов](../studio-capability-data-token.md),
STU-375…394: **реализован и слит в dev 2026-10-21 в [PR #75](https://github.com/QahNaaRlN/project-17/pull/75)**, merge `ad1e7c6`.
Виды `dataSource` (подключение страницы с меткой `pending`, `$data.<key>` по
предложенному результату, без загрузки) и `token` (значение `pending:<id>` в
дизайн-свойстве, предлагаемое значение в preview); блокировка публикации,
присоединение, совместимость по форме, автоматическая замена `cms-system`,
мастера Studio. [Локальная приёмка](../studio-capability-data-token-acceptance.md)
пройдена; CI итогового SHA — Go coverage 95.1%.

Пакет M5 — [условные политики агента](../studio-agent-policy.md),
STU-420…439: **реализован и слит в dev 2026-10-21 в [PR #78](https://github.com/QahNaaRlN/project-17/pull/78)**, merge `a6a9dc0ba0e0d5787df8ab91163a1a029f97b9cc`.
Документ политики v1 с правилами `allow`/`deny`/`requireApproval` и условиями
(окружение, путь, зона, тип операции, схема, риск, источник), неизменяемые версии
`set-agent-policy` с `expectedVersion`, dry-run `POST /agent-policy/explain`,
проверка каждой операции агента до записи (`403 POLICY_DENIED` с правилом),
повторная проверка на этапе 5 и при публикации, раздел «Политика агентов» в Studio.
[Локальная приёмка](../studio-agent-policy-acceptance.md), [CI итогового SHA](https://github.com/QahNaaRlN/project-17/actions/runs/39570897945)
и [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/39572946996) пройдены; Go coverage 95.08% — запас над порогом минимален.
Очередь пакетов M5 этим исчерпана.

Пакет M6 — [сквозной сценарий §2 в CI](../e2e-scenario.md),
STU-440…459: **реализован и слит в dev 2026-10-26 в [PR #81](https://github.com/QahNaaRlN/project-17/pull/81)**, merge `88b718120150160babf7b6d3fbf7007730ccc9b1`.
Playwright-сценарий из 10 шагов на `examples/react-store` против настоящих CMS,
PostgreSQL, River и Studio (`pnpm e2e:scenario`, стенд `scripts/e2e-stand.mjs`);
детерминированные замены: скриптованный MCP-клиент, сервер фактов GitHub
(тег сборки `e2e`), приёмник CDN purge. Сценарий нашёл и исправил пробелы продукта:
треки `columns`/`rows` у `Grid` в runtime, `content.read` для `cms-system`,
`onBehalfOf` в операциях CS, `cms bootstrap -max-mode`. Структура блока шага 2 и
привязки шага 4 создаются командами API, пока в Studio нет палитры и вставки
компонентов. [Локальная приёмка](../e2e-scenario-acceptance.md), [CI итогового SHA](https://github.com/QahNaaRlN/project-17/actions/runs/40288321699)
и [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/40294758824) пройдены. После пяти зелёных прогонов подряд (PR #81…#84 и слияния)
job `E2E scenario (§2)` 2026-10-26 включён обязательной проверкой для PR в `dev` и `main` (решение §11.1).

Пакет M4 — [перенос в production и откат production в Studio](../studio-promote.md),
STU-460…479: **реализован и слит в dev 2026-10-26 в [PR #84](https://github.com/QahNaaRlN/project-17/pull/84)**, merge `2cba1437cf3e25cb7b58cbb24ca37555e8e40626`.
Целевые окружения `promotionTargets` в конфигурации Studio, предпросмотр
переноса без записи (транзакция откатывается) со снимком и `409 PROMOTE_SNAPSHOT_STALE` в команде `promote`,
выбор окружения в «Публикациях», перенос с причиной и откат production тем же экраном.
Шаги 8–9 сценария §2 выполняются в Studio. [Локальная приёмка](../studio-promote-acceptance.md),
[CI итогового SHA](https://github.com/QahNaaRlN/project-17/actions/runs/40311593042) и [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/40313597429) пройдены.

Пакет M4 — [сохранить поддерево как компонент](../studio-component-extract.md),
STU-400…419: **реализован и слит в dev 2026-10-20 в [PR #67](https://github.com/QahNaaRlN/project-17/pull/67)**, merge `951a5deff58ae92996e0e2367689d126b14de715`.
Операция `component.extract` с обратной операцией и rebase, мастер STU-024,
MCP-инструмент `create_component` и рендер `Composed` в витрине и адаптере Studio.
[Локальная приёмка](../studio-component-extract-acceptance.md) шага 3 MVP
и [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/39385815988) пройдены.
Конструкторы данных (STU-219…236) и поведения/условий (STU-237…254) слиты в dev.
Каталог и вставка компонентов, advanced bindings, буфер/wrap и NFR-приёмка
остаются отдельными задачами.

## 2. Сквозной сценарий приёмки

Автоматизированный E2E-тест (Playwright) на `examples/react-store`. Каждый шаг соответствует пункту MVP архитектуры v0.1 §26.

| # | Шаг | Проверка | Требования |
|---|---|---|---|
| 1 | CI эталонного приложения выполняет `cms manifest push --env staging` | Manifest активен; палитра Studio содержит компоненты приложения и токены | MF-001…004, MF-021 |
| 2 | Дизайнер в зоне SYSTEM собирает блок «ProductShowcase»: `Grid` (1 колонка на base, `3fr 2fr` на md) → `Stack` с `Heading`, `Text`, `Button` + `Image`; задаёт токены отступов | В IR только токены; preview на 375 и 1024 px соответствует ожиданию | FR-010, FR-014, DS-020 |
| 3 | Дизайнер сохраняет поддерево как компонент с входами `title`, `description`, `image`, `products` и слотом `footer` | Документ `kind=component`; на странице — экземпляр `Composed` | FR-011, IR-060…063 |
| 4 | Редактор создаёт сущность `Collection`, страница привязывается к ней; блок наполняется: `Repeat` по `$data.featured.items` рендерит `ProductCard` | Контент отображается в preview; правка поля сущности видна в preview < 1 с | FR-015, FR-021, IR-044 |
| 5 | Через MCP агент получает `get_page_structure`, меняет `gap` на `lg` для `lg`-breakpoint'а и вариант карточек на `compact` | Операции агента в том же CS с `actor.kind=agent`, `onBehalfOf` дизайнера; виден diff | FR-050, FR-051, AGT-022 |
| 6 | Studio показывает preview настоящего приложения; выбор узла кликом в iframe выделяет его в дереве | Сообщения `node.click` → выделение; `document.patch` применяется < 400 мс | FR-040, FR-041, NFR-005 |
| 7 | CS отправляется на проверку; проходят этапы 1–9; согласует lead-designer, не являющийся автором | Попытка согласования автором или агентом — `403 APPROVAL_SELF` / `POLICY_DENIED` | PUB-001…004 |
| 8 | Публикация в staging, затем `promote` в production | Приложение production показывает изменения ≤ 10 с; `Surrogate-Key`-инвалидация выполнена | FR-031, PUB-020, PUB-021 |
| 9 | Откат публикации production | Прежняя версия видна ≤ 10 с; новых версий не создано | FR-032, PUB-030 |
| 10 | Дизайнер в зоне CODE запрашивает «быстрый просмотр»; создаётся Missing Capability с context pack; тестовый PR добавляет действие; после push manifest запрос `fulfilled`, заглушка заменена | Публикация с заглушкой блокировалась; после выполнения — проходит | FR-052, FR-053, AGT-060…081 |

## 3. Критерии готовности MVP

1. Сквозной сценарий §2 проходит автоматически в CI.
2. Нагрузочные тесты подтверждают NFR-001…007 при профиле [01 §5](01-requirements.md#5-профиль-нагрузки-mvp).
3. Conformance-фикстуры IR проходят на Go и TS.
4. Нет открытых уязвимостей уровня high/critical.
5. Спецификация обновлена по итогам реализации; все отступления оформлены в ADR.

## 4. Протокол эксперимента

### 4.1. Дизайн

- **Участники:** не менее 9 дизайнеров интерфейсов с опытом ≥ 2 лет; уровень знакомства с Webflow фиксируется.
- **Условия:** A — Webflow-подобный редактор; B — CMS вручную; C — CMS + агент.
- **Схема:** внутригрупповая, порядок условий уравновешен латинским квадратом 3×3; три эталонных макета сопоставимой сложности (по 15–25 блоков, 3 breakpoint'а), макеты чередуются между условиями.
- **Обучение:** 45 минут на каждый инструмент до начала, одинаковые учебные материалы.
- **Ограничение времени:** 3 часа на задание.
- **Инженер на связи:** доступен по запросу во всех условиях; каждое обращение и его длительность фиксируются.

### 4.2. Метрики и способ измерения

| Метрика | Измерение |
|---|---|
| Время | До момента, когда результат проходит адаптивную проверку на 375/768/1440 px |
| Зависимость от разработчика | Число и суммарная длительность обращений к инженеру; число созданных Missing Capability |
| Визуальное соответствие | Доля несовпадающих пикселей относительно эталона на трёх ширинах (после выравнивания контента) + экспертная оценка 1–5 |
| Design System | Число `raw`-значений и нарушений токенов (B, C — из IR; A — аудит CSS) |
| Адаптивность | Число дефектов вёрстки на трёх ширинах (перекрытия, горизонтальная прокрутка, обрезка) |
| Accessibility | Нарушения axe-core уровней critical/serious + ручная проверка клавиатурой |
| Сложность IR | Число узлов, максимальная глубина, число `raw`-overrides, число зон и исключений политики |
| Субъективная оценка | SUS и NASA-TLX после каждого условия |

### 4.3. Предлагаемые пороги подтверждения гипотезы

Подлежат утверждению (Q-08).

| Критерий | Порог |
|---|---|
| Время B относительно A | медиана B ≤ 1,25 × медианы A |
| Время C относительно B | медиана C ≤ 0,7 × медианы B |
| Зависимость от разработчика в B и C | медиана ≤ 1 обращения, без обращений за кодом вне Missing Capability |
| Нарушения дизайн-системы в B и C | 0 нарушений токенов в зонах SYSTEM; `raw` только в зонах FREE |
| Доступность в B и C | 0 нарушений critical; serious не больше, чем в A |
| Визуальное соответствие B и C | не хуже A более чем на 0,5 балла экспертной оценки |
| SUS для B | ≥ 68 |

Гипотеза считается подтверждённой, если для B выполнены все пороги, а для C — порог по времени, пороги по дизайн-системе и доступности.
