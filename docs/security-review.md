# Сверка безопасности MVP

Дата: 2026-10-27. Пакет: [безопасность MVP](security-hardening.md), STU-500…519.
PR #90 (`feature/security-hardening` от `dev` `0f80028`), слит в `dev` `4b3bdce`.
Критерий готовности MVP 4 ([план §3](spec/14-mvp-plan.md#3-критерии-готовности-mvp))
выполнен: задача `Security` зелёная в CI и обязательна для PR в `dev` и `main`.

## 1. Сканеры

| Сканер | До пакета | После пакета |
|---|---|---|
| `govulncheck` (v1.9.0, тулчейн модуля) | 32 вызываемые: stdlib Go 1.26.0, `golang.org/x/net` v0.59.0, тестовая `moby/go-archive` v0.2.0 | 0 вызываемых; 1 невызываемая в модулях — в отчёте без отказа (решение §11.5) |
| `pnpm audit` | 0 critical, 5 high (`vite` 7.3.1, `zod` через Stryker), 14 moderate, 4 low | Уязвимостей нет |
| Trivy, образ сервера (Debian 12, бинарь `cms`) | Не сканировался | 0 high/critical |

Исправления: Go 1.26.10 (`go.mod`, образ сборки `golang:1.26.10-alpine`),
`golang.org/x/net` v0.61.0, `moby/go-archive` v0.3.3; `vite` 7.3.7, Vitest 4.1.12;
`pnpm.overrides`: `esbuild` ^0.28.1, `fast-uri` ^3.1.9, `qs` ^6.16.0, `zod` ^4.6.6.
Исключений нет: [`security/exceptions.json`](../security/exceptions.json) пуст.

## 2. Требования SEC

| ID | Состояние | Доказательство |
|---|---|---|
| SEC-001 OIDC, без паролей | Выполнено | `identity`: `TestOIDCSessionLifecycle`; паролей в схеме нет |
| SEC-002 секреты токенов только как SHA-256 | Выполнено | `auth.NewToken`/`HashToken`; `api_tokens.token_hash`, `human_sessions.token_hash` |
| SEC-003 CSP Studio | Выполнено | Studio: `SEC-003, STU-511` (директивы, без `unsafe-inline` в `script-src`, без инлайн-скриптов в `index.html`) |
| SEC-004 ассеты | Выполнено | Изображения — отдельный origin `CMS_ASSET_PUBLIC_URL`, `nosniff`, без `Set-Cookie` (`images_test`); неизображения (PDF) CMS не отдаёт — `attachment` не требуется до появления такой выдачи |
| SEC-005 журнал | Выполнено | `TestSecurityJournal`: `access.denied` (Query API и команды, люди, сервисы, агенты), `auth.login`, `auth.login_failed`, `auth.logout`, `token.created`, `role.*`, `policy.changed`; чтение `GET /api/v1/security-events` для `project.admin` |
| SEC-006 подпись вебхуков | Выполнено | `capabilitywebhook`: HMAC-SHA256 с меткой времени, отказ на устаревшую метку (`transport_test`) |
| SEC-007 сканеры в CI | Выполнено | Задача `Security` (§1) |
| SEC-008 анонимизация | Выполнено | `cms anonymize-user`; `TestAnonymizeUser`, CLI в `TestMigrateAndBootstrap` |

## 3. Модель угроз (12 §3.1)

| Угроза | Меры | Доказательство |
|---|---|---|
| Повышение прав агентом | Пересечение прав с делегирующим, политики, квоты, нет опасных инструментов | `agents`: `TestDelegationInvariants`, `TestConcurrentAgentCommandsCannotOverspendSessionQuota`, `TestHistoricalAgentPolicy`; сценарий §2, шаг 7 (`POLICY_DENIED`) |
| Обход согласования | PUB-002/003, привязка к хэшу (PUB-004) | `workflow`: `TestApprovalRules`, `TestRebaseKeepsApprovals`; сценарий §2, шаг 7 (`APPROVAL_SELF`) |
| XSS через контент | Нет HTML в IR и rich text, белый список схем URL | `packages/runtime` (`actions-data`, `plan`), `packages/react` (`rich-text`, экранирование `cms-render-diagnostics`) |
| Подмена preview, clickjacking | Проверка origin, `frame-ancestors`, короткие preview-токены | `packages/react` (`preview-*`), Studio CSP (`frame-ancestors 'none'`), витрина `frame-ancestors` |
| Утечка черновиков | Черновой режим только по preview-токену, `no-store` | `TestDeliveryDraft`, `TestSTU322PRPreviewTokenOnBaseEnvironment` |
| Межпроектный доступ | `project_id` во всех запросах | `TestProjectIsolationSweep` — обход всех GET-маршрутов `/api/v1` с ID чужого проекта и команд над ними |
| Вредоносные файлы | MIME по содержимому, лимиты, санитизация SVG | `assets`: `TestCNT042SVGAllowlist`, `TestSVGAndStorageMalformedInputs` |
| Компрометация CI-токена | Узкие права, срок жизни | Токены с областями и сроком (`token.created` в журнале); **остаточный риск:** команды ротации и отзыва сервисных токенов нет — после MVP |
| Утечка кода | Индекс кода без содержимого файлов | `codeindex`: `TestMetadataContract` |
| Секреты в журналах | Журналы без токенов и cookie | Сценарий §2: `stand logs carry no secrets` (CMS, Studio, витрины, OIDC, исполнитель проверок) |

## 4. Остаточные риски

- Ротация и отзыв сервисных токенов командой (сейчас — срок жизни и CLI выпуска).
- Row-Level Security PostgreSQL (Q-07) — после MVP.
- Внешний пентест и WAF — вне MVP.
