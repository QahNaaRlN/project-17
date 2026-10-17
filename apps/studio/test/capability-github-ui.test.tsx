// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { CapabilityGithubPanel, GithubConfigPanel } from "../src/capability-github-panel.js";
import { createEditor, type Editor } from "../src/editor.js";
import { mockApi, documentId, changesetId } from "./fixture.js";
import { StudioError, type Api } from "../src/types.js";
import type { CapabilityRequest } from "../src/capabilities.js";
import type { GithubView, GithubObservation } from "../src/capability-github.js";
let root: Root, editor: Editor;
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});
const card: CapabilityRequest = {
  id: documentId,
  kind: "action",
  environment: "staging",
  key: "store.favorite",
  title: "Favorite",
  description: "Task",
  proposedContract: { args: {} },
  acceptance: ["Works"],
  status: "submitted",
  revision: 4,
  changesetId,
  documentId,
  nodeId: "n_root",
  event: "click",
  ownerId: documentId,
  externalRefs: [],
};
const observation: GithubObservation = {
  id: documentId,
  generation: 1,
  configRevision: 1,
  observedAt: "2026-10-17T12:00:00Z",
  freshUntil: "2026-10-17T12:15:00Z",
  digest: "sha256:" + "a".repeat(64),
  facts: {
    repositoryId: "42",
    prId: "43",
    number: 1,
    state: "open",
    draft: false,
    headSha: "a".repeat(40),
    baseSha: "b".repeat(40),
    integrationSha: "c".repeat(40),
    integrationType: "test_merge",
    ci: "success",
    results: [
      {
        source: "check_run",
        name: "Check",
        target: "integration",
        outcome: "success",
        exactSha: "c".repeat(40),
      },
    ],
  },
};
const view: GithubView = {
  link: {
    id: documentId,
    requestId: documentId,
    revision: 1,
    generation: 1,
    active: true,
    pullNumber: 1,
    configRevision: 1,
    syncId: changesetId,
    syncState: "idle",
    lastError: "",
    nextAllowedAt: "2026-10-17T12:01:00Z",
    nextAttemptAt: "2026-10-17T12:05:00Z",
    pollsLeft: 4,
    attempts: 0,
  },
  current: observation,
  lastKnown: observation,
  syncState: "idle",
  canManage: true,
  configRevision: 1,
  repository: { owner: "owner", name: "repo", baseBranch: "dev" },
  url: "https://github.com/owner/repo/pull/1",
};
const button = (s: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find(
    (b) => b.textContent?.trim() === s,
  )!;
async function click(s: string) {
  await act(async () => button(s).click());
}
async function input(label: string, value: string) {
  await act(async () => {
    const e = [...document.querySelectorAll("label")]
      .find((e) => e.textContent?.startsWith(label))!
      .querySelector<HTMLInputElement>("input")!;
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(e, value);
    e.dispatchEvent(new Event("input", { bubbles: true }));
  });
}
async function submit() {
  await act(async () =>
    document
      .querySelector("form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })),
  );
}
async function setup(mode: "card" | "config" = "card", prepare?: (api: Api) => void, ru = true) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("crypto", webcrypto);
  const { api } = mockApi();
  api.account = vi.fn(async () => ({
    authenticated: true,
    user: { id: documentId, name: "Engineer" },
    rights: ["code.propose", "project.admin"],
    csrfToken: "csrf",
    hasAccess: true,
  }));
  api.github = vi.fn(async () => structuredClone(view));
  api.githubHistory = vi.fn(async () => ({
    items: [structuredClone(observation)],
    nextCursor: "next",
  }));
  api.githubConfig = vi.fn(async () => ({
    configured: true,
    revision: 1,
    ready: true,
    enabled: true,
    configId: "repo",
    choices: [
      { configId: "repo", owner: "owner", name: "repo", baseBranch: "dev", requiredChecks: [] },
    ],
  }));
  api.githubCommand = vi.fn(async () => ({}));
  prepare?.(api);
  editor = createEditor(api);
  if (!ru) editor.locale("en");
  document.body.innerHTML = "<div id='root'></div>";
  root = createRoot(document.getElementById("root")!);
  await act(async () =>
    root.render(
      mode === "card" ? (
        <CapabilityGithubPanel {...{ api, editor }} request={card} />
      ) : (
        <GithubConfigPanel {...{ api, editor }} />
      ),
    ),
  );
  return api;
}
it("STU-312: separate GitHub facts, exact SHA, history and explicit refresh", async () => {
  const api = await setup();
  expect(document.body.textContent).toContain("не подтверждают развёртывание");
  expect(document.body.textContent).toContain("c".repeat(40));
  await input("Причина", "Check engineering result");
  expect(editor.store.getState().dirtyFields["property-capability-github"]).toBe(true);
  await click("Проверить GitHub");
  expect(api.githubCommand).toHaveBeenCalledWith(
    expect.objectContaining({
      name: "refresh-capability-github-pr",
      payload: expect.objectContaining({
        requestId: documentId,
        expectedRequestRevision: 4,
        expectedLinkRevision: 1,
        expectedConfigRevision: 1,
      }),
    }),
  );
  await click("История наблюдений");
  expect(api.githubHistory).toHaveBeenCalled();
  await click("Ещё наблюдения");
  expect(api.githubHistory).toHaveBeenLastCalledWith(documentId, "next");
  await click("Обновить отображение");
  expect(api.github).toHaveBeenCalledTimes(3);
});
it("STU-297/312: explicit replace and unlink preserve human status", async () => {
  const api = await setup();
  await input("Номер PR", "2");
  await input("Причина", "Implementation in another PR");
  const checkbox = document.querySelector<HTMLInputElement>("input[type=checkbox]")!;
  await act(async () => checkbox.click());
  await submit();
  expect(api.githubCommand).toHaveBeenCalledWith(
    expect.objectContaining({
      name: "link-capability-github-pr",
      payload: expect.objectContaining({ pullNumber: 2, confirmReplace: true }),
    }),
  );
  await input("Причина", "Wrong association");
  await click("Отвязать PR");
  expect(api.githubCommand).toHaveBeenLastCalledWith(
    expect.objectContaining({
      name: "unlink-capability-github-pr",
      payload: { requestId: documentId, expectedRequestRevision: 4, expectedLinkRevision: 1 },
    }),
  );
});
it("STU-298/312: lost response retries frozen payload and key", async () => {
  const api = await setup("card", (api) => {
    api.githubCommand = vi
      .fn()
      .mockRejectedValueOnce(new StudioError("RESPONSE_INVALID", true))
      .mockResolvedValue({});
  });
  await input("Причина", "Refresh explicitly");
  await click("Проверить GitHub");
  expect(document.body.textContent).toContain("Некорректный ответ");
  await click("Повторить тот же запрос");
  expect(vi.mocked(api.githubCommand!).mock.calls[0]![0]).toEqual(
    vi.mocked(api.githubCommand!).mock.calls[1]![0],
  );
  expect(editor.store.getState().dirtyFields["property-capability-github"]).toBeFalsy();
});
it("STU-304/308: last-known errors and read-only state are explicit in English", async () => {
  await setup(
    "card",
    (api) => {
      api.github = vi.fn(async () => ({
        ...structuredClone(view),
        current: null,
        canManage: false,
        syncState: "stale",
        link: { ...view.link!, lastError: "permission_denied" },
      }));
    },
    false,
  );
  expect(document.body.textContent).toContain("Last-known information");
  expect(document.body.textContent).toContain("not current verification");
  expect(document.querySelector("form")).toBeNull();
});
it("STU-312: form errors retain input and permit discard", async () => {
  await setup("card", (api) => {
    api.githubCommand = vi
      .fn()
      .mockRejectedValue(new StudioError("CAPABILITY_GITHUB_REVISION_CONFLICT"));
    api.githubHistory = vi.fn().mockRejectedValue(new StudioError("CAPABILITY_GITHUB_UNAVAILABLE"));
  });
  await input("Номер PR", "2");
  await input("Причина", "Test");
  await submit();
  expect(document.body.textContent).toContain("Контекст изменился");
  await click("История наблюдений");
  await click("Сбросить ввод GitHub");
  expect(editor.store.getState().dirtyFields["property-capability-github"]).toBeFalsy();
});
it("STU-295: admin explicitly enables metadata projection without reading requests", async () => {
  const api = await setup("config");
  expect(document.body.textContent).toContain("включая приватные PR");
  await input("Причина", "Enable read-only GitHub");
  const checkboxes = document.querySelectorAll<HTMLInputElement>("input[type=checkbox]");
  await act(async () => checkboxes[1]!.click());
  await submit();
  expect(api.githubCommand).toHaveBeenCalledWith(
    expect.objectContaining({
      name: "set-capability-github-config",
      payload: {
        environment: "staging",
        configId: "repo",
        enabled: true,
        expectedConfigRevision: 1,
      },
    }),
  );
  await input("Причина", "Discard this");
  await click("Сбросить ввод");
  expect(editor.store.getState().dirtyFields["property-github-config"]).toBeFalsy();
});
it("STU-295/312: disabled config error, English labels and failed initial query", async () => {
  await setup(
    "config",
    (api) => {
      api.githubConfig = vi.fn().mockRejectedValue(new StudioError("CAPABILITY_GITHUB_FORBIDDEN"));
    },
    false,
  );
  expect(document.body.textContent).toContain("Insufficient permissions");
  expect(document.querySelector("form")).toBeNull();
});

it("STU-295/312: empty operator catalogue explains the disabled setup", async () => {
  await setup("config", (api) => {
    api.githubConfig = vi.fn(async () => ({
      configured: false,
      revision: 0,
      ready: false,
      choices: [],
    }));
  });
  expect(document.body.textContent).toContain("Каталог репозиториев пуст");
  expect(button("Сохранить настройки").disabled).toBe(true);
});
