# Сквозной сценарий §2 в CI: реализация и проверка

Дата: 2026-10-21. Ветка: `feature/e2e-scenario` от `dev` `1f6a6e1`; слита в dev
2026-10-26 в [PR #81](https://github.com/QahNaaRlN/project-17/pull/81), merge `88b7181`; [CI merge-коммита](https://github.com/QahNaaRlN/project-17/actions/runs/40294758824) пройден.
[Спецификация STU-440…459](e2e-scenario.md) одобрена пользователем 2026-10-21
вместе с решениями §11.

## Реализовано

- Сценарий [`e2e/scenario/mvp.spec.ts`](../examples/react-store/e2e/scenario/mvp.spec.ts):
  10 шагов §2 одной историей, один проект и одна БД, один CS для шагов 2–7;
  отдельный `playwright.scenario.config.ts` (`retries: 0`, один worker), фикстурные
  E2E исключают `e2e/scenario/**`.
- Стенд [`scripts/e2e-stand.mjs`](../scripts/e2e-stand.mjs): CMS (тег сборки `e2e`),
  PostgreSQL, River, Studio-BFF, витрины staging и production, локальный OIDC
  (Alice — designer, Bob — lead-designer и project.admin, Erin — editor), приёмник
  CDN purge и сервер фактов GitHub; всё на loopback. Запуск: `pnpm e2e:scenario`.
- Детерминированные замены: скриптованный MCP-клиент вместо LLM; сервер фактов PR/CI
  ([`e2e-github-fixture.mjs`](../scripts/e2e-github-fixture.mjs)), доступный только
  бинарю с тегом `e2e` (`CMS_E2E_GITHUB_URL`, только `http://127.0.0.1:<port>`);
  приёмник purge ([`e2e-purge-receiver.mjs`](../scripts/e2e-purge-receiver.mjs)) по
  существующему `CMS_CDN_PURGE_URL`.
- Фикстуры витрины: схема `Collection`, компонент `ProductCard` (`variant`), действие
  `store.quickView` — только патчем шага 10 (`e2e/scenario/quick-view.patch`);
  после шага патч откатывается и витрина пересобирается.
- Реестр незавершённого [`pending.ts`](../examples/react-store/e2e/scenario/pending.ts):
  этапы pipeline 8–9, `promote` в Studio, откат production в Studio. Запись падает,
  как только часть становится доступной.
- Метрики `e2e-metrics.json`; строгий гейт — видимость в production ≤ 10 с; IR-044
  и NFR-005 падают только при p95 > 2 с и > 800 мс (решение §11.3).
- CI: job `E2E scenario (§2)` (PostgreSQL как `services`, таймаут 25 мин), артефакт
  `e2e-scenario`: журналы стенда, трассы, видео, скриншоты шагов, метрики; файлы
  `*.private.*` и бинарь не загружаются. Job не сделан обязательным (решение §11.1).

## Изменения продукта, найденные сценарием

| Изменение | Причина |
|---|---|
| `@cms/runtime`: дизайн-свойства `columns`/`rows` у `Grid` (целое 1…12, треки `Nfr`, `auto`; 03 §3) | Рантайм отвергал `columns` (`CMS_DESIGN_UNSUPPORTED`), хотя сервер и IR его принимают; без этого шаг 2 невыполним. Сырые треки (px, %, `minmax`) пока отвергаются |
| `cms-system` получает `content.read` при автоматической замене | Замена обработчика на странице с основным контентом падала `FORBIDDEN` и помечала привязку `invalid_args`; этап 5 уже считал `content.read` правом `cms-system` |
| `GET /changesets/{id}/operations` отдаёт `onBehalfOf` | STU-449 требует проверить делегата операций агента через публичный API |
| `cms bootstrap -max-mode` | Режим CODE включался только SQL; сценарию прямые записи в БД запрещены |
| Тег сборки `e2e` в `capabilitygithub` (keep-alive на loopback) | STU-445; без keep-alive Node.js сбрасывает соединения на Windows. Продуктовый бинарь закреплён на `api.github.com` — проверено тестом |
| `dev-oidc`: третий пользователь Erin | Редактор контента шага 4 (STU-443) |

## Отступления и уточнения при реализации

| Тема | Решение | Основание |
|---|---|---|
| Сборка структуры | Структура блока шага 2, привязки и `Repeat` шага 4 создаются командами API из сессии дизайнера; мастер «Сохранить как компонент», превью 375/1024, выбор в iframe, подача, согласование, публикация, запрос действия и аргументы выполнения — в Studio | Палитра и вставка компонентов — незавершённая часть M4; публичный командный API тот же, что у Studio |
| Вход `image` компонента | Не создаётся: `Image` без `src` | На стенде нет S3/imgproxy; вход изображения проверен приёмкой component.extract |
| Откат production | Командой API от Bob, запись `studio-production-rollback` в pending | Studio работает в одном окружении (STU-001); вопрос — ниже |
| «Прежняя версия» шага 9 | Страница в production впервые появилась промоушеном шага 8; после отката её нет | Одна история без отдельной предварительной публикации в production |
| Этапы 1–7, 13 | Проверяются по результатам `checks` ревью (все `passed`) | Этапы сервера сгруппированы в проверки `ir`, `policy` и т. п. |
| Аргумент действия | Дизайнер подтверждает `id` в разделе «Выполнение» Studio | У `store.quickView` обязательный аргумент: без подтверждения замена ждёт (`awaiting_args`) |
| Общий оркестратор | Новый `e2e-stand.mjs`; `studio-review-stand.mjs` не переведён | Не менять поведение прошлых приёмок без необходимости |

## Локальная проверка

Windows, PostgreSQL 16 в Docker, бинарь CMS с тегом `e2e`. Три прогона подряд —
**PASS** (82–93 с, сценарий 1,3–1,5 мин).

| Шаг | Проверено |
|---|---|
| 1 | `cms manifest push` в staging и production, schema-CS опубликованы; манифест содержит `ProductCard`, `Collection`, токены |
| 2 | В IR нет `raw`; `Grid` — 1 колонка на 375 px и `3fr 2fr` на 1024 px |
| 3 | Мастер Studio: `kind=component`, входы `title`, `description`, `products`, слот `footer`, экземпляр `Composed` |
| 4 | Erin публикует `Collection`; `Repeat` по `$data.featured.items` даёт 3 карточки; правка поля видна в превью |
| 5 | MCP: `get_page_structure`, `update_design` (`gap` lg), `update_props` (`compact`); операции `source=agent`, `onBehalfOf` = Alice; diff ревью |
| 6 | Клик в iframe выделяет экземпляр; правка отступа доходит до превью |
| 7 | Alice — `APPROVAL_SELF`; токен агента — `POLICY_DENIED`; MCP без инструмента согласования; Bob согласует; проверки `passed` |
| 8 | Публикация в Studio; promote; production за ~0,1 с; purge с ключами production |
| 9 | Откат; production без страницы за ~0,03 с; версии не созданы; purge |
| 10 | Запрос «Quick view» в зоне CODE, подача `failed` (`ACTION_PENDING_CAPABILITY`), PR через фикстуру, сборка с патчем, `manifest push`, аргумент, `replaced`, `fulfilled`; повторная подача, согласование и публикация |

Измерения (три прогона):

| Ограничение | p95 | Максимум | Гейт |
|---|---|---|---|
| IR-044, правка поля в превью (10 правок) | 0,91–1,93 с | 1,93 с | > 2 с |
| NFR-005, `document.patch` (20 правок) | 133–154 мс | 168 мс | > 800 мс |
| Видимость в production | 97–114 мс | — | ≤ 10 с |
| Откат в production | 25–27 мс | — | ≤ 10 с |

## Автоматические проверки

- Go: `go test -race` в Linux-контейнере зелёный, кроме `assets`/`imagedelivery`
  (нет S3/imgproxy); покрытие 95,09% в контейнере (в CI обычно на 0,1 выше);
  тесты тега `e2e` (`go test -tags e2e`), продуктовой привязки к `api.github.com`,
  `-max-mode`, `onBehalfOf`, прав `cms-system`.
- TS: `@cms/runtime` (треки `Grid`), витрина (`ProductCard`), `dev-oidc`; ESLint,
  typecheck, Prettier; 61 фикстурный E2E витрины и Studio.

## Замечания для следующих пакетов

- **IR-044 не выполняется:** правка поля доходит до превью за 0,9–1,9 с — поток
  событий сервера опрашивает изменения раз в секунду. Гейт 2× пока проходит, но
  запас мал; точный порог — нагрузочные тесты M6.
- Откат и перенос production в Studio невозможны, пока Studio работает в одном
  окружении (pending `studio-promote`, `studio-production-rollback`).

## Не проверено

- Первые прогоны job в CI (см. PR) и пять зелёных прогонов подряд для
  обязательности.
- Реальные GitHub, CDN и языковая модель.
