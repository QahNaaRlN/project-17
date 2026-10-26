// STU-455: parts of §2 that are not implemented yet. Each entry names the
// step, the strict check, the package that delivers it and the temporary
// workaround. When the part becomes available the scenario fails until the
// package removes its entry, so the scenario only ever gets stricter.
import { test } from "@playwright/test";

export const pending = {
  "pipeline-8-9": {
    step: 7,
    check: "Этапы pipeline 8 (preview build) и 9 (axe) пройдены",
    plan: "M6. Укрепление: этапы pipeline 8–11 (docs/spec/14-mvp-plan.md)",
    workaround: "проверяются этапы 1–7 и 13",
  },
  "studio-promote": {
    step: 8,
    check: "Перенос в production выполняется в Studio",
    plan: "M4. Studio: promote в Studio (docs/spec/14-mvp-plan.md)",
    workaround: "promote командой API от имени lead-designer",
  },
  "studio-production-rollback": {
    step: 9,
    check: "Откат production выполняется в Studio",
    plan: "M4. Studio: Studio работает в одном окружении (STU-001); откат production — с promote в Studio",
    workaround: "rollback командой API от имени lead-designer",
  },
} as const;

export type Pending = keyof typeof pending;

/** Annotates a pending part, or fails when it has become available. */
export function stillPending(id: Pending, available: boolean) {
  const entry = pending[id];
  if (available)
    throw new Error(
      `Pending «${id}» уже доступен (${entry.check}): удалите запись из e2e/scenario/pending.ts и сделайте проверку строгой`,
    );
  test.info().annotations.push({
    type: "pending",
    description: `${id}: шаг ${entry.step}, ${entry.check}; временно ${entry.workaround}; ${entry.plan}`,
  });
}
