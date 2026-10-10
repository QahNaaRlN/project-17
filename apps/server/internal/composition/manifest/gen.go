// Package manifest — формат manifest приложения 1.0 на стороне сервера (docs/spec/04-manifest.md):
// валидация (JSON Schema и семантические проверки) и канонический хэш (MF-002). Поведение
// совпадает с пакетом @cms/manifest (TypeScript); совпадение проверяется общими фикстурами
// packages/manifest/fixtures.
package manifest

//go:generate go run ../../../cmd/manifestgen
