// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { AgentPolicyPanel } from "../src/agent-policy-panel.js";
import {
  denialText,
  describeWhen,
  emptyPolicy,
  explanation,
  normalizePolicy,
  policyDiff,
  policyDocument,
  policyMessage,
  policyState,
  policyVersion,
  reasonText,
  type PolicyDocument,
  type PolicyState,
  type PolicyVersion,
} from "../src/agent-policy.js";
import { createApi } from "../src/api.js";
import { createEditor, type Editor } from "../src/editor.js";
import { agentPolicyViolations } from "../src/review.js";
import { StudioError, type Api } from "../src/types.js";
import { documentId, mockApi } from "./fixture.js";

const policy: PolicyDocument = {
  version: 1,
  rules: [
    {
      id: "no-checkout",
      subject: { agent: "*" },
      deny: ["design.compose"],
      when: { pathPrefix: ["/checkout"] },
    },
  ],
  risk: [{ id: "checkout-risk", min: "high", when: { pathPrefix: ["/checkout"] } }],
  approvals: [{ id: "prod", require: { count: 1 }, when: { environment: ["production"] } }],
};
const version: PolicyVersion = {
  version: 2,
  hash: "sha256:" + "a".repeat(64),
  policy,
  authorId: documentId,
  authorName: "Bob",
  reason: "freeze checkout",
  createdAt: "2026-10-21T10:00:00Z",
};
const state: PolicyState = {
  ...version,
  known: {
    rights: ["content.write", "design.compose", "design.read"],
    environments: ["production", "staging"],
    operationTypes: ["node.insert", "node.setProps"],
    schemas: ["Product"],
    agents: ["copy-assistant"],
  },
};

it("STU-420/431/433: DTOs are validated, normalized and diffed by rule id", () => {
  expect(policyDocument({ version: 1 })).toEqual(emptyPolicy());
  expect(() => policyDocument({ version: 2 })).toThrow(StudioError);
  expect(() => policyDocument({ version: 1, rules: [{ id: "x", subject: {} }] })).toThrow();
  expect(() =>
    policyDocument({
      version: 1,
      rules: [{ id: "x", subject: { agent: "*" }, when: { moon: [] } }],
    }),
  ).toThrow();
  expect(() => policyDocument({ version: 1, risk: [{ id: "x" }] })).toThrow();
  expect(() =>
    policyDocument({ version: 1, approvals: [{ id: "x", require: { count: "1" } }] }),
  ).toThrow();
  expect(policyVersion({ version: 0, hash: "", policy: { version: 1 } })).toMatchObject({
    authorId: null,
    authorName: "",
    reason: "",
  });
  expect(() => policyVersion({ version: -1, hash: "", policy: { version: 1 } })).toThrow();
  expect(() =>
    policyVersion({ version: 1, hash: "", authorId: 3, policy: { version: 1 } }),
  ).toThrow();
  expect(policyState(state).known.agents).toEqual(["copy-assistant"]);
  expect(() => policyState({ ...version, known: { rights: [] } })).toThrow();
  expect(() => explanation({ decision: {} })).toThrow();
  const normal = normalizePolicy({
    version: 1,
    rules: [
      {
        id: " a ",
        subject: { agent: " * " },
        allow: [],
        deny: ["design.compose"],
        when: { pathPrefix: [" /x ", ""], zoneId: [] },
      },
      { id: "b", subject: { agent: "*" }, requireApproval: ["content.write"], when: {} },
    ],
    risk: [{ id: "r", min: "high", when: { source: [] } }],
    approvals: [{ id: "p", require: { count: 2, roles: [" lead ", ""] } }],
  });
  expect(normal.rules[0]).toEqual({
    id: "a",
    subject: { agent: "*" },
    deny: ["design.compose"],
    when: { pathPrefix: ["/x"] },
  });
  expect(normal.rules[1]!.when).toBeUndefined();
  expect(normal.risk[0]).toEqual({ id: "r", min: "high" });
  expect(normal.approvals[0]!.require).toEqual({ count: 2, roles: ["lead"] });
  const next = structuredClone(policy);
  next.rules[0]!.deny = ["content.write"];
  next.risk = [];
  next.approvals.push({ id: "two", require: { count: 2 } });
  expect(policyDiff(policy, next)).toEqual([
    { kind: "changed", section: "rules", id: "no-checkout" },
    { kind: "removed", section: "risk", id: "checkout-risk" },
    { kind: "added", section: "approvals", id: "two" },
  ]);
  expect(describeWhen({ environment: ["staging", "production"], pathPrefix: ["/x"] })).toBe(
    "environment=staging|production; pathPrefix=/x",
  );
  for (const ru of [true, false]) {
    for (const code of [
      "POLICY_INVALID",
      "POLICY_VERSION_CONFLICT",
      "VALIDATION_FAILED",
      "FORBIDDEN",
      "NETWORK_UNCERTAIN",
    ])
      expect(policyMessage(code, ru)).not.toBe(code);
    expect(policyMessage("OTHER", ru)).toBe("OTHER");
    for (const reason of ["denied_by_rule", "no_rule", "outside_ceiling", "delegate_lacks_right"])
      expect(reasonText(reason, ru)).not.toBe(reason);
  }
  expect(reasonText("custom", true)).toBe("custom");
  expect(reasonText(undefined, true)).toBe("");
  // STU-434: the step names the rule and the condition only.
  expect(denialText({ ruleId: "no-checkout", condition: "pathPrefix=/checkout" }, true)).toBe(
    "Запрещено политикой: правило no-checkout, pathPrefix=/checkout",
  );
  expect(denialText({ reason: "no_rule" }, false)).toBe("Denied by policy: no rule allows it");
  expect(denialText({}, false)).toBe("Denied by policy");
  expect(denialText(undefined, false)).toBe("");
  expect(
    agentPolicyViolations({
      agentPolicy: {
        violations: [
          { operation: "node.setProps", right: "design.compose", ruleId: "x", condition: "c" },
          { operation: 1 },
        ],
      },
    }),
  ).toEqual([{ operation: "node.setProps", right: "design.compose", ruleId: "x", condition: "c" }]);
  expect(agentPolicyViolations({})).toEqual([]);
});

