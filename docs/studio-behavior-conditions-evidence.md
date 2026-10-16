# Studio: проверка действий и условий показа

2026-10-16. Реализация STU-237…254 в [PR #59](https://github.com/QahNaaRlN/project-17/pull/59), ветка `feature/studio-behavior`, от
`origin/dev c4b8286`. В dev пакет ещё не слит; полный M4/IR-044/M5 не закрыт.
Контракт и ограничения: [спецификация](studio-behavior-conditions.md).

## Реализовано

Поведение сохраняется по событию через node.setBehavior, when — отдельным
node.setCondition. Формы не исполняют обработчики. Цепочки ограничены 8 шагами,
конструктор предикатов — глубиной 8, 16 дочерними условиями и 64 узлами.
Неподдержанный IR сохраняется; замена всего выражения требует явного выбора.
Открытая форма защищает ввод между панелями, режимами, локалями и страницами.

Go/Studio используют раздельные advisory-права; Command Bus повторяет
авторизацию, primary content gate и locked-проверки. Прямые args строго
совместимы без преобразований. Зависимости on/when учитываются при смене
контента/источника; несовместимость предлагает явное снятие выражения.

Preview блокирует действия до актуального плана, включая ошибку/отмену загрузки,
и показывает состояние обновления. Цепочка останавливается при первом отказе.
Цели Repeat сохраняют prefix конкретного экземпляра; внешняя цель не получает
item prefix. Ошибка источника в when скрывает зависимый узел и сохраняет соседние
секции. sourceFailures(data) переносит идентичность ошибок отдельно от данных:
обычное поле error в успешном result не становится ошибкой loader. При ручном
loadSources результат надо передать в scope.sourceErrors; preparePageWithData
делает это автоматически. Preview JSON витрины сохраняет карту безопасных кодов.

## Проверяемые требования

| Требования  | Доказательства                                                                                                                                                                                                              |
| ----------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| STU-237…240 | [формы и права](../apps/studio/test/behavior-ui.test.tsx), [совместимость](../apps/studio/test/behavior.test.ts), [Go strict args](../apps/server/internal/composition/bindingdoc/behavior_test.go)                         |
| STU-241…244 | [зависимости и операции](../apps/studio/test/behavior.test.ts), [Go rights/history](../apps/server/internal/changes/behavior_test.go), [общий Go/TS fixture](../packages/ir/fixtures/studio-behavior.json)                  |
| STU-245…248 | [конструктор/пределы](../apps/studio/test/behavior.test.ts), [формы](../apps/studio/test/behavior-ui.test.tsx), [runtime scopes/targets](../packages/runtime/test/behavior.test.ts)                                         |
| STU-249…250 | [runtime failures](../packages/runtime/test/behavior.test.ts), [preview loading/failure/dispose](../packages/react/test/preview-core.test.ts), [primary projection](../apps/server/internal/delivery/behavior_test.go)      |
| STU-251…253 | [advisory rights](../apps/server/internal/workflow/studio_test.go), [locked](../apps/server/internal/composition/policydoc/behavior_test.go), [legacy/dirty/conflict/reset/RU/EN](../apps/studio/test/behavior-ui.test.tsx) |
| STU-254     | [настоящая браузерная приёмка](../scripts/studio-live-behavior.mjs), отдельный loopback CMS/PostgreSQL/OIDC/React                                                                                                           |

## Валидация

Настоящая локальная приёмка подтвердила формы on/when, два экземпляра Repeat,
design click/Enter без эффектов, порядок builtin обработчиков, остановку на ошибке,
favorite с item args, реальный anchor, RU/EN, точный повтор после потерянного ответа,
сохранение ввода при конфликте, undo/redo, 375 px и публикацию Alice → Bob.
Новый публичный браузер подтвердил опубликованное поведение и отсутствие draft
до публикации. Задержанный браузерный запрос подтвердил запрет старых обработчиков
во время смены локали. Тест задерживает исходный запрос, сохраняя область
partitioned preview cookie, а не подменяет его запросом из API-контекста.
Отчёт behavior-acceptance.json сохраняет SHA/dirty, CS/seq, manifest hash,
pageVersionId и версии primary content без credentials/приватных args.
CI проверяется для последнего head [PR #59](https://github.com/QahNaaRlN/project-17/pull/59/checks).

Локальные TypeScript-наборы проходят пороги покрытия: Studio 314 тестов,
React 70; существующая браузерная регрессия — 60 сценариев.
Format/lint/typecheck и сборки прошли. go tool sqlc generate,
Go build и интеграционные наборы затронутых модулей проверяются на отдельном
PostgreSQL. Windows не содержит инструмента covdata для полного покрытия и
C-компилятора для race: полный pnpm check остановился на этой проверке;
окончательное подтверждение race/coverage даёт Linux CI.
При полном локальном Go-прогоне один MinIO-контейнер исчез во время запуска;
узкий повтор internal/assets прошёл.

## Осталось за границами пакета

Advanced bindings, вложенные Repeat, event payload/Form/Input, Modal/track/loadMore
редакторы, page resolver, component.extract, Missing Capability, Figma и production.
Дата/datetime в новых ordered comparisons не поддерживаются. Следующий M5-пакет
Missing Capability/context pack требует отдельной спеки по актуальному dev.
