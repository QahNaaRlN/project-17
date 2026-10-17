# Серверная авторизация GitHub App

Расширение чтения фактов [STU-295…314](studio-capability-github.md), необходимое
для приватных Checks. Серверная реализация подготовлена; внешняя регистрация,
установка и приёмка App пока не выполнены. Полный STU-314 остаётся открытым.

## Граница доступа

Оператор выбирает ровно один способ авторизации записи каталога: существующий
`token` либо `installation`. Оба одновременно и отсутствие обоих отклоняются.
GitHub App устанавливается только в разрешённый репозиторий. Необходимы только
Metadata, Contents, Pull requests, Checks, Commit statuses — read. Contents
используется для родителей commit; получение исходников не добавлено.
Аккаунтные права, webhooks и пользовательский OAuth не требуются.

Доступ к GitHub-фактам по-прежнему только через существующие человеческие
настройку/link/refresh и права CMS. GitHub App не открывает PR, не запускает CI
и не меняет Missing Capability, IR, pack или публикацию.

## Конфигурация и секреты

Вместо `token` оператор задаёт:

```json
{
  "installation": {
    "appId": "<числовой App ID>",
    "installationId": "<числовой installation ID>",
    "privateKeyFile": "/absolute/private/github-app.pem"
  }
}
```

Остальные поля каталога, `credentialId` и requiredChecks сохраняются.
Это серверная конфигурация; App/installation identity, путь, PEM, JWT и access
токены не добавлены в DTO, context pack или идемпотентные ответы.
Ключ читается при старте: отдельный обычный файл, без прав группы/остальных,
RSA 2048…8192 бит, PKCS#1 или PKCS#8, до 64 KiB, без лишних PEM-блоков.
Ошибки возвращают безопасный общий код без пути и содержимого ключа.

App/installation identity входят в fingerprint конфигурации; путь и секретный
материал исключены. PAT fingerprint совместим с прежним форматом. Изменение
секретного ключа требует нового credentialId, перезапуска и явного подтверждения
настройки оператором. Один credentialId не допускается для разных авторизаций.

## Ограниченное получение токена

Для App разрешён единственный исходящий POST:
`https://api.github.com/app/installations/{installationId}/access_tokens`.
JWT подписан RS256, iat с запасом 60 секунд, exp через 9 минут. Тело явно
ограничивает `repository_ids` одним числовым ID и перечисляет пять прав чтения.
JWT используется только для выпуска токена, resource GET — только с access token.

После ответа 201 сервер проверяет срок и точный набор прав, затем фиксированным
`GET /installation/repositories?per_page=100&page=1` подтверждает один репозиторий
с ожидаемыми ID и owner/name. Лишний/другой репозиторий, лишние права, write,
неполный ответ или истекающий токен не допускаются. Формат/длина токена не
считаются legacy-40; допустима ограниченная строка до 16 KiB.

Ключ и токены хранятся только в памяти текущего процесса. Токен обновляется
по необходимости за 2 минуты до expiry; конкурентные обновления одного scope
объединяются, ожидание поддерживает отмену. Разные worker instances того же
каталога используют общий ограниченный cache записи. Между процессами
сохраняется существующий DB lease и credential budget. Перезапуск очищает cache.
Ошибка обновления не возвращает старый токен. 401/403/404 от resource API
инвалидируют совпадающий cache, не затрагивая более новый результат.

POST и проверка installation scope входят в прежние 40 запросов/10 MiB/60 секунд
на наблюдение. Transport остаётся фиксированным api.github.com, с проверкой
DNS/IP/peer, TLS, без proxy/redirect и ограниченными ответами. Rate limits и
Retry-After сохраняют прежнюю обработку. Общие lease/generation/rights/CAS gates
перед сетью и перед commit не меняются. Отказ доступа приводит к last-known,
без актуального success; обычный GET фактов CMS не выпускает токен и не читает GitHub.

## Проверка

Локальные тесты проверяют настоящий RSA/JWT, ограниченное тело POST, полный
Observe через HTTP boundary, cache/refresh/revocation, конкуренцию/отмену,
излишние права и репозитории, некорректные ответы и небезопасные PEM-файлы.
Это не внешняя приёмка GitHub App. После регистрации и отдельного разрешения
на установку нужны реальные private PR200/Checks200, переходы CI и независимый
CI merge SHA, сохранение инвариантов и pending publication block.
Ограничение запуска приватного GitHub Actions проверяется отдельно от авторизации.

Источники: [JWT GitHub App](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-json-web-token-jwt-for-a-github-app),
[installation token](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app),
[API выпуска токена](https://docs.github.com/en/rest/apps/apps#create-an-installation-access-token-for-an-app).
