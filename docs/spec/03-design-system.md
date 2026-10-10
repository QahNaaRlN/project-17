# 03. Design Runtime и дизайн-система

Design Runtime — ограниченная декларативная среда исполнения интерфейса. Она позволяет собирать новые визуальные блоки без кода, но не является неограниченным CSS-конструктором.

## 1. Токены

Токены объявляются приложением в manifest ([04-manifest.md](04-manifest.md)) вместе со значениями. Значения нужны runtime (CSS custom properties) и валидаторам (контраст, диапазоны).

| Категория | Ключ manifest | Пример имён | Значение |
|---|---|---|---|
| Отступы | `spacing` | `none, xs, sm, md, lg, xl, 2xl` | длина (`"16px"`) |
| Цвета | `colors` | `surface, surfaceAccent, text, textMuted, primary, primaryHover, onPrimary, border, focus` | цвет |
| Радиусы | `radius` | `none, small, medium, large, full` | длина |
| Типографика | `typography` | `body, bodySmall, label, heading, display` | составное: `fontFamily`, `fontSize` (адаптивный), `lineHeight`, `fontWeight`, `letterSpacing`, `large: bool` |
| Тени | `shadow` | `none, sm, md, lg` | box-shadow |
| Границы | `borderWidth` | `none, thin, thick` | длина |
| Контейнеры | `container` | `sm, md, lg, xl, prose` | длина |
| Слои | `layer` | `base, raised, sticky, overlay, modal` | целое |
| Переходы | `transition` | `none, fast, normal` | длительность + easing |
| Breakpoint'ы | `breakpoints` | `sm, md, lg, xl` | minWidth |

| ID | Требование |
|---|---|
| DS-001 | Имена токенов ДОЛЖНЫ соответствовать `^[a-z][A-Za-z0-9]*$` и быть уникальными в категории. |
| DS-002 | Runtime ДОЛЖЕН публиковать токены как CSS custom properties `--cms-<категория>-<имя>`. |
| DS-003 | Цветовые токены МОГУТ иметь значения для тем (`modes: { light, dark }`). Поддержка тем в Studio — после MVP; формат manifest поддерживает её с MVP. |

## 2. Примитивы

### 2.1. Каталог

| Примитив | Группа | Контейнер | Свойства (`props`) | Контентные свойства | События |
|---|---|---|---|---|---|
| `Box` | Layout | да | `as: div\|section\|article\|aside\|header\|footer\|nav\|main` | `backgroundImage: asset` | — |
| `Stack` | Layout | да | `direction: vertical\|horizontal` (responsive) | — | — |
| `Flex` | Layout | да | `wrap: bool` (responsive) | — | — |
| `Grid` | Layout | да | — | — | — |
| `Container` | Layout | да | `size: container-токен` | — | — |
| `Divider` | Layout | нет | `orientation: horizontal\|vertical` | — | — |
| `Modal` | Layout | да | `size: sm\|md\|lg\|full`, `dismissible: bool` | `title: text` | `close` |
| `Heading` | Typography | нет | `level: 1..6` | `text: text` | — |
| `Text` | Typography | нет | `as: p\|span` | `text: text` | — |
| `RichText` | Typography | нет | — | `value: richText` | — |
| `Label` | Typography | нет | — | `text: text` | — |
| `Image` | Media | нет | `fit: cover\|contain`, `loading: lazy\|eager`, `decorative: bool` | `src: asset(image)`, `alt: text` | — |
| `Video` | Media | нет | `autoplay`, `muted`, `loop`, `controls` | `src: asset(video)`, `poster: asset(image)`, `caption: text` | — |
| `Icon` | Media | нет | `name: enum из manifest.icons`, `size: sm\|md\|lg` | `label: text` | — |
| `Button` | Actions | нет | `variant: default\|primary\|secondary\|ghost`, `size: sm\|md\|lg`, `disabled: bool` | `label: text` | `click` |
| `Link` | Actions | да | `to: Link`, `newTab: bool` | `label: text` | `click` |
| `Repeat` | Structural | да (шаблон) | `as: string`, `limit: 1..200`, `key: path` | `items` (только привязка, тип `list`) | — |
| `Slot` | Structural | нет | `name: string` | — | — |
| `Composed` | Structural | слоты | входы компонента | входы с `content: true` | — |

Источник встроенных контрактов — `packages/manifest/schema/builtin-catalogue.json`; из него генерируются каталоги Go и TypeScript.

Каталог встроенных свойств фиксирован для MVP: `Button.variant` — `default`, `primary`, `secondary`, `ghost`; `Button.size` — `sm`, `md`, `lg`; `Divider.orientation` — `horizontal`, `vertical`. Manifest не переопределяет встроенные примитивы. Произвольные варианты оформляются своим native-компонентом (например, `AppButton`). Отсутствие необязательного свойства не добавляет значение по умолчанию в IR.

