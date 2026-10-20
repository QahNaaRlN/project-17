# Сохранить как компонент: реализация и приёмка

Дата: 2026-10-20. Ветка: `feature/component-extract` от `dev` `9437fb5`
(после PR #65 и #66). Черновой PR:
[#67](https://github.com/QahNaaRlN/project-17/pull/67).
[Спецификация STU-400…419](studio-component-extract.md) одобрена пользователем
2026-10-19 вместе с решениями Q-1…Q-9. Пакет закрывает шаг 3 сквозного
сценария MVP. Полный M4 и автоматический сценарий §2 в CI этим не закрыты.

## Реализовано

- **Чистая операция** [`component.extract`](../apps/server/internal/composition/ops/extract.go):
  - входы из свойств (literal и привязки) и новые входы по типу;
  - список берётся из контракта источника (Q-3);
  - слоты: диапазон соседних узлов или пустой слот с `at` и `min: 0` (Q-7);
  - `$local`: `copy` по умолчанию, явный `move` (Q-4);
  - внешние области, ссылки на узлы через границу, защищённые поддеревья и пределы;
  - экземпляр `Composed` с `ref.version = "live"` без `design` (Q-2).
- **Обратная операция** `component.revertExtract` создаётся только историей.
  Канонический payload хранит назначенные ID, поэтому redo и rebase
  детерминированы.
- **[План извлечения](../apps/server/internal/composition/ops/extract_plan.go)**
  тем же кодом, что проверяет операция, и Query
  `GET /api/v1/documents/{id}/component-extract-plan`.
- **[Слой Change Set](../apps/server/internal/changes/component.go):**
  - объект компонента создаётся в том же CS;
  - права `component.write` и `design.compose`, а `content.read` — при переносе `$content`;
  - имя уникально в проекте (Q-6);
  - undo исключает компонент из CS, redo восстанавливает тот же ID;
  - rebase: `COMPONENT_EXTRACT_BASE_CHANGED` разрешается только через `theirs`,
    зависимые операции исключаются каскадно (Q-9).
- **STRICT (Q-5):** несертифицированный экземпляр отклоняется существующей
  проверкой L6 `POLICY_COMPONENT_MODE_REQUIRED`.
- **MCP:** `plan_component_extract` и `create_component`. Встроенный Agent читает
  компонент, на который ссылается страница сессии.
- **Studio:**
  - мастер STU-024 (имя → входы → слоты → итог) с повтором замороженного тела и
    ключа после неопределённого исхода;
  - коды и указатели отказа операции;
  - формы входов экземпляра через существующие панели;
  - выбор экземпляра кликом по холсту;
  - документ компонента в Structure с `$props` и Repeat по входу-списку;
  - подписи в истории и в панели агента; RU/EN.
- **Runtime/React:** общая Go/TS-фикстура
  [component-extract.json](../packages/ir/fixtures/component-extract.json) и проверка
  эквивалентности плана рендера. `@cms/react/server` экспортирует `loadComponents`.
  Эталонная витрина рендерит экземпляры в SSR и в bridge preview.

## Дефекты, найденные приёмкой и проверками

| Где | Дефект | Исправление и регрессия |
|---|---|---|
| Studio BFF | `component.extract` не входил в список разрешённых операций адаптера: `STUDIO_OPERATION_UNAVAILABLE` | Тип добавлен; внутренний `component.revertExtract` по-прежнему отклоняется ([тест](../apps/studio/test/component-extract-bff.test.ts)) |
| Витрина | `preparePage` вызывался без компонентов: после извлечения preview и публичная страница отдавали 503 | `loadComponents` в SSR и bridge ([тест](../examples/react-store/test/load.test.ts)) |
| Studio | Фоновое перечитывание, наложившееся на собственную запись, оставляло `CHANGESET_SEQ_CONFLICT`, и форма данных не закрывалась (2 из 3 прогонов) | Удалённое перечитывание откладывается и повторяется ([тест](../apps/studio/test/realtime.test.ts)) |
| Сценарий | Точное сравнение DOM ловило различие сериализации `style` между SSR и клиентским рендером и субпиксельную высоту | Стили нормализуются через `style.cssText`, размеры сравниваются с точностью 1 px; разметка — точно |

## Проверяемые требования

| Требования | Доказательства |
|---|---|
| STU-400…401 | [план Go](../apps/server/internal/composition/ops/extract_test.go), [маршрут](../apps/server/internal/httpapi/component_extract_test.go), [мастер](../apps/studio/test/component-extract-ui.test.tsx), [API/BFF](../apps/studio/test/component-extract-api.test.ts) |
| STU-402…405 | [операция и ветки](../apps/server/internal/composition/ops/extract_branches_test.go), [модель мастера](../apps/studio/test/component-extract.test.ts) |
| STU-406…410 | [Change Set, права, STRICT, locked, rebase](../apps/server/internal/changes/component_test.go), свойство extract + revert на случайных документах |
| STU-411…412 | публикация в Go-тесте, [эквивалентность runtime](../packages/runtime/test/component-extract.test.ts), [витрина](../examples/react-store/test/load.test.ts) |
| STU-413…416, 418 | [UI мастера, повтор, отказ, история](../apps/studio/test/component-extract-ui.test.tsx), [`$props`/Repeat](../apps/studio/test/component-extract.test.ts) |
| STU-417 | [MCP-инструменты](../apps/server/internal/agents/component_test.go) |
| STU-419 | [живая приёмка](../scripts/studio-live-component-extract.mjs), §ниже |

## Локальные проверки

- **Go:**
  - целевые пакеты `composition/ops`, `changes`, `httpapi`, `agents` прошли с
    настоящим PostgreSQL (testcontainers);
  - `gofmt` и `go vet` чистые; `sqlc` перегенерирован (новый запрос `ComponentTitleTaken`);
    миграций нет.
- **TypeScript:**
  - Studio — 378 тестов, покрытие statements 92,69 %, branches 85,82 %,
    functions 92,27 %, lines 94,23 %;
  - runtime-тест эквивалентности, React — 73 теста, витрина — 119 тестов;
  - ESLint и TypeScript чистые.
- **Ограничения Windows:**
  - Go `-race` недоступен без CGO;
  - полный профиль `-coverpkg` занижается падающими здесь Windows-тестами
    `capabilitygithub`, которые не относятся к пакету.
  
  Поэтому race и порог Go coverage 95 % считаются пройденными только по CI.
- **Браузерные регрессии:** локально пройден 61 сценарий Playwright витрины и Studio.
- **CI:** [CI SHA `44ecf23`](https://github.com/QahNaaRlN/project-17/actions/runs/39316267362)
  пройден — Check (TS + Go) и Docker image. В том числе пройдены Go race и
  coverage: 95,0 % при пороге 95 %, запас минимальный. Studio functions — 92,21 %.
  Первый прогон CI до тестов веток показывал Go 94,4 %. Пороги не снижались.

## Живая приёмка STU-419 (шаг 3 MVP)

Стенд изолирован на loopback:

- [studio-review-stand.mjs](../scripts/studio-review-stand.mjs) с `CMS_REVIEW_BINDINGS=true` и новым
  `CMS_REVIEW_COMPONENTS=true` (право `component.write` у автора);
- PostgreSQL 16 в отдельном контейнере;
- MinIO и imgproxy (`CMS_IMGPROXY_URL` на свободном порту);
- подписанный локальный OIDC двух людей и React-витрина;
- проект с локалями RU/EN.

Через API подготовлены только применение схемы Article, PNG-ассет, сущность
с обложкой, блок шага 2 (`Grid` → `Stack`(`Heading`, `Text`, `Button`) + `Image`)
под корнем `Storefront` и подключение `featured` → `store.products`.
Всё остальное выполнено через Studio:

1. Мастер извлекает блок:
   - входы `title`, `description`, `image` (обязателен как зависимость от
     `$content`) и новый `products` из контракта источника;
   - слот `footer` с `Button`; `alt` остаётся общим скопированным текстом.
2. Первый ответ сохранения обрывается после коммита. «Повторить сохранение»
   отправляет тот же ключ и тело; второго компонента нет.
3. На сервере:
   - документ `kind=component` с четырьмя входами и `slotDefs.footer`;
   - на странице один `Composed` с `live`, привязками и `slots.footer`;
   - в истории одна запись «Сохранено как компонент «…»».
4. Preview на 375 и 1024 px:
   - перерисован экземпляром (есть `data-cms-instance`);
   - разметка блока совпала с исходной;
   - размеры совпали с точностью до 1 px;
   - картинка загружена (`naturalWidth > 0`).
5. Клик по заголовку внутри экземпляра выделяет экземпляр в дереве.
6. Undo возвращает страницу байт-в-байт, компонент выходит из CS. Redo
   восстанавливает тот же компонент; после reload состояние то же.
7. В Structure компонента создан Repeat по `$props.products` с карточкой
   `$item.title`. На экземпляре `products` привязан к `$data.featured.items`;
   preview показывает 3 товара.
8. EN-интерфейс и ширина 375 px без горизонтальной прокрутки.
9. Права и согласование:
   - Bob без `component.write` получает план с блокером `FORBIDDEN`;
   - компонент до публикации недоступен в Delivery (404);
   - Alice не может согласовать свою работу;
   - Bob согласует и публикует;
   - Delivery отдаёт `kind=component`.
10. Новый публичный браузер видит заголовок, описание, кнопку слота, картинку
    и 3 товара без preview-атрибутов.

Прогоны на свежем стенде (новая БД на каждый):

- `accept-a` на чистом дереве `c59a7ed`: PASS — CS `01a155c2-4cc7-7510-b758-128052ecb213`,
  страница `01a155c2-51f1-73bb-b339-0f576ee479f6`, компонент
  `01a155c2-5d2b-7747-89bf-740c5113bde5`, seq 20, manifest приложения
  `sha256:4d182a4989570e4c02a56405b1d20a9a2ca0936b8ab621984736168153c16cce`;
- `accept-b` на том же SHA: PASS — CS `01a155c2-c7e2-7675-b364-28effb1211e0`,
  страница `01a155c2-cca4-768e-b557-ac1001aa7d10`, компонент
  `01a155c2-d5ed-77de-86e8-649e7ad34e53`, seq 20, тот же manifest.

До исправления гонки перечитывания 2 из 3 прогонов падали на закрытии формы
данных; после исправления оба прогона на свежих стендах проходят.

Отчёт `component-extract-acceptance.json` и снимки `before-*/after-*`,
`component-mobile-en.png`, `component-published.png` хранятся в локальном
каталоге доказательств вне Git. Секреты остаются в private-файлах стенда.

Повторение:

```bash
CMS_REVIEW_DIR=<dir> CMS_REVIEW_BINARY=<cms> CMS_DATABASE_URL=<loopback db> CMS_REVIEW_BINDINGS=true CMS_REVIEW_COMPONENTS=true CMS_S3_*=… CMS_ASSET_*=… CMS_IMGPROXY_*=… node scripts/studio-review-stand.mjs
```

```bash
STUDIO_REVIEW_ADMIN_FILE=<dir>/studio-work-admin.private.json STUDIO_REVIEW_OUTPUT=<out> node scripts/studio-live-component-extract.mjs
```

Перед сценарием в проекте задаются `locales: ["ru","en"]`. Маршрут `/store`
публикуется сценарием, поэтому каждый прогон требует свежей БД.

## Отступления и ограничения

- `create_component` принимает аргументы извлечения в `payload`, как остальные
  изменяющие инструменты MCP, а не плоским списком из таблицы §7 спецификации.
- Запрет типа в зоне сервер выдаёт существующим кодом `POLICY_TYPE_FORBIDDEN`
  (в §4.3 спецификации — `POLICY_TYPE_DENIED`).
- Отказ в STRICT-месте проверен Go-тестом. Браузерного сценария для него нет:
  витрина не содержит STRICT-зоны с разрешёнными типами.
- Блокер плана `FORBIDDEN` у Bob проверен через BFF, а не через кнопку Studio.
- Presence и SSE доступны только для страниц, поэтому при открытом документе
  компонента они не работают (400), и правки компонента видны после
  возврата к странице.
- `sourceDirty` в отчёте фиксирует состояние дерева. Порог Go race/coverage
  подтверждается только CI.
- NFR (скорость preview), IR-044, `promote` в Studio, вставка компонентов из
  палитры (Q-1) и сертификация в мастере в пакет не входят.
