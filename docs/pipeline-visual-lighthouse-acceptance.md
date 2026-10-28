# Этапы pipeline 10–11: отчёт о приёмке

Спецификация: [pipeline-visual-lighthouse.md](pipeline-visual-lighthouse.md), STU-570…589.
Дата: 2026-10-28. Ветка `feature/pipeline-visual-lighthouse`,
[PR #101](https://github.com/QahNaaRlN/project-17/pull/101).

## Автоматическая приёмка

[CI итогового SHA `de8bbc5`](https://github.com/QahNaaRlN/project-17/actions/runs/40691379068):
`Check (TS + Go)`, `E2E scenario (§2)`, `Security`, `Docker image` — пройдены; покрытие Go
95 % (порог 95 %).

| Критерий §9 | Чем подтверждён |
|---|---|
| Согласующий видит «до / после / разница» на трёх ширинах | Сценарий §2 «stages 10–11 compare screenshots…»: изменённая опубликованная страница получает `before`, `after`, `diff`; файл разницы читается через адаптер Studio как `image/png`. Просмотр в Studio — `check-artifacts.test.tsx` |
| Риск `high`: accessibility ниже порога — `failed`, performance — `warning` | `TestLighthouseStagePolicy` (Go), `lighthouseStatus` и прогон Lighthouse против фикстуры (`stages.test.ts`); сценарий §2 проверяет баллы и отчёт на настоящей витрине |
| Незавершённый `visual` не задерживает согласование и публикацию | `TestVisualStageArtifacts`, `TestVisualStageSkips` (`CHANGESET_MERGED`); шаг 7 сценария §2 согласует до завершения этапа 10 |
| Артефакт недоступен без права чтения CS; после срока — `410` | `TestCheckArtifactRoutes` (401, 404, права исполнителя), `TestVisualStageArtifacts` (`CHECK_ARTIFACT_EXPIRED`) |
| Существующие проверки не ослаблены | Пороги покрытия Go и TS прежние; job сценария §2 обязательный, 2,1 мин на 4 теста |

## Что не проверено

- Ручная приёмка двумя людьми в Studio не проводилась: путь согласования не менялся, новые
  панели проверены компонентными тестами и сценарием §2.
- Объектное хранилище S3/MinIO проверено тестами `assets` (MinIO в контейнере); в стенде
  §2 артефакты лежат в каталоге (отступление спецификации).
- Баллы Lighthouse на общем runner плавают; в сценарии §2 статус этапа 11 сверяется с
  фактическим баллом accessibility, а не с ожидаемым значением.
