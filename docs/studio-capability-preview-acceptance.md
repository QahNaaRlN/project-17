# preview/<ref>: реализация и проверка

Дата: 2026-10-19. Ветка: `feature/studio-capability-preview` от `dev` `9437fb5`.
[Спецификация STU-315…334](studio-capability-preview.md) одобрена пользователем,
решения §11 и уточнение STU-323 приняты 2026-10-19. Пакет не слит; полный M5 не завершён.

## Реализовано

- Миграция 00023: источник preview-окружения (base standard, строка GitHub-конфигурации,
  repository ID, PR), журнал `preview_registrations`, `capability_preview_args`,
  аудит; `ON UPDATE CASCADE` для ссылок по имени окружения. Up/down/up проверены
  на PostgreSQL 16.
- `create-environment` с `source` (имя строго `preview/pr-<N>`, repository только
  из конфигурации, идемпотентный повтор CI), `register-manifest` с `headSha` = commit
  code index и `appUrl` из `CMS_PREVIEW_APP_ORIGINS` (только точные origin).
- `delete-environment`, River-задача `capability_preview_expiry` (14 дней без
  регистраций) и архив по проверенному наблюдению closed/merged. Архив освобождает
  имя (переименование в `archived/<id>`), снимает manifest и меняет `preview_key`.
- Вычисляемое сопоставление запрос ↔ preview (STU-318/319) и состояния `ready`,
  `ready_last_known`, `commit_behind`, `commit_unverified`, `key_missing`,
  `contract_incompatible`, `not_registered`, `no_github_link`, `environment_expired`.
- `set-capability-preview-args` с проверкой L1–L4 по preview-manifest; Delivery preview
  подставляет действие вместо `pending:<id>` только в ответе этого preview.
  Сохранённый черновик, standard-preview, проверки и публикация видят pending.
- Preview-токен привязан к номеру сборки; новая регистрация и архив делают его
  недействительным. Токен требует design.read и CS базового окружения.
- Query: `GET /capability-requests/{id}/preview`, `GET /changesets/{id}/preview`,
  расширенный `GET /environments`.
- Studio: раздел «Предпросмотр» в карточке запроса (состояние с возрастом наблюдения,
  коммит, архив, форма аргументов из пакета действий, exact retry), режим просмотра
  PR с баннером, origin сборки и запретом правки; BFF и CSP с `previewAppOrigins`.

## Отступления и уточнения при реализации

| Тема | Решение | Основание |
|---|---|---|
| STU-323 | Содержимое preview — рабочие версии CS поверх head, как в текущем draft preview | Решение пользователя 2026-10-19 (вариант A), формулировка спецификации исправлена |
| STU-324 | Используется существующий `ENVIRONMENT_NOT_PUBLISHABLE` | Код уже описан в 08-api.md; новый код не вводится |
| Путь Delivery | Сборка PR настроена на базовое окружение (`staging`) и читает черновики preview своим токеном | `preview/pr-<N>` содержит `/` и не помещается в сегмент `/delivery/v1/{project}/{env}`; SDK также запрещает `/`. Токен действует только в черновом режиме и только на пути своего base |
| Архив по закрытому PR | В транзакции сохранения наблюдения, а не отдельной задачей | Атомарность с наблюдением под той же блокировкой окружений |
| Realtime | Используется существующее событие invalidate по ревизии проекта | Новое событие с именем окружения не требуется; команды и задачи архива повышают ревизию |
| Переключение окружения | Открытые формы карточки запроса не блокируют переключение; правки страницы блокируют | Карточка остаётся смонтированной; иначе открыть preview из карточки было невозможно |
| 375 px | Полосы preview/ошибки переносят содержимое | Существующая полоса `CMS_ACTION_PENDING_CAPABILITY` выходила за ширину на 20 px |

## Локальная приёмка

Скрипт [studio-live-capability-preview.mjs](../scripts/studio-live-capability-preview.mjs)
на изолированном стенде [studio-review-stand.mjs](../scripts/studio-review-stand.mjs)
(`CMS_REVIEW_CAPABILITIES=true`, `CMS_REVIEW_CAPABILITY_PREVIEW=true`): настоящий
CMS/PostgreSQL 16/River, подписанный локальный OIDC с Alice (автор) и Bob
(разработчик), Studio/BFF, две сборки React-витрины. Результат: **PASS** на
`692dcaf` с изменениями этой ветки.

GitHub-факты PR #1 записаны фикстурой в изолированную БД; запросов к GitHub нет.
Каталог содержит неиспользуемый ключ. Клиент GitHub и происхождение фактов
приняты отдельно в STU-314; внешняя проверка этого пакета с реальным GitHub
не выполнялась.

| Шаг | Результат |
|---|---|
| Оператор включает GitHub-конфигурацию; Alice создаёт и отправляет запрос `store.favorite`; Bob ведёт его до `pr_open` | Выполнено формами/командами людей |
| CI: `create-environment preview/pr-1` (повтор — то же окружение), неверный `headSha` → `PREVIEW_COMMIT_MISMATCH`, регистрация сборки с действием | seq 1, repository 42 из конфигурации |
| Наблюдение head = commit сборки | `ready`; Bob видит состояние, но не может открыть страницу |
| Аргументы формой, потеря ответа и повтор | Два одинаковых запроса с тем же ключом, аргументы сохранены |
| «Открыть в предпросмотре» | Баннер «только просмотр», iframe на origin сборки PR, клик выполняет действие PR с аргументами |
| Возврат в рабочее окружение | Iframe staging, pending не выполняется; черновик хранит `pending:<id>` |
| Публикация в preview | `ENVIRONMENT_NOT_PUBLISHABLE` |
| Новый head в GitHub, затем новая сборка | `commit_behind` → `ready`, аргументы `stale`, старый токен 401 |
| 375 px | Раздел «Предпросмотр» и страница без горизонтальной прокрутки |
| `delete-environment` | Архив `deleted`, состояние `environment_expired`, регистрация → `ENVIRONMENT_ARCHIVED` |
| Итог | Статус/ревизия запроса и manifest staging не изменились |

TTL и архив по слитому PR проверены Go-тестами с управляемым временем и
наблюдением (ожидание 14 дней в приёмке не моделируется).

## Автоматические проверки

- Go: новые тесты с ID требований (`capabilities`, `delivery`, `httpapi`,
  `previeworigins`, `config`); пакеты затронутых модулей проходят.
- Studio: 368 тестов, 43 файла, с порогами покрытия; ESLint и typecheck всех пакетов.
- На этой машине (Windows) нет `-race` (нужен cgo); под нагрузкой параллельных
  прогонов тесты доставки `capabilities` зависают по таймауту, отдельно проходят на
  dev и на ветке. `TestInstallation*` в `capabilitygithub` падают и на dev из-за
  проверки прав файла ключа в Windows. Обязательный гейт — CI на Linux.

## Не проверено

- Реальный GitHub и реальный хостинг PR-развёртываний.
- Gremlins/Stryker не запускались (еженедельный workflow).
