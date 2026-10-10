# CMS

CMS нового поколения: контент и визуальная композиция управляются редактором и дизайнером без программирования, а программный агент получает формальный и проверяемый интерфейс изменений.

- Спецификация: [`docs/spec/`](docs/spec/00-overview.md)
- Правила работы с репозиторием: [`CONTRIBUTING.md`](CONTRIBUTING.md)

## Структура

| Каталог | Содержимое |
|---|---|
| `packages/ir` | Формат IR: JSON Schema, типы, валидатор, фикстуры |
| `packages/manifest` | Формат manifest: JSON Schema, типы, валидатор, канонический хэш, фикстуры |
| `apps/server` | CMS Core на Go: HTTP API, Command Bus, проекты и окружения, IR ([README](apps/server/README.md)) |
| `deploy/compose` | Локальное окружение Docker Compose |
| `docs/spec` | Техническая спецификация |

## Разработка

Требуются Node.js 22.12+, pnpm 10 (`corepack enable`) и Go 1.26+.

```bash
pnpm install
pnpm check   # формат, линтер, типы, тесты, сборка
```

Тестам сервера нужен PostgreSQL: Docker (тесты поднимут контейнер сами) или `CMS_TEST_DATABASE_URL`.
