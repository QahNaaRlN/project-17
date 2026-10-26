// STU-455: parts of §2 that are not implemented yet. Each entry names the
// step, the strict check, the package that delivers it and the temporary
// workaround. When the part becomes available the scenario fails until the
// package removes its entry, so the scenario only ever gets stricter.
import { test } from "@playwright/test";

// Empty since STU-496: every part of §2 is checked strictly. New entries follow the
// same shape: { step, check, plan, workaround }.
export const pending: Record<
  string,
  { step: number; check: string; plan: string; workaround: string }
> = {};

export type Pending = keyof typeof pending;

/** Annotates a pending part, or fails when it has become available. */
export function stillPending(id: Pending, available: boolean) {
  const entry = pending[id];
  if (!entry) throw new Error(`Pending «${id}» не зарегистрирован в e2e/scenario/pending.ts`);
  if (available)
    throw new Error(
      `Pending «${id}» уже доступен (${entry.check}): удалите запись из e2e/scenario/pending.ts и сделайте проверку строгой`,
    );
  test.info().annotations.push({
    type: "pending",
    description: `${id}: шаг ${entry.step}, ${entry.check}; временно ${entry.workaround}; ${entry.plan}`,
  });
}
