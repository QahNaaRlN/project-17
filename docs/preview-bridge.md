# Preview bridge приложения

Пакет продолжает M3 после PR №37, реализуя сторону приложения SDK-030…042.
Studio и его оверлеи остаются отдельным M4; тестовый iframe-host проверяет протокол.

## Контракт

- Server helper проверяет preview JWT запросом к Delivery API до установки
  cookie `cms_preview`: HttpOnly, Secure, SameSite=None, Partitioned, Path=/,
  срок не больше exp токена и 900 секунд. Разбор cookie не заменяет проверку CMS
  при чтении страницы. Token не передаётся в модель браузера или postMessage.
- Origin приложения и Studio задаёт приложение. Допустим HTTPS либо HTTP
  loopback для локальной разработки; path редиректа всегда локальный. CSP
  разрешает frame-ancestors только Studio и только после успешного preview-чтения.
- DOM-атрибуты появляются только в preview. Native-компонент переносит
  previewAttributes на свой внешний элемент. Instance — существующий стабильный
  ключ runtime с областью Repeat/Composed, а не новый индекс.
- Bridge слушает только заданный origin и Window родителя, отправляет сообщения
  с точным targetOrigin. Протокол v1; init предшествует управляющим сообщениям.
  Design блокирует клики приложения и отправляет node.click; interact включает
  действия. Selection/hover/zones передаются callbacks, оверлеи рисует Studio.
- JSON Patch использует add/remove/replace/move/copy/test, до 128 операций.
  Применение атомарно на копии, с защитой JSON Pointer, лимитами JSON и L1 IR.
  Hash — sha256 канонического JSON переданного публичного IR snapshot.
  Несовпадение baseHash/hash или невалидный patch вызывает полный reload;
  прежний snapshot сохраняется, если reload/renderer завершается ошибкой.
  Изменение dataSources/localContent всегда перечитывает данные с сервера;
  приложение также направляет изменения своего контентного контекста через invalidate.
  L1 schema validator генерируется как standalone AJV-код при сборке; CSP
  приложения сохраняет запрет динамической компиляции через unsafe-eval.
- Приложение предоставляет reload и render callbacks; они проверяют manifest и
  исполняют источники на серверной границе. Bridge не исполняет код из сообщений.
  Init сверяет Change Set с уже проверенной preview-сессией. Invalidate seq
  монотонен, сообщения применяются последовательно; dispose отменяет запросы.
  `controller.refresh()` синхронизирует ручное/фоновое обновление приложения
  с той же очередью и snapshot. Render callback завершается после commit DOM.
- ready/rendered/diagnostics, node.click/node.hover и layout соответствуют §10.
  Layout группирует rect по nodeId и instance, viewport/scroll передаются отдельно,
  отправка не чаще одного раза в 60 мс. Token.refresh и navigate — callbacks хоста.
  Сообщения ограничены 256 KiB, очередь — 32; ошибки содержат только безопасные коды.

## Приёмка

SSR и гидратация совпадают; published DOM не имеет preview-атрибутов.
Чужой origin/source, версия, scope и некорректный payload не меняют состояние.
В design Enter/click выбирает нужный экземпляр; в interact действие выполняется.
Patch меняет preview без публикации; fallback перечитывает серверный черновик.
Истёкший/неподтверждённый token не устанавливает cookie, redirect не выходит за origin.
Dispose убирает listeners/timers и не допускает позднего commit.

Первый [Studio editor](studio-editor.md) теперь выпускает/обновляет preview token,
рисует рамки выбора и сохраняет draft через публичные команды.
Остаются полный Studio, production identity, серверная загрузка
code-index, общий route resolver, streaming/selective hydration и измерение NFR-005
под полным профилем MVP. Preview bridge не изменяет CMS и не публикует контент.

Источники протокола: [10-runtime-sdk-preview.md](spec/10-runtime-sdk-preview.md).
JSON Patch: [RFC 6902](https://www.rfc-editor.org/rfc/rfc6902).
Браузерные контракты: [postMessage](https://developer.mozilla.org/en-US/docs/Web/API/Window/postMessage),
[Set-Cookie](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Set-Cookie).