`Repeat` дополнительно поддерживает слот `empty` — узлы, рендерящиеся при пустом списке.

Приложение МОЖЕТ расширять реестр примитивов через manifest (`primitives`). Расширенный примитив описывается так же, как native-компонент, но допускает дизайн-свойства своей группы.

### 2.2. Требования

| ID | Требование |
|---|---|
| DS-010 | Каждый примитив ДОЛЖЕН иметь реализацию в `@cms/runtime` (разметка + стили) и адаптере фреймворка. |
| DS-011 | Примитивы ДОЛЖНЫ генерировать семантическую разметку согласно `as` / `level`. |
| DS-012 | Приложение МОЖЕТ заменить реализацию примитива своей (например, `Image` → оптимизированный компонент фреймворка), сохраняя контракт свойств. |

## 3. Дизайн-свойства

### 3.1. Каталог и режимы

Столбцы SYSTEM и FREE указывают, какие значения допустимы. `—` — свойство в режиме недоступно. Ни одно дизайн-свойство не доступно в STRICT, кроме помеченных **P** (свойства размещения).

| Свойство | Применимо к | SYSTEM | FREE |
|---|---|---|---|
| `padding`, `paddingX`, `paddingY`, `paddingTop/Right/Bottom/Left` | все контейнеры, `Button` | `spacing` | + raw 0…512px, rem |
| `gap`, `rowGap`, `columnGap` | `Stack`, `Flex`, `Grid` | `spacing` | + raw 0…512px |
| `marginTop`, `marginBottom` **P** | все | `spacing` | + raw −256…512px |
| `marginLeft`, `marginRight`, `marginX` | все | — | `spacing`, raw −256…512px, `auto` |
| `width` **P** | все | `auto, full, fit, 1/2, 1/3, 2/3, 1/4, 3/4` | + raw 0…4000px, 0…100%, rem, vw |
| `minWidth`, `minHeight` | все | `screen` (только `minHeight`) | raw 0…4000px, %, vh |
| `maxWidth` **P** | все | `container` | + raw |
| `height` | все | `auto, full` | + raw 0…4000px, %, vh |
| `aspectRatio` | `Box`, `Image`, `Video` | `1:1, 4:3, 3:2, 16:9, 21:9, 3:4, 9:16` | + raw `<w>:<h>` (1…32) |
| `align`, `justify` | `Stack`, `Flex`, `Grid` | `start, center, end, stretch, between` | то же |
| `alignSelf`, `justifySelf` **P** | дети `Stack/Flex/Grid` | `start, center, end, stretch` | то же |
| `columns`, `rows` | `Grid` | целое 1…12 или треки `Nfr` (N 1…12), `auto` | + px, %, `minmax(a,b)`, `repeat(auto-fill, minmax(a,b))` |
| `colSpan`, `rowSpan` **P** | дети `Grid` | 1…12, `full` | то же |
| `colStart`, `rowStart` | дети `Grid` | — | 1…13 |
| `order` **P** | дети `Stack/Flex/Grid` | −10…10 | то же |
| `background` | контейнеры, `Button` | `colors` | + raw цвет* |
| `backgroundSize`, `backgroundPosition` | `Box` | `cover, contain` / `center, top, bottom` | то же |
| `color` | все | `colors` | + raw цвет* |
| `typography` | Typography, `Button`, `Link` | `typography` | то же |
| `textAlign` | Typography | `start, center, end` | то же |
| `lineClamp` | `Text`, `Heading` | 1…10 | то же |
| `fontWeight` | Typography | — | веса из `typography`-токенов |
| `radius` | все | `radius` | + raw 0…512px |
| `borderWidth`, `borderColor` | все | `borderWidth`, `colors` | + raw цвет* |
| `borderSides` | все | `all, top, bottom, x, y` | то же |
| `shadow` | все | `shadow` | то же |
| `opacity` | все | — | 0…1, шаг 0,05 |
| `overflow` | контейнеры | `visible, hidden` | + `auto` |
| `position` | все | `static, relative` | + `sticky, absolute` |
| `top`, `right`, `bottom`, `left` | при `position` ≠ static | — | `spacing`, raw −2000…4000px, % |
| `zIndex` | все | — | только `layer` (raw запрещён во всех режимах) |
| `hidden` **P** | все | `bool` | то же |
| `transition` | все | `transition` | то же |
| `outline` | интерактивные | `colors` (только в `states.focusVisible`) | то же |

\* Сырой цвет (`#rrggbb`, `#rrggbbaa`) допустим только при `allowRawColors: true` в политике проекта или документа.

