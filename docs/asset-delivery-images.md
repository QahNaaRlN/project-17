# Выдача изображений ассетов

Следующий пакет после PR #30: CNT-043, CNT-050–052, API-033/040,
SDK-001. Контракт реализован в gateway и первом примитиве `@cms/runtime`.

## Контракт

- `GET /delivery/v1/{project}/{env}/asset/{id}/image` по действующему
  delivery/preview ключу возвращает модель изображения: размеры/соотношение сторон,
  fit, format, срок и массив `{width,url}`. Ответ `no-store`.
- Параметры: `fmt=avif|webp|jpeg|png` (webp по умолчанию),
  `fit=contain|cover` (contain по умолчанию). Для cover нужны `w` из списка
  CNT-051 и `h` от 1 до 2560: они задают соотношение сторон всех вариантов.
  Для contain высота определяется исходником. Варианты с результирующей
  высотой больше 2560 исключаются; если вариантов нет — отказ.
  Для cover размеры модели задают целевое соотношение сторон.
- URL `/assets/v1/{env}/{assetId}/{fileHash}` содержит подписанные параметры
  проекта, версии, ревизии тела, области доступа, срока и трансформации.
  Изменение любого параметра, лишний/повторный параметр или истечение срока
  вызывает отказ до обращения к imgproxy. Максимальный срок — 15 минут.
- Gateway повторно разрешает текущую версию в указанном проекте/окружении.
  Published URL работает только для текущей опубликованной версии. Draft URL
  связан с head или конкретным CS, сроком preview-токена и ключом окружения;
  ротация preview key отзывает его. Draft и ошибки всегда `no-store`.
- Тело/версия, готовность файла и хэш должны совпасть. Worker/storage key
  клиента не принимается. Gateway создаёт отдельную imgproxy-подпись для
  `s3://bucket/projects/{project}/assets/{hash}`. Imgproxy доступен только
  внутри сети, читает только ready-префикс закрытого bucket.
- Растровые изображения и очищенные SVG преобразуются imgproxy в явно
  указанный формат. PDF/видео не являются изображениями. Для cover gravity
  берётся из версии `focalPoint`, по умолчанию центр. Результат ограничен
  2560 по каждой стороне, исходник — 40 MP/25 MiB.
- Published ответ имеет ETag, Surrogate-Key проекта/окружения/ассета,
  `s-maxage` не более 300 секунд и оставшегося срока URL, без stale-выдачи.
  CDN должен учитывать все query параметры и выполнять существующий purge
  webhook после публикации/удаления/замены. CDN не обходит gateway.
  Purge асинхронный: до выполнения задачи кэш может отдать предыдущую версию
  в пределах s-maxage. На origin устаревшая версия сразу отклоняется.
- `@cms/runtime` начинает с framework-neutral примитива `Image`: готовые
  signed варианты → src/srcset/sizes, адаптивная ширина → media sizes,
  objectFit и objectPosition. Ключи подписи не попадают в браузер.

## Проверки и эксплуатация

Тесты покрывают опубликованный/неопубликованный ассет, чужой проект и
окружение, старую версию/хэш/ревизию, preview CS/head/rotation/expiry,
все допустимые размеры/форматы, tampering, отсутствие обращения к upstream
при отказе, cache/ETag/purge ключи, ошибки/лимиты upstream, реальный imgproxy
с приватным MinIO и адаптивный runtime без секретов.

Реальный внешний CDN и production deployment не входят в PR. Общий runtime renderer, React
адаптер, preview bridge, SVG upload preview и видео остаются отдельными пакетами.

## Конфигурация

Параметры выдачи опциональны как целое; без них endpoints отвечают
`503 ASSET_IMAGES_UNAVAILABLE`. Частичная/неверная конфигурация останавливает
запуск. S3 bucket остаётся `CMS_S3_BUCKET`.

| Параметр | Значение |
|---|---|
| CMS_ASSET_PUBLIC_URL | Origin gateway/CDN без пути, query, fragment и credentials |
| CMS_ASSET_SIGNING_KEY | Отдельный hex ключ не менее 32 байт для браузерных URL |
| CMS_IMGPROXY_URL | Внутренний HTTP(S) base URL imgproxy |
| CMS_IMGPROXY_KEY | Hex ключ imgproxy не менее 32 байт, совпадает с IMGPROXY_KEY |
| CMS_IMGPROXY_SALT | Hex salt не менее 16 байт, совпадает с IMGPROXY_SALT |

В production imgproxy закрыт от внешних запросов и имеет отдельную S3 identity
только с GetObject на `arn:aws:s3:::<bucket>/projects/*`. Staging `uploads/`
ему не доступен. Ключи случайные и разные для gateway/imgproxy. Настройки
обработчика: allowed sources `s3://<bucket>/projects/`, allowed processing
options `rs,g`, max source 40 MP/26214400 bytes, max result dimension 2560,
max animation frames 1, strip metadata true. Запретите cookie passthrough и
отключение проверки URL signatures. Gateway не пересылает cookies/auth
клиента и не следует upstream redirects; HTTP timeout — 30 секунд,
предел ответа — 32 MiB.

HMAC imgproxy и `s3://` источник соответствуют официальным протоколам:
[подпись](https://docs.imgproxy.net/usage/signing_url),
[S3](https://docs.imgproxy.net/image_sources/amazon_s3),
[настройки](https://docs.imgproxy.net/configuration/options).
Версия контейнера закреплена tag+digest и проверяется интеграционным тестом.

Локальный профиль:
`docker compose -f deploy/compose/docker-compose.images.yml --profile assets up --build`.
MinIO слушает localhost:9000, console — localhost:9001; создайте приватный
bucket `assets`. Фиктивные development credentials — `test-access/test-secret`.
CMS запускается на хосте: S3 endpoint `localhost:9000`, secure false,
public image origin `http://localhost:8080`, imgproxy URL `http://localhost:8081`.
Hex ключ gateway — 32 байта `11`, ключ imgproxy — 32 байта `22`, salt —
16 байт `33`, как в profile. Это обеспечивает общий S3 адрес для браузера и
CMS при подписанном PUT. В production используйте общедоступное для браузера
имя S3 endpoint и внутреннюю сеть для imgproxy.

Получайте новую image model при SSR/render после истечения expiresAt. Правила
адаптивного Image и обновления модели: [runtime README](../packages/runtime/README.md).
