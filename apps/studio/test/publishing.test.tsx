// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { PublishingPanel } from "../src/publishing.js";
import { createEditor, type Editor } from "../src/editor.js";
import { mockApi, documentId, changesetId } from "./fixture.js";
import { StudioError } from "../src/types.js";
let root: Root, editor: Editor;
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});
async function setup(state = "open") {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const { api, session } = mockApi();
  session.changeset.state = state;
  editor = createEditor(api);
  await editor.open(documentId, changesetId);
  document.body.innerHTML = '<div id="root"></div>';
  root = createRoot(document.getElementById("root")!);
  await act(async () => root.render(<PublishingPanel editor={editor} />));
  return { api, session };
}
const button = (label: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === label)!;
async function click(label: string) {
  await act(async () => button(label).click());
}
async function reread() {
  await act(async () => editor.refresh());
}
it("STU-070/071/073: submit then explicit whole-CS confirmation publishes and shows receipt", async () => {
  const { api, session } = await setup();
  session.review.objects.push({ id: changesetId, kind: "entity", path: null, deleted: true });
  await reread();
  expect(document.body.textContent).toContain("Изменённые объекты (2)");
  await click("Отправить на проверку");
  expect(api.workflow).toHaveBeenCalledTimes(1);
  expect(document.body.textContent).toContain("Готов к публикации");
  await click("Опубликовать");
  expect(api.workflow).toHaveBeenCalledTimes(1);
  expect(document.activeElement?.className).toBe("publication-confirmation");
  expect(document.querySelector(".publication-confirmation")!.textContent).toContain("entity");
  await click("Отмена");
  await click("Опубликовать");
  await act(async () =>
    document.activeElement!.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
    ),
  );
  expect(document.querySelector(".publication-confirmation")).toBeNull();
  await click("Опубликовать");
  await click("Подтвердить публикацию");
  expect(api.workflow).toHaveBeenCalledTimes(2);
  expect(document.body.textContent).toContain("Публикация подтверждена: staging");
  expect(document.querySelector("a")!.href).toBe("http://127.0.0.1:18083/store");
  expect(document.body.textContent).toContain("Опубликован");
});
it("STU-072: reopen warns that approvals will be reset, before enabling edits", async () => {
  const { api } = await setup("failed");
  await click("Вернуть в работу");
  expect(document.body.textContent).toContain("сбросит полученные согласования");
  expect(api.workflow).not.toHaveBeenCalled();
  await click("Вернуть в работу и сбросить согласования");
  expect(editor.store.getState().session!.changeset.state).toBe("open");
});
it("STU-071: checks retain environment/object scope and select only a node of this page", async () => {
  const { session } = await setup("failed");
  session.review.risk = "high";
  session.review.needsAttention = true;
  session.review.missingRequiredRoles = ["designer"];
  session.review.requiredRoles = ["designer"];
  session.review.checks = [
    {
      stage: "ir",
      status: "failed",
      blocking: true,
      details: {
        environments: {
          staging: {
            documents: {
              [documentId]: [
                {
                  code: "PROP_REQUIRED",
                  severity: "error",
                  pointer: "/nodes/n_heading/props/text",
                },
              ],
              [changesetId]: [
                { code: "OTHER_PAGE", severity: "warning", pointer: "/nodes/n_heading" },
              ],
            },
          },
        },
      },
    },
  ];
  session.review.history = [
    {
      approverId: changesetId,
      decision: "approve",
      comment: "Review comment",
      valid: false,
      createdAt: "2026-10-12T00:00:00Z",
    },
  ];
  session.review.checks.push({
    stage: "policy",
    status: "failed",
    blocking: true,
    details: {
      violations: [{ actorId: changesetId, operation: "node.setDesign", right: "design.compose" }],
      agentPolicy: {
        violations: [
          {
            operation: "node.setProps",
            right: "design.compose",
            ruleId: "no-checkout",
            condition: "pathPrefix=/checkout",
          },
        ],
      },
    },
  });
  session.review.changeset.policyApproval = ["prod-review"];
  await reread();
  // STU-427/430: the refusal of the current agent policy and its approval mark.
  expect(document.body.textContent).toContain("node.setProps · design.compose · no-checkout");
  expect(document.body.textContent).toContain("по политике агентов: prod-review");
  expect(document.body.textContent).toContain("устаревшие проверки");
  expect(document.body.textContent).toContain("designer");
  expect(document.body.textContent).toContain("Восстановите права");
  expect(document.body.textContent).toContain("Review comment");
  expect(document.body.textContent).toContain("Сброшено");
  expect(
    [...document.querySelectorAll("button")].filter((b) => b.textContent === "Выбрать блок"),
  ).toHaveLength(1);
  await click("Выбрать блок");
  expect(editor.store.getState().selected).toBe("n_heading");
});
it("STU-072: human review requires a comment for changes; approval remains a separate command", async () => {
  const { api, session } = await setup("in_review");
  await act(async () => editor.locale("en"));
  await click("Request changes");
  expect(button("Request changes").disabled).toBe(false);
  const confirm = document.querySelector<HTMLButtonElement>(".publication-confirmation button")!;
  expect(confirm.disabled).toBe(true);
  const input = document.querySelector("textarea")!;
  await act(async () => {
    Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, "value")!.set!.call(
      input,
      "Please revise",
    );
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
  await act(async () => confirm.click());
  expect(api.workflow).toHaveBeenLastCalledWith(
    expect.anything(),
    "request-changes",
    "Please revise",
    expect.any(String),
    expect.any(AbortSignal),
  );
  session.changeset.state = "in_review";
  session.review.history = [
    {
      approverId: changesetId,
      decision: "request_changes",
      comment: null,
      valid: true,
      createdAt: "2026-10-12T00:00:00Z",
    },
  ];
  await reread();
  await click("Approve");
  expect(api.workflow).toHaveBeenLastCalledWith(
    expect.anything(),
    "approve",
    "",
    expect.any(String),
    expect.any(AbortSignal),
  );
});
it("STU-072/074: server action reasons and busy states disable commands", async () => {
  const { session, api } = await setup("in_review");
  session.review.actions.approve = "APPROVAL_FORBIDDEN_ACTOR";
  session.review.actions["request-changes"] = "APPROVAL_SELF";
  session.review.actions.reopen = "CHANGESET_NOT_OWNER";
  await reread();
  expect(button("Согласовать").disabled).toBe(true);
  expect(document.body.textContent).toContain("требует входа от имени человека");
  await click("Согласовать");
  expect(api.workflow).not.toHaveBeenCalled();
  await act(async () => editor.store.setState({ status: "saving", error: "REBASE_REQUIRED" }));
  expect(document.body.textContent).toContain("Исходные версии изменились");
  expect(button("Вернуть в работу").disabled).toBe(true);
});
it("STU-074: uncertain result exposes an idempotent retry and never infers publication", async () => {
  const { api } = await setup("approved");
  vi.mocked(api.workflow).mockRejectedValueOnce(new StudioError("NETWORK_UNCERTAIN", true));
  await click("Опубликовать");
  await click("Подтвердить публикацию");
  expect(editor.store.getState().status).toBe("uncertain");
  expect(document.body.textContent).not.toContain("Публикация подтверждена");
  await click("Повторить тот же запрос");
  expect(document.body.textContent).toContain("Публикация подтверждена");
  const [first, second] = vi.mocked(api.workflow).mock.calls;
  expect(second!.slice(0, 4)).toEqual(first!.slice(0, 4));
});
it("STU-070: panel without a session is empty; abandoned drafts offer no workflow", async () => {
  await setup("abandoned");
  expect(document.querySelector(".publishing-actions")!.children).toHaveLength(0);
  await act(async () => editor.store.setState({ session: null }));
  expect(document.body.textContent).toBe("");
});
it("STU-494: checking shows stage 8–9 progress, findings and only reopen", async () => {
  const { session } = await setup("checking");
  session.review.changeset.state = "checking";
  session.review.checks = [
    {
      stage: "preview",
      status: "passed",
      blocking: true,
      details: { pages: [] },
      environment: "staging",
    },
    {
      stage: "axe",
      status: "queued",
      blocking: true,
      environment: "staging",
      stale: true,
      details: {
        pages: [
          { path: "/store", violations: [{ id: "button-name", impact: "serious", nodes: 1 }] },
        ],
      },
    },
    {
      stage: "preview",
      status: "failed",
      blocking: true,
      environment: "production",
      details: { reason: "CHECK_APP_URL_MISSING" },
    },
  ];
  await reread();
  const text = document.body.textContent ?? "";
  expect(text).toContain("Идут проверки страниц");
  expect(text).toContain("Согласование станет доступно, когда проверки страниц завершатся.");
  expect(text).toContain("Сборка страниц (этап 8) · staging: Пройдена");
  expect(text).toContain("Доступность axe (этап 9) · staging: В очереди");
  expect(text).toContain("Исполнитель проверок не забирает проверку больше 15 минут.");
  expect(text).toContain("/store: button-name (serious, 1)");
  expect(text).toContain("CHECK_APP_URL_MISSING");
  expect(button("Вернуть в работу")).toBeDefined();
  expect(button("Согласовать")).toBeUndefined();
});
it("STU-494: a checking draft is reread until the runner decides", async () => {
  vi.useFakeTimers();
  try {
    const { session, api } = await setup("checking");
    session.review.changeset.state = "checking";
    await reread();
    const before = vi.mocked(api.open).mock.calls.length;
    session.changeset.state = "in_review";
    session.review.changeset.state = "in_review";
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3100);
    });
    expect(vi.mocked(api.open).mock.calls.length).toBeGreaterThan(before);
    expect(document.body.textContent).toContain("На согласовании");
  } finally {
    vi.useRealTimers();
  }
});