it("STU-431/433: API reads versions, posts the dry run and keeps exact command envelopes", async () => {
  const fetcher = vi.fn<typeof fetch>(async (input) => {
    const path = String(input);
    if (path.includes("/auth/session"))
      return Response.json({
        authenticated: true,
        user: { id: documentId, name: "Bob" },
        rights: ["project.admin"],
        csrfToken: "c".repeat(43),
        expiresAt: "2030-01-01T00:00:00Z",
        hasAccess: true,
      });
    if (path.endsWith("/agent-policy")) return Response.json(state);
    if (path.endsWith("/agent-policy/versions")) return Response.json({ items: [version] });
    if (path.endsWith("/explain"))
      return Response.json({
        decision: { allowed: false, ruleId: "no-checkout", reason: "denied_by_rule" },
        approvals: 0,
        approvalRoles: [],
        approvalRules: [],
        version: 2,
      });
    return Response.json({ result: version });
  });
  const api = createApi(fetcher);
  expect((await api.agentPolicy!()).known.rights).toHaveLength(3);
  expect(await api.agentPolicyVersions!()).toEqual([version]);
  const situation = {
    agent: "copy-assistant",
    right: "design.compose",
    environment: "",
    path: "/checkout",
    zoneMode: "",
    zoneId: "",
    operationType: "",
    schema: "",
    risk: "",
  };
  expect((await api.explainAgentPolicy!({ policy, situation })).decision.ruleId).toBe(
    "no-checkout",
  );
  const explain = fetcher.mock.calls.find(([url]) => String(url).endsWith("/explain"));
  expect(JSON.parse(String(explain![1]!.body))).toEqual({ payload: { policy, situation } });
  const attempt = {
    payload: { policy, expectedVersion: 2 },
    key: "exact",
    reason: "freeze",
    actor: documentId,
  };
  expect(await api.agentPolicyCommand!(attempt)).toEqual(version);
  await api.agentPolicyCommand!(attempt);
  const calls = fetcher.mock.calls.filter(([url]) => String(url).includes("/set-agent-policy"));
  expect(calls[0]![1]).toEqual(calls[1]![1]);
  await expect(api.agentPolicyCommand!({ ...attempt, actor: "other" })).rejects.toMatchObject({
    code: "SESSION_CHANGED",
  });
  fetcher.mockImplementation(async () => Response.json({ items: "x" }));
  await expect(api.agentPolicyVersions!()).rejects.toMatchObject({ code: "RESPONSE_INVALID" });
  const { actor: _actor, ...anonymous } = attempt;
  await expect(api.agentPolicyCommand!(anonymous)).rejects.toMatchObject({
    code: "RESPONSE_INVALID",
    uncertain: true,
  });
});

