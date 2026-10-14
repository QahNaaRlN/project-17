import type { Page } from "@playwright/test";
// A pending protected read may dispose the editor before an explicit refresh.
// The outcome assertions still require the new identity and removal of old data.
export async function refreshMountedEditor(page: Page, label = "Перечитать черновик") {
  await page.evaluate((text) => {
    [...document.querySelectorAll<HTMLButtonElement>("button")]
      .find((button) => button.textContent === text)
      ?.click();
  }, label);
}
