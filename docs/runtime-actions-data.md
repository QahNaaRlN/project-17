# Действия и источники данных

Продолжает M3 после PR №36. Реализованы контракты registry, CLI/sourceRef,
загрузка данных до SSR, проверка событий и исполнение действий в React.

## Контракт и границы

- MF-010…012: `defineAction` и `defineDataSource` используют ту же систему типов,
  registry и CLI, что компоненты. Manifest содержит только JSON-контракты;
  обработчики и загрузчики остаются кодом приложения. CLI определяет sourceRef
  и проверяет идентичность экспортированной реализации.
- IR-050…052: события разрешены только объявленным типом узла. До исполнения
  аргументы проверяются; цепочки до восьми действий последовательны и прерываются
  при ошибке. Приложение получает безопасный код, имя действия и ID узла.
- SDK-014: независимые загрузчики выполняются параллельно, по умолчанию с
  таймаутом 3000 мс и AbortSignal. Ошибка превращается в `{error: {code}}`.
  Параметры берутся только из разрешённых областей, результаты — проверенный JSON.
- Источник с `paginated: true` объявляет `result` типа list. Загрузчик возвращает
  `{items, hasMore, cursor?}`; при `hasMore: true` обязателен непустой opaque cursor.
  Следующий запрос получает cursor отдельно от параметров через контекст загрузчика.
- Начальные результаты используются при SSR и гидратации. Клиентское `data.loadMore`
  вызывает адаптер приложения; серверные загрузчики и секреты не передаются браузеру.
  Приложение отвечает за авторизацию своего адаптера. SDK проверяет следующую порцию.
- Кэш загрузчиков в этом пакете ограничен жизнью одной загрузки страницы. Метаданные
  `cache.ttlSeconds` сохраняются в manifest; общий межпользовательский кэш не вводится.
- SDK-033: явно переданный preview использует режим design по умолчанию; обработчики
  не исполняются. Режим interact задаётся приложением. Мост Studio остаётся отдельным.

## Подключение

```ts
export function select(args: {id: string}, context: ActionContext<MyApp>) {
  return context.app.select(args.id);
}
export function products(params: {limit?: number | null}, context: LoaderContext<MyServer>) {
  return context.app.products(params.limit ?? 20, context.signal);
}
const registry = createRegistry({
  components: [],
  actions: [defineAction({name: "shop.select", args: {id: t.string().required()}, handler: select})],
  dataSources: [defineDataSource({name: "shop.products", params: {limit: t.number().default(20)}, result: t.list(t.string()), loader: products})],
});
// Сервер. CMS credentials и серверные сервисы остаются здесь.
const plan = await loadPage(cms, "/catalog", {registry, sourceOptions: {app: server}});
// SSR и браузер получают только plan. Начальные загрузчики повторно не вызываются.
// <CmsPage plan={plan} actions={registry.actions} app={browserServices} />
```

Импортируйте factory/t/context types из `@cms/component-registry`, `loadPage` из
`@cms/react/server`. Собирайте manifest тем же CLI. Список обработчиков, переданный
браузеру, должен импортировать только клиентские сервисы; серверные credentials
не помещаются в registry. Native-компонент получает `events.<имя>()` в props.
Объявленный event payload пока не передаётся: аргументы действия вычисляются
из IR и текущей области данных при подготовке плана. Для DOM, аналитики и пагинации
приложение может переопределить `services` в `CmsPage`.

Для следующей страницы приложение вызывает свой adapter из `services.loadMore`:
получает cursor от предыдущего результата, использует `loadSource(..., {cursor})`
на сервере, проверяет/объединяет ответ через `appendPage` и обновляет план. Автоматическое
хранилище состояния и HTTP endpoint пагинации в SDK не вводятся. Предел накопления —
2000 элементов; общий предел источников — 64 на страницу с экземплярами Composed.
Неизвестный источник и ошибка загрузчика сохраняют независимые разделы/источники.

Встроенные `navigate`, `openModal`, `closeModal`, `scrollTo`, `track`, `data.loadMore`
проверяют контракт аргументов. `navigate` к странице требует route resolver
приложения в `services.navigate`. Modal требует native-реализацию с DOM `dialog`;
общий примитив Modal остаётся вне текущего renderer. DOM-цели проверяются в текущем
документе и получают prefix экземпляра Repeat/Composed.

## Приёмка

Типы обработчика согласованы со схемой. CLI индексирует компоненты, действия и
источники. Загрузчики работают до SSR; ошибка одного источника не раскрывает
исключение и не ломает остальные. `$item`/`$props` сохраняют правильную область
в событиях Repeat/Composed. Неизвестное действие исключает узел с диагностикой.
Витрина демонстрирует загрузку каталога и действие из CMS, сохраняя клавиатурную
навигацию, существующий дизайн и отсутствие оформления заказа.

Product/Collection как сущности CMS, Studio, preview bridge, streaming/selective
hydration и загрузка code-index на сервер остаются следующими пакетами.