### 3.2. Грамматика сырых значений

```
length  = number ("px" | "rem" | "%" | "vw" | "vh")
number  = ["-"] 1*DIGIT ["." 1*4DIGIT]
fr      = 1*2DIGIT "fr"
color   = "#" 6HEXDIG [2HEXDIG]
ratio   = 1*2DIGIT ":" 1*2DIGIT
track   = length | fr | "auto" | "minmax(" length "," (length | fr) ")"
```

1 rem при проверке диапазонов приравнивается к 16px. Диапазоны в таблице §3.1 — значения по умолчанию; политика проекта МОЖЕТ их сузить.

### 3.3. Свойства состояний

Внутри `design.states.<state>` допустимы только `background`, `color`, `borderColor`, `shadow`, `opacity`, `outline`. Состояния не бывают адаптивными.

### 3.4. Всегда запрещено

Произвольные CSS-строки и имена свойств вне каталога; селекторы и псевдоэлементы; `!important`; `position: fixed` (кроме внутренней реализации `Modal`); произвольные `z-index`; `transform`, `filter`, `animation` (в MVP); внешние шрифты и URL в дизайн-свойствах.

## 4. Режимы свободы

| Режим | Допустимые типы узлов | Дизайн-свойства | Особые права |
|---|---|---|---|
| STRICT | Native и Generated компоненты из manifest; Composed с флагом `certified` | Только свойства размещения (**P**) с токенами | — |
| SYSTEM | Всё из STRICT + примитивы + любые Composed | Каталог §3.1, столбец SYSTEM | — |
| FREE | Всё из SYSTEM | Каталог §3.1, столбец FREE | — |
| CODE | Всё из FREE | Как FREE | Создание Missing Capability из зоны; заглушки `pending:` в черновике |

| ID | Требование |
|---|---|
| DS-020 | Валидатор ДОЛЖЕН вычислять эффективный режим каждого узла ([02-ir.md §9](02-ir.md#91-уровни-и-режимы)) и проверять тип узла и каждое дизайн-свойство по таблице. |
| DS-021 | Флаг `certified` у Composed-компонента устанавливает актор с правом `design.components.certify`. Сертифицированный компонент имеет `requiredMode = STRICT`. Любое изменение компонента снимает сертификацию до повторного подтверждения. |
| DS-022 | Studio ДОЛЖНА показывать в палитре и панели свойств только то, что допустимо в эффективном режиме выбранного узла. |
| DS-023 | Понижение режима зоны (например, FREE → SYSTEM) ДОЛЖНО сопровождаться отчётом о нарушениях в существующих узлах; применение возможно только после их исправления в том же Change Set. |

## 5. Генерация стилей

| ID | Требование |
|---|---|
| DS-030 | Runtime ДОЛЖЕН преобразовывать дизайн-свойства в атомарные CSS-классы с детерминированными именами (`c-` + хэш свойства и значения) и медиазапросами для breakpoint'ов. Инлайн-стили не используются, за исключением значений, известных только во время выполнения. |
| DS-031 | При SSR runtime ДОЛЖЕН выводить только правила, использованные на странице. |
| DS-032 | Значения токенов ДОЛЖНЫ подставляться через `var(--cms-…)`, чтобы смена значения токена в manifest не требовала повторной публикации IR. |

## 6. Статические проверки доступности

| Код | Правило | Уровень |
|---|---|---|
| `A11Y_IMAGE_ALT_MISSING` | `Image` без привязки `alt` и без `decorative: true` | error |
| `A11Y_HEADING_MULTIPLE_H1` | Более одного `Heading level=1` на странице | error |
| `A11Y_HEADING_ORDER` | Пропуск уровня заголовка по порядку обхода | warning |
| `A11Y_BUTTON_LABEL` | `Button` без `label` | error |
| `A11Y_ICON_ONLY_LABEL` | Интерактивный узел, содержащий только `Icon` без `label` | error |
| `A11Y_LINK_TARGET` | `Link` без `to` и без обработчика `click` | error |
| `A11Y_NESTED_INTERACTIVE` | `Button`/`Link` внутри `Link` или `Button` | error |
| `A11Y_CONTRAST` | Контраст `color` и эффективного `background` предка ниже 4,5:1 (3:1 для токенов с `large: true`) | error для токенов; warning для raw |
| `A11Y_VIDEO_AUTOPLAY` | `autoplay` без `muted` | error |
| `A11Y_MODAL_TITLE` | `Modal` без `title` | error |

Динамические проверки (axe-core на собранном preview) выполняются в pipeline публикации для изменений с риском medium и выше ([06-changes-publishing.md §5](06-changes-publishing.md#5-pipeline-публикации)).
