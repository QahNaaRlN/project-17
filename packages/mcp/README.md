# CMS MCP

Запуск: `node packages/mcp/dist/stdio.js`. Настройте переменные окружения
`CMS_MCP_URL=https://cms.example/mcp`, `CMS_PROJECT` и `CMS_AGENT_TOKEN`.
Используется только делегированная сессия. Клиент создаёт случайный секрет
`cms_agt_` + base64url от 32 случайных байт, сохраняет его в CMS_AGENT_TOKEN,
а start-agent-session получает SHA-256 полного секрета в tokenHash (base64),
agentId, changesetIds, rights, ttlSeconds и opsQuota. Ответ содержит только
идентификатор и срок сессии; в БД и повторяемых ответах секрет отсутствует.
Не передавайте секрет в аргументах командной строки. Локальный HTTP разрешён
только для loopback. Сервер определяет каталог и проверяет каждый инструмент.

Поддержаны stateless Streamable HTTP и stdio proxy, MCP 2025-06-18/2025-03-26.
Сервер не требует SSE GET или `Mcp-Session-Id`; сообщения POST возвращают JSON.
Повтор того же tools/call с тем же JSON-RPC id и аргументами идемпотентен внутри
agent session. Используйте новый request id для нового вызова.