let root: Root, editor: Editor;
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});
const button = (s: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) =>
    b.textContent?.trim().startsWith(s),
  )!;
async function settle() {
  for (let i = 0; i < 100 && document.querySelector('section[aria-busy="true"]'); i++)
    await act(async () => {
      await new Promise<void>((resolve) => setTimeout(resolve, 10));
    });
}
async function click(s: string) {
  expect(button(s), s).toBeDefined();
  await act(async () => button(s).click());
  await settle();
}
function field(s: string) {
  return [...document.querySelectorAll("label")]
    .find((l) => l.textContent?.startsWith(s))!
    .querySelector<HTMLInputElement | HTMLSelectElement>("input,select")!;
}
async function type(s: string, v: string) {
  await act(async () => {
    const e = field(s);
    const proto =
      e instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, "value")!.set!.call(e, v);
    e.dispatchEvent(
      new Event(e instanceof HTMLSelectElement ? "change" : "input", { bubbles: true }),
    );
  });
}
async function many(s: string, values: string[]) {
  await act(async () => {
    const e = field(s) as HTMLSelectElement;
    for (const o of e.options) o.selected = values.includes(o.value);
    e.dispatchEvent(new Event("change", { bubbles: true }));
  });
}
async function submit(index = 0) {
  await act(async () => {
    const form = document.querySelectorAll("form").item(index);
    form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  });
  await settle();
}
async function setup(prepare?: (api: Api) => void, ru = true) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("crypto", webcrypto);
  const { api } = mockApi();
  api.account = vi.fn(async () => ({
    authenticated: true,
    user: { id: documentId, name: "Bob" },
    rights: ["project.admin"],
    csrfToken: "csrf",
    hasAccess: true,
  }));
  api.agentPolicy = vi.fn(async () => structuredClone(state));
  api.agentPolicyVersions = vi.fn(async () => [
    structuredClone(version),
    { ...structuredClone(version), version: 1, authorName: "", policy: emptyPolicy(), reason: "" },
  ]);
  api.explainAgentPolicy = vi.fn(async () => ({
    decision: {
      allowed: false,
      ruleId: "no-checkout",
      condition: "pathPrefix=/checkout",
      reason: "denied_by_rule",
    },
    risk: "high",
    riskRule: "checkout-risk",
    approvals: 1,
    approvalRoles: ["lead"],
    approvalRules: ["prod"],
    version: 2,
  }));
  api.agentPolicyCommand = vi.fn(async () => ({ ...version, version: 3 }));
  prepare?.(api);
  editor = createEditor(api);
  if (!ru) editor.locale("en");
  document.body.innerHTML = "<div id='root'></div>";
  root = createRoot(document.getElementById("root")!);
  await act(async () => root.render(<AgentPolicyPanel {...{ api, editor }} />));
  await settle();
  return api;
}

