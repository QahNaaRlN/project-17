// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { StudioApp } from "../src/account.js";
import { mockApi, documentId, changesetId } from "./fixture.js";
import type { Account } from "../src/types.js";
let root: Root;
afterEach(async () => {
  await act(async () => root?.unmount());
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});
const user: Account = {
  authenticated: true,
  user: { id: documentId, name: "Alice" },
  csrfToken: "a".repeat(43),
  rights: ["design.read", "content.read"],
  hasAccess: true,
  expiresAt: "2099-01-01T00:00:00Z",
};
async function setup(account: Account | Error = user, service = false) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  window.history.replaceState({}, "", "/editor");
  const { api, session } = mockApi();
  if (!service) session.config.authMode = "oidc";
  if (account instanceof Error) vi.mocked(api.account).mockRejectedValue(account);
  else vi.mocked(api.account).mockResolvedValue(account);
  document.body.innerHTML = '<div id="root"></div>';
  root = createRoot(document.getElementById("root")!);
  const makeApi = vi.fn(() => ({ ...api }));
  await act(async () => root.render(<StudioApp makeApi={makeApi} />));
  await act(async () => {
    await new Promise((r) => setTimeout(r, 10));
  });
  return { api, session, makeApi };
}
const button = (label: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === label)!;
async function click(label: string) {
  await act(async () => button(label).click());
}
it("STU-082: guest sees localized login; no workspace queries precede authentication", async () => {
  const { api } = await setup({ authenticated: false, rights: [], hasAccess: false });
  expect(document.querySelector("a")!.getAttribute("href")).toBe("/api/studio/auth/login");
  expect(api.pages).not.toHaveBeenCalled();
  const select = document.querySelector("select")!;
  await act(async () => {
    select.value = "en";
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await act(async () => {
    await new Promise((r) => setTimeout(r, 10));
  });
  expect(document.body.textContent).toContain("Sign in");
  await click("Check again");
});
it("STU-082: newly registered human gets explicit missing access and an ID to provide to an administrator", async () => {
  const { api } = await setup({ ...user, hasAccess: false, rights: [] });
  expect(document.body.textContent).toContain("Доступ к проекту ещё не назначен");
  expect(api.open).not.toHaveBeenCalled();
  await click("Выйти");
  expect(api.logout).toHaveBeenCalled();
});
it("STU-082: failed session lookup can retry; service mode stays explicit", async () => {
  const { api } = await setup(Error("private"));
  expect(document.body.textContent).toContain("Не удалось проверить вход");
  expect(document.body.textContent).not.toContain("private");
  vi.mocked(api.account).mockResolvedValue(user);
  await click("Проверить снова");
  expect(document.querySelector(".person-bar")!.textContent).toContain("Alice");
});
it("STU-082: logout clears work/cache and a failed logout keeps the account visible", async () => {
  const { api } = await setup();
  vi.mocked(api.logout).mockRejectedValueOnce(Error("secret"));
  await click("Выйти");
  expect(document.body.textContent).toContain("Не удалось подтвердить выход");
  expect(document.querySelector(".person-bar")!.textContent).toContain("Alice");
  vi.mocked(api.account).mockResolvedValue({ authenticated: false, rights: [], hasAccess: false });
  await click("Выйти");
  expect(document.querySelector(".workspace")).toBeNull();
  expect(window.location.hash).toBe("");
});
it("STU-082: legacy service setup mounts without any human session lookup", async () => {
  const { api } = await setup(user, true);
  expect(api.account).not.toHaveBeenCalled();
  expect(document.querySelector(".workspace")).not.toBeNull();
});
it("STU-083: work list opens another author's reviewed page without editable-owner assumptions", async () => {
  const { api, session } = await setup();
  session.review.canEdit = false;
  await act(async () => {
    await vi.waitFor(() => expect(button("Обновить работы").disabled).toBe(false));
  });
  vi.mocked(api.workItems).mockResolvedValue({
    items: [
      {
        id: changesetId,
        documentId,
        path: "/foreign",
        title: "Other work",
        ownerId: changesetId,
        ownerName: "Bob",
        own: false,
        state: "in_review",
      },
    ],
    nextCursor: null,
  });
  await click("Обновить работы");
  await act(async () => {
    await vi.waitFor(() => expect(document.body.textContent).toContain("Other work"));
  });
  await click("Открыть работу");
  expect(api.open).toHaveBeenCalledWith(documentId, changesetId, expect.any(AbortSignal));
  expect(window.location.hash).toContain(changesetId);
  expect(button("Сохранить Внутренний отступ").disabled).toBe(true);
  expect(document.body.textContent).toContain("Этот черновик доступен только для чтения");
});
