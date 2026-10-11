# @cms/runtime

Framework-neutral runtime содержит `Image` (CNT-052, SDK-001) и `preparePage`:
привязки/условия, Repeat, live Composed/Slot и первый набор атомарных стилей.
`preparePage(document, {scope, components, natives, tokens, breakpoints})` возвращает
сериализуемый план `{roots, css, diagnostics}`. React-адаптер находится в `@cms/react`.
Полные границы и оставшиеся зависимости: [IR/React renderer](../../docs/react-ir-renderer.md).
`preparePageWithData` загружает источники registry до подготовки плана;
`loadSource`, `appendPage` поддерживают проверку результатов и пагинацию.
`executeActions` проверяет аргументы и последовательно исполняет события.
[Контракт действий и данных](../../docs/runtime-actions-data.md) описывает API,
ошибки, таймаут, лимиты и границы кэширования. `applyDocumentPatch` и `documentHash`
обеспечивают атомарный JSON Patch/L1 и hash для [preview bridge](../../docs/preview-bridge.md).
Pinned Composed
и остальные дизайн-свойства ещё не реализованы.

Получите image model через аутентифицированный
`GET /delivery/v1/{project}/{env}/asset/{id}/image?fmt=webp&fit=cover&w=640&h=480`.
Передайте её адаптеру:

```ts
import { Image } from "@cms/runtime";

const element = Image(model, {
  alt: "Фотография товара",
  width: {
    base: "100vw",
    breakpoints: [
      { minWidth: 768, width: 480 },
      { minWidth: 1024, width: 640 },
    ],
  },
});
// element.tag === "img"; adapter spreads element.props onto its img element.
```

Выход сериализуем: `src`, `srcSet`, `sizes`, размеры/соотношение сторон,
`alt`, loading/decoding, `objectFit` и `objectPosition`. Варианты уже подписаны
сервером, runtime не меняет URL и не хранит ключи. Передавайте `alt` выбранной
локали; locale fallback не реализуется этим примитивом.

Модель и URL действуют не больше 15 минут. Повторно запрашивайте модель перед
новым SSR/render после истечения `expiresAt`; для долгоживущей страницы адаптер
обновляет её до смены `src` или viewport, если срок уже истёк. `Image` отклоняет
истёкшую модель, неверные варианты/ширины/точку фокуса. Он не проверяет HMAC —
это делает gateway перед выдачей байтов.

Проверки: Vitest и property tests, пороги пакета 90/80/90/90;
Stryker — минимум 70%. Исторический разбор Image: четыре
проверки типа эквивалентны другим проверкам внутри `Width = number | vw`;
два мутанта меняют только текст ошибки; удаление отдельной проверки пустых
variants всё равно отклоняет вход до возврата модели. Контракт не фиксирует
текст/класс исключения, поэтому эти семь вариантов эквивалентны. Границы,
порядок breakpoints, единицы vw и часы проверяются отдельными тестами.