it("STU-435: forms edit rules, save a version with expectedVersion and reason, show history", async () => {
  const api = await setup();
  expect(document.body.textContent).toContain("Версия: 2 · Bob");
  expect(document.body.textContent).toContain("+ rules/no-checkout");
  expect(document.body.textContent).toContain("миграция");
  await click("Добавить правило");
  expect(editor.store.getState().dirtyFields["agent-policy"]).toBe(true);
  await type("Агент", "*");
  await many("Правило rule-2 · Требовать согласования", ["content.write"]);
  await many("Правило rule-2 · Окружение", ["production"]);
  await type("Правило rule-2 · Префикс пути", "/store, /promo");
  await click("Удалить правило риска checkout-risk");
  await click("Добавить правило риска");
  await type("Не ниже", "medium");
  await click("Удалить правило согласований prod");
  await click("Добавить правило согласований");
  await type("Число согласований", "2");
  await type("Согласование approval-1 · Роли", "lead");
  expect(button("Сохранить новую версию").disabled).toBe(true);
  await type("Причина изменения", "production review");
  await submit();
  const attempt = vi.mocked(api.agentPolicyCommand!).mock.calls[0]![0];
  expect(attempt.reason).toBe("production review");
  expect(attempt.payload.expectedVersion).toBe(2);
  expect(attempt.payload.policy.rules[1]).toEqual({
    id: "rule-2",
    subject: { agent: "*" },
    requireApproval: ["content.write"],
    when: { environment: ["production"], pathPrefix: ["/store", "/promo"] },
  });
  expect(attempt.payload.policy.risk).toEqual([{ id: "risk-1", min: "medium" }]);
  expect(attempt.payload.policy.approvals).toEqual([
    { id: "approval-1", require: { count: 2, roles: ["lead"] } },
  ]);
  expect(api.agentPolicy).toHaveBeenCalledTimes(2);
  await click("Удалить правило no-checkout");
  expect(editor.store.getState().dirtyFields["agent-policy"]).toBe(true);
  await click("Отменить правки");
  expect(editor.store.getState().dirtyFields["agent-policy"]).toBeFalsy();
});

it("STU-433/435: the dry run explains the decision of the draft", async () => {
  const api = await setup();
  await type("Право", "design.compose");
  await type("Путь", "/checkout/cart");
  await type("Окружение", "production");
  await submit(1);
  expect(vi.mocked(api.explainAgentPolicy!).mock.calls[0]![0].situation).toMatchObject({
    right: "design.compose",
    path: "/checkout/cart",
    environment: "production",
  });
  const status = document.querySelector('[role="status"]')!.textContent!;
  expect(status).toContain("Запрещено: запрещено правилом");
  expect(status).toContain("правило no-checkout");
  expect(status).toContain("риск high (checkout-risk)");
  expect(status).toContain("согласований: 1 (lead)");
  vi.mocked(api.explainAgentPolicy!).mockResolvedValueOnce({
    decision: { allowed: true, approval: true, ruleId: "r" },
    approvals: 0,
    approvalRoles: [],
    approvalRules: [],
    version: 2,
  });
  await submit(1);
  expect(document.querySelector('[role="status"]')!.textContent).toContain(
    "Разрешено с согласованием",
  );
  vi.mocked(api.explainAgentPolicy!).mockRejectedValueOnce(new StudioError("POLICY_INVALID"));
  await submit(1);
  expect(document.querySelector('[role="alert"]')!.textContent).toContain("Политика некорректна");
});

it("STU-435: conflicts, uncertain saves with the exact retry and load errors in English", async () => {
  const api = await setup((a) => {
    a.agentPolicyCommand = vi
      .fn()
      .mockRejectedValueOnce(new StudioError("NETWORK_UNCERTAIN", true))
      .mockResolvedValueOnce(version)
      .mockRejectedValueOnce(new StudioError("POLICY_VERSION_CONFLICT"));
  }, false);
  await click("Add rule");
  await type("Reason for the change", "retry");
  await submit();
  expect(document.body.textContent).toContain("Retry the exact request");
  expect(button("Reload policy").disabled).toBe(true);
  await click("Retry exact request");
  const calls = vi.mocked(api.agentPolicyCommand!).mock.calls;
  expect(calls[1]![0]).toEqual(calls[0]![0]);
  await click("Add rule");
  await type("Reason for the change", "stale");
  await submit();
  expect(document.querySelector('[role="alert"]')!.textContent).toContain("The policy changed");
  vi.mocked(api.agentPolicy!).mockRejectedValueOnce(new Error("down"));
  await click("Reload policy");
  expect(document.querySelector('[role="alert"]')!.textContent).toContain(
    "AGENT_POLICY_UNAVAILABLE",
  );
  vi.mocked(api.explainAgentPolicy!).mockRejectedValueOnce(new Error("down"));
});
